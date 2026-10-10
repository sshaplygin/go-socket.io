// Checks the Go WebSocket transport against an independent peer, ws@8.18.3.
// Usage: node verify-framing.mjs <host:port>. The Go test TestNodeOracle starts
// the echo server (an engineio websocket.Transport with MaxPayload 64 that sends
// every packet back) and runs this script against it.
import assert from "node:assert/strict";
import { once } from "node:events";
import WebSocket from "ws";

const address = process.argv[2];
assert.match(address ?? "", /^127\.0\.0\.1:\d+$/, "usage: node verify-framing.mjs <host:port>");
const sockets = new Set();
let cases = 0;

function wait(emitter, event) {
  return once(emitter, event, { signal: AbortSignal.timeout(5000) });
}

async function connect() {
  const socket = new WebSocket(`ws://${address}`);
  sockets.add(socket);
  // wait() still rejects on errors; this prevents unhandled errors in cleanup.
  socket.on("error", () => {});
  await wait(socket, "open");
  return socket;
}

async function close(socket) {
  const closed = wait(socket, "close");
  socket.close(1000, "done");
  const [code, reason] = await closed;
  assert.equal(code, 1000);
  // wsutil.ControlFrameHandler echoes the status only (Node ws echoes both).
  assert.equal(reason.length, 0);
  sockets.delete(socket);
}

try {
  const socket = await connect();
  // [sent, sent as binary, expected echo, echoed as binary]: the echo is the
  // Engine.IO v4 re-encoding of the decoded packet.
  for (const [body, binary, expected, expectedBinary] of [
    ["4hello", false], ["4€🙂\x1ehello", false], ["2", false], ["3probe", false],
    [Buffer.from([0, 4, 255]), true], [Buffer.alloc(0), true],
    [Buffer.alloc(63, 255), true], [Buffer.alloc(64, 255), true], ["4" + "x".repeat(63), false],
    // "b" + base64 is a binary message; the echo is the raw binary form.
    ["bAAEC", false, Buffer.from([0, 1, 2]), true],
  ]) {
    const received = wait(socket, "message");
    socket.send(body, { binary });
    const [actual, isBinary] = await received;
    assert.equal(isBinary, expectedBinary ?? binary);
    assert.deepEqual(actual, Buffer.from(expected ?? body));
    cases++;
  }

  // UTF-8 is validated across fragments; a control frame can split a code point.
  const received = wait(socket, "message");
  const pong = wait(socket, "pong");
  socket.send(Buffer.from("4\xe2", "latin1"), { binary: false, fin: false });
  socket.ping("probe");
  socket.send(Buffer.from([0x82, 0xac]), { binary: false, fin: true });
  const [[body, binaryMessage], [pongBody]] = await Promise.all([received, pong]);
  assert.equal(binaryMessage, false);
  assert.equal(body.toString(), "4€");
  assert.equal(pongBody.toString(), "probe");
  cases++;
  const emptyPong = wait(socket, "pong");
  socket.ping();
  assert.equal((await emptyPong)[0].length, 0);
  cases++;
  await close(socket);
  cases++;

  // Each violation ends the connection with the close status of RFC 6455, which
  // Node reports as the code of the close event (1006 would mean a bare TCP close).
  for (const [name, expected, send] of [
    ["oversized frame", 1009, (s) => s.send(Buffer.alloc(65))],
    ["oversized fragmented message", 1009, (s) => {
      s.send(Buffer.alloc(32), { fin: false });
      s.send(Buffer.alloc(33), { fin: true });
    }],
    ["invalid UTF-8", 1007, (s) => s.send(Buffer.from([255]), { binary: false })],
    ["invalid UTF-8 across fragments", 1007, (s) => {
      s.send(Buffer.from("4\xe2", "latin1"), { binary: false, fin: false });
      s.send(Buffer.from([0x28]), { binary: false, fin: true });
    }],
    ["unmasked client frame", 1002, (s) => s.send("4", { mask: false })],
    ["empty text message", 1002, (s) => s.send("", { binary: false })],
    ["unknown packet type", 1002, (s) => s.send("9x", { binary: false })],
    ["malformed base64 packet", 1002, (s) => s.send("b%%", { binary: false })],
  ]) {
    const s = await connect();
    const closed = wait(s, "close");
    let delivered = false;
    s.on("message", () => { delivered = true; });
    send(s);
    const [code] = await closed;
    assert.equal(code, expected, name);
    assert.equal(delivered, false, name);
    sockets.delete(s);
    cases++;
  }
  console.log(`Verified ${cases} websocket transport scenarios against ws@8.18.3`);
} finally {
  for (const socket of sockets) socket.terminate();
}

import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import WebSocket from "ws";

const work = await mkdtemp(join(tmpdir(), "eio4-framing-"));
let child;
const sockets = new Set();
let cases = 0;

function wait(emitter, event) {
  return once(emitter, event, { signal: AbortSignal.timeout(5000) });
}

try {
  const binary = join(work, process.platform === "win32" ? "echo.exe" : "echo");
  execFileSync("go", ["build", "-o", binary, "./cmd/echo"], {
    cwd: new URL("../", import.meta.url), stdio: "inherit", timeout: 120000,
  });
  child = spawn(binary, [], { stdio: ["ignore", "pipe", "inherit"] });
  const lines = createInterface({ input: child.stdout });
  const [address] = await Promise.race([
    wait(lines, "line"),
    wait(child, "exit").then(([code]) => { throw new Error(`echo exited early: ${code}`); }),
  ]);
  assert.match(address, /^127\.0\.0\.1:\d+$/);
  lines.close();

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

  const socket = await connect();
  for (const [body, binary] of [
    ["4hello", false], ["4€🙂\x1ehello", false], ["", false],
    [Buffer.from([0, 4, 255]), true], [Buffer.alloc(0), true],
    [Buffer.alloc(64, 255), true], ["x".repeat(64), false],
  ]) {
    const received = wait(socket, "message");
    socket.send(body, { binary });
    const [actual, isBinary] = await received;
    assert.equal(isBinary, binary);
    assert.deepEqual(actual, Buffer.from(body));
    cases++;
  }

  // UTF-8 is validated across fragments; a control frame can split a code point.
  const received = wait(socket, "message");
  const pong = wait(socket, "pong");
  socket.send(Buffer.from([0xe2]), { binary: false, fin: false });
  socket.ping("probe");
  socket.send(Buffer.from([0x82, 0xac]), { binary: false, fin: true });
  const [[body, binaryMessage], [pongBody]] = await Promise.all([received, pong]);
  assert.equal(binaryMessage, false);
  assert.equal(body.toString(), "€");
  assert.equal(pongBody.toString(), "probe");
  cases++;
  const emptyPong = wait(socket, "pong");
  socket.ping();
  assert.equal((await emptyPong)[0].length, 0);
  cases++;
  await close(socket);
  cases++;

  for (const [name, send] of [
    ["oversized frame", (s) => s.send(Buffer.alloc(65))],
    ["oversized fragmented message", (s) => {
      s.send(Buffer.alloc(32), { fin: false });
      s.send(Buffer.alloc(33), { fin: true });
    }],
    ["invalid UTF-8", (s) => s.send(Buffer.from([255]), { binary: false })],
    ["unmasked client frame", (s) => s.send("4", { mask: false })],
  ]) {
    const s = await connect();
    const closed = wait(s, "close");
    let delivered = false;
    s.on("message", () => { delivered = true; });
    send(s);
    const [code] = await closed;
    assert.equal(code, 1006, name); // Prototype closes TCP on protocol failures.
    assert.equal(delivered, false, name);
    sockets.delete(s);
    cases++;
  }
  console.log(`Verified ${cases} gobwas framing scenarios against ws@8.18.3`);
} finally {
  for (const socket of sockets) socket.terminate();
  if (child && child.exitCode === null && child.signalCode === null) {
    const exited = wait(child, "exit");
    child.kill();
    await exited;
  }
  await rm(work, { recursive: true, force: true });
}

// Verify WebSocket message contents against the official Engine.IO parser.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { encodePacket, decodePacket, protocol } = require("engine.io-parser");

const types = ["open", "close", "ping", "pong", "message", "upgrade", "noop"];
const fixtures = JSON.parse(fs.readFileSync(path.join(__dirname, "../packets.json"), "utf8"));

function normalize(packet) {
  return Buffer.isBuffer(packet.data)
    ? { type: packet.type, binary: true, base64: packet.data.toString("base64") }
    : { type: packet.type, binary: false, text: packet.data ?? "" };
}

async function main() {
  assert.equal(protocol, 4);
  assert.ok(fixtures.length > 0);
  for (const fixture of fixtures) {
    const packet = {
      type: types[fixture.type],
      data: fixture.binary ? Buffer.from(fixture.base64, "base64") : fixture.text,
    };
    for (const supportsBinary of [false, true]) {
      const expected = supportsBinary && fixture.binary
        ? Buffer.from(fixture.base64, "base64") : fixture.wire;
      const encoded = await new Promise((resolve) => encodePacket(packet, supportsBinary, resolve));
      assert.deepEqual(encoded, expected, `${fixture.name}/${supportsBinary}: encoded message`);
      assert.deepEqual(normalize(decodePacket(expected)), normalize(packet), `${fixture.name}/${supportsBinary}: decoded packet`);
    }
  }
  console.log(`Verified ${fixtures.length * 2} WebSocket cases in both directions with engine.io-parser@5.2.3.`);
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});

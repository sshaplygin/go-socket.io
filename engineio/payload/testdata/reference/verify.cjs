// Verify shared wire fixtures against the official Engine.IO v4 implementation.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { encodePayload, decodePayload, protocol } = require("engine.io-parser");

const types = ["open", "close", "ping", "pong", "message", "upgrade", "noop"];
const fixtures = JSON.parse(fs.readFileSync(path.join(__dirname, "../payloads.json"), "utf8"));

function normalize(p) {
  return Buffer.isBuffer(p.data)
    ? { type: p.type, binary: true, base64: p.data.toString("base64") }
    : { type: p.type, binary: false, text: p.data ?? "" };
}

async function main() {
  assert.equal(protocol, 4);
  assert.ok(fixtures.length > 0, "fixture set must not be empty");
  for (const fixture of fixtures) {
    const packets = fixture.packets.map((p) => ({
      type: types[p.type],
      data: p.binary ? Buffer.from(p.base64, "base64") : p.text,
    }));
    const encoded = await new Promise((resolve) => encodePayload(packets, resolve));
    assert.equal(encoded, fixture.wire, `${fixture.name}: encoded wire`);
    assert.deepEqual(
      decodePayload(fixture.wire).map(normalize),
      packets.map(normalize),
      `${fixture.name}: decoded packets`,
    );
  }
  console.log(`Verified ${fixtures.length} fixtures in both directions with engine.io-parser@5.2.3 (EIO=${protocol}).`);
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});

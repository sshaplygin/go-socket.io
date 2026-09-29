// Exercise the pinned client's actual batching method without opening a socket.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { SocketWithoutUpgrade } = require("engine.io-client");
const { encodePayload, decodePayload } = require("engine.io-parser");

const types = ["open", "close", "ping", "pong", "message", "upgrade", "noop"];
const fixtures = JSON.parse(fs.readFileSync(path.join(__dirname, "../batches.json"), "utf8"));
const select = SocketWithoutUpgrade.prototype._getWritablePackets;
assert.equal(typeof select, "function", "pinned client batching method must be available");
assert.ok(fixtures.length > 0, "batch fixture set must not be empty");

async function encode(packets) {
  // The reference encoder does not invoke its callback for an empty array.
  return packets.length === 0 ? "" : new Promise((resolve) => encodePayload(packets, resolve));
}

async function main() {
  for (const fixture of fixtures) {
    const packets = fixture.packets.map((p) => ({
      type: types[p.type],
      data: p.binary ? Buffer.from(p.base64, "base64") : p.text,
    }));
    const original = packets.slice();
    const selected = select.call({
      _maxPayload: fixture.limit, transport: { name: "polling" }, writeBuffer: packets,
    });
    assert.equal(selected.length, fixture.nodeCount, `${fixture.name}: JS selection`);
    assert.equal(await encode(selected), fixture.nodeWire, `${fixture.name}: JS wire`);
    assert.deepEqual(packets, original, `${fixture.name}: queue unchanged`);

    // Check the Go prefix's exact wire size using the independent JS encoder,
    // including the fixtures where JS's estimated selection exceeds the limit.
    const prefix = packets.slice(0, fixture.count);
    const wire = await encode(prefix);
    assert.equal(wire, fixture.wire, `${fixture.name}: Go prefix wire`);
    assert.ok(Buffer.byteLength(wire) <= fixture.limit, `${fixture.name}: Go size bound`);
    if (prefix.length > 0) {
      assert.equal(decodePayload(wire).length, prefix.length);
    }
    if (fixture.count < packets.length) {
      const next = await encode(packets.slice(0, fixture.count + 1));
      assert.ok(Buffer.byteLength(next) > fixture.limit, `${fixture.name}: maximal prefix`);
    }
    if (fixture.tooLarge) {
      assert.equal(fixture.count, 0, `${fixture.name}: oversized head is a local error`);
    }
  }
  console.log(`Verified ${fixtures.length} batch fixtures with engine.io-client@6.6.3 and the Node encoder.`);
}

main().catch((err) => {
  console.error(err);
  process.exitCode = 1;
});

const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const msgpack = require("notepack.io");

function normalize(value) {
  if (Buffer.isBuffer(value)) return { $binary: value.toString("base64") };
  if (Array.isArray(value)) return value.map(normalize);
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, normalize(v)]));
  }
  return value;
}

const expected = JSON.parse(fs.readFileSync(path.join(__dirname, "../publications.json")));
// The Go side re-encodes every publication with adapter/codec (generic MessagePack or
// JSON for the reference-only ones) and writes the result to CODEC_ROUNDTRIP_OUT.
const out = path.join(os.tmpdir(), `adapter-codec-roundtrip-${process.pid}.json`);
execFileSync("go", ["test", "-count=1", "-run", "^TestRoundtripExport$", "."], {
  cwd: path.join(__dirname, "../.."), encoding: "utf8", timeout: 120000,
  env: { ...process.env, CODEC_ROUNDTRIP_OUT: out },
});
const actual = JSON.parse(fs.readFileSync(out, "utf8"));
fs.rmSync(out);
assert.equal(actual.cases.length, expected.cases.length);
let count = 0;
for (let i = 0; i < actual.cases.length; i++) {
  const got = actual.cases[i], want = expected.cases[i];
  assert.equal(got.name, want.name);
  assert.equal(got.publications.length, want.publications.length);
  for (let j = 0; j < got.publications.length; j++) {
    const p = got.publications[j], expectedPublication = want.publications[j];
    assert.equal(p.channel, expectedPublication.channel);
    assert.equal(p.encoding, expectedPublication.encoding);
    const wire = Buffer.from(p.wireBase64, "base64");
    const value = p.encoding === "json" ? JSON.parse(wire) : msgpack.decode(wire);
    assert.deepEqual(normalize(value), expectedPublication.value, got.name);
    count++;
  }
}
console.log(`Verified ${count} Go-produced publications with Node decoders`);

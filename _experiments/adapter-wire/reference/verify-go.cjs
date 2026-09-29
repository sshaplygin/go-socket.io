const assert = require("node:assert/strict");
const fs = require("node:fs");
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

const expected = JSON.parse(fs.readFileSync(path.join(__dirname, "../testdata/publications.json")));
const actual = JSON.parse(execFileSync("go", ["run", "./cmd/roundtrip"], {
  cwd: path.join(__dirname, ".."), encoding: "utf8", timeout: 120000,
}));
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

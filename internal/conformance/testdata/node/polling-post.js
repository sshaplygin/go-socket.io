'use strict';
// Posts Engine.IO text payloads over polling with Node's http module, which adds
// no Content-Type unless asked to, and checks that each echo is acknowledged.
const http = require('http');
const assert = require('assert');

const base = new URL(process.argv[2]);
setTimeout(() => { console.error('timed out'); process.exit(1); }, 15000).unref();

function request(method, path, headers, body) {
  return new Promise((resolve, reject) => {
    const req = http.request({host: base.hostname, port: base.port, method, path, headers}, res => {
      const chunks = [];
      res.on('data', c => chunks.push(c));
      res.on('end', () => resolve({status: res.statusCode, body: Buffer.concat(chunks).toString()}));
    });
    req.on('error', reject);
    req.end(body);
  });
}

function packets(payload) {
  const out = [];
  while (payload.length) {
    const colon = payload.indexOf(':');
    const size = parseInt(payload.slice(0, colon), 10);
    out.push(payload.slice(colon + 1, colon + 1 + size));
    payload = payload.slice(colon + 1 + size);
  }
  return out;
}

async function main() {
  const open = await request('GET', '/?EIO=3&transport=polling&b64=1', {});
  assert.strictEqual(open.status, 200, open.body);
  const sid = JSON.parse(packets(open.body)[0].slice(1)).sid;
  const path = '/?EIO=3&transport=polling&b64=1&sid=' + sid;
  const types = [undefined, 'text/plain', 'application/x-www-form-urlencoded', 'application/json'];
  for (const [i, type] of types.entries()) {
    const event = '42' + (i + 1) + '["echo","' + String(type) + '"]';
    const body = event.length + ':' + event;
    const headers = {'Content-Length': Buffer.byteLength(body)};
    if (type !== undefined) headers['Content-Type'] = type;
    const post = await request('POST', path, headers, body);
    assert.strictEqual(post.status, 200, String(type) + ': ' + post.body);
    assert.strictEqual(post.body, 'ok');
    let found = false;
    for (let n = 0; n < 3 && !found; n++) {
      const poll = await request('GET', path, {});
      assert.strictEqual(poll.status, 200, poll.body);
      found = packets(poll.body).map(p => p.trim()).includes('43' + (i + 1) + '["' + String(type) + '"]');
    }
    assert.ok(found, 'echo acknowledgement for ' + String(type));
  }
  console.log('ok');
}

main().catch(err => { console.error(err); process.exit(1); });

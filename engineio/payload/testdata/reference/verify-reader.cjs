// Check shared body-size fixtures against a real, pinned Node Engine.IO server.
const assert = require("node:assert/strict");
const { once } = require("node:events");
const fs = require("node:fs");
const http = require("node:http");
const path = require("node:path");
const { test } = require("node:test");
const { Server } = require("engine.io");
const { decodePayload } = require("engine.io-parser");

const fixtures = JSON.parse(fs.readFileSync(path.join(__dirname, "../body-limits.json"), "utf8"));
assert.ok(fixtures.length > 0, "body-limit fixture set must not be empty");

function request(port, requestPath, wire, chunked) {
  return new Promise((resolve, reject) => {
    const body = wire === undefined ? undefined : Buffer.from(wire);
    const req = http.request({
      hostname: "127.0.0.1", port, path: requestPath, agent: false,
      method: body === undefined ? "GET" : "POST",
      headers: body === undefined ? {} : {
        "Content-Type": "text/plain;charset=UTF-8",
        ...(chunked ? {} : { "Content-Length": body.length }),
      },
    }, (res) => {
      let data = "";
      res.setEncoding("utf8");
      res.on("data", (chunk) => { data += chunk; });
      res.on("end", () => resolve({ status: res.statusCode, body: data }));
      res.on("error", reject);
    });
    req.on("error", reject);
    req.setTimeout(3000, () => req.destroy(new Error("reference request timed out")));
    if (body !== undefined && chunked) {
      // Split inside multibyte UTF-8 sequences as well as between packets.
      for (let i = 0; i < body.length; i++) req.write(body.subarray(i, i + 1));
      req.end();
    } else {
      req.end(body);
    }
  });
}

for (const fixture of fixtures) {
  for (const chunked of [false, true]) {
    test(`${fixture.name}/${chunked ? "chunked" : "content-length"}`, { timeout: 5000 }, async (t) => {
      const httpServer = http.createServer();
      const engine = new Server({
        transports: ["polling"], allowUpgrades: false,
        maxHttpBufferSize: fixture.limit,
      });
      const messages = [];
      engine.on("connection", (socket) => socket.on("message", (data) => messages.push(data ?? "")));
      engine.attach(httpServer);
      t.after(async () => {
        engine.close();
        await new Promise((resolve, reject) => httpServer.close((err) => err ? reject(err) : resolve()));
      });
      httpServer.listen(0, "127.0.0.1");
      await once(httpServer, "listening");
      const port = httpServer.address().port;
      const endpoint = "/engine.io/?EIO=4&transport=polling";
      const handshake = await request(port, endpoint);
      assert.equal(handshake.status, 200);
      const open = decodePayload(handshake.body)[0];
      assert.equal(open.type, "open");
      const { sid, maxPayload } = JSON.parse(open.data);
      assert.equal(maxPayload, fixture.limit);
      const result = await request(port, `${endpoint}&sid=${encodeURIComponent(sid)}`, fixture.wire, chunked);
      assert.equal(result.status, fixture.status);
      if (fixture.status === 200) {
        assert.equal(result.body, "ok");
        const expected = decodePayload(fixture.wire).map((packet) => {
          assert.equal(packet.type, "message");
          return packet.data ?? "";
        });
        assert.deepEqual(messages, expected);
      } else {
        assert.deepEqual(messages, [], "oversized body must not dispatch any packets");
      }
    });
  }
}

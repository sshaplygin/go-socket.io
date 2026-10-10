const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { EventEmitter } = require("node:events");
const msgpack = require("notepack.io");
const { Encoder } = require("socket.io-parser");

// Only random identifiers are substituted. All channels, encoders and request
// handlers below are the installed upstream adapter, not a copied implementation.
require("uid2");
require.cache[require.resolve("uid2")].exports = () => "fixture-id";
const { RedisAdapter } = require("@socket.io/redis-adapter");

function normalize(value) {
  if (Buffer.isBuffer(value)) return { $binary: value.toString("base64") };
  if (Array.isArray(value)) return value.map(normalize);
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, normalize(v)]));
  }
  return value;
}

const cases = [];
async function capture(name, options, action) {
  const publications = [];
  let adapter;
  class RedisStub extends EventEmitter {
    psubscribe() {}
    subscribe() {}
    punsubscribe() {}
    unsubscribe() {}
    send_command(command, args, callback) {
      assert.equal(command, "PUBSUB");
      assert.equal(args[0], "NUMSUB");
      callback(null, [args[1], 2]);
    }
    publish(channel, payload) {
      const encoding = typeof payload === "string" ? "json" : "msgpack";
      const wire = Buffer.from(payload);
      const value = encoding === "json" ? JSON.parse(wire) : msgpack.decode(wire);
      publications.push({ channel, encoding, wireBase64: wire.toString("base64"), value: normalize(value) });
      // Supply one peer reply to finish upstream request timers deterministically.
      if (channel.includes("-request#") && value.requestId && value.type !== 7) {
        const response = { requestId: value.requestId };
        if (value.type === 1) response.rooms = ["peer-room"];
        else if (value.type === 5) response.sockets = [];
        else if (value.type === 6) { response.type = 6; response.data = "peer-ok"; }
        else return Promise.resolve(1);
        queueMicrotask(() => adapter.onresponse(
          `${options.key || "socket.io"}-response#${options.nsp || "/chat"}#`,
          Buffer.from(JSON.stringify(response)),
        ));
      }
      return Promise.resolve(1);
    }
  }
  const socket = {
    id: "socket-1", rooms: new Set(["socket-1", "room-1"]),
    handshake: { headers: { "x-example": "fixture" }, auth: { role: "reader" }, sessionStore: {} },
    data: { nickname: "alice" }, acks: new Map(),
    client: { writeToEngine() {} },
  };
  const nsp = {
    name: options.nsp || "/chat", _ids: 0,
    server: { encoder: new Encoder() }, sockets: new Map(),
    _onServerSideEmit(args) {
      const ack = args[args.length - 1];
      if (typeof ack === "function") ack({ accepted: true });
    },
  };
  adapter = new RedisAdapter(nsp, new RedisStub(), new RedisStub(), {
    key: options.key, publishOnSpecificResponseChannel: options.specific || false,
    requestsTimeout: 1000,
  });
  if (options.socket) {
    nsp.sockets.set(socket.id, socket);
    adapter.addAll(socket.id, socket.rooms);
  }
  try {
    await action(adapter, socket);
    cases.push({ name, publications });
  } finally {
    adapter.close();
    assert.equal(adapter.requests.size, 0, `${name}: pending request`);
    assert.equal(adapter.ackRequests.size, 0, `${name}: pending broadcast ack`);
  }
}

const opts = (rooms = [], except = [], flags = {}) => ({ rooms: new Set(rooms), except: new Set(except), flags });
const packet = (data = ["message", "hello"]) => ({ type: 2, data });
const request = (a, value) => a.onrequest("socket.io-request#/chat#", Buffer.from(JSON.stringify(value)));

async function main() {
  await capture("broadcast-root", { nsp: "/" }, a => a.broadcast(packet(), opts()));
  await capture("broadcast-room", {}, a => a.broadcast(packet(["message", { text: "€🙂" }]), opts(["room-1"])));
  await capture("broadcast-union-except", {}, a => a.broadcast(packet(), opts(["room-1", "room-2"], ["blocked"], { volatile: true, compress: false })));
  await capture("broadcast-custom-prefix", { key: "app", nsp: "/日本" }, a => a.broadcast(packet(), opts(["room#one"])));
  await capture("broadcast-binary", {}, a => a.broadcast(packet(["image", { file: Buffer.from([0, 4, 255]) }]), opts()));
  await capture("broadcast-empty-binary", {}, a => a.broadcast(packet(["image", Buffer.alloc(0)]), opts()));
  await capture("broadcast-local-no-publish", {}, a => a.broadcast(packet(), opts([], [], { local: true })));
  await capture("broadcast-ack-request", {}, async a => {
    a.broadcastWithAck(packet(["image", Buffer.from([0, 255])]), opts(["room-1"], [], { timeout: 1 }), count => assert.equal(count, 0), () => {});
    // Upstream expires broadcast-ack bookkeeping after the configured timeout.
    await new Promise(resolve => setTimeout(resolve, 10));
  });
  await capture("join-request", {}, a => a.addSockets(opts(["room-1"], ["blocked"]), ["new-room"]));
  await capture("leave-request", {}, a => a.delSockets(opts(["room-1"]), ["old-room"]));
  await capture("disconnect-request", {}, a => a.disconnectSockets(opts(), true));
  await capture("server-emit-request", {}, a => a.serverSideEmit(["notice", { count: 2 }]));
  await capture("server-emit-binary-json", {}, a => a.serverSideEmit(["notice", Buffer.from([0, 255])]));
  await capture("server-emit-ack-request", {}, a => new Promise((resolve, reject) => {
    a.serverSideEmit(["notice", (err, replies) => {
      if (err) return reject(err);
      assert.deepEqual(replies, ["peer-ok"]);
      resolve();
    }]);
  }));
  await capture("all-rooms-request", {}, async a => assert.deepEqual([...await a.allRooms()], ["peer-room"]));
  await capture("fetch-sockets-request", {}, async a => assert.deepEqual(await a.fetchSockets(opts(["room-1"], ["blocked"])), []));
  await capture("sockets-response", { socket: true }, a => request(a, { uid: "peer", requestId: "req", type: 0, rooms: ["room-1"] }));
  await capture("all-rooms-response", { socket: true }, a => request(a, { uid: "peer", requestId: "req", type: 1 }));
  await capture("fetch-sockets-response", { socket: true }, a => request(a, { uid: "peer", requestId: "req", type: 5, opts: { rooms: [], except: [] } }));
  await capture("specific-fetch-response", { socket: true, specific: true }, a => request(a, { uid: "peer", requestId: "req", type: 5, opts: { rooms: [], except: [] } }));
  await capture("server-emit-response", {}, a => request(a, { uid: "peer", requestId: "req", type: 6, data: ["notice"] }));
  await capture("broadcast-ack-responses", { socket: true, specific: true }, async (a, socket) => {
    await a.onrequest("socket.io-request#/chat#", msgpack.encode({ uid: "peer", requestId: "req", type: 7, packet: packet(), opts: { rooms: [], except: [] } }));
    assert.equal(socket.acks.size, 1);
    socket.acks.values().next().value(Buffer.from([0, 255]));
  });

  const result = { formatVersion: 1, reference: "@socket.io/redis-adapter@8.3.0 + notepack.io@3.0.1", cases };
  const target = path.join(__dirname, "../publications.json");
  if (process.argv.includes("--write")) {
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, JSON.stringify(result, null, 2) + "\n");
  } else {
    assert.deepEqual(result, JSON.parse(fs.readFileSync(target, "utf8")));
  }
  console.log(`Verified ${cases.length} adapter scenarios, ${cases.reduce((n, c) => n + c.publications.length, 0)} real upstream publications`);
}

main().catch(err => { console.error(err); process.exitCode = 1; });

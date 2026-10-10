"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { Adapter } = require("socket.io-adapter");
const { Encoder } = require("socket.io-parser");

const sorted = (values) => [...values].sort();
const entries = (map) => [...map].map(([key, values]) => ({ key, values: sorted(values) }))
  .sort((a, b) => a.key < b.key ? -1 : a.key > b.key ? 1 : 0);
const connect = (id, rooms = []) => ({ op: "connect", id, rooms });
const base = [connect("a", ["red", "shared"]), connect("b", ["blue", "shared"]), connect("c", ["red"])];

async function capture(name, actions, rooms = [], except = [], flags = {}) {
  const writes = [];
  const notifications = [];
  const events = [];
  const nsp = { name: "/rooms", server: { encoder: new Encoder() }, sockets: new Map() };
  const adapter = new Adapter(nsp);
  for (const event of ["create-room", "join-room", "leave-room", "delete-room"]) {
    adapter.on(event, (...args) => events.push({ event, args }));
  }
  const trace = [];
  const snapshot = () => ({ rooms: entries(adapter.rooms), sids: entries(adapter.sids), live: sorted(nsp.sockets.keys()) });
  for (const action of actions) {
    switch (action.op) {
      case "connect":
        // Socket._onconnect joins its own ID room; Adapter.addAll alone does not.
        nsp.sockets.set(action.id, {
          id: action.id,
          notifyOutgoingListeners(packet) { notifications.push({ id: action.id, packet }); },
          client: { writeToEngine(packets, options) {
            writes.push({ id: action.id, packets, options: {
              preEncoded: options.preEncoded,
              ...(options.volatile !== undefined ? { volatile: options.volatile } : {}),
              ...(options.compress !== undefined ? { compress: options.compress } : {}),
            } });
          } },
        });
        adapter.addAll(action.id, new Set([action.id, ...action.rooms]));
        break;
      case "addAll": adapter.addAll(action.id, new Set(action.rooms)); break;
      case "del": adapter.del(action.id, action.room); break;
      case "delAll": adapter.delAll(action.id); break;
      case "unregister": nsp.sockets.delete(action.id); break;
      default: throw new Error(`unknown operation ${action.op}`);
    }
    trace.push({ action, state: snapshot() });
  }
  const queryRooms = [[], ["red"], ["shared", "red"], ["missing"], rooms];
  const queries = [];
  for (const selection of queryRooms) {
    queries.push({ rooms: selection, ids: sorted(await adapter.sockets(new Set(selection))) });
  }
  const ids = sorted(new Set(["a", "b", "c", "missing", ...adapter.sids.keys()]));
  const socketRooms = ids.map((id) => ({ id, rooms: adapter.socketRooms(id) === undefined ? null : sorted(adapter.socketRooms(id)) }));
  const packet = { type: 2, data: ["notice", { value: 1 }] };
  adapter.broadcast(packet, { rooms: new Set(rooms), except: new Set(except), flags });
  assert.equal(new Set(writes.map((write) => write.id)).size, writes.length, `${name}: duplicate recipient`);
  return { name, trace, events, queries, socketRooms, broadcast: {
    rooms, except, flags,
    writes: writes.sort((a, b) => a.id.localeCompare(b.id)),
    notifications: notifications.sort((a, b) => a.id.localeCompare(b.id)),
  } };
}

async function main() {
  const cases = [];
  const add = async (...args) => cases.push(await capture(...args));
  await add("empty", []);
  await add("all", base);
  await add("room-union-deduplicates", base, ["shared", "red", "shared"]);
  await add("unknown-target-room", base, ["missing"]);
  await add("known-and-unknown-room", base, ["missing", "blue"]);
  await add("exclude-room", base, [], ["red"]);
  await add("exclude-automatic-socket-room", base, [], ["a"]);
  await add("exclude-unknown-room", base, [], ["missing"]);
  await add("exclude-overlapping-rooms", base, ["red", "blue"], ["shared", "red"]);
  await add("id-is-also-another-sockets-room", [...base, { op: "addAll", id: "b", rooms: ["a"] }], [], ["a"]);
  await add("duplicate-add-is-idempotent", [...base, { op: "addAll", id: "a", rooms: ["red", "red", "shared"] }]);
  await add("leave-last-member-cleans-room", [...base, { op: "del", id: "b", room: "blue" }]);
  await add("leave-missing-is-idempotent", [...base, { op: "del", id: "missing", room: "red" }, { op: "del", id: "a", room: "missing" }]);
  await add("delAll-cleans-memberships", [...base, { op: "delAll", id: "a" }, { op: "delAll", id: "a" }, { op: "delAll", id: "missing" }]);
  await add("leave-final-room-retains-empty-sid", [connect("a"), { op: "del", id: "a", room: "a" }]);
  await add("empty-add-creates-empty-sid", [{ op: "addAll", id: "ghost", rooms: [] }]);
  await add("membership-without-live-socket-is-skipped", [...base, { op: "addAll", id: "ghost", rooms: ["red"] }], ["red"]);
  await add("stale-live-map-entry-is-skipped", [...base, { op: "unregister", id: "a" }]);
  await add("local-does-not-change-local-selection", base, ["red"], [], { local: true });
  await add("volatile-and-compress-are-forwarded", base, ["shared"], [], { volatile: true, compress: false });
  await add("explicit-false-volatile-is-forwarded", base, [], ["c"], { volatile: false, compress: true });
  await add("empty-and-unicode-room-names", [connect("a", ["", "комната"]), connect("b", [""])], ["", "комната"]);
  const corpus = { schema: 1, adapter: "2.5.5", parser: "4.2.7", cases };
  const target = path.join(__dirname, "../testdata/rooms.json");
  const canonical = JSON.stringify(corpus, null, 2) + "\n";
  if (process.argv.includes("--write")) fs.writeFileSync(target, canonical);
  else assert.equal(fs.readFileSync(target, "utf8"), canonical, "corpus drift; inspect upstream behavior before npm run capture");
  console.log(`${cases.length} room-selection cases verified`);
}

main().catch((error) => { console.error(error); process.exitCode = 1; });

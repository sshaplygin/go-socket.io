'use strict';
const assert = require('assert');
const http = require('http');
const serverIO = require('socket.io');
const adapter = require('socket.io-redis');
// Capture the pinned reference's request-prefix limitation without modifying it.
// Production mixed namespaces therefore exclude '#'; Go-only routing is tested
// separately. These fake clients perform no Redis I/O.
function assertReferenceNamespaceBoundary() {
 const EventEmitter=require('events');
 const sub=new EventEmitter();
 sub.psubscribe=(_channel,cb)=>cb(null);sub.subscribe=(_channels,cb)=>cb(null);
 const pub=new EventEmitter(),responses=[];
 pub.publish=(channel,message)=>responses.push({channel,message:JSON.parse(message)});
 const Reference=adapter({key:'reference-boundary',pubClient:pub,subClient:sub});
 let joined;
 new Reference({name:'/cluster',server:{encoder:{}},connected:{victim:{join(room,cb){joined=room;cb();}}}});
 new Reference({name:'/cluster#other',server:{encoder:{}},connected:{}});
 sub.emit('messageBuffer',Buffer.from('reference-boundary-request#/cluster#other#'),Buffer.from(JSON.stringify({requestid:'boundary',type:3,sid:'victim',room:'wrong-namespace'})));
 assert.strictEqual(joined,'wrong-namespace','pinned Node request routing uses prefix matching');
 assert.deepStrictEqual(responses,[{channel:'reference-boundary-response#/cluster#',message:{requestid:'boundary'}}]);
}
assertReferenceNamespaceBoundary();
const path = require('path');
const clientPath=path.resolve(process.env.NODE_PATH,'socket.io-client');
const clientIO = require(clientPath);
const clientVersion=require(path.join(clientPath,'package.json')).version;
if(process.env.CONFORMANCE_CLIENT_VERSION)assert.strictEqual(clientVersion,process.env.CONFORMANCE_CLIENT_VERSION);
console.log('Redis client reference: '+clientVersion);
const redis = require('redis');
const [goA,goB,addr,prefix,transport] = process.argv.slice(2);
const split = addr.lastIndexOf(':');
const redisOpts = {host:addr.slice(0,split),port:Number(addr.slice(split+1))};
const pub=redis.createClient(redisOpts),sub=redis.createClient(redisOpts);
const server=http.createServer();
const io=serverIO(server);
io.adapter(adapter({pubClient:pub,subClient:sub,key:prefix,requestsTimeout:1500}));
const observed=[];
for(const name of ['/','/cluster','/cluster#other','/lifecycle']) {
 const ns=io.of(name);
 ns.on('connection',s=>{
  s.on('identity',cb=>cb(s.id));
  s.on('join',(room,cb)=>s.join(room,()=>cb('ok')));
 });
 ns.adapter.customHook=(data,cb)=>cb({node:data});
 const original=ns.adapter.broadcast;
 ns.adapter.broadcast=function(packet,opts,remote){
  if(remote)observed.push({namespace:name,event:packet.data[0],opts});
  return original.apply(this,arguments);
 };
}
const sockets=[],managers=[];
const delay=ms=>new Promise(r=>setTimeout(r,ms));
function bounded(promise,label){let timer;return Promise.race([promise,new Promise((_,reject)=>{timer=setTimeout(()=>reject(new Error('timeout: '+label)),4000);})]).finally(()=>clearTimeout(timer));}
const callback=(obj,method,...args)=>new Promise((resolve,reject)=>obj[method](...args,(err,value)=>err?reject(err):resolve(value)));
function onceDisconnect(s){return bounded(new Promise(resolve=>s.once('disconnect',resolve)),'disconnect '+s.nsp);}
function ack(s,name,...args){return bounded(new Promise(resolve=>s.emit(name,...args,resolve)),'ACK '+name+' '+s.nsp);}
async function until(f,what){for(let n=0;n<200;n++){if(await f())return;await delay(20);}throw new Error('timeout: '+what);}
async function readySocket(s) {
 sockets.push(s);
 await bounded(new Promise((resolve,reject)=>{s.once('connect',resolve);s.once('connect_error',reject);}), 'connect '+s.io.uri+s.nsp);
 s.events=[];s.on('broadcast',v=>s.events.push(v));s.binary=[];s.on('binary',v=>s.binary.push(v));
 s.sid=await bounded(ack(s,'identity'),'identity '+s.nsp);return s;
}
async function connect(url,ns='/cluster') {
 const manager=new clientIO.Manager(url,{transports:[transport],forceNode:true,perMessageDeflate:true,reconnection:false,autoConnect:false});
 managers.push(manager);const socket=manager.socket(ns);const ready=readySocket(socket);socket.open();return ready;
}
async function sharedPair(url) {
 const manager=new clientIO.Manager(url,{transports:[transport],forceNode:true,perMessageDeflate:true,reconnection:false,autoConnect:false});
 managers.push(manager);const main=manager.socket('/cluster'),other=manager.socket('/lifecycle');
 const ready=Promise.all([readySocket(main),readySocket(other)]);
 main.open();other.open();return ready;
}
function compressedFrames(s) {
 const ws=s.io.engine.transport.ws;
 assert(ws && ws._socket,'Node websocket TCP socket required to inspect RSV1');
 let pending=Buffer.alloc(0),compressed=0;
 ws._socket.on('data',chunk=>{
  pending=Buffer.concat([pending,chunk]);
  while(pending.length>=2){
   let length=pending[1]&127,offset=2;
   if(length===126){if(pending.length<4)return;length=pending.readUInt16BE(2);offset=4;}
   else if(length===127){if(pending.length<10)return;assert.strictEqual(pending.readUInt32BE(2),0);length=pending.readUInt32BE(6);offset=10;}
   if(pending[1]&128)offset+=4;
   if(pending.length<offset+length)return;
   if((pending[0]&64) && [1,2].includes(pending[0]&15))compressed++;
   pending=pending.slice(offset+length);
  }
 });
 return ()=>compressed;
}
async function control(url,q){
 const response=await fetch(url+'/control',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(q)});
 const body=await response.text();assert(response.ok,body);return JSON.parse(body);
}
async function deliveries(clients,counts,send){
 clients.forEach(s=>s.events=[]);await send();
 await until(()=>clients.every((s,i)=>s.events.length>=counts[i]),'delivery');
 await delay(100);assert.deepStrictEqual(clients.map(s=>s.events.length),counts);
}
const watchdog=setTimeout(()=>{console.error('Redis cluster timeout');process.exit(1);},40000);
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const nodeURL='http://127.0.0.1:'+server.address().port;
 console.log('Redis scenario connecting '+transport);
 const peers=[await connect(goA),await connect(goB),await connect(nodeURL)];
 const isolated=await connect(goB,'/cluster#other');
 // Pinned Node server parses CONNECT namespace as a URL and strips '#'.
 // Its Redis adapter still publishes exact '#' packet namespaces; test those
 // messages against real Go namespace clients without patching the reference.
 const otherPeers=[await connect(goA,'/cluster#other'),isolated];
 const roots=[await connect(goA,'/'),await connect(goB,'/'),await connect(nodeURL,'/')];
 console.log('Redis namespace clients connected');
 const compressed=transport==='websocket'?peers.slice(0,2).map(compressedFrames):null;
 await deliveries(roots,[1,1,1],()=>control(goA,{Op:'broadcast',Namespace:'/'}));
 await deliveries(roots,[1,1,1],async()=>io.of('/').emit('broadcast','root-node'));
 await deliveries(peers.concat(otherPeers),[0,0,0,1,1],()=>control(goA,{Op:'broadcast',Namespace:'/cluster#other'}));
 await deliveries(peers.concat(otherPeers),[0,0,0,1,1],async()=>io.of('/cluster#other').emit('broadcast','isolated-node'));

 await until(async()=>{
  const reply=await new Promise((resolve,reject)=>pub.pubsub('NUMSUB',prefix+'-request#/cluster#',(err,v)=>err?reject(err):resolve(v)));
  return Number(reply[1])===3;
 },'three registered Redis subscribers');
 for(const s of peers){await ack(s,'join','r1');await ack(s,'join','r2');}
 isolated.events=[];
 await deliveries(peers,[1,1,1],()=>control(goA,{Op:'broadcast',Rooms:['r1','r2']}));
 assert.strictEqual(isolated.events.length,0);
 await deliveries(peers,[1,1,1],async()=>{io.of('/cluster').to('r1').to('r2').emit('broadcast','node');});
 await deliveries(peers,[0,1,1],()=>control(goA,{Op:'broadcast',Except:[peers[0].sid]}));
 await deliveries(peers,[1,0,0],()=>control(goA,{Op:'broadcast',Local:true}));

 // Metadata flags are checked separately from actual websocket compression.
 const observedBefore=observed.length;
 await deliveries(peers,[1,1,1],()=>control(goA,{Op:'broadcast',Volatile:true,NoCompress:true}));
 await until(()=>observed.length>observedBefore,'remote flag metadata');
 const flagPacket=observed.slice(observedBefore).find(p=>p.namespace==='/cluster'&&p.event==='broadcast');
 assert(flagPacket);assert.strictEqual(flagPacket.opts.flags.volatile,true);assert.strictEqual(flagPacket.opts.flags.compress,false);
 if(compressed){
  let before=compressed.map(read=>read());
  await deliveries(peers,[1,1,1],async()=>io.of('/cluster').emit('broadcast','compressed'));
  assert(compressed.every((read,i)=>read()>before[i]),'default remote packets must compress on both Go websocket servers');
  before=compressed.map(read=>read());
  await deliveries(peers,[1,1,1],async()=>io.of('/cluster').compress(false).emit('broadcast','uncompressed'));
  assert.deepStrictEqual(compressed.map(read=>read()),before,'remote compress(false) must disable RSV1 on both Go servers');
 }
 await control(goA,{Op:'binary'});
 await until(()=>peers.every(s=>s.binary.length===1),'Go binary');
 io.of('/cluster').emit('binary',{bytes:Buffer.from([0,255,2]),nested:[Buffer.from([3,254])]});
 await until(()=>peers.every(s=>s.binary.length===2),'Node binary');
 for(const s of peers)for(const v of s.binary){assert(Buffer.isBuffer(v.bytes));assert.deepStrictEqual([...v.bytes],[0,255,2]);assert(Buffer.isBuffer(v.nested[0]));assert.deepStrictEqual([...v.nested[0]],[3,254]);}
 console.log('Redis broadcasts and binary passed');
 const ids=peers.map(s=>s.sid).sort();
 assert.deepStrictEqual((await control(goA,{Op:'ids',Rooms:['r1','r2']})).sort(),ids);
 assert.deepStrictEqual((await callback(io.of('/cluster').adapter,'clients',['r1','r2'])).sort(),ids);
 assert((await control(goA,{Op:'rooms'})).includes('r1'));
 assert((await callback(io.of('/cluster').adapter,'allRooms')).includes('r2'));
 await control(goA,{Op:'join',SID:peers[2].sid,Room:'from-go'});
 assert((await control(goB,{Op:'clientRooms',SID:peers[2].sid})).includes('from-go'));
 await control(goA,{Op:'leave',SID:peers[2].sid,Room:'from-go'});
 assert(!(await callback(io.of('/cluster').adapter,'clientRooms',peers[2].sid)).includes('from-go'));
 await ack(peers[1],'leave-all');
 await callback(io.of('/cluster').adapter,'remoteJoin',peers[1].sid,'after-leave-all');
 assert((await callback(io.of('/cluster').adapter,'clientRooms',peers[1].sid)).includes('after-leave-all'));
 await callback(io.of('/cluster').adapter,'remoteLeave',peers[1].sid,'after-leave-all');
 const custom=await control(goA,{Op:'custom',Data:{hello:42}});
 assert.strictEqual(custom.length,3);assert(custom.some(v=>v&&v.node&&v.node.hello===42));
 assert.strictEqual(custom.filter(v=>v&&v.go===prefix&&v.data.hello===42).length,2,'each Go subscriber answers custom hook once');
 const nodeCustom=await callback(io.of('/cluster').adapter,'customRequest',{hello:43});
 assert.strictEqual(nodeCustom.length,3);
 assert.strictEqual(nodeCustom.filter(v=>v&&v.go===prefix&&v.data.hello===43).length,2);
 console.log('Redis queries and custom replies passed');
 const missing=prefix+'-missing-sid';
 const timeoutStart=Date.now();
 const missingResponse=await fetch(goA+'/control',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({Op:'join',SID:missing,Room:'absent',TimeoutMillis:100})});
 assert.strictEqual(missingResponse.status,500);assert((await missingResponse.text()).includes('deadline exceeded'));
 assert(Date.now()-timeoutStart<1500,'caller context must bound unknown-ID wait');
 await assert.rejects(callback(io.of('/cluster').adapter,'remoteLeave',missing,'absent'),/timeout/);
 // Disconnect(false) leaves another namespace on the same engine usable.
 console.log('Redis unknown-ID bounds passed');
 const goPair=await sharedPair(goB);
 const goMainClosed=onceDisconnect(goPair[0]);
 await callback(io.of('/cluster').adapter,'remoteDisconnect',goPair[0].sid,false);await goMainClosed;
 assert.strictEqual(goPair[1].connected,true);assert.strictEqual(await ack(goPair[1],'identity'),goPair[1].sid);
 const goWhole=await sharedPair(goB),goClosed=goWhole.map(onceDisconnect);
 await callback(io.of('/cluster').adapter,'remoteDisconnect',goWhole[0].sid,true);await Promise.all(goClosed);
 const nodePair=await sharedPair(nodeURL),nodeMainClosed=onceDisconnect(nodePair[0]);
 await control(goA,{Op:'disconnect',SID:nodePair[0].sid,Close:false});await nodeMainClosed;
 assert.strictEqual(nodePair[1].connected,true);assert.strictEqual(await ack(nodePair[1],'identity'),nodePair[1].sid);
 const nodeWhole=await sharedPair(nodeURL),nodeClosed=nodeWhole.map(onceDisconnect);
 await control(goA,{Op:'disconnect',SID:nodeWhole[0].sid,Close:true});await Promise.all(nodeClosed);
 await control(goA,{Op:'clear',Room:'r1'});
 assert.deepStrictEqual(await callback(io.of('/cluster').adapter,'clients',['r1']),[]);
 const disconnected=new Promise(resolve=>peers[1].once('disconnect',resolve));
 await callback(io.of('/cluster').adapter,'remoteDisconnect',peers[1].sid,false);await disconnected;
 const nodeDisconnected=new Promise(resolve=>peers[2].once('disconnect',resolve));
 await control(goA,{Op:'disconnect',SID:peers[2].sid,Close:true});await nodeDisconnected;
 console.log('Redis mixed cluster passed: '+transport);
})().catch(err=>{console.error(err);process.exitCode=1;}).finally(()=>{
 clearTimeout(watchdog);
 const timings=[];
 function cleanup(name,action){const start=Date.now();action();timings.push(name+'='+String(Date.now()-start)+'ms');}
 cleanup('sockets',()=>sockets.forEach(s=>s.close()));
 cleanup('managers',()=>managers.forEach(m=>m.close()));
 cleanup('socket.io',()=>io.close());
 cleanup('redis',()=>{pub.quit();sub.quit();});
 cleanup('http',()=>server.close());
 // engine.io 3.6.2 Polling.doClose arms a closure-private 30s closeTimeout
 // timer when the server closes a polling socket while no GET is pending. The
 // Go-initiated Close:true disconnects of Node-hosted clients above flush their
 // DISCONNECT packets on the pending GET and close at once, so the timer is
 // armed during the scenario. Only a later GET clears it, and the disconnected
 // clients never send one, so no cleanup call can release the event loop.
 // All assertions and cleanup calls have completed; exit with their status.
 process.stdout.write('Redis cleanup: '+timings.join(', ')+'\n',()=>process.exit(process.exitCode||0));
});

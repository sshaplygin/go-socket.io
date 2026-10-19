'use strict';
const assert = require('assert');
const http = require('http');
const serverIO = require('socket.io');
const adapter = require('socket.io-redis');
const clientIO = require('socket.io-client');
const redis = require('redis');
const [goA,goB,addr,prefix,transport] = process.argv.slice(2);
const split = addr.lastIndexOf(':');
const redisOpts = {host:addr.slice(0,split),port:Number(addr.slice(split+1))};
const pub=redis.createClient(redisOpts),sub=redis.createClient(redisOpts);
const server=http.createServer();
const io=serverIO(server);
io.adapter(adapter({pubClient:pub,subClient:sub,key:prefix,requestsTimeout:1500}));
for(const name of ['/cluster','/cluster#other']) {
 const ns=io.of(name);
 ns.on('connection',s=>{
  s.on('identity',cb=>cb(s.id));
  s.on('join',(room,cb)=>s.join(room,()=>cb('ok')));
 });
 ns.adapter.customHook=(data,cb)=>cb({node:data});
}
const sockets=[];
const delay=ms=>new Promise(r=>setTimeout(r,ms));
const callback=(obj,method,...args)=>new Promise((resolve,reject)=>obj[method](...args,(err,value)=>err?reject(err):resolve(value)));
function ack(s,name,...args){return new Promise(resolve=>s.emit(name,...args,resolve));}
async function until(f,what){for(let n=0;n<200;n++){if(await f())return;await delay(20);}throw new Error('timeout: '+what);}
async function connect(url,ns='/cluster') {
 const s=clientIO(url+ns,{transports:[transport],forceNew:true,reconnection:false});sockets.push(s);
 await new Promise((resolve,reject)=>{s.once('connect',resolve);s.once('connect_error',reject);});
 s.events=[];s.on('broadcast',v=>s.events.push(v));s.binary=[];s.on('binary',v=>s.binary.push(v));
 s.sid=await ack(s,'identity');return s;
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
const watchdog=setTimeout(()=>{console.error('Redis cluster timeout');process.exit(1);},35000);
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const nodeURL='http://127.0.0.1:'+server.address().port;
 const peers=[await connect(goA),await connect(goB),await connect(nodeURL)];
 const isolated=await connect(goB,'/cluster#other');
 await until(async()=>{
  const reply=await new Promise((resolve,reject)=>pub.pubsub('NUMSUB',prefix+'-request#/cluster#',(err,v)=>err?reject(err):resolve(v)));
  return Number(reply[1])===3;
 },'three registered Redis subscribers');
 for(const s of peers){await ack(s,'join','r1');await ack(s,'join','r2');}
 await deliveries(peers,[1,1,1],()=>control(goA,{Op:'broadcast',Rooms:['r1','r2']}));
 assert.strictEqual(isolated.events.length,0);
 await deliveries(peers,[1,1,1],async()=>{io.of('/cluster').to('r1').to('r2').emit('broadcast','node');});
 await deliveries(peers,[0,1,1],()=>control(goA,{Op:'broadcast',Except:[peers[0].sid]}));
 await deliveries(peers,[1,0,0],()=>control(goA,{Op:'broadcast',Local:true}));
 await control(goA,{Op:'binary'});
 await until(()=>peers.every(s=>s.binary.length===1),'Go binary');
 io.of('/cluster').emit('binary',{bytes:Buffer.from([0,255,2]),nested:[Buffer.from([3,254])]});
 await until(()=>peers.every(s=>s.binary.length===2),'Node binary');
 for(const s of peers)for(const v of s.binary){assert(Buffer.isBuffer(v.bytes));assert.deepStrictEqual([...v.bytes],[0,255,2]);assert(Buffer.isBuffer(v.nested[0]));assert.deepStrictEqual([...v.nested[0]],[3,254]);}
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
 const nodeCustom=await callback(io.of('/cluster').adapter,'customRequest',{hello:43});
 assert.strictEqual(nodeCustom.length,3);
 await control(goA,{Op:'clear',Room:'r1'});
 assert.deepStrictEqual(await callback(io.of('/cluster').adapter,'clients',['r1']),[]);
 const disconnected=new Promise(resolve=>peers[1].once('disconnect',resolve));
 await callback(io.of('/cluster').adapter,'remoteDisconnect',peers[1].sid,false);await disconnected;
 const nodeDisconnected=new Promise(resolve=>peers[2].once('disconnect',resolve));
 await control(goA,{Op:'disconnect',SID:peers[2].sid,Close:true});await nodeDisconnected;
 console.log('Redis mixed cluster passed: '+transport);
})().catch(err=>{console.error(err);process.exitCode=1;}).finally(()=>{
 clearTimeout(watchdog);sockets.forEach(s=>s.close());io.close();pub.quit();sub.quit();server.close();
});

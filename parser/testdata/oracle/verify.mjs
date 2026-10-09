import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { Encoder, Decoder, protocol } from 'socket.io-parser';

assert.equal(protocol, 5);
const encoder = new Encoder();
const packets = [
 {type:0,nsp:'/'},
 {type:0,nsp:'/admin',data:{token:'秘密',nested:{roles:['reader']}}},
 {type:0,nsp:'/',data:{sid:'server-generated'}},
 {type:1,nsp:'/admin'},
 {type:2,nsp:'/',data:['message','Привет 🌍',null,42]},
 {type:2,nsp:'/unicode/世界',id:0,data:['join',{nested:[true,false]}]},
 {type:2,nsp:'/',data:[42,'numeric name']},
 {type:3,nsp:'/chat',id:9007199254740991,data:[]},
 {type:3,nsp:'/',id:8,data:[null,{receipt:1}]},
 {type:3,nsp:'/',id:9,data:[{code:'invalid_payload',message:'invalid'}]},
 {type:4,nsp:'/admin',data:{message:'denied',data:{code:403}}},
 {type:4,nsp:'/',data:'denied'},
 {type:2,nsp:'/bin',id:3,data:['file',Buffer.from([0,4,255]),{deep:[Buffer.alloc(0),Buffer.from('世界')]}]},
 {type:3,nsp:'/',id:5,data:[null,{result:Buffer.from([255,0])}]},
];
function normalize(v) {
 if (Buffer.isBuffer(v)) return {$binary:v.toString('base64')};
 if (Array.isArray(v)) return v.map(normalize);
 if (v && typeof v === 'object') return Object.fromEntries(Object.entries(v).map(([k,x])=>[k,normalize(x)]));
 return v ?? null;
}
const requests = packets.map(p=>{const [envelope,...bins]=encoder.encode(p);return {envelope,attachments:bins.map(b=>b.toString('base64'))};});
const invalid = [
 {envelope:'7'}, {envelope:'0null'}, {envelope:'1{}'}, {envelope:'2[]'},
 {envelope:'2["connect"]'}, {envelope:'3{}'}, {envelope:'4[]'}, {envelope:'50-["x"]'},
 {envelope:'51-["x",{"_placeholder":true,"num":1}]',attachments:['AA==']},
 {envelope:'51-["x",{"_placeholder":true,"num":-1}]',attachments:['AA==']},
 {envelope:'2["x",'},
];
const stricter = [
 {envelope:'2'}, {envelope:'3'}, {envelope:'4'}, {envelope:'0/admin'},
 {envelope:'51.0-["x",{"_placeholder":true,"num":0}]',attachments:['AA==']},
 {envelope:'51-["x",{"_placeholder":true,"num":0.5}]',attachments:['AA==']},
 {envelope:'39007199254740992[]'},
];
const limits = [
 {envelope:'2["é"]',limits:{MaxEventBytes:6}}, // 7 UTF-8 bytes
 {envelope:'51-["x"]',attachments:['AAE='],limits:{MaxEventBytes:9}},
 {envelope:'52-["x"]',attachments:['',''],limits:{MaxAttachments:1}},
 {envelope:'2["x",[]]',limits:{MaxDepth:1}},
 {envelope:'51-["x"]',attachments:[]},
 {envelope:'2["x"]',attachments:['']},
];
const goOrigins = [
 {packet:{type:0,namespace:'/go',data:{token:'go-origin'}},attachments:[],expected:{type:0,nsp:'/go',data:{token:'go-origin'}}},
 {packet:{type:2,namespace:'/',id:7,data:['go-event',{one:1},[2,3]]},attachments:[],expected:{type:2,nsp:'/',id:7,data:['go-event',{one:1},[2,3]]}},
 {packet:{type:3,namespace:'/go',id:42,data:[null,{_placeholder:true,num:0}]},attachments:['AP8='],expected:{type:3,nsp:'/go',id:42,data:[null,Buffer.from([0,255])]}},
];
const all=[...requests,...invalid,...stricter,...limits,...goOrigins];
const proc=spawnSync('go',['run','./parser/testdata/oracle'],{cwd:fileURLToPath(new URL('../../..',import.meta.url)),input:JSON.stringify(all),encoding:'utf8',timeout:120000,maxBuffer:8<<20});
assert.equal(proc.status,0,proc.stderr || String(proc.error));
const results=JSON.parse(proc.stdout);assert.equal(results.length,all.length);
function decode(r) {
 const d=new Decoder({maxAttachments:64});let value;
 d.on('decoded',p=>{value=p;});
 try {d.add(r.envelope);for(const b of r.attachments??[])d.add(Buffer.from(b,'base64'));return value;} finally {d.destroy();}
}
for(let i=0;i<requests.length;i++) {
 const r=results[i];assert.equal(r.error,undefined,JSON.stringify(r));
 assert.equal(r.envelope,requests[i].envelope);assert.deepEqual(r.attachments,requests[i].attachments);
 assert.deepEqual(r.data,normalize(packets[i].data));
 const decoded=decode(r);assert.ok(decoded);assert.equal(decoded.type,packets[i].type);assert.equal(decoded.nsp,packets[i].nsp);assert.equal(decoded.id,packets[i].id);assert.deepEqual(normalize(decoded.data),normalize(packets[i].data));
}
for(let i=requests.length;i<results.length-goOrigins.length;i++) assert.ok(results[i].error,JSON.stringify(all[i]));
for(const r of invalid) assert.throws(()=>decode(r),JSON.stringify(r));
// These are intentional policy differences, checked against the actual Node decoder.
for(const r of stricter) assert.ok(decode(r),JSON.stringify(r));
for (let i=0;i<goOrigins.length;i++) {
 const r=results[results.length-goOrigins.length+i];assert.equal(r.error,undefined);
 const expected=goOrigins[i].expected;const decoded=decode(r);
 assert.equal(decoded.type,expected.type);assert.equal(decoded.nsp,expected.nsp);assert.equal(decoded.id,expected.id);
 assert.deepEqual(normalize(decoded.data),normalize(expected.data));
 const [envelope,...bins]=encoder.encode(expected);assert.equal(r.envelope,envelope);assert.deepEqual(r.attachments,bins.map(b=>b.toString('base64')));
}
console.log(`${goOrigins.length} Go-origin packets, ${requests.length} Node→Go→Node message groups, ${invalid.length} shared invalid cases, ${stricter.length} strictness differences, ${limits.length} limit/count cases: PASS`);

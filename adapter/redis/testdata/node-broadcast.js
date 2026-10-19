// Oracle: npm socket.io-redis@5.4.0 uses notepack.io@2.2.x.
const mp = require('notepack.io');
const packet = {type:5,nsp:'/chat#nested',data:['binary',{renamed:7,bin:Buffer.from([0,255]),nested:[Buffer.from([1,254])]}]};
console.log(mp.encode(['node-peer',packet,{rooms:['r','s'],except:['excluded'],flags:{volatile:true,compress:false}}]).toString('hex'));
// Decode a Go-produced packet as an additional notepack compatibility oracle.
if (process.argv[2]) {
 const [uid, p, opts] = mp.decode(Buffer.from(process.argv[2], 'hex'));
 const assert = require('assert');
 assert.strictEqual(uid,'go-peer'); assert.strictEqual(p.nsp,'/chat');
 assert.strictEqual(p.type,5); assert.strictEqual(p.data[0],'from-go');
 assert(Buffer.isBuffer(p.data[1].binary)); assert.deepStrictEqual([...p.data[1].binary],[0,255]);
 assert.strictEqual(p.data[1].renamed,7); assert.strictEqual(p.data[1].Ignored,undefined);
 assert.deepStrictEqual(opts.rooms,['r','s']); assert.deepStrictEqual(opts.except,['excluded']);
 assert.strictEqual(opts.flags.volatile,true); assert.strictEqual(opts.flags.compress,false);
}

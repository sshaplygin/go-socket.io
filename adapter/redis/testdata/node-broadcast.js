// Oracle: npm socket.io-redis@5.4.0 uses notepack.io@2.2.x.
const mp = require('notepack.io');
const packet = {type:5,nsp:'/chat#nested',data:['binary',{renamed:7,bin:Buffer.from([0,255]),nested:[Buffer.from([1,254])]}]};
console.log(mp.encode(['node-peer',packet,{rooms:['r','s'],except:['excluded'],flags:{volatile:true,compress:false}}]).toString('hex'));

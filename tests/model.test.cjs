'use strict';
const test=require('node:test'),assert=require('node:assert/strict');
let LogModel;test.before(async()=>{({LogModel}=await import('../src/renderer/model.mjs'));});
const r=(seq,data,direction='RX')=>({seq,time:new Date().toISOString(),direction,length:data.length,dataBase64:data.toString('base64')});
test('UTF-8 split at every byte, fragmented CRLF, and multiple lines preserve original bytes',()=>{
  const data=Buffer.from('温度=24.6\r\n电流=12.0\nEND');
  for(let split=1;split<data.length;split++){const m=new LogModel();m.append(r(1,data.subarray(0,split)));m.append(r(2,data.subarray(split)));assert.deepEqual(m.rows.map(x=>x.text),['温度=24.6','电流=12.0','END']);assert.deepEqual(Buffer.concat(m.rows.flatMap(x=>x.parts.map(p=>Buffer.from(p)))),data);}
});
test('GBK decoding survives fragmentation and invalid binary remains inspectable',()=>{
  const m=new LogModel('gbk');m.append(r(1,Buffer.from([0xce])));m.append(r(2,Buffer.from([0xc2,0xb6])));m.append(r(3,Buffer.from([0xc8,0x0d])));m.append(r(4,Buffer.from([0x0a])));assert.deepEqual(m.rows.map(x=>x.text),['温度']);
  const b=new LogModel();const raw=Buffer.from(Array.from({length:256},(_,i)=>i));b.append(r(1,raw));assert.deepEqual(Buffer.concat(b.rows.flatMap(x=>x.parts.map(p=>Buffer.from(p)))),raw);assert(b.rows.some(x=>x.text.includes('␀')));
});
test('TX uses actual bytes; decoding changes do not alter raw buffers',()=>{
  const m=new LogModel();m.append(r(1,Buffer.from('ps\r\n'),'TX'));assert.equal(m.rows[0].text,'ps');assert.deepEqual([...m.prefix(m.rows[0])],[112,115,13,10]);m.setEncoding('gbk');assert.equal(m.records[0].dataBase64,Buffer.from('ps\r\n').toString('base64'));
});
test('newline-heavy traffic and encoding rebuild stay bounded; ASCII retains raw high bytes',()=>{
  const m=new LogModel();m.append(r(1,Buffer.alloc(65536,10)));assert.equal(m.rows.length,10000);assert.equal(m.trimmed,55536);
  m.setEncoding('gbk');assert.equal(m.rows.length,10000);assert(m.rowBytes<=16*1024*1024);
  const a=new LogModel('ascii');a.append(r(1,Buffer.from([65,0,128,255])));assert.equal(a.rows[0].text,'A␀��');assert.deepEqual([...a.prefix(a.rows[0])],[65,0,128,255]);
});

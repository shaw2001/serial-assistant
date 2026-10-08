'use strict';
const test=require('node:test'),assert=require('node:assert/strict');
const fs=require('node:fs/promises'),os=require('node:os'),path=require('node:path');
const {EventEmitter}=require('node:events');
const {payload,serialOptions}=require('../src/main/payload.cjs');
const {SerialService}=require('../src/main/serial-service.cjs');
const {ConfigStore,defaults,validate}=require('../src/main/config.cjs');
const delay=ms=>new Promise(r=>setTimeout(r,ms));
const packet=(content,mode='text',ending='none',encoding='utf-8')=>({content,mode,ending,encoding});
const settings=path=>({path,baudRate:115200,dataBits:8,parity:'none',stopBits:1,flow:'none'});
class FakePort extends EventEmitter{
  constructor(options){super();this.options=options;this.isOpen=false;this.writes=[];this.writeDelay=0;this.writeError=false;}
  open(callback){setImmediate(()=>{this.isOpen=true;callback(null);});}
  close(callback){setImmediate(()=>{this.isOpen=false;this.emit('close');callback?.(null);});}
  write(data,callback){this.writes.push(Buffer.from(data));setTimeout(()=>callback(this.writeError?new Error('write failed'):!this.isOpen?new Error('disconnected'):null),this.writeDelay);}
  drain(callback){setImmediate(()=>callback(this.isOpen?null:new Error('disconnected')));}
}
async function fixture(t,options={}){const directory=await fs.mkdtemp(path.join(os.tmpdir(),'serial-assistant-test-')),ports=[];const service=new SerialService({directory,createPort:opt=>{const p=new FakePort(opt);ports.push(p);return p;},...options});t.after(async()=>{await service.shutdown();await fs.rm(directory,{recursive:true,force:true});});return{service,ports,directory};}
test('payload bytes, endings, UTF-8/GBK/ASCII, invalid HEX and 64 KiB bounds',()=>{
  assert.deepEqual([...payload(packet('ps','text','crlf'))],[112,115,13,10]);
  assert.deepEqual([...payload(packet('AA 55 00 ff','hex'))],[170,85,0,255]);
  assert.equal(payload(packet('温度','text','none','gbk')).toString('hex'),'cec2b6c8');
  assert.equal(payload(packet('温度')).toString('hex'),'e6b8a9e5baa6');
  for(const invalid of ['AA5','GG','  '])assert.throws(()=>payload(packet(invalid,'hex')));
  assert.throws(()=>payload(packet('中文','text','none','ascii')));
  assert.throws(()=>payload(packet('😀','text','none','gbk')));
  assert.equal(payload(packet('00'.repeat(65536),'hex')).length,65536);
  assert.throws(()=>payload(packet('00'.repeat(65537),'hex')));
  assert.equal(serialOptions({...settings('COM7'),baudRate:250000}).baudRate,250000);
  assert.throws(()=>serialOptions({...settings('COM7'),baudRate:0}));
  assert.throws(()=>serialOptions(settings('/etc/passwd')));
});
test('four independent sessions; actual send counts after drain, raw 00..FF capture and export',async t=>{
  const {service,ports}=await fixture(t,{journalOptions:{segmentBytes:600}});
  for(let i=1;i<=4;i++)await service.open('session-'+i,settings('COM'+i));
  await assert.rejects(service.open('session-5',settings('COM5')),/4 个/);
  await assert.rejects(service.open('session-2',settings('COM2')),/正在使用/);
  const raw=Buffer.from(Array.from({length:256},(_,i)=>i));ports[0].emit('data',raw);ports[1].emit('data',Buffer.from('AT\r\n'));
  await service.send('session-3',packet('ps','text','crlf'));
  assert.equal(service.get('session-1').rx,256);assert.equal(service.get('session-2').rx,4);assert.equal(service.get('session-1').tx,0);assert.equal(service.get('session-3').tx,4);
  const saved=[];for await(const r of service.records('session-1','all'))saved.push(r);
  assert.deepEqual(Buffer.concat(saved.filter(r=>r.direction==='RX').map(r=>Buffer.from(r.dataBase64,'base64'))),raw);
  const clear=service.clear('session-1');assert(clear.cutoff>0);assert.equal(service.get('session-1').records.length,0);
  const after=[];for await(const r of service.records('session-1','all'))after.push(r);assert(after.some(r=>r.direction==='RX'));
  assert(service.get('session-1').journal.files.length>=2,'journal rotates without losing raw data');
});
test('failure never counts TX; queue bounded, ordered, and cancelled on close',async t=>{
  const {service,ports}=await fixture(t);await service.open('session-a',settings('COM5'));const p=ports[0];
  p.writeError=true;await assert.rejects(service.send('session-a',packet('bad')),/write failed/);assert.equal(service.get('session-a').tx,0);
  p.writeError=false;p.writeDelay=30;
  const promises=[];for(let i=0;i<8;i++)promises.push(service.send('session-a',packet(String(i))).catch(e=>e));
  await delay(3);await service.close('session-a');const results=await Promise.all(promises);assert(results.some(x=>x instanceof Error));const count=p.writes.length;await delay(80);assert.equal(p.writes.length,count);
  await service.open('session-a',settings('COM5'));assert.equal(service.get('session-a').queue.length,0);assert.equal(service.get('session-a').periodic,undefined);
});
test('periodic congestion skips rounds and unplugging one device preserves another',async t=>{
  const {service,ports}=await fixture(t);await service.open('session-a',settings('COM5'));await service.open('session-b',settings('COM8'));
  ports[0].writeDelay=35;service.startPeriodic('session-a',packet('P'),10);service.startPeriodic('session-b',packet('Q'),20);await delay(130);
  assert(service.get('session-a').periodic.skipped>0);assert(service.get('session-a').queue.length<=1);assert(ports[1].writes.length>2);
  ports[0].isOpen=false;ports[0].emit('close',new Error('USB removed'));const before=ports[0].writes.length,beforeOther=ports[1].writes.length;await delay(100);
  assert.equal(service.get('session-a').periodic,null);assert.equal(ports[0].writes.length,before);assert(ports[1].writes.length>beforeOther);assert.equal(service.get('session-b').status,'connected');
});
test('100 open/close cycles release resources; close during opening cancels cleanly',async t=>{
  const {service}=await fixture(t);for(let i=0;i<100;i++){await service.open('session-a',settings('COM5'));await service.close('session-a');}assert.equal(service.get('session-a').status,'disconnected');assert.equal(service.get('session-a').queue.length,0);
  const opening=service.open('session-b',settings('COM8')).catch(e=>e);await service.close('session-b');assert(await opening instanceof Error);assert(!service.get('session-b').port.isOpen);
});
test('display cache is bounded and disk capture continues; overflow disables capture visibly',async t=>{
  const {service,ports}=await fixture(t,{journalOptions:{queueBytes:2048}});await service.open('session-a',settings('COM5'));
  ports[0].emit('data',Buffer.alloc(3000));assert.equal(service.get('session-a').capture,false);assert(service.get('session-a').captureDropped>=3000);assert(service.get('session-a').errors>0);
  for(let i=0;i<11000;i++)ports[0].emit('data',Buffer.from([i%256]));assert(service.get('session-a').records.length<=10000);assert(service.get('session-a').trimmed>0);assert.equal(service.get('session-a').rx,14000);
});
test('atomic preferences, corrupt config recovery, and malformed import rejection',async t=>{
  const directory=await fs.mkdtemp(path.join(os.tmpdir(),'serial-config-test-'));t.after(()=>fs.rm(directory,{recursive:true,force:true}));const file=path.join(directory,'config.json'),store=new ConfigStore(file),config=defaults();
  await Promise.all([store.save(config),store.save({...config,activeId:'session-b'})]);assert.equal((await store.load()).config.activeId,'session-b');
  await fs.writeFile(file,'{broken');const result=await store.load();assert(result.warning);assert.equal(await fs.readFile(file,'utf8'),'{broken');
  assert.throws(()=>validate({...defaults(),schema:99}));
  assert.throws(()=>validate({...defaults(),devices:[{id:'session-a',path:'COM5',baud:'115200',receiveEncoding:'utf-8',params:{data:8,parity:'none',stop:1,flow:'none'},history:[null]}]}));
});
test('slow renderer bounds pending IPC while raw capture and RX statistics continue',async t=>{
  let ready=false,delivered=0;const {service,ports}=await fixture(t,{canDeliver:()=>ready,onBatch:()=>delivered++,journalOptions:{queueBytes:16*1024*1024}});
  await service.open('session-a',settings('COM5'));const chunk=Buffer.alloc(65536,255);
  for(let i=0;i<65;i++)ports[0].emit('data',chunk);
  service.flushEvents();assert.equal(delivered,0);assert(service.eventBytes<=4*1024*1024);assert.equal(service.get('session-a').viewDropped,65536);assert.equal(service.get('session-a').rx,65*65536);
  const records=[];for await(const r of service.records('session-a','all'))if(r.direction==='RX')records.push(r);
  assert.equal(records.reduce((n,r)=>n+r.length,0),65*65536);assert.equal(service.get('session-a').captureDropped,0);
  ready=true;service.flushEvents();assert.equal(delivered,1);assert.equal(service.eventBytes,0);
});

'use strict';
const {SerialPort}=require('serialport');
const {performance}=require('node:perf_hooks');
const {payload,serialOptions}=require('./payload.cjs');
const {Journal,readJournal}=require('./journal.cjs');
const MAX_RECORDS=10000,MAX_CACHE=16*1024*1024,MAX_EVENTS=4*1024*1024;
class SerialService {
  constructor({directory,createPort=options=>new SerialPort(options),listPorts=()=>SerialPort.list(),onBatch=()=>{},canDeliver=()=>true,journalOptions={}}) {
    this.directory=directory;this.createPort=createPort;this.listPorts=listPorts;this.onBatch=onBatch;this.canDeliver=canDeliver;
    this.journalOptions=journalOptions;this.sessions=new Map();this.events=[];this.eventBytes=0;
    this.batchTimer=setInterval(()=>this.flushEvents(),40);this.batchTimer.unref?.();
  }
  async list() { return this.listPorts(); }
  get(id) { const s=this.sessions.get(id);if(!s)throw new Error('串口会话不存在。');return s; }
  emit(event) {
    const n=event.record?.length||0;
    this.changed=true;
    if (this.eventBytes+n>MAX_EVENTS || this.events.length>=20000) { const s=this.sessions.get(event.id);if(s&&event.type==='record')s.viewDropped+=n;return; }
    this.events.push(event);this.eventBytes+=n;
  }
  flushEvents() {
    if(!this.canDeliver())return;
    if (!this.events.length&&!this.changed) return;this.changed=false;
    const events=this.events;this.events=[];this.eventBytes=0;
    this.onBatch({events,states:[...this.sessions.values()].map(s=>this.snapshot(s))});
  }
  snapshot(s) { return {id:s.id,path:s.config.path,status:s.status,rx:s.rx,tx:s.tx,errors:s.errors,trimmed:s.trimmed,viewDropped:s.viewDropped,captureDropped:s.captureDropped,capture:s.capture,recorded:s.recorded,logFiles:s.files.concat(s.journal?.files||[]),periodic:s.periodic?{interval:s.periodic.interval,count:s.periodic.count,skipped:s.periodic.skipped}:null,queued:s.queue.length,busy:s.busy}; }
  state(s) { this.changed=true; }
  system(s,text) { this.record(s,'SYS',Buffer.alloc(0),text); }
  record(s,direction,data,text='') {
    const r={type:'record',seq:++s.seq,time:new Date().toISOString(),timezoneOffset:-new Date().getTimezoneOffset(),monotonicMs:Number((process.hrtime.bigint()-s.started)/1000000n),direction,length:data.length,dataBase64:data.toString('base64'),...(text?{text}:{})};
    s.records.push(r);s.cacheBytes+=data.length+text.length*2+256;
    while(s.records.length>MAX_RECORDS||s.cacheBytes>MAX_CACHE){const old=s.records.shift();s.cacheBytes-=old.length+(old.text?.length||0)*2+256;s.trimmed++;}
    if(direction==='RX')s.rx+=data.length;if(direction==='TX')s.tx+=data.length;
    if(s.capture&&s.journal){if(!s.journal.append(r))s.captureDropped+=data.length;}
    this.emit({type:'record',id:s.id,record:r});
  }
  async beginCapture(s) {
    if(s.journal){await s.journal.close();s.files.push(...s.journal.files);}
    s.journal=new Journal(this.directory,{id:s.id,port:s.config.path,config:s.config,startedAt:new Date().toISOString()},error=>{
      s.capture=false;s.errors++;s.captureDropped+=s.journal?.queuedRaw||0;this.emit({type:'warning',id:s.id,text:'记录失败：'+error.message});this.state(s);
    },{...this.journalOptions,onWritten:()=>{s.recorded++;this.state(s);}});
    await s.journal.flush();
    if(s.journal.failed)throw s.journal.failed;
  }
  async open(id,config) {
    if(typeof id!=='string'||!/^session-[a-z0-9-]{1,64}$/i.test(id))throw new Error('无效的会话标识。');
    const options=serialOptions(config),key=options.path.replace('\\\\.\\','').match(/^COM\d+$/i)?options.path.replace('\\\\.\\','').toUpperCase():options.path;
    let s=this.sessions.get(id);
    if(s&&s.status!=='disconnected'&&s.status!=='error')throw new Error('当前串口正在使用，请先断开。');
    if([...this.sessions.values()].some(x=>x!==s&&x.key===key&&['opening','connected','closing'].includes(x.status)))throw new Error('该串口已在其他会话中打开。');
    if(!s&&this.sessions.size>=4)throw new Error('最多支持 4 个串口会话。');
    if(!s){s={id,status:'disconnected',generation:0,rx:0,tx:0,seq:0,errors:0,trimmed:0,viewDropped:0,captureDropped:0,recorded:0,records:[],cacheBytes:0,files:[],capture:true,queue:[],queueBytes:0,busy:false,started:process.hrtime.bigint()};this.sessions.set(id,s);}
    s.config={...config};s.key=key;s.status='opening';s.cancelled=false;const generation=++s.generation;
    s.capture=config.capture!==false;this.state(s);
    const work=(async()=>{
      if(s.capture){try{await this.beginCapture(s);}catch{ s.capture=false; }}
      const port=this.createPort(options);s.port=port;
      port.on('data',data=>{if(s.port===port&&s.status==='connected'&&s.generation===generation)this.record(s,'RX',Buffer.from(data));});
      port.on('error',error=>{if(s.port!==port)return;s.errors++;this.system(s,'串口错误：'+error.message);this.emit({type:'warning',id:s.id,text:error.message});this.stopPeriodic(id);this.state(s);});
      port.on('close',error=>{if(s.port!==port)return;this.stopPeriodic(id);this.cancelQueue(s,'串口已关闭。');s.status=error?'error':'disconnected';s.generation++;if(error){s.errors++;this.system(s,'设备断开：'+error.message);}this.state(s);});
      await new Promise((resolve,reject)=>port.open(error=>error?reject(error):resolve()));
      if(s.cancelled||s.generation!==generation){if(port.isOpen)await new Promise(resolve=>port.close(()=>resolve()));throw new Error('连接已取消。');}
      s.status='connected';this.system(s,'已连接 '+config.path+' · '+config.baudRate+' · '+config.dataBits+({none:'N',even:'E',odd:'O',mark:'M',space:'S'})[config.parity]+config.stopBits);this.state(s);return this.snapshot(s);
    })();
    s.openPromise=work;
    try{return await work;}catch(error){s.status='disconnected';s.errors++;this.system(s,'连接失败：'+error.message);this.state(s);if(s.journal){await s.journal.close();s.files.push(...s.journal.files);s.journal=null;}throw error;}finally{s.openPromise=null;}
  }
  cancelQueue(s,reason) { const jobs=s.queue.splice(0);s.queueBytes=0;for(const job of jobs)job.reject(new Error(reason)); }
  async close(id) {
    const s=this.sessions.get(id);if(!s)return;
    this.stopPeriodic(id);s.cancelled=true;this.cancelQueue(s,'串口关闭，待发送命令已取消。');
    if(s.status==='opening'&&s.openPromise){s.status='closing';this.state(s);try{await s.openPromise;}catch{}}
    if(s.port?.isOpen){s.status='closing';this.state(s);await new Promise(resolve=>s.port.close(()=>resolve()));}
    s.generation++;s.status='disconnected';this.system(s,'串口已断开，周期发送已停止。');
    if(s.journal){await s.journal.close();s.files.push(...s.journal.files);s.journal=null;}
    this.state(s);return this.snapshot(s);
  }
  async remove(id) {await this.close(id);this.sessions.delete(id);}
  operation(s,method,data) {
    const port=s.port;
    return new Promise((resolve,reject)=>{
      let timer;
      const finish=error=>{clearTimeout(timer);port.removeListener('error',onError);port.removeListener('close',onClose);error?reject(error):resolve();};
      const onError=error=>finish(error),onClose=()=>finish(new Error('设备已断开。'));
      port.once('error',onError);port.once('close',onClose);timer=setTimeout(()=>finish(new Error('串口写入超时。')),10000);timer.unref?.();
      try{if(method==='write')port.write(data,error=>finish(error));else port.drain(error=>finish(error));}catch(error){finish(error);}
    });
  }
  send(id,config,periodic=false) {
    const s=this.get(id),data=payload(config);
    if(s.status!=='connected'||!s.port.isOpen)throw new Error('请先连接当前串口。');
    if(s.queue.length>=64||s.queueBytes+data.length>256*1024)throw new Error('发送队列已满，请降低发送频率。');
    return new Promise((resolve,reject)=>{s.queue.push({data,config,periodic,generation:s.generation,resolve,reject});s.queueBytes+=data.length;this.pump(s);});
  }
  async pump(s) {
    if(s.busy)return;s.busy=true;
    try{while(s.queue.length){const job=s.queue.shift();s.queueBytes-=job.data.length;
      try{if(s.status!=='connected'||s.generation!==job.generation)throw new Error('待发送命令已取消。');await this.operation(s,'write',job.data);await this.operation(s,'drain');if(s.status!=='connected'||s.generation!==job.generation)throw new Error('设备已断开，发送未确认。');this.record(s,'TX',job.data);if(job.periodic&&s.periodic)s.periodic.count++;job.resolve({length:job.data.length,status:'submitted'});
      }catch(error){s.errors++;this.system(s,'发送失败：'+error.message);this.stopPeriodic(s.id);job.reject(error);}
    }}finally{s.busy=false;this.state(s);}
  }
  startPeriodic(id,config,interval) {
    const s=this.get(id);if(s.status!=='connected')throw new Error('请先连接当前串口。');payload(config);
    if(!Number.isInteger(interval)||interval<10||interval>3600000)throw new Error('发送间隔应为 10–3600000 ms。');
    this.stopPeriodic(id);const p={config:{...config},interval,count:0,skipped:0,started:performance.now(),due:0};s.periodic=p;
    const tick=()=>{if(s.periodic!==p||s.status!=='connected')return;const due=Math.floor((performance.now()-p.started)/interval);p.skipped+=Math.max(0,due-p.due);p.due=due+1;if(s.busy||s.queue.length){p.skipped++;this.state(s);return;}Promise.resolve().then(()=>{if(s.periodic===p&&s.status==='connected')return this.send(id,p.config,true);}).catch(error=>{this.stopPeriodic(id);this.emit({type:'warning',id,text:'周期发送已停止：'+error.message});});};
    p.timer=setInterval(tick,interval);tick();this.system(s,'周期发送开始 · '+interval+' ms');this.state(s);return this.snapshot(s);
  }
  stopPeriodic(id) {const s=this.sessions.get(id);if(!s?.periodic)return;clearInterval(s.periodic.timer);s.periodic=null;this.state(s);}
  async setCapture(id,on) {const s=this.get(id);if(typeof on!=='boolean')throw new Error('无效的记录设置。');if(on&&(!s.journal||s.journal.failed))await this.beginCapture(s);s.capture=on;this.system(s,on?'会话记录已开启。':'会话记录已暂停。');this.state(s);return this.snapshot(s);}
  clear(id) {const s=this.get(id);s.records=[];s.cacheBytes=0;return {...this.snapshot(s),cutoff:s.seq};}
  async *records(id,scope,ids) {
    const s=this.get(id),cutoff=s.seq;
    if(scope==='all'){if(s.journal)await s.journal.flush();yield* readJournal(s.files.concat(s.journal?.files||[]),cutoff);}
    else {const selected=scope==='visible'?new Set(ids):null;for(const record of [...s.records])if(!selected||selected.has(record.seq))yield record;}
  }
  async shutdown() {clearInterval(this.batchTimer);await Promise.allSettled([...this.sessions.keys()].map(id=>this.close(id)));this.flushEvents();}
}
module.exports={SerialService,MAX_RECORDS,MAX_CACHE};

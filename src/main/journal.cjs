'use strict';
const fs = require('node:fs');
const fsp = require('node:fs/promises');
const path = require('node:path');
const readline = require('node:readline');
const crypto = require('node:crypto');
class Journal {
  constructor(directory, metadata, onError, {segmentBytes=100*1024*1024, queueBytes=4*1024*1024,onWritten=()=>{}}={}) {
    this.directory=directory; this.metadata=metadata; this.onError=onError;
    this.segmentBytes=segmentBytes; this.queueLimit=queueBytes;
    this.files=[]; this.queued=0; this.queuedRaw=0; this.size=0; this.closed=false; this.failed=null;this.onWritten=onWritten;
    this.prefix=new Date().toISOString().replace(/[:.]/g,'-')+'-'+metadata.id+'-'+crypto.randomBytes(4).toString('hex');
    this.tail=fsp.mkdir(directory,{recursive:true}).then(()=>this.rotate()).catch(e=>this.fail(e));
  }
  fail(error) { if (!this.failed) { this.failed=error; this.onError(error); } }
  async rotate() {
    if (this.handle) await this.handle.close();
    const name=path.join(this.directory,this.prefix+'-'+String(this.files.length+1).padStart(3,'0')+'.jsonl');
    this.handle=await fsp.open(name,'wx'); this.files.push(name);
    const header=JSON.stringify({type:'metadata',schema:1,...this.metadata})+'\n';
    await this.handle.writeFile(header); this.size=Buffer.byteLength(header);
  }
  append(record) {
    if (this.closed || this.failed) return false;
    const line=JSON.stringify(record)+'\n',length=Buffer.byteLength(line);
    if (this.queued+length>this.queueLimit) { this.fail(new Error('日志写盘队列已满，记录已停止；串口接收继续。')); return false; }
    this.queued+=length;this.queuedRaw+=record.length||0;
    this.tail=this.tail.then(async()=>{
      if (this.failed) return;
      if (this.size+length>this.segmentBytes) await this.rotate();
      await this.handle.writeFile(line); this.size+=length;this.onWritten(record);
    }).catch(e=>this.fail(e)).finally(()=>{this.queued-=length;this.queuedRaw-=record.length||0;});
    return true;
  }
  async flush() { await this.tail; }
  async close() { this.closed=true; await this.flush(); if (this.handle) { await this.handle.close(); this.handle=null; } }
}
async function* readJournal(files,cutoff=Infinity) {
  for (const file of files) {
    const input=fs.createReadStream(file),lines=readline.createInterface({input,crlfDelay:Infinity});
    try {
      for await (const line of lines) {
        if (!line.trim()) continue;
        let record; try { record=JSON.parse(line); } catch { throw new Error('会话日志包含不完整记录，请保留原文件进行恢复：'+path.basename(file)); }
        if (record.type==='metadata') continue;
        if (record.seq<=cutoff) yield record;
      }
    } finally { lines.close(); input.destroy(); }
  }
}
module.exports={Journal,readJournal};

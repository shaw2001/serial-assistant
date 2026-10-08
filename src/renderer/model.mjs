export const fromBase64=s=>Uint8Array.from(atob(s),c=>c.charCodeAt(0));
export const toHex=b=>Array.from(b,x=>x.toString(16).padStart(2,'0').toUpperCase()).join(' ');
export class LogModel{
  constructor(encoding='utf-8'){this.encoding=encoding;this.records=[];this._rows=[];this._rowStart=0;this.recordBytes=0;this.rowBytes=0;this.trimmed=0;this.nextRow=0;this.breakStream();}
  get rows(){return this._rowStart?this._rows.slice(this._rowStart):this._rows;}
  prune(){while(this._rows.length-this._rowStart>10000||this.rowBytes>16*1024*1024){const r=this._rows[this._rowStart++];this.rowBytes-=r.length;this.trimmed++;if(this.current===r)this.current=null;if(this.pendingCR===r)this.pendingCR=null;}if(this._rowStart>4096){this._rows=this._rows.slice(this._rowStart);this._rowStart=0;}}
  decode(decoder,data,stream=false){return this.encoding==='ascii'?Array.from(data,b=>b<128?String.fromCharCode(b):'�').join(''):decoder.decode(data,{stream});}
  breakStream(){if(this.decoder){const rest=this.decoder.decode();if(rest&&this.current)this.current.text+=rest;}this.decoder=new TextDecoder(this.encoding==='ascii'?'windows-1252':this.encoding);this.current=null;this.pendingCR=null;}
  clear(){this.records=[];this._rows=[];this._rowStart=0;this.recordBytes=0;this.rowBytes=0;this.breakStream();}
  setEncoding(encoding){if(encoding===this.encoding)return;this.encoding=encoding;const records=this.records.slice();this._rows=[];this._rowStart=0;this.rowBytes=0;this.breakStream();for(const r of records)this.consume(r);}
  row(record){const r={id:++this.nextRow,time:record.time,direction:record.direction,text:'',parts:[],length:0,sources:[],closed:false};this._rows.push(r);this.prune();return r;}
  part(row,data,seq){if(data.length){row.parts.push(data);row.length+=data.length;this.rowBytes+=data.length;}if(row.sources[row.sources.length-1]!==seq)row.sources.push(seq);this.prune();}
  append(record){this.records.push(record);this.recordBytes+=record.length+256;while(this.records.length>10000||this.recordBytes>16*1024*1024){const r=this.records.shift();this.recordBytes-=r.length+256;}this.consume(record);}
  consume(record){
    if(record.direction==='SYS'){const row=this.row(record);row.text=record.text||'';row.sources=[record.seq];return;}
    const data=fromBase64(record.dataBase64);
    if(record.direction==='TX'){let at=0;const decoder=new TextDecoder(this.encoding==='ascii'?'windows-1252':this.encoding);while(at<data.length){const row=this.row(record),part=data.subarray(at,Math.min(at+8192,data.length));this.part(row,part,record.seq);row.text=this.decode(decoder,part,at+part.length<data.length).replace(/[\r\n]/g,'').replace(/\0/g,'␀');at+=part.length;}return;}
    let at=0;
    if(this.pendingCR){if(data[0]===10){this.part(this.pendingCR,data.subarray(0,1),record.seq);this.decoder.decode(data.subarray(0,1),{stream:true});at=1;}this.pendingCR=null;}
    while(at<data.length){if(!this.current)this.current=this.row(record);const row=this.current;let end=at;const limit=Math.min(data.length,at+8192-row.length);while(end<limit&&data[end]!==10&&data[end]!==13)end++;const separator=end<limit&&(data[end]===10||data[end]===13);if(separator)end++;const part=data.subarray(at,end);this.part(row,part,record.seq);row.text+=this.decode(this.decoder,part,true).replace(/[\r\n]/g,'').replace(/\0/g,'␀');at=end;if(separator){row.closed=true;if(data[end-1]===13){if(at<data.length&&data[at]===10){this.part(row,data.subarray(at,at+1),record.seq);this.decoder.decode(data.subarray(at,at+1),{stream:true});at++;}else this.pendingCR=row;}this.current=null;}else if(row.length>=8192){this.current=null;}}
  }
  prefix(row,max=512){const result=new Uint8Array(Math.min(row.length,max));let at=0;for(const part of row.parts){const n=Math.min(part.length,result.length-at);result.set(part.subarray(0,n),at);at+=n;if(at===result.length)break;}return result;}
}

'use strict';
const fsp=require('node:fs/promises');
const path=require('node:path');
const crypto=require('node:crypto');
const GROUPS=['rtt','at','binary','custom'];
const ENCODINGS=['utf-8','ascii','gbk'];
const validPayload=c=>c&&typeof c.content==='string'&&c.content.length<=196608&&['text','hex'].includes(c.mode)&&ENCODINGS.includes(c.encoding)&&['none','cr','lf','crlf'].includes(c.ending);
const commands={rtt:[['线程列表','ps'],['定时器列表','list_timer'],['低功耗状态','pm_dump'],['系统信息','sysinfo'],['CPU 使用率','cpu']],at:[['连通测试','AT'],['设备信息','ATI'],['信号强度','AT+CSQ'],['注册状态','AT+CREG?'],['查询卡号','AT+CCID']],binary:[['读取寄存器','01 03 00 00 00 02 C4 0B','hex','none']],custom:[]};
function defaults(){return{schema:1,devices:[],commands:Object.fromEntries(Object.entries(commands).map(([group,list])=>[group,list.map((c,i)=>({id:group+'-'+i,name:c[0],content:c[1],mode:c[2]||'text',encoding:'utf-8',ending:c[3]||'crlf'}))]))};}
function validate(config){
  if(!config||config.schema!==1||!Array.isArray(config.devices)||config.devices.length>4)throw new Error('配置版本或设备列表无效。');
  if(Buffer.byteLength(JSON.stringify(config))>2*1024*1024)throw new Error('配置文件不能超过 2 MiB。');
  const ids=new Set();
  for(const d of config.devices){if(!d||!/^session-[a-z0-9-]{1,64}$/i.test(d.id)||ids.has(d.id)||typeof d.path!=='string'||d.path.length>256||!/^\d+$/.test(String(d.baud))||Number(d.baud)<1||Number(d.baud)>12000000||!ENCODINGS.includes(d.receiveEncoding))throw new Error('设备配置无效。');ids.add(d.id);
    if(!d.params||![5,6,7,8].includes(d.params.data)||!['none','even','odd','mark','space'].includes(d.params.parity)||![1,1.5,2].includes(d.params.stop)||!['none','rtscts','xonxoff'].includes(d.params.flow))throw new Error('串口参数无效。');
    if(d.draft&&!validPayload(d.draft))throw new Error('发送草稿无效。');
    if(d.history&&(!Array.isArray(d.history)||d.history.length>100||!d.history.every(validPayload)))throw new Error('发送历史无效。');
    if(d.group&&!GROUPS.includes(d.group))throw new Error('快捷指令分组无效。');
    if(d.mode&&!['text','hex','mixed'].includes(d.mode))throw new Error('显示模式无效。');
  }
  if(!config.commands||!GROUPS.every(g=>Array.isArray(config.commands[g])&&config.commands[g].length<=200))throw new Error('快捷指令分组无效。');
  for(const c of Object.values(config.commands).flat()){if(!c||typeof c.id!=='string'||typeof c.name!=='string'||c.name.length>80||typeof c.content!=='string'||c.content.length>196608||!['text','hex'].includes(c.mode)||!ENCODINGS.includes(c.encoding)||!['none','cr','lf','crlf'].includes(c.ending))throw new Error('快捷指令格式无效。');}
  return config;
}
class ConfigStore{
  constructor(file){this.file=file;this.pending=Promise.resolve();}
  async load(){try{return{config:validate(JSON.parse(await fsp.readFile(this.file,'utf8'))),warning:''};}catch(e){return{config:defaults(),warning:e.code==='ENOENT'?'':'本机配置读取失败，已使用默认配置。原文件保留。'};}}
  save(value){const text=JSON.stringify(validate(value),null,2)+'\n';const work=this.pending.then(async()=>{await fsp.mkdir(path.dirname(this.file),{recursive:true});const temp=this.file+'.'+crypto.randomBytes(6).toString('hex')+'.tmp';try{await fsp.writeFile(temp,text,{flag:'wx',mode:0o600});await fsp.rename(temp,this.file);}catch(e){await fsp.unlink(temp).catch(()=>{});throw e;}});this.pending=work.catch(()=>{});return work;}
}
module.exports={ConfigStore,defaults,validate};

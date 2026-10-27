// Quick-command batch import/export: a human-editable JSON or CSV file.
// JSON: {"格式":"串口助手快捷指令","version":1,"groups":[{"name":"RT-Thread","commands":[
//         {"name":"线程列表","command":"ps","hex":false,"newline":"crlf","encoding":"utf-8","description":"…"}]}]}
// CSV : group,name,command,hex,newline,encoding,description  (Chinese headers also accepted)
export const BUILTIN_GROUPS={rtt:'RT-Thread',at:'AT 指令',binary:'Modbus',custom:'常用'};
const BUILTIN_BY_NAME={'rt-thread':'rtt','rtthread':'rtt','rtt':'rtt','at 指令':'at','at指令':'at','at':'at','modbus':'binary','binary':'binary','常用':'custom','custom':'custom'};
export const MAX_GROUPS=32,MAX_PER_GROUP=200;
export const groupLabel=k=>BUILTIN_GROUPS[k]||String(k).replace(/^g:/,'');
export function groupKey(name){const n=String(name??'').trim();if(!n)return 'custom';const b=BUILTIN_BY_NAME[n.toLowerCase()];if(b)return b;return 'g:'+n;}
export const groupKeys=commands=>[...Object.keys(BUILTIN_GROUPS),...Object.keys(commands||{}).filter(k=>!(k in BUILTIN_GROUPS))];
const NEWLINES={none:'none','无':'none','':'crlf',cr:'cr',lf:'lf',crlf:'crlf','\\r':'cr','\\n':'lf','\\r\\n':'crlf'};
const ENCODINGS={'utf-8':'utf-8',utf8:'utf-8','':'utf-8',gbk:'gbk',ascii:'ascii'};
const TRUE=new Set(['true','1','yes','y','是','hex','十六进制']),FALSE=new Set(['false','0','no','n','否','text','文本','']);
function bool(v,where){if(typeof v==='boolean')return v;const s=String(v??'').trim().toLowerCase();if(TRUE.has(s))return true;if(FALSE.has(s))return false;throw new Error(where+'“hex”只能填 true/false（或 是/否、1/0）。');}
export const normalizeHex=s=>String(s).replace(/0x/gi,' ').replace(/[,;]/g,' ').trim().replace(/\s+/g,' ').toUpperCase();
function hexOk(s){const t=s.replace(/\s/g,'');return /^[0-9a-f]*$/i.test(t)&&t.length%2===0&&t.length>0;}
// Normalises one command; throws a Chinese error that names the location.
export function makeCommand(raw,where){
  const name=String(raw.name??'').trim(),content=String(raw.command??raw.content??'');
  if(!name)throw new Error(where+'缺少“名称”。');
  if([...name].length>80)throw new Error(where+'名称“'+name.slice(0,20)+'…”超过 80 个字符。');
  if(!content.trim())throw new Error(where+'指令“'+name+'”缺少“指令内容”。');
  if(content.length>4096)throw new Error(where+'指令“'+name+'”内容超过 4096 个字符。');
  const hex=raw.hex===undefined&&raw.mode!==undefined?String(raw.mode).toLowerCase()==='hex':bool(raw.hex,where);
  const nlRaw=String(raw.newline??raw.ending??'').trim(),nlKey=nlRaw.toLowerCase();
  let ending=NEWLINES[nlKey];if(ending===undefined)throw new Error(where+'“newline”只能是 none/cr/lf/crlf（或 无），当前为“'+nlRaw+'”。');
  if(hex&&!nlRaw)ending='none';
  const encRaw=String(raw.encoding??'').trim().toLowerCase(),encoding=ENCODINGS[encRaw];
  if(!encoding)throw new Error(where+'“encoding”只能是 utf-8/gbk/ascii，当前为“'+raw.encoding+'”。');
  const hexText=hex?normalizeHex(content):'';if(hex&&!hexOk(hexText))throw new Error(where+'HEX 指令“'+name+'”包含非法字符或字节不完整：'+content);
  const description=String(raw.description??'').trim();if(description.length>500)throw new Error(where+'说明超过 500 个字符。');
  const c={name,content:hex?hexText:content,mode:hex?'hex':'text',encoding,ending};if(description)c.description=description;return c;
}
function toGroups(list){ // [{group,where,raw}] -> [{key,name,commands}]
  const map=new Map();
  for(const {group,where,raw} of list){const key=groupKey(group),c=makeCommand(raw,where);if(!map.has(key))map.set(key,{key,name:groupLabel(key),commands:[]});const g=map.get(key);if(g.commands.length>=MAX_PER_GROUP)throw new Error('分组“'+g.name+'”超过 '+MAX_PER_GROUP+' 条指令。');g.commands.push(c);}
  if(!map.size)throw new Error('文件中没有找到任何指令。');
  for(const g of map.values())if(g.key.startsWith('g:')&&[...g.name].length>32)throw new Error('分组名称“'+g.name.slice(0,20)+'…”超过 32 个字符。');
  return [...map.values()];
}
export function parseCSV(text){
  const rows=[];let row=[],f='',q=false,line=1,start=1;
  for(let i=0;i<text.length;i++){const ch=text[i];
    if(q){if(ch==='"'){if(text[i+1]==='"'){f+='"';i++;}else q=false;}else{if(ch==='\n')line++;f+=ch;}continue;}
    if(ch==='"'&&f===''){q=true;continue;}
    if(ch===','){row.push(f);f='';continue;}
    if(ch==='\r')continue;
    if(ch==='\n'){row.push(f);rows.push({cells:row,line:start});row=[];f='';line++;start=line;continue;}
    f+=ch;}
  if(q)throw new Error('CSV 第 '+start+' 行：引号没有闭合。');
  if(f!==''||row.length){row.push(f);rows.push({cells:row,line:start});}
  return rows.filter(r=>r.cells.some(c=>c.trim()!==''));
}
const HEAD={group:'group','分组':'group','组':'group',name:'name','名称':'name','名字':'name',command:'command','指令':'command','指令内容':'command','内容':'command',content:'command',hex:'hex','十六进制':'hex',newline:'newline','结尾':'newline','结尾符':'newline','换行':'newline',ending:'newline',encoding:'encoding','编码':'encoding',description:'description','说明':'description','备注':'description'};
export function parseCommandsCSV(text){
  const rows=parseCSV(String(text).replace(/^\uFEFF/,''));if(!rows.length)throw new Error('CSV 文件为空。');
  const head=rows[0].cells.map(h=>HEAD[h.trim().toLowerCase()]||HEAD[h.trim()]);
  for(const need of ['name','command'])if(!head.includes(need))throw new Error('CSV 表头缺少“'+(need==='name'?'name/名称':'command/指令')+'”列。第一行应为：group,name,command,hex,newline,encoding,description');
  const unknown=rows[0].cells.filter((h,i)=>!head[i]&&h.trim());if(unknown.length)throw new Error('CSV 表头有无法识别的列：'+unknown.join('、'));
  return toGroups(rows.slice(1).map(r=>{const raw={};head.forEach((k,i)=>{if(k)raw[k]=r.cells[i]??'';});return {group:raw.group,where:'CSV 第 '+r.line+' 行：',raw};}));
}
export function parseCommandsJSON(text){
  let data;try{data=JSON.parse(String(text).replace(/^\uFEFF/,''));}catch(e){throw new Error('JSON 格式错误：'+e.message);}
  const groups=Array.isArray(data)?data:data&&Array.isArray(data.groups)?data.groups:null;
  if(!groups)throw new Error('JSON 中缺少 “groups” 数组。');
  const list=[];groups.forEach((g,gi)=>{if(!g||typeof g!=='object'||!Array.isArray(g.commands))throw new Error('JSON 第 '+(gi+1)+' 个分组缺少 “commands” 数组。');g.commands.forEach((c,ci)=>{if(!c||typeof c!=='object')throw new Error('JSON 分组“'+(g.name||gi+1)+'”第 '+(ci+1)+' 条不是对象。');list.push({group:g.name,where:'分组“'+(g.name||'常用')+'”第 '+(ci+1)+' 条：',raw:c});});});
  return toGroups(list);
}
export function parseCommandsFile(name,text){return /\.csv$/i.test(name)||(!/\.json$/i.test(name)&&!/^\s*[\[{]/.test(text))?parseCommandsCSV(text):parseCommandsJSON(text);}
// Applies parsed groups. mode 'merge': same group+name is updated, others appended. 'replace': all commands replaced.
export function applyImport(commands,groups,mode,newId){
  const next={};for(const k of groupKeys(commands))next[k]=mode==='replace'?[]:(commands[k]||[]).map(c=>({...c}));
  for(const k of Object.keys(BUILTIN_GROUPS))next[k]||=[];
  let added=0,updated=0;
  for(const g of groups){next[g.key]||=[];const list=next[g.key];
    for(const c of g.commands){const i=list.findIndex(x=>x.name===c.name);if(i>=0){list[i]={...c,id:list[i].id};updated++;}else{if(list.length>=MAX_PER_GROUP)throw new Error('分组“'+g.name+'”超过 '+MAX_PER_GROUP+' 条指令。');list.push({...c,id:newId()});added++;}}}
  for(const k of Object.keys(next))if(k.startsWith('g:')&&!next[k].length)delete next[k];
  if(Object.keys(next).length>MAX_GROUPS)throw new Error('分组总数不能超过 '+MAX_GROUPS+' 个。');
  return {commands:next,added,updated};
}
const out=c=>{const o={name:c.name,command:c.content,hex:c.mode==='hex',newline:c.ending,encoding:c.encoding};if(c.description)o.description=c.description;return o;};
export function exportJSON(commands){return JSON.stringify({'格式':'串口助手快捷指令',version:1,'说明':'groups 为分组列表；hex=true 表示按十六进制发送；newline 可选 none/cr/lf/crlf；encoding 可选 utf-8/gbk/ascii；description 可留空。',groups:groupKeys(commands).filter(k=>(commands[k]||[]).length).map(k=>({name:groupLabel(k),commands:commands[k].map(out)}))},null,2)+'\n';}
const cell=v=>{const s=String(v??'');return /[",\r\n]/.test(s)||/^\s|\s$/.test(s)?'"'+s.replace(/"/g,'""')+'"':s;};
export function exportCSV(commands){const rows=[['group','name','command','hex','newline','encoding','description']];for(const k of groupKeys(commands))for(const c of commands[k]||[])rows.push([groupLabel(k),c.name,c.content,c.mode==='hex'?'true':'false',c.ending,c.encoding,c.description||'']);return rows.map(r=>r.map(cell).join(',')).join('\r\n')+'\r\n';}
// Template: the built-in examples plus a custom group showing every column.
export function templateCommands(){const t=(name,content,description='',o={})=>({id:'t',name,content,mode:'text',encoding:'utf-8',ending:'crlf',...(description?{description}:{}),...o});
  return {rtt:[t('线程列表','ps','列出 RT-Thread 线程'),t('定时器列表','list_timer'),t('低功耗状态','pm_dump'),t('系统信息','sysinfo'),t('CPU 使用率','cpu')],
    at:[t('连通测试','AT','模组应答 OK'),t('设备信息','ATI'),t('信号强度','AT+CSQ'),t('注册状态','AT+CREG?'),t('查询卡号','AT+CCID')],
    binary:[t('读取寄存器','01 03 00 00 00 02 C4 0B','Modbus RTU：从站 1 读 2 个保持寄存器',{mode:'hex',ending:'none'})],
    custom:[t('重启','reboot','',{ending:'lf'})],
    'g:电源管理':[t('关机','poweroff','自定义分组示例：分组不存在时自动新建'),t('GBK 中文指令','显示 你好','encoding=gbk 时按 GBK 字节发送',{encoding:'gbk'}),t('无结尾符','status','newline=none：不追加换行',{ending:'none'})]};}

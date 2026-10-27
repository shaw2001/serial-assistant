'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(r=>setTimeout(r,ms));
const io=()=>import('../src/renderer/commands-io.mjs');

test('shipped templates parse (JSON and CSV identical) and contain the built-in examples',async()=>{
  const m=await io();
  const lf=s=>s.replace(/\r\n/g,'\n');
  const json=await fs.readFile('docs/快捷指令导入模板.json','utf8'),csv=await fs.readFile('docs/快捷指令导入模板.csv','utf8');
  assert.equal(lf(json),lf(m.exportJSON(m.templateCommands())));assert.equal(lf(csv),lf('\uFEFF'+m.exportCSV(m.templateCommands())));
  const a=m.parseCommandsFile('t.json',json),b=m.parseCommandsFile('t.csv',csv);
  assert.deepEqual(a,b);
  assert.deepEqual(a.map(g=>g.key),['rtt','at','binary','custom','g:电源管理']);
  assert.equal(a[0].commands[0].content,'ps');assert.equal(a[2].commands[0].mode,'hex');assert.equal(a[2].commands[0].ending,'none');
  assert.equal(a[4].commands[1].encoding,'gbk');
});

test('CSV/JSON parsing: Chinese headers, quotes, hex normalisation and Chinese errors with locations',async()=>{
  const m=await io();
  const g=m.parseCommandsCSV('分组,名称,指令,十六进制,结尾符,说明\n电源,"关机, 立即","poweroff ""now""",否,LF,多行\n"Modbus",读,"0x01,0x03",是,,\n');
  assert.equal(g[0].key,'g:电源');assert.deepEqual(g[0].commands[0],{name:'关机, 立即',content:'poweroff "now"',mode:'text',encoding:'utf-8',ending:'lf',description:'多行'});
  assert.equal(g[1].key,'binary');assert.equal(g[1].commands[0].content,'01 03');assert.equal(g[1].commands[0].ending,'none');
  const bad=[['a.csv','group,name\nx,y',/表头缺少“command\/指令”/],['a.csv','name,command,hex\na,01 0G,true',/CSV 第 2 行：HEX 指令“a”包含非法字符/],['a.csv','name,command,hex\na,b,maybe',/第 2 行：“hex”只能填/],['a.csv','name,command,newline\na,b,x',/“newline”只能是 none\/cr\/lf\/crlf/],['a.csv','name,command\n,b',/缺少“名称”/],['a.csv','name,command,foo\na,b,c',/无法识别的列：foo/],['a.csv','name,command\n"a,b',/引号没有闭合/],['a.json','{x',/JSON 格式错误/],['a.json','{"groups":[{"name":"x"}]}',/缺少 “commands” 数组/],['a.json','{"groups":[]}',/没有找到任何指令/],['a.csv','name,command,encoding\na,b,big5',/“encoding”只能是/]];
  for(const [n,t,re] of bad)assert.throws(()=>m.parseCommandsFile(n,t),re,t);
});

test('merge updates same-named commands and appends others; replace drops everything else',async()=>{
  const m=await io();let n=0;const id=()=>'n'+(++n);
  const base={rtt:[{id:'a',name:'线程列表',content:'ps',mode:'text',encoding:'utf-8',ending:'crlf'}],at:[{id:'b',name:'连通测试',content:'AT',mode:'text',encoding:'utf-8',ending:'crlf'}],binary:[],custom:[]};
  const groups=m.parseCommandsCSV('group,name,command\nRT-Thread,线程列表,list_thread\nRT-Thread,内存,free\n电源,关机,poweroff\n');
  const merged=m.applyImport(base,groups,'merge',id);
  assert.equal(merged.added,2);assert.equal(merged.updated,1);
  assert.deepEqual(merged.commands.rtt.map(c=>[c.id,c.content]),[['a','list_thread'],['n1','free']]);
  assert.equal(merged.commands.at.length,1);assert.equal(merged.commands['g:电源'][0].name,'关机');
  assert.equal(base.rtt[0].content,'ps','input not mutated');
  const replaced=m.applyImport(merged.commands,m.parseCommandsCSV('group,name,command\n常用,x,y\n'),'replace',id);
  assert.deepEqual(Object.keys(replaced.commands).sort(),['at','binary','custom','rtt']);
  assert.equal(replaced.commands.rtt.length,0);assert.equal(replaced.commands.custom[0].name,'x');
});

async function setup(extra={}){
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  const saved=[],errors=[],files=[];let batch,nextFile=null;
  w.serialAPI={initialize:async()=>({version:'0.4.1',config:defaults()}),listPorts:async()=>[{path:'COM5'}],preview:async()=>({bytes:[]}),
    open:async(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:0,tx:0,errors:0,capture:false,periodic:null}),close:async id=>({id,status:'disconnected'}),
    saveConfig:async c=>saved.push(JSON.parse(JSON.stringify(c))),setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{},
    openTextFile:async()=>nextFile,saveTextFile:async(name,text)=>{files.push({name,text});return {canceled:false,path:'C:\\\\x\\\\'+name};},...extra};
  w.addEventListener('error',e=>errors.push(e.error));
  w.eval(bundle);await tick();
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  return {w,q,el,saved,errors,files,batch:b=>batch(b),setFile:f=>nextFile=f};
}

test('sidebar import dialog: preview, merge into a new group, group filter lists it; export and templates save files',async()=>{
  const h=await setup(),{w,q,el,saved,errors,files}=h;
  try{
    q('[data-action="commands-io"]').click();await tick(50);
    assert.equal(el('dialog-title').textContent,'批量导入 / 导出快捷指令');
    h.setFile({name:'bad.csv',text:'name,command,hex\na,ZZ,true'});q('[data-action="pick-commands-file"]').click();await tick(50);
    assert.match(q('[data-dialog-error]').textContent,/HEX 指令“a”/);assert(el('apply-commands-import').disabled);
    h.setFile({name:'my.csv',text:'group,name,command\n电源管理,关机,poweroff\nRT-Thread,内存,free\n'});q('[data-action="pick-commands-file"]').click();await tick(50);
    assert.match(el('import-preview').textContent,/2 个分组 · 2 条指令/);assert.match(el('import-preview').textContent,/电源管理（新分组）/);
    el('apply-commands-import').click();await tick(100);
    const cfg=saved.at(-1);assert.equal(cfg.commands['g:电源管理'][0].content,'poweroff');assert.equal(cfg.commands.rtt.at(-1).name,'内存');assert.equal(cfg.commands.rtt.length,6);
    assert.equal(el('overlay').hidden,true);
    assert([...el('command-group').options].some(o=>o.value==='g:电源管理'&&o.textContent==='电源管理'));
    assert.match(el('commands').textContent,/电源管理/);
    q('[data-action="commands-io"]').click();await tick(50);
    q('[data-action="export-commands-csv"]').click();q('[data-action="template-json"]').click();await tick(50);
    assert.equal(files[0].name,'快捷指令.csv');assert.match(files[0].text,/电源管理,关机,poweroff/);
    assert.equal(files[1].name,'快捷指令导入模板.json');assert.match(files[1].text,/"格式": "串口助手快捷指令"/);
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('collapsed sidebar is a clean rail with an expand button; state persists; overflow badge shows driver overruns',async()=>{
  const h=await setup(),{w,q,el,errors}=h;
  try{
    const btn=q('[data-action="toggle-rail"]');btn.click();
    assert(q('#sui2').classList.contains('su-rail-off'));assert.equal(btn.title,'展开侧栏');assert.equal(btn.getAttribute('aria-expanded'),'false');
    assert.equal(w.localStorage.getItem('sa-rail'),'off');
    const css=await fs.readFile('src/renderer/app.css','utf8');
    assert.match(css,/#sui2\.su-rail-off \.su-commands>:not\(\.su-rh\),#sui2\.su-rail-off \.su-rh>:not\(\.su-rh-title\):not\(\[data-action="toggle-rail"\]\)\{display:none!important\}/);
    assert(!/su-rail-off[^{]*\{font-size:0\}/.test(css),'no more font-size:0 hack');
    btn.click();assert(!q('#sui2').classList.contains('su-rail-off'));assert.equal(btn.title,'收起侧栏');
    // overflow badge
    const port=el('port');port.value='COM5';port.dispatchEvent(new w.Event('change',{bubbles:true}));q('[data-action="connect"]').click();await tick();
    const id=q('.su-tab-wrap.active [data-session]').dataset.session;
    assert.equal(el('overflow').hidden,true);
    h.batch({events:[],states:[{id,path:'COM5',baudRate:115200,status:'connected',rx:10,tx:0,errors:0,overruns:3,frameErrors:1,parityErrors:0,rxQueue:1048576,inQueueMax:4096,capture:false,periodic:null}]});await tick(300);
    assert.equal(el('overflow').hidden,false);assert.equal(el('overflow').textContent,'溢出 3');assert.match(el('overflow').title,/驱动接收缓冲 1024 KiB，峰值占用 4 KiB/);
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

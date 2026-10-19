'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(resolve=>setTimeout(resolve,ms));

async function boot(exportLog){
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  const snapshot=(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:false,periodic:null});
  w.serialAPI={initialize:async()=>({version:'0.2.5',config:defaults()}),listPorts:async()=>[{path:'COM5'}],preview:async()=>({bytes:[]}),
    open:async(id,o)=>snapshot(id,o),close:async()=>({}),remove:async()=>{},send:async()=>({status:'submitted',length:0}),saveConfig:async()=>{},setAlwaysOnTop:async()=>{},onBatch:()=>{},onClosing:()=>{},ack:async()=>{},quit:async()=>{},exportLog};
  const errors=[];w.addEventListener('error',e=>errors.push(e.error));
  w.eval(bundle);await tick();
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  const port=el('port');port.value='COM5';port.dispatchEvent(new w.Event('change',{bubbles:true}));q('[data-action="connect"]').click();await tick();
  return {w,q,el,errors};
}

test('保存日志: toolbar → dialog → native exportLog with valid options, success notice shows path and count',async()=>{
  const calls=[];const {w,q,el,errors}=await boot(async o=>{calls.push(o);return {canceled:false,path:'C:\\logs\\serial-log.txt',count:42};});
  try{
    const button=q('[data-action="export"]');assert(button,'toolbar save-log button exists');
    assert.equal(button.getAttribute('aria-label'),'保存日志');
    button.click();await tick(50);
    assert.equal(el('overlay').hidden,false,'dialog opened');
    assert.match(el('dialog-title').textContent,/保存日志/);
    q('[data-action="download-log"]').click();await tick(100);
    assert.equal(calls.length,1,'exportLog called once');
    const o=calls[0];
    assert.equal(o.scope,'all');assert.equal(o.format,'txt');
    assert(['utf-8','gbk','ascii'].includes(o.encoding),'encoding accepted by the native side: '+o.encoding);
    assert(typeof o.id==='string'&&o.id.startsWith('session-'));
    assert(Array.isArray(o.ids)&&o.ids.length<=10000);
    assert.equal(el('overlay').hidden,true,'dialog closed after saving');
    assert.match(w.document.body.textContent,/日志已保存（42 条）：C:\\logs\\serial-log\.txt/);
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('保存日志: a native failure is shown to the user instead of doing nothing',async()=>{
  const {w,q,errors}=await boot(async()=>{throw new Error('当前会话没有可导出的记录。请先连接并收发数据后再导出。');});
  try{
    q('[data-action="export"]').click();await tick(50);
    q('[data-action="download-log"]').click();await tick(100);
    assert.match(w.document.body.textContent,/当前会话没有可导出的记录/);
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(resolve=>setTimeout(resolve,ms));

async function boot(api={}){
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  let batch;const snapshot=(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:false,periodic:null,history:0,historyTrimmed:0});
  const opened=[];
  w.serialAPI={initialize:async()=>({version:'0.2.8',config:defaults()}),listPorts:async()=>[{path:'COM5'}],preview:async()=>({bytes:[]}),
    open:async(id,o)=>{opened.push(o);return snapshot(id,o);},close:async()=>({}),remove:async()=>{},send:async()=>({status:'submitted',length:0}),saveConfig:async()=>{},setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{},...api};
  const errors=[];w.addEventListener('error',e=>errors.push(e.error));
  w.eval(bundle);await tick();
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  const port=el('port');port.value='COM5';port.dispatchEvent(new w.Event('change',{bubbles:true}));q('[data-action="connect"]').click();await tick();
  const id=q('[data-session]').dataset.session;
  return {w,q,el,errors,id,batch:(...a)=>batch(...a),opened};
}
const rec=(seq,text)=>({seq,time:'2026-10-17T06:00:00.000Z',direction:'RX',dataBase64:Buffer.from(text).toString('base64'),length:Buffer.byteLength(text)});

test('log pane renders the newest 5,000 lines and loads older ones (up to 50,000) on scroll',async()=>{
  const {w,el,errors,id,batch}=await boot();
  try{
    const lines=Array.from({length:7000},(_,i)=>'line '+(i+1)+'\n').join('');
    batch({events:[{id,type:'record',record:rec(1,lines)}],states:[]});await tick(400);
    // Following the tail (自动滚动 on) keeps only the newest 400 rows in the DOM (60 fps pass).
    assert.equal(el('logs').querySelectorAll('.su-log-row').length,400,'tail window while following');
    assert.match(el('records').textContent,/^400 条显示 \/ 700\d 条缓存$/);
    // Ctrl+A expands to the full page (newest 5,000) before selecting.
    w.document.dispatchEvent(new w.KeyboardEvent('keydown',{key:'a',ctrlKey:true,bubbles:true,cancelable:true}));await tick(50);
    const rendered=el('logs').querySelectorAll('.su-log-row').length;
    assert.equal(rendered,5000,'newest 5,000 lines available for selection');
    assert.match(el('records').textContent,/^5000 条显示 \/ 700\d 条缓存$/);
    assert.match(el('logs').lastElementChild.textContent,/line 7000/);
    // Scroll to the top with auto-scroll off: older lines load.
    const auto=el('autoscroll');if(auto&&auto.checked){auto.click();await tick(50);}
    el('screen').scrollTop=0;el('screen').dispatchEvent(new w.Event('scroll'));await tick(50);
    assert(el('logs').querySelectorAll('.su-log-row').length>5000,'scrolling back shows more lines');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('保存日志 works without ever touching 落盘: full-session scope by default, counts from the native history',async()=>{
  const calls=[];
  const {w,q,el,errors,id,batch,opened}=await boot({exportLog:async o=>{calls.push(o);return {canceled:false,path:'C:\\\\logs\\\\all.txt',count:60001};}});
  try{
    assert.equal(opened[0].capture,false,'落盘 is off by default');
    assert.equal(el('capture').checked,false);
    assert.match(q('.su-rec').textContent,/落盘/);
    // The native side reports a history larger than the 50,000-record display cache.
    batch({events:[{id,type:'record',record:rec(1,'hello\n')}],states:[{id,status:'connected',rx:60000,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:10000,capture:false,periodic:null,history:60001,historyTrimmed:0}]});await tick(300);
    q('[data-action="export"]').click();await tick(50);
    assert.equal(q('[data-export-scope]').value,'all');
    assert.match(q('[data-export-scope] option[value="all"]').textContent,/全部已接收数据/);
    assert.match(el('dialog-body').textContent,/60001 条可保存（本次会话全部数据）/);
    q('[data-action="download-log"]').click();await tick(100);
    assert.equal(calls.length,1);assert.equal(calls[0].scope,'all');
    assert.equal(el('capture').checked,false,'saving never turns 落盘 on');
    assert.match(w.document.body.textContent,/日志已保存（60001 条）/);
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

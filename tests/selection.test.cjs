'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(resolve=>setTimeout(resolve,ms));

test('Ctrl+A selects only the log, inputs keep native select-all, copy yields log lines only',async()=>{
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  let batch;const snapshot=(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:true,periodic:null});
  w.serialAPI={initialize:async()=>({version:'0.2.3',config:defaults()}),listPorts:async()=>[{path:'COM5'}],preview:async()=>({bytes:[]}),
    open:async(id,o)=>snapshot(id,o),close:async()=>({}),remove:async()=>{},send:async()=>({status:'submitted',length:0}),saveConfig:async()=>{},setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{}};
  const errors=[];w.addEventListener('error',e=>errors.push(e.error));
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  const press=(node,opts={})=>{const ev=new w.KeyboardEvent('keydown',{key:'a',ctrlKey:true,bubbles:true,cancelable:true,...opts});node.dispatchEvent(ev);return ev;};
  try{
    w.eval(bundle);await tick();
    const port=el('port');port.value='COM5';port.dispatchEvent(new w.Event('change',{bubbles:true}));q('[data-action="connect"]').click();await tick();
    const id=q('[role=tab][aria-selected="true"]')?.dataset.session||q('[data-session]').dataset.session;
    const rec=(n,t)=>({seq:n,time:'2026-10-16T09:00:0'+n+'.000Z',direction:'RX',dataBase64:Buffer.from(t).toString('base64'),length:t.length});
    batch({events:[{id,type:'record',record:rec(1,'hello\n')},{id,type:'record',record:rec(2,'world\n')}],states:[]});await tick(250);
    assert.equal(el('logs').querySelectorAll('.su-log-row').length>=2,true,'log rows rendered');
    // Ctrl+A on the page body selects the log rows, not the chrome.
    w.document.body.focus();const ev=press(w.document.body);assert(ev.defaultPrevented);
    const sel=w.getSelection();assert(!sel.isCollapsed);const range=sel.getRangeAt(0);
    assert(el('logs').contains(range.commonAncestorContainer)||range.commonAncestorContainer===el('logs'));
    assert(!sel.toString().includes('快捷指令'),'chrome labels are not selected');
    // Copy produces one line per log row.
    let copied='';const copy=new w.Event('copy',{bubbles:true,cancelable:true});copy.clipboardData={setData:(type,value)=>{copied=value;}};
    w.document.dispatchEvent(copy);assert(copy.defaultPrevented);
    const lines=copied.split('\r\n');assert(lines.length>=2);assert(lines.some(l=>l.includes('hello')));assert(lines.some(l=>l.includes('world')));assert(!copied.includes('快捷指令'));
    // Inside an input Ctrl+A stays native.
    sel.removeAllRanges();el('input').focus();assert(!press(el('input')).defaultPrevented);
    assert(!press(el('command-filter')).defaultPrevented);
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

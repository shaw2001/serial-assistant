'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs'),{payload}=require('../src/main/payload.cjs');
test('desktop UI routes asynchronous sends, preserves pause, rejects bad HEX, and restores per-device drafts',async()=>{
  const bundle=await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'});
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  let batch,resolveSend;const sent=[],saved=[],states=new Map(),errors=[];
  const snapshot=(id,path,status='connected')=>({id,path,status,rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:true,periodic:null});
  const api={ack:async()=>{},initialize:async()=>({version:'0.1.0',config:defaults()}),listPorts:async()=>[{path:'COM5',manufacturer:'Test'},{path:'COM8',manufacturer:'Test'}],setAlwaysOnTop:async()=>{},preview:async c=>({bytes:[...payload(c)]}),open:async(id,o)=>{const s=snapshot(id,o.path);states.set(id,s);return s;},close:async id=>({...states.get(id),status:'disconnected'}),remove:async()=>{},send:(id,c)=>{sent.push({id,c});return new Promise(resolve=>resolveSend=resolve);},saveConfig:async c=>saved.push(c),onBatch:f=>batch=f,onClosing:()=>{},showLogs:async()=>{},clear:async()=>({cutoff:100}),startPeriodic:async(id,c,interval)=>({...states.get(id),periodic:{count:0,skipped:0,interval}}),stopPeriodic:async()=>{},quit:async()=>{}};
  w.serialAPI=api;w.addEventListener('error',e=>errors.push(e.error));
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]'),settle=()=>new Promise(r=>setTimeout(r,15));
  const change=(node,value,type='change')=>{node.value=value;node.dispatchEvent(new w.Event(type,{bubbles:true}));};
  const click=s=>q(s).click();
  try{
    w.eval(bundle.outputFiles[0].text);await settle();assert.equal(el('version').textContent,'v0.1.0');assert(el('send').disabled);
    change(el('port'),'COM5');change(el('baud'),'custom');change(el('custom-baud'),'250000','input');click('[data-action="connect"]');await settle();assert.equal(el('status').textContent,'已连接 · COM5');
    const id5=q('[role="tab"]').dataset.session;change(el('input'),'one','input');await settle();click('[data-action="send"]');assert.equal(sent[0].id,id5);
    click('[data-action="add-port"]');await settle();change(q('[data-new-path]'),'COM8','input');click('[data-action="create-connect-port"]');await settle();assert.equal(el('status').textContent,'已连接 · COM8');
    const id8=q('[role="tab"][aria-selected="true"]').dataset.session;assert.notEqual(id5,id8);resolveSend({status:'submitted',length:5});await settle();assert.equal(el('input').value,'');
    change(el('send-mode'),'hex');change(el('input'),'0F X0','input');await settle();assert(el('send').disabled);assert(!el('input-error').hidden);change(el('input'),'00 FF','input');await settle();assert(!el('send').disabled);assert.equal(el('preview').textContent,'00 FF 0D 0A');
    const record=(seq,text)=>({seq,time:new Date().toISOString(),direction:'RX',length:Buffer.byteLength(text),dataBase64:Buffer.from(text).toString('base64')});
    batch({events:[{id:id5,type:'record',record:record(1,'port5\n')},{id:id8,type:'record',record:record(1,'ERROR <script>\n')}],states:[states.get(id5),states.get(id8)]});await new Promise(r=>setTimeout(r,120));assert(el('logs').textContent.includes('ERROR <script>'));assert.equal(el('logs').querySelector('script'),null);assert(!el('logs').textContent.includes('port5'));
    change(el('search'),'ERROR','input');assert.equal(el('logs').querySelector('mark').textContent,'ERROR');
    click('[data-action="pause"]');const frozen=el('logs').textContent;assert(el('receive-encoding').disabled);batch({events:[{id:id8,type:'record',record:record(2,'new data\n')}],states:[{...states.get(id8),rx:30}]});await new Promise(r=>setTimeout(r,120));assert.equal(el('logs').textContent,frozen);assert.equal(el('rx').textContent,'30 B');click('[data-action="pause"]');assert(el('logs').textContent.includes('new data'));
    q('[data-session="'+id5+'"]').click();await settle();assert.equal(el('input').value,'one');assert.equal(el('history').options.length,2);assert(el('logs').textContent.includes('port5'));assert.equal(el('custom-baud').value,'250000');
    q('[data-command-send]').click();resolveSend({status:'submitted',length:4});await settle();q('[data-command-send]').click();resolveSend({status:'submitted',length:4});await settle();assert.equal(el('history').options.length,3,'quick-command history is deduplicated');
    await new Promise(r=>setTimeout(r,300));assert(saved.length);assert.equal(saved.at(-1).devices.length,2);assert.equal(saved.at(-1).devices[1].draft.mode,'hex');assert.deepEqual(errors,[]);
  }finally{w.close();}
});

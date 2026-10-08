'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs'),{payload}=require('../src/main/payload.cjs');
const tick=()=>new Promise(resolve=>setTimeout(resolve,20));
let bundle;
async function setup(){
  bundle||=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  const writes=[],saved=[],errors=[],states=new Map();let batch,hold=false,release,fail=false;
  const snapshot=(id,o,status='connected')=>({id,path:o.path,baudRate:o.baudRate,status,rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:true,periodic:null});
  w.serialAPI={initialize:async()=>({version:'0.1.2',config:defaults()}),listPorts:async()=>[{path:'COM5'},{path:'COM8'}],preview:async c=>({bytes:[...payload(c)]}),
    open:async(id,o)=>{const s=snapshot(id,o);states.set(id,s);return s;},close:async id=>{const s={...states.get(id),status:'disconnected'};states.set(id,s);return s;},remove:async()=>{},
    send:async(id,c)=>{if(fail)throw new Error('测试设备写入失败');const bytes=[...payload(c)];writes.push({id,bytes});if(hold)await new Promise(resolve=>release=resolve);return{status:'submitted',length:bytes.length};},
    saveConfig:async c=>saved.push(c),setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{}};
  w.addEventListener('error',e=>errors.push(e.error));
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  const change=(node,value,type='change')=>{node.value=value;node.dispatchEvent(new w.Event(type,{bubbles:true}));};
  const press=(key,opts={},node=el('input'))=>{const event=new w.KeyboardEvent('keydown',{key,bubbles:true,cancelable:true,...opts});node.dispatchEvent(event);return event;};
  w.eval(bundle);await tick();change(el('port'),'COM5');q('[data-action="connect"]').click();await tick();el('input').focus();
  return{w,q,el,change,press,writes,saved,errors,states,batch:b=>batch(b),hold:on=>hold=on,release:()=>{hold=false;release();},fail:on=>fail=on,close:()=>dom.window.close()};
}

test('Enter sends latest input, Shift+Enter keeps native newline, IME confirmation and repeated keys do not send',async()=>{
  const h=await setup(),{w,el,press,change,writes}=h;
  try{
    // Enter can arrive before the asynchronous byte preview has finished.
    change(el('input'),'help','input');assert(press('Enter').defaultPrevented);await tick();
    assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('help\r\n')]);assert.equal(w.document.activeElement,el('input'));assert.equal(el('input').value,'help');
    const count=writes.length;assert(!press('Enter',{shiftKey:true}).defaultPrevented);await tick();assert.equal(writes.length,count);
    // JSDOM has no native textarea editing; supply the value a native newline produces.
    change(el('input'),'line1\nline2','input');press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('line1\nline2\r\n')]);
    change(el('input'),'测试','input');const before=writes.length;
    el('input').dispatchEvent(new w.CompositionEvent('compositionstart',{bubbles:true}));assert(!press('Enter').defaultPrevented);
    el('input').dispatchEvent(new w.CompositionEvent('compositionend',{bubbles:true}));assert(!press('Enter',{isComposing:true}).defaultPrevented);assert(!press('Enter',{keyCode:229}).defaultPrevented);
    await tick();assert.equal(writes.length,before);press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('测试\r\n')]);
    h.hold(true);change(el('input'),'once','input');press('Enter');await tick();const busyCount=writes.length;
    press('Enter');press('Enter',{repeat:true});await tick();assert.equal(writes.length,busyCount);h.release();await tick();
    press('Enter',{repeat:true});await tick();assert.equal(writes.length,busyCount);
    press('Enter',{ctrlKey:true});await tick();assert.equal(writes.length,busyCount+1,'legacy Ctrl+Enter remains compatible');
    change(el('send-mode'),'hex');change(el('input'),'0F X0','input');const badCount=writes.length;press('Enter');await tick();assert.equal(writes.length,badCount,'invalid input is rejected before a wire write');
    assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});

test('RTT Tab submits the prefix once without an ending; repeated Tab and Enter use only the device-owned continuation',async()=>{
  const h=await setup(),{w,el,press,change,writes,q}=h;
  try{
    change(el('input'),'he','input');assert(press('Tab').defaultPrevented);await tick();
    assert.deepEqual(writes.at(-1).bytes,[0x68,0x65,0x09]);assert.equal(el('input').value,'');assert.equal(w.document.activeElement,el('input'));
    assert.equal(el('preview').textContent,'0D 0A');assert.equal(el('send').textContent,'执行');assert(!el('send').disabled);assert(el('send-mode').disabled);assert(el('periodic').disabled);assert(q('[data-command-send]').disabled);
    press('Tab');await tick();assert.deepEqual(writes.at(-1).bytes,[9]);
    const count=writes.length;press('Tab',{repeat:true});await tick();assert.equal(writes.length,count);
    change(el('input'),' --verbose','input');press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[...Buffer.from(' --verbose\r\n')]);
    assert.equal(el('input').value,'');assert(!el('send-mode').disabled);assert(!q('[data-command-send]').disabled);
    change(el('input'),'help\nreboot','input');const beforeMultiline=writes.length;press('Tab');await tick();assert.equal(writes.length,beforeMultiline,'Tab must never execute a pasted multiline command');assert.equal(el('input').value,'help\nreboot');
    change(el('input'),'pm_','input');press('Tab');await tick();assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('pm_\t')]);
    el('send').click();await tick();assert.deepEqual(writes.at(-1).bytes,[13,10],'Send/Execute button also avoids resending the prefix');
    assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});

test('empty Tab, encodings and selected endings work; Shift+Tab, HEX and dialogs retain focus navigation',async()=>{
  const h=await setup(),{w,el,press,change,writes,q}=h;
  try{
    for(const [ending,bytes]of [['cr',[13]],['lf',[10]],['crlf',[13,10]]]){
      change(el('ending'),ending);press('Tab');await tick();assert.deepEqual(writes.at(-1).bytes,[9],'empty Tab asks the shell for its commands');press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,bytes);
    }
    change(el('encoding'),'gbk');change(el('input'),'串口','input');change(el('ending'),'none');press('Tab');await tick();assert.deepEqual(writes.at(-1).bytes,[...payload({content:'串口',mode:'text',encoding:'gbk',ending:'none'}),9]);
    const count=writes.length;press('Enter');await tick();assert.equal(writes.length,count,'none is respected; an empty Enter never invents CRLF');
    assert(!press('Tab',{shiftKey:true}).defaultPrevented);assert(!press('Tab',{},el('ending')).defaultPrevented);assert(!press('Tab',{ctrlKey:true}).defaultPrevented);
    change(el('ending'),'cr');press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[13]);
    change(el('send-mode'),'hex');change(el('input'),'01 02','input');const hexCount=writes.length;assert(!press('Tab').defaultPrevented);await tick();assert.equal(writes.length,hexCount);press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[1,2,13]);
    q('[data-action="help"]').click();assert(!el('overlay').hidden);const before=writes.length;press('Enter');press('Tab');await tick();assert.equal(writes.length,before,'dialog keys never transmit background data');
    const first=el('overlay').querySelector('button'),last=el('overlay').querySelector('[data-el="dialog-body"] button:last-child');last.focus();assert(press('Tab',{},last).defaultPrevented);assert.equal(w.document.activeElement,first);press('Escape',{},first);assert(el('overlay').hidden);
    assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});

test('completion state belongs to its serial session and is reset on disconnection',async()=>{
  const h=await setup(),{el,press,change,writes,q}=h;
  try{
    const id5=q('[role="tab"]').dataset.session;change(el('input'),'pm_','input');press('Tab');await tick();
    q('[data-action="add-port"]').click();await tick();change(q('[data-new-path]'),'COM8','input');q('[data-action="create-connect-port"]').click();await tick();
    const id8=q('[role="tab"][aria-selected="true"]').dataset.session;assert.notEqual(id5,id8);change(el('input'),'version','input');press('Enter');await tick();assert.equal(writes.at(-1).id,id8);assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('version\r\n')]);
    q('[data-session="'+id5+'"]').click();await tick();assert.equal(el('input').value,'');assert.equal(el('send').textContent,'执行');press('Enter');await tick();assert.equal(writes.at(-1).id,id5);assert.deepEqual(writes.at(-1).bytes,[13,10]);
    press('Tab');await tick();h.batch({events:[],states:[{...h.states.get(id5),status:'error'}]});await tick();assert.equal(el('send').textContent,'发送');assert(!el('send-mode').disabled);const before=writes.length;press('Tab');await tick();assert.equal(writes.length,before);assert(el('notice').textContent.includes('连接'));
    assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});

test('failed completion retains the draft and a busy Tab never repeats the prefix or steals focus',async()=>{
  const h=await setup(),{w,el,press,change,writes}=h;
  try{
    change(el('input'),'help','input');h.fail(true);press('Tab');await tick();assert.equal(el('input').value,'help');assert.equal(writes.length,0);assert(!el('send-mode').disabled);assert(!el('input').readOnly);
    h.fail(false);h.hold(true);press('Tab');await tick();assert(el('input').readOnly);assert.equal(w.document.activeElement,el('input'));press('Tab');press('Enter');await tick();assert.equal(writes.length,1);assert.deepEqual(writes[0].bytes,[...Buffer.from('help\t')]);
    h.release();await tick();assert(!el('input').readOnly);assert.equal(el('input').value,'');assert.equal(w.document.activeElement,el('input'));press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[13,10]);
    assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});

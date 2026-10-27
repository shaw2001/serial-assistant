'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs'),{payload}=require('../src/main/payload.cjs');
const tick=()=>new Promise(resolve=>setTimeout(resolve,500));
let bundle;
async function setup(){
  bundle||=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  const writes=[],saved=[],errors=[],states=new Map(),lines=new Map();let seq=0,batch,hold=false,release,fail=false,autoResponses=true;
  const snapshot=(id,o,status='connected')=>({id,path:o.path,baudRate:o.baudRate,status,rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:true,periodic:null});
  w.serialAPI={initialize:async()=>({version:'0.1.2',config:defaults()}),listPorts:async()=>[{path:'COM5'},{path:'COM8'}],preview:async c=>({bytes:[...payload(c)]}),
    open:async(id,o)=>{const s=snapshot(id,o);states.set(id,s);return s;},close:async id=>{const s={...states.get(id),status:'disconnected'};states.set(id,s);return s;},remove:async()=>{},
    send:async(id,c)=>{if(fail)throw new Error('测试设备写入失败');const bytes=[...payload(c)];writes.push({id,bytes});if(hold)await new Promise(resolve=>release=resolve);let line=lines.get(id)||'';for(const b of bytes){if(b===8)line=line.slice(0,-1);else if(b===13||b===10)line='';else if(b!==9)line+=String.fromCharCode(b);}lines.set(id,line);if(autoResponses&&bytes.at(-1)===9)setTimeout(()=>{return batch({events:[{id,type:'record',record:{seq:++seq,time:new Date().toISOString(),direction:'RX',dataBase64:Buffer.concat([Buffer.from('\r\nmsh />'),Buffer.from(line,'latin1')]).toString('base64'),length:line.length+8}}],states:[]});},0);return{status:'submitted',length:bytes.length};},
    saveConfig:async c=>saved.push(c),setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{}};
  w.addEventListener('error',e=>errors.push(e.error));
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  const change=(node,value,type='change')=>{node.value=value;node.dispatchEvent(new w.Event(type,{bubbles:true}));};
  const press=(key,opts={},node=el('input'))=>{const event=new w.KeyboardEvent('keydown',{key,bubbles:true,cancelable:true,...opts});node.dispatchEvent(event);return event;};
  w.eval(bundle);await tick();change(el('port'),'COM5');q('[data-action="connect"]').click();await tick();el('input').focus();
  return{w,q,el,change,press,writes,saved,errors,states,batch:b=>batch(b),auto:on=>autoResponses=on,hold:on=>hold=on,release:()=>{hold=false;release();},fail:on=>fail=on,close:()=>dom.window.close()};
}

test('Enter sends latest input, Shift+Enter keeps native newline, IME confirmation and repeated keys do not send',async()=>{
  const h=await setup(),{w,el,press,change,writes}=h;
  try{
    // Enter can arrive before the asynchronous byte preview has finished.
    change(el('input'),'help','input');assert(press('Enter').defaultPrevented);await tick();
    assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('help\r\n')]);assert.equal(w.document.activeElement,el('input'));assert.equal(el('input').value,'');
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
    change(el('input'),'once','input');press('Enter',{ctrlKey:true});await tick();assert.equal(writes.length,busyCount+1,'legacy Ctrl+Enter remains compatible');
    change(el('send-mode'),'hex');change(el('input'),'0F X0','input');const badCount=writes.length;press('Enter');await tick();assert.equal(writes.length,badCount,'invalid input is rejected before a wire write');
    assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});

test('normal sends clear submitted drafts, retain failed drafts and never erase edits or unrelated quick-command input',async()=>{
  const h=await setup(),{el,press,change,writes,q}=h;
  try{
    change(el('input'),'help','input');press('Enter');await tick();assert.equal(el('input').value,'');assert(el('history').options.length>1);
    press('Enter');await tick();assert.equal(writes.length,1,'an empty field cannot resend help');
    change(el('input'),'ps','input');h.fail(true);press('Enter');await tick();assert.equal(el('input').value,'ps');h.fail(false);
    h.hold(true);press('Enter');await tick();change(el('input'),'version','input');h.release();await tick();assert.equal(el('input').value,'version');
    q('[data-command-send]').click();await tick();assert.equal(el('input').value,'version','quick commands preserve the local draft');
    change(el('send-mode'),'hex');change(el('input'),'01 02','input');press('Enter');await tick();assert.equal(el('input').value,'');
    assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});

test('补全功能已移除: Tab 不写串口、不弹补全框；help+CRLF 恰好 6 B；发送成功不弹提示',async()=>{
  const h=await setup(),{el,q,press,change,writes}=h;
  try{
    assert.equal(q('[data-el="completion"]'),null);assert.equal(q('[data-el="device-complete"]'),null);
    assert(!/补全/.test(el('input').placeholder));
    change(el('input'),'help','input');press('Enter');await tick();
    assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('help\r\n')]);
    assert(el('notice').hidden||!/已提交驱动/.test(el('notice').textContent));
    const n=writes.length;change(el('input'),'he','input');
    assert.equal(press('Tab').defaultPrevented,false,'Tab keeps default focus behaviour');await tick();
    assert.equal(writes.length,n,'Tab writes nothing');assert.equal(el('input').value,'he');
    press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('he\r\n')]);
    // Rarely used controls live in the “更多” menu; Escape closes it.
    assert(el('more-pop').hidden);q('[data-action="more"]').click();assert(!el('more-pop').hidden);
    assert(el('more-pop').querySelector('[data-action="manual-port"]')&&el('more-pop').querySelector('[data-el="always-top"]')&&el('more-pop').querySelector('[data-action="help"]'));
    press('Escape');assert(el('more-pop').hidden);
  }finally{h.close();}
});

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

test('RTT Tab submits the prefix once without an ending; repeated Tab and Enter use only the device-owned continuation',async()=>{
  const h=await setup(),{w,el,press,change,writes,q}=h;
  try{
    change(el('input'),'he','input');assert(press('Tab').defaultPrevented);await tick();
    assert.deepEqual(writes.at(-1).bytes,[0x68,0x65,0x09]);assert.equal(el('input').value,'he');assert.equal(w.document.activeElement,el('input'));
    assert.equal(el('preview').textContent,'0D 0A');assert.equal(el('send').textContent,'执行');assert(!el('send').disabled);assert(el('send-mode').disabled);assert(el('periodic').disabled);assert(q('[data-command-send]').disabled);
    press('Tab');await tick();assert.deepEqual(writes.at(-1).bytes,[9]);
    const count=writes.length;press('Tab',{repeat:true});await tick();assert.equal(writes.length,count);
    change(el('input'),'he --verbose','input');press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[...Buffer.from(' --verbose\r\n')]);
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
    q('[data-session="'+id5+'"]').click();await tick();assert.equal(el('input').value,'pm_');assert.equal(el('send').textContent,'执行');press('Enter');await tick();assert.equal(writes.at(-1).id,id5);assert.deepEqual(writes.at(-1).bytes,[13,10]);
    press('Tab');await tick();h.batch({events:[],states:[{...h.states.get(id5),status:'error'}]});await tick();assert.equal(el('send').textContent,'发送');assert(!el('send-mode').disabled);const before=writes.length;press('Tab');await tick();assert.equal(writes.length,before);assert(el('notice').textContent.includes('连接'));
    assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});

test('failed completion retains the draft and a busy Tab never repeats the prefix or steals focus',async()=>{
  const h=await setup(),{w,el,press,change,writes}=h;
  try{
    change(el('input'),'help','input');h.fail(true);press('Tab');await tick();assert.equal(el('input').value,'help');assert.equal(writes.length,0);assert(!el('send-mode').disabled);assert(!el('input').readOnly);
    h.fail(false);h.hold(true);press('Tab');await tick();assert(el('input').readOnly);assert.equal(w.document.activeElement,el('input'));press('Tab');press('Enter');await tick();assert.equal(writes.length,1);assert.deepEqual(writes[0].bytes,[...Buffer.from('help\t')]);
    h.release();await tick();assert(!el('input').readOnly);assert.equal(el('input').value,'help');assert.equal(w.document.activeElement,el('input'));press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[13,10]);
    assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});


test('device completion populates field, deduplicates shared-prefix choices and synchronizes edits without duplicate commands',async()=>{
  const h=await setup(),{el,press,change,writes,q}=h;
  try{
    change(el('input'),'pm_','input');press('Tab');await tick();
    const id=q('[role="tab"]').dataset.session;
    const rx=text=>h.batch({events:[{id,type:'record',record:{seq:100,time:new Date().toISOString(),direction:'RX',dataBase64:Buffer.from(text).toString('base64'),length:Buffer.byteLength(text)}}],states:[]});
    // A second Tab captures a fresh, fragmented device reply.
    h.auto(false);
    press('Tab');await new Promise(r=>setTimeout(r,30));
    rx('\r\npm_dump - power status\r\npm_test - test\r\npm_dump - duplicate\r\nmsh /');rx('>pm_');await tick();
    assert.equal(el('input').value,'pm_');assert.equal(q('[data-el="completion-candidates"]').children.length,2);
    q('[data-candidate="0"]').click();await tick();assert.equal(el('input').value,'pm_dump');
    press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('dump\r\n')]);
    change(el('input'),'he','input');press('Tab');await new Promise(r=>setTimeout(r,30));rx('\r\nhelp - help\r\nmsh />help');await tick();
    assert.equal(el('input').value,'help');change(el('input'),'heap','input');press('Enter');await tick();
    assert.deepEqual(writes.at(-1).bytes,[8,8,...Buffer.from('ap\r\n')]);assert.deepEqual(h.errors,[]);
  }finally{h.close();}
});

test('unrecognized logs never overwrite input or execute a pending completion; in-flight edits survive the prompt redraw',async()=>{
  const h=await setup(),{el,press,change,writes,q}=h;
  try{
    h.auto(false);change(el('input'),'he','input');press('Tab');await tick();
    change(el('input'),'he --verbose','input');
    const id=q('[role="tab"]').dataset.session;
    const rx=text=>h.batch({events:[{id,type:'record',record:{seq:200,time:new Date().toISOString(),direction:'RX',dataBase64:Buffer.from(text).toString('base64'),length:Buffer.byteLength(text)}}],states:[]});
    rx('sensor: 24\r\n');await tick();const count=writes.length;press('Enter');press('Tab');await tick();assert.equal(writes.length,count);assert.equal(el('input').value,'he --verbose');
    rx('help - help\r\n\x1b[32mmsh />\x1b[0mhelp');await tick();assert.equal(el('input').value,'help --verbose');
    press('Enter');await tick();assert.deepEqual(writes.at(-1).bytes,[...Buffer.from(' --verbose\r\n')]);
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

test('completion timeout and Escape block all sends until explicit device-input confirmation',async()=>{
 const h=await setup(),{el,press,change,writes,q}=h;
 try{
  h.auto(false);change(el('input'),'help','input');press('Tab');await new Promise(r=>setTimeout(r,3200));const count=writes.length;
  assert(el('send').disabled);press('Enter');press('Tab');await tick();assert.equal(writes.length,count,'timeout must not duplicate the device prefix');
  q('[data-command-send]').click();assert.equal(writes.length,count);
  q('[data-action="recover-input"]').click();q('[data-action="confirm-input-cleared"]').click();await tick();press('Enter');await tick();assert.equal(writes.length,count+1);
  change(el('input'),'he','input');press('Tab');await tick();press('Escape');press('Enter');await tick();assert.equal(writes.length,count+2,'Escape must not silently re-enable sending');
  assert(h.saved.at(-1).devices[0].inputUncertain);assert.equal(h.saved.at(-1).devices[0].draft.content,'');assert.equal(h.saved.at(-1).devices[0].history.length,0);
 }finally{h.close()}
});

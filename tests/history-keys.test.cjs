'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs'),{payload}=require('../src/main/payload.cjs');
const tick=(ms=250)=>new Promise(resolve=>setTimeout(resolve,ms));
let bundle;
async function setup(config=defaults(),connect=true){
  bundle||=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  const writes=[],errors=[];
  const snapshot=(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:false,periodic:null});
  w.serialAPI={initialize:async()=>({version:'0.3.3',config}),listPorts:async()=>[{path:'COM5'}],preview:async c=>({bytes:[...payload(c)]}),
    open:async(id,o)=>snapshot(id,o),close:async id=>({id,status:'disconnected'}),remove:async()=>{},
    send:async(id,c)=>{writes.push({id,c:JSON.parse(JSON.stringify(c)),bytes:[...payload(c)]});return {status:'submitted',length:payload(c).length};},
    startPeriodic:async(id,p,t)=>({id,path:'COM5',status:'connected',rx:0,tx:0,errors:0,periodic:{interval:t,count:0,skipped:0}}),stopPeriodic:async()=>{},
    saveConfig:async()=>{},setAlwaysOnTop:async()=>{},onBatch:()=>{},onClosing:()=>{},ack:async()=>{},quit:async()=>{}};
  w.addEventListener('error',e=>errors.push(e.error));
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  const type=v=>{el('input').value=v;el('input').selectionStart=el('input').selectionEnd=v.length;el('input').dispatchEvent(new w.Event('input',{bubbles:true}));};
  const press=(key,opts={})=>{const ev=new w.KeyboardEvent('keydown',{key,bubbles:true,cancelable:true,...opts});el('input').dispatchEvent(ev);return ev;};
  w.eval(bundle);await tick();
  if(connect){const port=el('port');if(port.value!=='COM5'&&!q('#sui2').classList.contains('su-kind-rtt')){port.value='COM5';port.dispatchEvent(new w.Event('change',{bubbles:true}));}q('[data-action="connect"]').click();await tick();}
  el('input').focus();
  return {w,q,el,type,press,writes,errors};
}
async function sendAll(h,list){for(const v of list){h.type(v);await tick(60);h.press('Enter');await tick(120);}}

test('Up/Down walk the send history like a terminal: newest first, stop at oldest, restore the in-progress draft',async()=>{
  const h=await setup(),{el,press,type,writes,errors,w}=h;
  try{
    await sendAll(h,['ps','list_timer','sysinfo']);
    assert.deepEqual(writes.map(x=>x.c.content),['ps','list_timer','sysinfo']);
    assert.equal(el('input').value,'');
    type('free');await tick(60);
    assert(press('ArrowUp').defaultPrevented);assert.equal(el('input').value,'sysinfo');
    assert.equal(el('input').selectionStart,'sysinfo'.length,'caret at end of the recalled command');
    press('ArrowUp');assert.equal(el('input').value,'list_timer');
    press('ArrowUp');assert.equal(el('input').value,'ps');
    assert(press('ArrowUp').defaultPrevented);assert.equal(el('input').value,'ps','stops at the oldest, no wrap');
    press('ArrowDown');assert.equal(el('input').value,'list_timer');
    press('ArrowDown');assert.equal(el('input').value,'sysinfo');
    assert(press('ArrowDown').defaultPrevented);assert.equal(el('input').value,'free','past the newest the typed draft comes back');
    assert.equal(press('ArrowDown').defaultPrevented,false,'Down on the live line is left alone');
    assert.equal(el('input').value,'free');
    // Recall then Enter sends the recalled command (and it moves to the top).
    press('ArrowUp');press('ArrowUp');assert.equal(el('input').value,'list_timer');
    await tick(60);press('Enter');await tick(150);
    assert.equal(writes.at(-1).c.content,'list_timer');assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('list_timer\r\n')]);
    press('ArrowUp');assert.equal(el('input').value,'list_timer','most recent is now list_timer');
    press('ArrowUp');assert.equal(el('input').value,'sysinfo');
    // Editing a recalled entry makes it the live line; Up then starts again from the newest.
    type('sysinfo -v');press('ArrowUp');assert.equal(el('input').value,'list_timer');
    press('ArrowDown');assert.equal(el('input').value,'sysinfo -v');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('HEX and text entries come back with their own mode, encoding and ending; the draft mode is restored',async()=>{
  const h=await setup(),{el,press,type,writes,w,errors}=h;
  try{
    await sendAll(h,['AT']);
    const mode=el('send-mode');mode.value='hex';mode.dispatchEvent(new w.Event('change',{bubbles:true}));
    const ending=el('ending');ending.value='none';ending.dispatchEvent(new w.Event('change',{bubbles:true}));
    await sendAll(h,['01 03 00 00']);
    assert.deepEqual(writes.at(-1).bytes,[1,3,0,0]);
    // Back to a text draft.
    mode.value='text';mode.dispatchEvent(new w.Event('change',{bubbles:true}));ending.value='crlf';ending.dispatchEvent(new w.Event('change',{bubbles:true}));
    type('draft');
    press('ArrowUp');assert.equal(el('input').value,'01 03 00 00');assert.equal(el('send-mode').value,'hex');assert.equal(el('ending').value,'none');
    press('ArrowUp');assert.equal(el('input').value,'AT');assert.equal(el('send-mode').value,'text');assert.equal(el('ending').value,'crlf');
    press('ArrowDown');press('ArrowDown');assert.equal(el('input').value,'draft');assert.equal(el('send-mode').value,'text');
    // The 发送历史 menu is the same list.
    assert.deepEqual([...el('history').options].slice(1).map(o=>o.textContent),['01 03 00 00','AT']);
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('multi-line drafts keep normal caret movement; Shift+Enter, Tab completion and periodic send are not hijacked',async()=>{
  const h=await setup(),{el,press,type,w,errors,writes}=h;
  try{
    await sendAll(h,['ps']);
    const x=el('input');
    type('line1\nline2');
    // Caret on the last line: Up is a normal caret move, not history.
    x.selectionStart=x.selectionEnd=x.value.length;
    assert.equal(press('ArrowUp').defaultPrevented,false);assert.equal(x.value,'line1\nline2');
    // Caret on the first line: Up recalls; Down from the recalled entry restores the multi-line draft.
    x.selectionStart=x.selectionEnd=2;
    assert(press('ArrowUp').defaultPrevented);assert.equal(x.value,'ps');
    press('ArrowDown');assert.equal(x.value,'line1\nline2');
    // Down with the caret on the first line of a multi-line draft is a caret move.
    x.selectionStart=x.selectionEnd=1;assert.equal(press('ArrowDown').defaultPrevented,false);
    // A selection is never replaced by history.
    x.selectionStart=0;x.selectionEnd=3;assert.equal(press('ArrowUp').defaultPrevented,false);
    // Modified arrows (Shift/Ctrl/Alt) keep their native meaning.
    x.selectionStart=x.selectionEnd=0;
    for(const m of [{shiftKey:true},{ctrlKey:true},{altKey:true}])assert.equal(press('ArrowUp',m).defaultPrevented,false);
    // Shift+Enter is still a native newline, not a send.
    const before=writes.length;assert.equal(press('Enter',{shiftKey:true}).defaultPrevented,false);await tick(100);assert.equal(writes.length,before);
    // Tab still asks the device to complete.
    type('p');await tick(60);assert(press('Tab').defaultPrevented);await tick(150);
    assert.deepEqual(writes.at(-1).bytes,[...Buffer.from('p\t')]);
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('history is ignored while periodic send runs (input is locked)',async()=>{
  const h=await setup(),{el,press,type,q,w,errors}=h;
  try{
    await sendAll(h,['ps']);type('cpu');await tick(80);
    q('[data-action="periodic"]').click();await tick(100);
    assert.equal(el('input').disabled,true);
    assert.equal(press('ArrowUp').defaultPrevented,false);assert.equal(el('input').value,'cpu');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('RTT tabs use the same send box history (persisted per session)',async()=>{
  const config=defaults();
  config.ui={privacyVersion:1,rememberInput:true};
  config.devices=[{id:'session-rtt1',path:'RTT · nRF54L15_M33',baud:'115200',receiveEncoding:'utf-8',params:{data:8,parity:'none',stop:1,flow:'none'},draft:{content:'',mode:'text',encoding:'utf-8',ending:'crlf'},history:[{content:'kernel uptime',mode:'text',encoding:'utf-8',ending:'crlf'},{content:'help',mode:'text',encoding:'utf-8',ending:'crlf'}],kind:'rtt',rtt:{dir:'C:\\ncs\\SEGGER\\JLink_V918',device:'nRF54L15_M33',iface:'SWD',speed:4000,channel:0,addrMode:'auto',backend:'commander',telnetPort:19021}}];
  const h=await setup(config),{el,press,q,w,writes,errors}=h;
  try{
    assert(q('#sui2').classList.contains('su-kind-rtt'));
    press('ArrowUp');assert.equal(el('input').value,'kernel uptime');
    press('ArrowUp');assert.equal(el('input').value,'help');
    await tick(80);press('Enter');await tick(150);
    assert.equal(writes.at(-1).c.content,'help');assert.equal(writes.at(-1).id,'session-rtt1');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

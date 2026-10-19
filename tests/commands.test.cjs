'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(resolve=>setTimeout(resolve,ms));

test('quick-command group dropdown filters the list and combines with the text filter',async()=>{
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  const saved=[];
  w.serialAPI={initialize:async()=>({version:'0.2.3',config:defaults()}),listPorts:async()=>[],preview:async()=>({bytes:[]}),saveConfig:async c=>saved.push(c),setAlwaysOnTop:async()=>{},onBatch:()=>{},onClosing:()=>{},ack:async()=>{},quit:async()=>{}};
  const errors=[];w.addEventListener('error',e=>errors.push(e.error));
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  const set=(node,value,type)=>{node.value=value;node.dispatchEvent(new w.Event(type,{bubbles:true}));};
  const groups=()=>[...el('commands').querySelectorAll('.su-gh')].map(h=>h.firstChild.textContent);
  const names=()=>[...el('commands').querySelectorAll('.su-command-name')].map(n=>n.textContent);
  try{
    w.eval(bundle);await tick();
    assert.equal(el('command-group').value,'all');
    assert.deepEqual(groups(),['RT-Thread','AT 指令','Modbus']);
    set(el('command-group'),'binary','change');
    assert.deepEqual(groups(),['Modbus'],'choosing a group shows only that group');
    set(el('command-group'),'at','change');
    assert.deepEqual(groups(),['AT 指令']);
    set(el('command-filter'),'csq','input');
    assert.deepEqual(names(),['信号强度'],'text filter matches command content within the group');
    set(el('command-filter'),'列表','input');
    assert.equal(names().length,0,'RT-Thread names are not shown while AT is selected');
    assert(el('commands').textContent.includes('没有匹配的指令'));
    set(el('command-group'),'all','change');
    assert(names().includes('线程列表')&&names().includes('定时器列表'),'全部 + text filter searches every group by name');
    set(el('command-filter'),'','input');
    await tick(700);
    const last=saved.at(-1);assert(last,'config saved');
    assert(last.devices.every(d=>['rtt','at','binary','custom'].includes(d.group)),'persisted group stays a real group, never "all"');
    assert.equal(last.devices[0].group,'at','last chosen group remains the target for new commands');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

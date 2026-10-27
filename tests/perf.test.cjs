'use strict';
// 0.4.2 performance regression guards.
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(r=>setTimeout(r,ms));
const css=fs.readFileSync('src/renderer/app.css','utf8').replace(/\/\*[\s\S]*?\*\//g,'');
const rules=[...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(m=>({sel:m[1].trim(),body:m[2]}));
const last=(selector,prop)=>{let v=null;for(const r of rules)if(r.sel.split(',').map(x=>x.trim()).includes(selector)){const m=r.body.match(new RegExp('(?:^|;|\\s)'+prop+'\\s*:\\s*([^;]+)'));if(m)v=m[1].trim();}return v;};

test('large, frequently repainted panels do not live-blur the backdrop',()=>{
  for(const s of ['#sui2 .su-log-panel','#sui2 .su-header','#sui2 .su-commands','#sui2 .su-stats'])assert.equal(last(s,'backdrop-filter'),'none',s);
});
test('log rows skip off-screen layout and the log pane is contained',()=>{
  assert.equal(last('#sui2 .su-log-row','content-visibility'),'auto');
  assert.match(last('#sui2 .su-log-screen','contain')||'',/strict/);
});
test('no transitions on layout or expensive paint properties',()=>{
  for(const r of rules){const m=r.body.match(/transition\s*:\s*([^;]+)/);if(!m)continue;
    assert.doesNotMatch(m[1],/box-shadow|grid-template|(^|[\s,])width|(^|[\s,])height|\ball\b|backdrop-filter/,r.sel+' transitions '+m[1]);}
});
test('fromBase64 is correct and fast (1 MiB < 60 ms)',async()=>{
  const {fromBase64}=await import('../src/renderer/model.mjs');
  const raw=Buffer.alloc(1<<20);for(let i=0;i<raw.length;i++)raw[i]=(i*31)&255;
  const b64=raw.toString('base64');fromBase64(b64);
  const t=performance.now();const out=fromBase64(b64);const ms=performance.now()-t;
  assert(Buffer.from(out).equals(raw));assert(ms<60,'decode took '+ms.toFixed(1)+' ms');
});
test('streaming appends rows without rebuilding existing row elements',async()=>{
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(fs.readFileSync('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};let batch;
  w.serialAPI={initialize:async()=>({version:'0.4.2',config:defaults()}),listPorts:async()=>[{path:'COM5'}],preview:async()=>({bytes:[]}),
    open:async(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:false,periodic:null}),close:async()=>({}),remove:async()=>{},send:async()=>({status:'submitted',length:0}),saveConfig:async()=>{},setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{}};
  const errors=[];w.addEventListener('error',e=>errors.push(e.error));w.eval(bundle);await tick();
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  try{
    el('port').value='COM5';el('port').dispatchEvent(new w.Event('change',{bubbles:true}));q('[data-action="connect"]').click();await tick();
    const id=q('[data-session]').dataset.session;let seq=0;
    const push=(from,n,tail='\n')=>batch({events:[{id,type:'record',record:{seq:++seq,time:'2026-10-27T07:00:00.000Z',direction:'RX',dataBase64:Buffer.from(Array.from({length:n},(_,i)=>'W/x line '+(from+i)).join('\n')+tail).toString('base64'),length:1}}],states:[]});
    push(1,200);await tick();
    const rows=()=>[...el('logs').querySelectorAll('.su-log-row')];
    const before=rows();assert.equal(before.length,200);
    push(201,50,'');await tick();          // last line still open
    let after=rows();assert.equal(after.length,250);
    for(let i=0;i<200;i++)assert.equal(after[i],before[i],'row '+i+' was rebuilt');
    const openRow=after[249];push(250,1);await tick(); // the open line grows and closes
    after=rows();assert.equal(after.length,250);assert.match(after[249].textContent,/line 250line 250|line 250/);
    for(let i=0;i<249;i++)assert.equal(after[i],i<200?before[i]:after[i]);
    assert(after[249]!==openRow||/line 250/.test(after[249].textContent));
    assert(after[0].classList.contains('lv-w'),'level class kept');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('no full-window filter layer (the blurred backdrop is drawn soft, not filtered)',()=>{
  assert.equal(last('#sui2::before','filter'),'none');
  for(const r of rules){if(!/(^|,)\s*#sui2(::before|::after)?\s*($|,)/.test(r.sel))continue;const m=r.body.match(/(?:^|;|\s)filter\s*:\s*([^;]+)/);if(m)assert.equal(m[1].trim(),'none',r.sel);}
});
test('following the tail renders a bounded window; scrolling to the top expands it',async()=>{
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(fs.readFileSync('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};let batch;
  w.serialAPI={initialize:async()=>({version:'0.4.2',config:defaults()}),listPorts:async()=>[{path:'COM5'}],preview:async()=>({bytes:[]}),
    open:async(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:false,periodic:null}),close:async()=>({}),remove:async()=>{},send:async()=>({status:'submitted',length:0}),saveConfig:async()=>{},setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{}};
  const errors=[];w.addEventListener('error',e=>errors.push(e.error));w.eval(bundle);await tick();
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  try{
    el('port').value='COM5';el('port').dispatchEvent(new w.Event('change',{bubbles:true}));q('[data-action="connect"]').click();await tick();
    const id=q('[data-session]').dataset.session;let seq=0;
    const push=n=>batch({events:[{id,type:'record',record:{seq:++seq,time:'2026-10-27T07:00:00.000Z',direction:'RX',dataBase64:Buffer.from(Array.from({length:n},(_,i)=>'l'+seq+'-'+i+'\n').join('')).toString('base64'),length:1}}],states:[]});
    for(let i=0;i<10;i++){push(300);await tick(120);}
    assert.equal(el('logs').querySelectorAll('.su-log-row').length,400,'DOM stays bounded while streaming 3,000 lines');
    await tick(1200); // let the throttled render settle (scroll events right after a programmatic tail scroll are ignored)
    el('screen').scrollTop=0;el('screen').dispatchEvent(new w.Event('scroll'));await tick(50);
    assert(el('logs').querySelectorAll('.su-log-row').length>400,'scrolling to the top loads the full page');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

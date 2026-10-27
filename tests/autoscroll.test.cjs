'use strict';
// 0.4.2: with 自动滚动 off, new data must still render (0.4.1 skipped rendering
// entirely), the viewport must stay put, and a “有 N 条新数据 ↓” pill jumps to the end.
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(resolve=>setTimeout(resolve,ms));
const ROW=20,VIEW=200;
async function boot(){
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  let batch;const snapshot=(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:false,periodic:null,history:0,historyTrimmed:0});
  w.serialAPI={initialize:async()=>({version:'0.4.2',config:defaults()}),listPorts:async()=>[{path:'COM5'}],preview:async()=>({bytes:[]}),
    open:async(id,o)=>snapshot(id,o),close:async()=>({}),remove:async()=>{},send:async()=>({status:'submitted',length:0}),saveConfig:async()=>{},setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{}};
  const errors=[];w.addEventListener('error',e=>errors.push(e.error));
  w.eval(bundle);await tick();
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  // Simple layout model: every log row is ROW px tall, the screen shows VIEW px.
  const screen=el('screen'),logs=el('logs');let top=0;
  const rowsN=()=>logs.querySelectorAll('.su-log-row').length;
  Object.defineProperty(screen,'scrollHeight',{get:()=>rowsN()*ROW});
  Object.defineProperty(screen,'clientHeight',{get:()=>VIEW});
  Object.defineProperty(screen,'scrollTop',{get:()=>top,set:v=>{top=Math.max(0,Math.min(v,Math.max(0,rowsN()*ROW-VIEW)));}});
  let cache={};const orig=w.HTMLElement.prototype.getBoundingClientRect;
  w.HTMLElement.prototype.getBoundingClientRect=function(){
    if(this===screen)return{top:0,bottom:VIEW,left:0,right:800,height:VIEW,width:800};
    if(this.classList.contains('su-log-row')){const k=logs.firstElementChild;if(cache.key!==k||cache.n!==logs.children.length){cache={key:k,n:logs.children.length,map:new Map([...logs.children].map((e,i)=>[e,i]))};}const i=cache.map.get(this);return{top:i*ROW-top,bottom:(i+1)*ROW-top,left:0,right:800,height:ROW,width:800};}
    return orig.call(this);
  };
  const port=el('port');port.value='COM5';port.dispatchEvent(new w.Event('change',{bubbles:true}));q('[data-action="connect"]').click();await tick();
  const id=q('[data-session]').dataset.session;
  return {w,q,el,errors,id,batch:(...a)=>batch(...a),rowsN,screen,setTop:v=>{screen.scrollTop=v;screen.dispatchEvent(new w.Event('scroll'));}};
}
let seq=0;
const lines=(from,n)=>({seq:++seq,time:'2026-10-27T06:00:00.000Z',direction:'RX',dataBase64:Buffer.from(Array.from({length:n},(_,i)=>'line '+(from+i)+'\n').join('')).toString('base64'),length:1});

test('自动滚动关闭：新数据继续显示，视口不动，出现“新数据”提示；滚到底或点击提示后消失',async()=>{
  const h=await boot(),{el,id,batch,rowsN,screen}=h;
  try{
    batch({events:[{id,type:'record',record:lines(1,50)}],states:[]});await tick();
    assert.equal(rowsN(),50);assert.equal(screen.scrollTop,50*ROW-VIEW,'auto-scroll on follows the tail');
    el('autoscroll').click();await tick(50);assert.equal(el('autoscroll').checked,false);
    h.setTop(300);await tick(50);
    batch({events:[{id,type:'record',record:lines(51,30)}],states:[]});await tick();
    assert.equal(rowsN(),80,'new lines are rendered while auto-scroll is off');
    assert.match(el('logs').lastElementChild.textContent,/line 80/);
    assert.equal(screen.scrollTop,300,'viewport stays where the user is');
    assert.equal(screen.scrollHeight,80*ROW,'scroll height grows');
    assert.equal(el('new-data').hidden,false);assert.match(el('new-data').textContent,/有 30 条新数据/);
    batch({events:[{id,type:'record',record:lines(81,5)}],states:[]});await tick();
    assert.match(el('new-data').textContent,/有 35 条新数据/);assert.equal(screen.scrollTop,300);
    // Scrolling to the bottom by hand clears the pill; the view still does not follow.
    h.setTop(screen.scrollHeight);await tick(50);assert.equal(el('new-data').hidden,true);
    const bottom=screen.scrollTop;
    batch({events:[{id,type:'record',record:lines(86,3)}],states:[]});await tick();
    assert.equal(rowsN(),88);assert.equal(screen.scrollTop,bottom,'auto-scroll off never moves the view');
    assert.match(el('new-data').textContent,/有 3 条新数据/);
    // Clicking the pill jumps to the newest line.
    el('new-data').click();await tick(50);
    assert.equal(screen.scrollTop,88*ROW-VIEW);assert.equal(el('new-data').hidden,true);
    // Turning auto-scroll back on follows the tail again.
    el('autoscroll').click();await tick(50);
    batch({events:[{id,type:'record',record:lines(89,4)}],states:[]});await tick();
    assert.equal(screen.scrollTop,92*ROW-VIEW);assert.equal(el('new-data').hidden,true);
    assert.deepEqual(h.errors,[]);
  }finally{h.w.close();}
});

test('自动滚动关闭且超过一页：首行锚定，已读内容不被挤走',async()=>{
  const h=await boot(),{el,id,batch,rowsN,screen}=h;
  try{
    batch({events:[{id,type:'record',record:lines(1,4990)}],states:[]});await tick(500);
    el('autoscroll').click();await tick(50);
    h.setTop(1000);await tick(50);
    const firstVisible=()=>[...el('logs').children][Math.floor(screen.scrollTop/ROW)].textContent;
    const before=firstVisible();
    batch({events:[{id,type:'record',record:lines(4991,100)}],states:[]});await tick(500);
    assert.equal(rowsN(),5090,'page grows instead of dropping the rows the user is reading');
    assert.equal(firstVisible(),before);assert.equal(screen.scrollTop,1000);
    assert.deepEqual(h.errors,[]);
  }finally{h.w.close();}
});

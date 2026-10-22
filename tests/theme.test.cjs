'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(resolve=>setTimeout(resolve,ms));

test('stylesheet is one light MasterImageBuilder glass theme over an in-app backdrop; the log stays near-opaque',async()=>{
  const css=await fs.readFile('src/renderer/app.css','utf8'),code=css.replace(/\/\*[\s\S]*?\*\//g,'');
  // Light only, no desktop see-through, no wallpaper.
  for(const bad of ['html.light','native-glass','wallpaper','prefers-color-scheme:dark','--g-blur'])assert(!code.includes(bad),'stylesheet still contains '+bad);
  assert.match(code,/html,body\{background:#eef2fb;/,'page base matches the opaque native window colour');
  assert.match(code,/color-scheme:light/);
  assert.match(code,/--accent:#0a6cff;/);
  // Backdrop.qml: gradient + colour blobs drawn inside the page.
  assert.match(code,/linear-gradient\(160deg,#eef2fb 0%,#f6f0fb 55%,#eef8f6 100%\)/);
  // LiquidGlass cards: frosted over the in-app backdrop, 24px radius.
  assert.match(code,/--glass-blur:blur\(28px\) saturate\(1\.7\)/);
  assert.match(code,/backdrop-filter:var\(--glass-blur\)/);
  assert.match(code,/\.su-log-panel\{border-radius:24px/);
  assert.match(code,/\.su-commands\{border-radius:24px/);
  assert.match(code,/\.su-dialog\{[^}]*border-radius:24px/);
  assert.match(code,/\.su-tab-wrap\{[^}]*border-radius:999px/);
  assert.match(code,/\.su-btn\{[^}]*border-radius:999px/);
  // The log is never see-through: near-opaque white, no blur on it.
  const log=[...code.matchAll(/--logbg:rgba\(255,255,255,([0-9.]+)\)/g)].map(m=>Number(m[1]));
  assert(log.length&&log.every(a=>a>=0.93),'log background alpha '+log);
  assert(!/\.su-log-screen\{[^}]*backdrop-filter/.test(code),'log surface must not be frosted');
});

test('app renders white with no theme toggle, ignores a stored dark preference, and never asks for a wallpaper',async()=>{
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  assert(!/wallpaper|setTheme|native-glass/.test(bundle),'bundle must not reference wallpaper, setTheme or glass');
  const html=await fs.readFile('src/renderer/index.html','utf8');
  assert(!html.includes('data-action="theme"'),'theme toggle removed');
  const dom=new JSDOM(html,{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  w.localStorage.setItem('sa-theme','dark');
  const calls=[];
  w.serialAPI=new Proxy({initialize:async()=>({version:'0.2.4',config:defaults(),platform:'win32'}),listPorts:async()=>[],preview:async()=>({bytes:[]}),
    saveConfig:async()=>{},setAlwaysOnTop:async()=>{},onBatch:()=>{},onClosing:()=>{},ack:async()=>{},quit:async()=>{},diag:async()=>{}},{get:(t,k)=>{calls.push(String(k));return t[k];}});
  const errors=[];w.addEventListener('error',e=>errors.push(e.error));
  try{
    w.eval(bundle);await tick();
    const cls=w.document.documentElement.className;
    assert(!/\b(light|dark|native-glass|no-glass)\b/.test(cls),'no theme classes: '+cls);
    assert.equal(w.localStorage.getItem('sa-theme'),null,'old theme preference is cleared');
    assert(!w.document.querySelector('[data-action="theme"]'));
    assert(!calls.includes('wallpaper')&&!calls.includes('setTheme'),'bridge calls: '+calls.join(','));
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

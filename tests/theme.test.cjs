'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(resolve=>setTimeout(resolve,ms));

test('stylesheet is a single solid-white light theme: no dark palette, glass, blur or wallpaper',async()=>{
  const css=await fs.readFile('src/renderer/app.css','utf8'),code=css.replace(/\/\*[\s\S]*?\*\//g,'');
  for(const bad of ['html.light','native-glass','wallpaper','backdrop-filter','filter:blur','prefers-color-scheme:dark','--g-blur'])assert(!code.includes(bad),'stylesheet still contains '+bad);
  assert.match(code,/html,body\{background:#ffffff;/);
  assert.match(code,/#sui2\{[^}]*background:#ffffff;/);
  assert.match(code,/--bg:#ffffff;/);
  assert.match(code,/color-scheme:light/);
  // Rounded styling stays on cards, pills and dialogs.
  assert.match(code,/\.su-log-panel\{[^}]*border-radius:10px/);
  assert.match(code,/\.su-tab-wrap\{[^}]*border-radius:999px/);
  assert.match(code,/\.su-dialog\{[^}]*border-radius:14px/);
  assert.match(code,/\.su-btn\{[^}]*border-radius:999px/);
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

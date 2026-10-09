'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
test('renderer escapes command metadata even if a malformed object reaches the UI',async()=>{
 const config=defaults();config.commands.rtt[0].mode='text"><meta http-equiv="refresh" content="0;url=https://security-probe.invalid/"><span title="';
 const bundle=await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,format:'iife',platform:'browser'});
 const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
 w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
 w.serialAPI={initialize:async()=>({config,version:'0.1.8'}),listPorts:async()=>[],setAlwaysOnTop:async()=>{},preview:async()=>({bytes:[]}),onBatch:()=>{},onClosing:()=>{},saveConfig:async()=>{}};
 try{w.eval(bundle.outputFiles[0].text);await new Promise(r=>setTimeout(r,30));const commands=w.document.querySelector('[data-el="commands"]');assert.equal(commands.querySelector('meta'),null);assert(commands.querySelector('code').title.includes('<META'));assert.equal(w.document.querySelector('[data-el="capture"]').checked,false)}finally{w.close()}
});

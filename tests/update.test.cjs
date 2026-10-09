'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');

test('update check announces a new release, opens GitHub and remembers the ignored tag',async()=>{
  const bundle=await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'});
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  const calls=[],opened=[],errors=[];
  const release={current:'0.1.6',latest:'v0.1.7',name:'v0.1.7 · 更新检查',url:'https://github.com/shaw2001/serial-assistant/releases/tag/v0.1.7',published:'2026-10-09T02:53:57Z',assetName:'SerialAssistant-0.1.7-win-x64.exe',assetSize:4545024,available:true,checked:true};
  let latest={...release};
  const api={ack:async()=>{},initialize:async()=>({version:'0.1.6',config:defaults()}),listPorts:async()=>[],setAlwaysOnTop:async()=>{},preview:async()=>({bytes:[13,10]}),open:async()=>{},close:async()=>{},remove:async()=>{},send:async()=>({status:'submitted',length:2}),saveConfig:async()=>{},onBatch:()=>{},onClosing:()=>{},showLogs:async()=>{},clear:async()=>({cutoff:0}),startPeriodic:async()=>{},stopPeriodic:async()=>{},quit:async()=>{},
    checkUpdate:async force=>{calls.push(force);return latest;},openExternal:async url=>{opened.push(url);}};
  w.serialAPI=api;w.addEventListener('error',e=>errors.push(e.error));
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]'),settle=()=>new Promise(r=>setTimeout(r,20));
  try{
    w.eval(bundle.outputFiles[0].text);await settle();
    assert.equal(el('version').textContent,'v0.1.6');
    await new Promise(r=>setTimeout(r,2700));await settle();
    assert.equal(calls[0],false,'the startup check stays silent and throttled by the backend');
    assert(!el('overlay').hidden,'a new release opens the update dialog');
    assert(el('dialog-title').textContent.includes('发现新版本'));
    assert(el('dialog-body').textContent.includes('SerialAssistant-0.1.7-win-x64.exe'));
    q('[data-action="open-release"]').click();await settle();
    assert.deepEqual(opened,[release.url],'the GitHub release page opens through the backend');
    q('[data-action="ignore-update"]').click();await settle();
    assert(el('overlay').hidden);
    assert.equal(w.localStorage.getItem('serial-assistant:ignored-release'),'v0.1.7');
    q('[data-action="close-dialog"]')&&q('[data-action="close-dialog"]').click();
    q('[data-action="check-update"]').click();await settle();await settle();
    assert.equal(calls.at(-1),true,'the footer button forces a check');
    assert(el('dialog-title').textContent.includes('发现新版本'),'a manual check ignores the remembered choice');
    q('[data-action="close-dialog"]').click();await settle();
    latest={...release,latest:'v0.1.6',available:false};
    q('[data-action="check-update"]').click();await settle();await settle();
    assert(el('dialog-body').textContent.includes('已是最新'));
    latest={...release,checked:false,skipped:true,current:'0.1.6'};
    q('[data-action="close-dialog"]').click();await settle();
    q('[data-action="check-update"]').click();await settle();await settle();
    assert(el('dialog-body').textContent.includes('24 小时内已检查'));
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('update check stays quiet when the backend has no update channel',async()=>{
  const bundle=await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'});
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  const errors=[];
  const api={ack:async()=>{},initialize:async()=>({version:'0.1.6',config:defaults()}),listPorts:async()=>[],setAlwaysOnTop:async()=>{},preview:async()=>({bytes:[13,10]}),open:async()=>{},close:async()=>{},remove:async()=>{},send:async()=>({status:'submitted',length:2}),saveConfig:async()=>{},onBatch:()=>{},onClosing:()=>{},showLogs:async()=>{},clear:async()=>({cutoff:0}),startPeriodic:async()=>{},stopPeriodic:async()=>{},quit:async()=>{}};
  w.serialAPI=api;w.addEventListener('error',e=>errors.push(e.error));
  try{
    w.eval(bundle.outputFiles[0].text);await new Promise(r=>setTimeout(r,50));
    w.document.querySelector('[data-action="check-update"]').click();await new Promise(r=>setTimeout(r,30));
    assert(w.document.querySelector('[data-el="dialog-body"]').textContent.includes('不支持在线更新检查'));
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

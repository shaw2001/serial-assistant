// Usage: node --experimental-websocket perf.mjs <label>
import {spawn} from 'node:child_process';import fs from 'node:fs';
const label=process.argv[2]||'run';
const D='/workspace/render/perf/site';fs.rmSync(D,{recursive:true,force:true});fs.cpSync('/workspace/serial-assistant/dist',D,{recursive:true});
fs.writeFileSync(D+'/cfg.js','window.__CFG='+fs.readFileSync('cfg.json','utf8')+';window.__TPL="";');fs.copyFileSync('mock.js',D+'/mock.js');
fs.writeFileSync(D+'/index.html',fs.readFileSync(D+'/index.html','utf8').replace('<script src="app.js">','<script src="cfg.js"></script><script src="mock.js"></script><script src="app.js">'));
const srv=spawn('python3',['-m','http.server','8767','-d',D],{stdio:'ignore'});
const chrome=spawn('google-chrome',['--headless=new','--no-sandbox','--remote-debugging-port=9341','--user-data-dir=/tmp/cdp-perf','--window-size=1440,900',...(process.env.FLAGS?process.env.FLAGS.split(' '):[]),'about:blank'],{stdio:'ignore'});
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
let tabs;for(let i=0;i<60;i++){try{tabs=await (await fetch('http://127.0.0.1:9341/json')).json();break}catch{await sleep(200)}}
const page=tabs.find(t=>t.type==='page');const ws=new WebSocket(page.webSocketDebuggerUrl);await new Promise(r=>ws.onopen=r);
let id=0;const pend=new Map();ws.onmessage=m=>{const d=JSON.parse(m.data);if(d.id&&pend.has(d.id)){pend.get(d.id)(d);pend.delete(d.id)}};
const send=(method,params={})=>new Promise(r=>{const i=++id;pend.set(i,r);ws.send(JSON.stringify({id:i,method,params}))});
const ev=async e=>(await send('Runtime.evaluate',{expression:e,returnByValue:true,awaitPromise:true})).result?.result?.value;
await send('Runtime.enable');await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride',{width:1440,height:900,deviceScaleFactor:1,mobile:false});
await send('Page.navigate',{url:'http://127.0.0.1:8767/index.html'});await sleep(2500);
await ev(`(()=>{const p=document.querySelector('[data-el=port]');p.value='COM5';p.dispatchEvent(new Event('change',{bubbles:true}));document.querySelector('[data-action=connect]').click();return 1})()`);await sleep(700);
if(process.env.PROF){await send('Profiler.enable');await send('Profiler.setSamplingInterval',{interval:500});await send('Profiler.start');}
await send('Performance.enable');const m0=Object.fromEntries((await send('Performance.getMetrics')).result.metrics.map(m=>[m.name,m.value]));
const result=await ev(`new Promise(done=>{
 const sid=document.querySelector('.su-tab-wrap.active [data-session]').dataset.session;
 const lv=['I','D','W','E','V'];let seq=1,line=0;const enc=s=>btoa(unescape(encodeURIComponent(s)));
 // 300 KB/s in 80 ms batches (~24 KB per batch), like the native poller.
 const RATE=${Number(process.env.RATE??300000)},SCROLL=${process.env.NOACT?0:1};const pump=setInterval(()=>{if(!RATE)return;let s='';while(s.length<RATE*0.08){line++;s+='10-27 15:00:'+String(line%60).padStart(2,'0')+' '+lv[line%5]+'/gnss-ble qw_gnss: \\x1b[1;32mframe\\x1b[0m '+line+' payload=0x'+(line*2654435761>>>0).toString(16)+' some more text to fill the line\\r\\n';}
   window.__push({events:[{id:sid,type:'record',record:{seq:seq++,time:new Date().toISOString(),direction:'RX',dataBase64:enc(s),length:s.length}}],states:[]});},80);
 const frames=[];let last=performance.now(),longT=0,longN=0;
 new PerformanceObserver(l=>{for(const e of l.getEntries()){longT+=e.duration;longN++;}}).observe({type:'longtask'});
 const loop=t=>{frames.push(t-last);last=t;if(t-t0<8000)requestAnimationFrame(loop);else finish();};const t0=performance.now();requestAnimationFrame(loop);
 // interactions: scroll the log, toggle the sidebar, hover-like tab switches
 const scr=document.querySelector('[data-el=screen]');let k=0;
 const act=setInterval(()=>{if(!SCROLL)return;k++;if(k%10===3)document.querySelector('[data-action=toggle-rail]')?.click();scr.scrollTop=k%2?scr.scrollTop-400:scr.scrollHeight;scr.dispatchEvent(new Event('scroll'));},250);
 function finish(){clearInterval(pump);clearInterval(act);const f=frames.slice(5).sort((a,b)=>a-b),q=p=>f[Math.min(f.length-1,Math.floor(p*f.length))];
  done({frames:f.length,fps:+(f.length/((performance.now()-t0)/1000)).toFixed(1),p50:+q(.5).toFixed(1),p95:+q(.95).toFixed(1),p99:+q(.99).toFixed(1),max:+f[f.length-1].toFixed(1),janky:f.filter(x=>x>50).length,longTaskMs:Math.round(longT),longTasks:longN,rows:document.querySelectorAll('.su-log-row').length});}
})`);
const m1=Object.fromEntries((await send('Performance.getMetrics')).result.metrics.map(m=>[m.name,m.value]));for(const k of ['TaskDuration','ScriptDuration','LayoutDuration','RecalcStyleDuration'])result[k]=Math.round((m1[k]-m0[k])*1000);result.layouts=m1.LayoutCount-m0.LayoutCount;result.styles=m1.RecalcStyleCount-m0.RecalcStyleCount;result.heapMB=+(m1.JSHeapUsedSize/1048576).toFixed(1);
console.log(label,JSON.stringify(result));
if(process.env.PROF){const p=(await send('Profiler.stop')).result.profile;const self=new Map(),byId=new Map(p.nodes.map(n=>[n.id,n]));const dt=p.timeDeltas;const cnt=new Map();p.samples.forEach((s,i)=>cnt.set(s,(cnt.get(s)||0)+(dt[i]||0)));for(const [nid,t] of cnt){const n=byId.get(nid),k=n.callFrame.functionName+' '+n.callFrame.url.split('/').pop()+':'+n.callFrame.lineNumber+':'+n.callFrame.columnNumber;self.set(k,(self.get(k)||0)+t);}console.log([...self].sort((a,b)=>b[1]-a[1]).slice(0,25).map(([k,t])=>(t/1000).toFixed(0)+'ms '+k).join('\n'));}
fs.appendFileSync('/workspace/render/perf/results.txt',label+' '+JSON.stringify(result)+'\n');
ws.close();chrome.kill();srv.kill();process.exit(0);

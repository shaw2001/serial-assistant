(()=>{
 const pending=new Map();let seq=0,delivery=null,closing=null,polling=false;
 window.__nativeResolve=(id,result)=>{const p=pending.get(id);if(!p)return;pending.delete(id);result.ok?p.resolve(result.value):p.reject(new Error(result.error||'操作失败'));};
 const call=(method,...args)=>new Promise((resolve,reject)=>{const id=++seq;pending.set(id,{resolve,reject});window.__enqueue({id,method,args}).catch(e=>{pending.delete(id);reject(new Error(String(e)));});});
 window.serialAPI={initialize:()=>call('initialize'),listPorts:()=>call('listPorts'),open:(id,o)=>call('open',id,o),close:id=>call('close',id),remove:id=>call('remove',id),send:(id,p)=>call('send',id,p),preview:p=>call('preview',p),startPeriodic:(id,p,t)=>call('startPeriodic',id,p,t),stopPeriodic:id=>call('stopPeriodic',id),setCapture:(id,on)=>call('setCapture',id,on),clear:id=>call('clear',id),exportLog:o=>call('exportLog',o),saveConfig:c=>call('saveConfig',c),importConfig:()=>call('importConfig'),exportConfig:c=>call('exportConfig',c),showLogs:()=>call('showLogs'),setAlwaysOnTop:on=>call('setAlwaysOnTop',on),quit:()=>call('quit'),ack:async()=>{},onClosing:f=>{closing=f;},onBatch:f=>{delivery=f;},licenses:()=>call('licenses'),ready:data=>call('ready',data),checkUpdate:force=>call('checkUpdate',!!force),openExternal:url=>call('openExternal',url)};
 window.__nativeClosing=()=>{if(closing)closing();else window.serialAPI.quit();};
 setInterval(async()=>{if(!delivery||polling)return;polling=true;try{const batch=await call('poll');if(batch)delivery(batch);}catch(e){console.error(e);}finally{polling=false;}},80);
})();

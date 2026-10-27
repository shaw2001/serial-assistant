(()=>{const cfg=window.__CFG;let batch=null;window.__push=b=>batch&&batch(b);
const snap=(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:18342,tx:512,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:false,periodic:null});
const rttd={dir:'C:\\ncs\\SEGGER\\JLink_V918',device:'nRF54L15_M33',iface:'SWD',speed:4000,channel:0,addrMode:'auto',address:'',ranges:'',serial:'',backend:'commander',telnetPort:19021};
const impl={initialize:async()=>({version:'0.4.0',config:cfg,platform:'win32',logsDirectory:'C:\\logs',rttDefaults:rttd}),
 listPorts:async()=>[{path:'COM5',friendly:'USB-SERIAL CH340'},{path:'COM8',friendly:'nRF USB CDC'}],
 preview:async c=>({bytes:[...new TextEncoder().encode(c.content+(c.ending==='crlf'?'\r\n':''))]}),
 open:async(id,o)=>snap(id,o),close:async id=>({id,status:'disconnected'}),send:async(id,c)=>({status:'submitted',length:c.content.length}),
 onBatch:f=>{batch=f},onClosing:()=>{},openTextFile:async()=>({name:'快捷指令导入模板.csv',text:window.__TPL}),saveTextFile:async(n)=>({canceled:false,path:'C:\\Users\\me\\'+n}),checkJLink:async()=>({ok:true,commander:rttd.dir+'\\JLink.exe',logger:rttd.dir+'\\JLinkRTTLogger.exe',dll:rttd.dir+'\\JLink_x64.dll'})};
window.serialAPI=new Proxy(impl,{get:(t,k)=>t[k]||(async()=>({}))});})();

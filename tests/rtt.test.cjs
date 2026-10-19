'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(resolve=>setTimeout(resolve,ms));
let R;test.before(async()=>{R=await import('../src/renderer/rtt.mjs');});

async function boot(api={},config=defaults()){
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  let batch;const opened=[],saved=[],checked=[],sent=[];
  const snapshot=(id,o)=>({id,path:o.path,baudRate:o.baudRate,config:o,status:'connected',rx:0,tx:0,errors:0,recorded:0,viewDropped:0,captureDropped:0,trimmed:0,capture:false,periodic:null,history:0,historyTrimmed:0});
  w.serialAPI={initialize:async()=>({version:'0.3.0',config,rttDefaults:{...R.RTT_DEFAULTS}}),listPorts:async()=>[{path:'COM5'}],preview:async p=>({bytes:[...Buffer.from(p.content+(p.ending==='crlf'?'\r\n':''))]}),
    open:async(id,o)=>{opened.push(o);return snapshot(id,o);},close:async id=>({id,status:'disconnected',rx:0,tx:0,errors:0}),remove:async()=>{},send:async(id,p)=>{sent.push({id,p:JSON.parse(JSON.stringify(p))});return {status:'submitted',length:p.content.length};},saveConfig:async c=>{saved.push(JSON.parse(JSON.stringify(c)));},setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{},
    checkJLink:async dir=>{checked.push(dir);if(dir.includes('JLink_V794i'))return {dir,commander:dir+'\\JLink.exe',logger:dir+'\\JLinkRTTLogger.exe',dll:dir+'\\JLink_x64.dll'};throw new Error('J-Link 软件路径不存在：'+dir+'。');},
    pickJLink:async()=>({canceled:false,dir:'D:\\Tools\\SEGGER\\JLink_V794i'}),...api};
  const errors=[];w.addEventListener('error',e=>errors.push(e.error));
  w.eval(bundle);await tick();
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  const set=(sel,v)=>{const x=q(sel);x.value=v;x.dispatchEvent(new w.Event('input',{bubbles:true}));x.dispatchEvent(new w.Event('change',{bubbles:true}));};
  return {w,q,el,set,errors,opened,saved,checked,sent,batch:(...a)=>batch(...a)};
}

test('RTT defaults and the J-Link Commander command line shown to the user',()=>{
  assert.deepEqual({...R.RTT_DEFAULTS},{dir:'C:\\Program Files\\SEGGER\\JLink_V794i',device:'nRF54L15_M33',iface:'SWD',speed:4000,channel:0,addrMode:'auto',address:'',ranges:'',serial:'',backend:'commander',telnetPort:19021});
  assert.equal(R.rttCommand(R.RTT_DEFAULTS),'"C:\\Program Files\\SEGGER\\JLink_V794i\\JLink.exe" -Device nRF54L15_M33 -If SWD -Speed 4000 -AutoConnect 1 -ExitOnError 1 -NoGui 1 -RTTTelnetPort 19021 -CommandFile <临时命令文件> · RTT Telnet 127.0.0.1:19021');
  assert.equal(R.rttCommand({...R.RTT_DEFAULTS,backend:'logger'}),'"C:\\Program Files\\SEGGER\\JLink_V794i\\JLinkRTTLogger.exe" -Device nRF54L15_M33 -If SWD -Speed 4000 -RTTChannel 0 <临时输出文件>');
  assert.match(R.validateRTT({...R.RTT_DEFAULTS,telnetPort:80}),/1024–65535/);
  assert.equal(R.normalizeRTT({device:'x'}).backend,'commander','old settings default to the bidirectional backend');
  assert.match(R.rttCommand({...R.RTT_DEFAULTS,backend:'logger',addrMode:'ranges',ranges:'0x20000000 0x40000',serial:'123'}),/-USB 123 -RTTSearchRanges "0x20000000 0x40000" -RTTChannel 0/);
  assert.match(R.rttCommand({...R.RTT_DEFAULTS,addrMode:'address',address:'0x20001000'}),/exec SetRTTAddr 0x20001000$/);
  assert.equal(R.validateRTT(R.normalizeRTT({})),'');
  assert.match(R.validateRTT({...R.RTT_DEFAULTS,dir:''}),/请填写 J-Link 软件路径/);
  assert.match(R.validateRTT({...R.RTT_DEFAULTS,device:'nRF54 L15'}),/目标芯片名称无效/);
  assert.match(R.validateRTT({...R.RTT_DEFAULTS,speed:NaN}),/kHz/);
  assert.match(R.validateRTT({...R.RTT_DEFAULTS,addrMode:'address',address:'2000'}),/十六进制/);
  assert.equal(R.rttLabel(R.RTT_DEFAULTS,['RTT · nRF54L15_M33']),'RTT · nRF54L15_M33 #2');
  assert.equal(R.rttSummary(R.RTT_DEFAULTS),'J-Link RTT · nRF54L15_M33 · SWD 4000 kHz · 通道 0');
});

test('添加 J-Link RTT: dialog → own session tab → native open with RTT options; send to down channel, serial controls hidden, settings persisted',async()=>{
  const {w,q,el,set,errors,opened,saved,checked,sent,batch}=await boot();
  try{
    q('[data-action="add-port"]').click();await tick(50);
    q('[data-new-kind="rtt"]').click();await tick(10);
    assert.equal(el('dialog-title').textContent,'添加 J-Link RTT');
    assert.equal(q('[data-kind-panel="rtt"]').hidden,false);assert.equal(q('[data-kind-panel="serial"]').hidden,true);
    for(const [k,v] of Object.entries({dir:'C:\\Program Files\\SEGGER\\JLink_V794i',device:'nRF54L15_M33',iface:'SWD',speed:'4000',channel:'0',addrMode:'auto'}))assert.equal(q('[data-rtt="'+k+'"]').value,v,k);
    assert.match(q('[data-rtt-cmd]').textContent,/JLink\.exe" -Device nRF54L15_M33 -If SWD -Speed 4000 -AutoConnect 1 -ExitOnError 1 -NoGui 1 -RTTTelnetPort 19021 -CommandFile/);
    assert.equal(q('[data-rtt="backend"]').value,'commander');assert.equal(q('[data-rtt="telnetPort"]').value,'19021');
    assert.match(q('[data-rtt-note]').textContent,/写入 RTT 下行通道 0/);
    // 检测 reports what was found; a wrong folder gives the native Chinese error.
    q('[data-action="check-jlink"]').click();await tick(50);
    assert.match(q('[data-jlink-status]').textContent,/已找到 .*JLink\.exe（收发） · JLinkRTTLogger\.exe（只读） · JLink_x64\.dll/);
    set('[data-rtt="dir"]','C:\\nowhere');q('[data-action="check-jlink"]').click();await tick(50);
    assert.match(q('[data-jlink-status]').textContent,/J-Link 软件路径不存在：C:\\nowhere/);
    q('[data-action="pick-jlink"]').click();await tick(80);
    assert.equal(q('[data-rtt="dir"]').value,'D:\\Tools\\SEGGER\\JLink_V794i');
    assert.deepEqual([...checked],['C:\\Program Files\\SEGGER\\JLink_V794i','C:\\nowhere','D:\\Tools\\SEGGER\\JLink_V794i']);
    // Manual control block address field appears only in that mode.
    assert.equal(q('[data-rtt-for="address"]').hidden,true);
    set('[data-rtt="addrMode"]','address');assert.equal(q('[data-rtt-for="address"]').hidden,false);
    set('[data-rtt="address"]','oops');q('[data-action="create-connect-port"]').click();await tick(50);
    assert.match(q('[data-dialog-error]').textContent,/十六进制/);assert.equal(opened.length,0);
    set('[data-rtt="addrMode"]','auto');set('[data-rtt="speed"]','8000');set('[data-rtt="channel"]','1');
    q('[data-action="create-connect-port"]').click();await tick(300);
    assert.equal(opened.length,1);
    assert.deepEqual(JSON.parse(JSON.stringify(opened[0].rtt)),{dir:'D:\\Tools\\SEGGER\\JLink_V794i',device:'nRF54L15_M33',iface:'SWD',speed:8000,channel:1,addrMode:'auto',address:'',ranges:'',serial:'',backend:'commander',telnetPort:19021});
    assert.equal(opened[0].kind,'rtt');assert.equal(opened[0].path,'RTT · nRF54L15_M33 · 通道 1');
    const tabs=[...w.document.querySelectorAll('.su-tab b')].map(b=>b.textContent);
    assert.deepEqual(tabs,['新会话','RTT · nRF54L15_M33 · 通道 1'],'RTT is its own tab');
    assert(q('#sui2').classList.contains('su-kind-rtt'));
    assert.equal(el('rtt-summary').textContent,'nRF54L15_M33 · SWD 8000 kHz · 通道 1');
    assert.match(el('status').textContent,/已连接 · J-Link RTT · nRF54L15_M33/);
    // Send is enabled and goes to RTT down channel 1 through the normal send path.
    assert.equal(el('send-target').textContent,'RTT 下行通道 1');
    assert.match(el('input').placeholder,/发送到 RTT 下行通道 1/);
    assert.match(el('command-hint').textContent,/J-Link RTT 下行通道 1/);
    el('input').value='ps';el('input').dispatchEvent(new w.Event('input',{bubbles:true}));await tick(100);
    assert.equal(el('send').disabled,false,'send enabled on a bidirectional RTT tab');
    el('send').click();await tick(100);
    assert.equal(sent.length,1);assert.equal(sent[0].p.content,'ps');assert.equal(sent[0].p.ending,'crlf');
    const quick=q('[data-command-send]');assert(quick&&!quick.disabled,'quick commands send on RTT');
    quick.click();await tick(100);assert.equal(sent.length,2);
    // RTT bytes go through the same log pipeline.
    const id=q('.su-tab-wrap.active [data-session]').dataset.session;
    batch({events:[{id,type:'record',record:{seq:1,time:'2026-10-17T07:00:00.000Z',direction:'RX',length:15,dataBase64:Buffer.from('[00:00:01] ok\r\n').toString('base64')}}],states:[]});await tick(300);
    assert.match(el('logs').textContent,/\[00:00:01\] ok/);
    // Settings dialog is locked while connected.
    q('[data-action="rtt-settings"]').click();await tick(30);
    assert.equal(q('[data-rtt="device"]').disabled,true);assert.equal(q('[data-action="save-rtt"]').disabled,true);
    q('[data-action="close-dialog"]').click();
    // Switching back to the serial tab restores the serial controls.
    q('.su-tab-wrap:not(.active) [data-session]').click();await tick(20);
    assert(!q('#sui2').classList.contains('su-kind-rtt'));
    await tick(400);
    const last=saved.at(-1);
    const dev=last.devices.find(d=>d.kind==='rtt');
    assert(dev&&dev.rtt.speed===8000&&dev.rtt.dir==='D:\\Tools\\SEGGER\\JLink_V794i','RTT device persisted');
    assert.equal(last.ui.jlink.dir,'D:\\Tools\\SEGGER\\JLink_V794i','last J-Link settings remembered');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('JLinkRTTLogger backend stays receive-only',async()=>{
  const {q,el,set,w,errors,opened,sent}=await boot();
  try{
    q('[data-action="add-port"]').click();await tick(50);q('[data-new-kind="rtt"]').click();
    set('[data-rtt="backend"]','logger');
    assert.equal(q('[data-rtt-backend="commander"]').hidden,true,'telnet port only for Commander');
    assert.match(q('[data-rtt-cmd]').textContent,/JLinkRTTLogger\.exe" .*-RTTChannel 0 <临时输出文件>/);
    assert.match(q('[data-rtt-note]').textContent,/只读/);
    q('[data-action="create-connect-port"]').click();await tick(300);
    assert.equal(opened[0].rtt.backend,'logger');
    assert.equal(el('send-target').textContent,'J-Link RTT（只读）');
    assert.match(el('rtt-summary').textContent,/· 只读$/);
    el('input').value='ps';el('input').dispatchEvent(new w.Event('input',{bubbles:true}));await tick(100);
    assert.equal(el('send').disabled,true);assert.match(el('input').placeholder,/只读/);
    assert.equal(sent.length,0);
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('persisted RTT session restores as an RTT tab; connect errors are shown in Chinese',async()=>{
  const config=defaults();
  config.devices=[{id:'session-rtt1',path:'RTT · nRF54L15_xxAA',baud:'115200',receiveEncoding:'utf-8',params:{data:8,parity:'none',stop:1,flow:'none'},draft:{content:'',mode:'text',encoding:'utf-8',ending:'crlf'},history:[],kind:'rtt',rtt:{dir:'C:\\Program Files\\SEGGER\\JLink_V794i',device:'nRF54L15_xxAA',iface:'SWD',speed:4000,channel:0,addrMode:'auto'}}];
  config.ui={privacyVersion:1,jlink:{dir:'E:\\JLink',device:'nRF54L15_M33',iface:'JTAG',speed:2000,channel:0,addrMode:'auto'}};
  const {q,el,w,errors}=await boot({open:async()=>{throw new Error('J-Link 不认识目标芯片名称“nRF54L15_xxAA”。请在“目标芯片”中填写 J-Link 设备库中的名称（nRF54L15 的应用核为 nRF54L15_M33）。 J-Link 输出：Connecting to target...ERROR: Unknown device');}},config);
  try{
    assert(q('#sui2').classList.contains('su-kind-rtt'));
    assert.equal(el('rtt-summary').textContent,'nRF54L15_xxAA · SWD 4000 kHz · 通道 0');
    assert.equal(el('connect').disabled,false);
    q('[data-action="connect"]').click();await tick(100);
    assert.match(el('notice').textContent,/连接失败：J-Link 不认识目标芯片名称“nRF54L15_xxAA”.*Unknown device/);
    assert.equal(el('status').textContent,'未连接');
    // New RTT dialogs start from the remembered settings.
    q('[data-action="add-port"]').click();await tick(50);q('[data-new-kind="rtt"]').click();
    assert.equal(q('[data-kind-panel="rtt"] [data-rtt="dir"]').value,'E:\\JLink');
    assert.equal(q('[data-kind-panel="rtt"] [data-rtt="iface"]').value,'JTAG');
    q('[data-action="close-dialog"]').click();await tick(20);
    // Fix the device name in the settings dialog.
    q('[data-action="rtt-settings"]').click();await tick(30);
    const d=q('[data-rtt="device"]');d.value='nRF54L15_M33';d.dispatchEvent(new w.Event('input',{bubbles:true}));
    q('[data-action="save-rtt"]').click();await tick(20);
    assert.equal(q('.su-tab b').textContent,'RTT · nRF54L15_M33');
    assert.equal(el('rtt-summary').textContent,'nRF54L15_M33 · SWD 4000 kHz · 通道 0');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

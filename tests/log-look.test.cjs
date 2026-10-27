'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs/promises');
const {JSDOM}=require('jsdom'),{build}=require('esbuild');
const {defaults}=require('../src/main/config.cjs');
const tick=(ms=300)=>new Promise(r=>setTimeout(r,ms));
const rec=(seq,text)=>({seq,time:'2026-10-26T02:29:30.000Z',direction:'RX',dataBase64:Buffer.from(text).toString('base64'),length:Buffer.byteLength(text)});

test('auto newline: CR CR LF and LF CR are one line break; real empty lines stay; bytes are kept',async()=>{
  const {LogModel}=await import('../src/renderer/model.mjs');
  const rows=parts=>{const m=new LogModel();parts.forEach((p,i)=>m.append(rec(i+1,p)));return {text:m._rows.map(r=>r.text),bytes:m._rows.reduce((a,r)=>a+r.length,0)};};
  assert.deepEqual(rows(['a\r\nb\r\n']).text,['a','b']);
  assert.deepEqual(rows(['a\r\r\nb\r\r\n']),{text:['a','b'],bytes:8},'CR CR LF used to add a blank row after every line');
  assert.deepEqual(rows(['a\n\rb\n\r']).text,['a','b'],'LF CR is one break');
  // Split across chunks at every awkward position.
  assert.deepEqual(rows(['a\r','\r\nb\r','\r','\n']).text,['a','b']);
  assert.deepEqual(rows(['a\n','\rb']).text,['a','b']);
  assert.deepEqual(rows(['a\r','\nb']).text,['a','b']);
  // Real empty lines remain.
  assert.deepEqual(rows(['a\n\nb\n']).text,['a','','b']);
  assert.deepEqual(rows(['a\r\n\r\nb\r\n']).text,['a','','b']);
  assert.deepEqual(rows(['a\r','\rb']).text,['a','','b']);
  // Explicit modes are unchanged.
  const m=new LogModel('utf-8','lf');m.append(rec(1,'a\r\r\nb\n'));assert.deepEqual(m._rows.map(r=>r.text),['a␍␍','b']);
});

test('log level detection: W is warning even when the text says failed; common formats',async()=>{
  const {logLevel,normalizeLevelColors,LEVEL_DEFAULTS}=await import('../src/renderer/levels.mjs');
  const cases={'10-26 02:29:30 W/WEB_MODULE qw_gnss: auto connect failed':'w','10-26 02:29:30 E/gnss-ble qw_gnss: ble request submit failed evt=4':'e','10-26 02:27:44 D/BLE_GAP KE_EVT2':'d','10-26 02:28:00 I/VOLTMETER qw_time: charge':'i','V/x verbose':'v','[W/net] retry':'w','[00:00:00.121,000] <wrn> ble: x':'w','[00:00:12.400,000] <err> s':'e','I (123) wifi: ok':'i','plain ERROR here':'e','tshell 20 running':''};
  for(const [t,l] of Object.entries(cases))assert.equal(logLevel(t),l,t);
  assert.notEqual(LEVEL_DEFAULTS.w,LEVEL_DEFAULTS.e,'W and E differ by default');
  assert.deepEqual(normalizeLevelColors({w:'#ABCDEF',e:'red',x:'#000000'}),{...LEVEL_DEFAULTS,w:'#abcdef'});
});

async function setup(ui){
  const bundle=(await build({entryPoints:['src/renderer/app.mjs'],bundle:true,write:false,platform:'browser',format:'iife'})).outputFiles[0].text;
  const dom=new JSDOM(await fs.readFile('src/renderer/index.html','utf8'),{runScripts:'outside-only',url:'https://local.invalid/'}),w=dom.window;
  w.TextDecoder=TextDecoder;w.HTMLElement.prototype.scrollIntoView=()=>{};
  const config=defaults();if(ui)config.ui={...config.ui,...ui};
  const saved=[],errors=[];let batch;
  w.serialAPI={initialize:async()=>({version:'0.4.1',config}),listPorts:async()=>[{path:'COM5'}],preview:async()=>({bytes:[]}),
    open:async(id,o)=>({id,path:o.path,baudRate:o.baudRate,status:'connected',rx:0,tx:0,errors:0,capture:false,periodic:null}),close:async id=>({id,status:'disconnected'}),
    saveConfig:async c=>saved.push(JSON.parse(JSON.stringify(c))),setAlwaysOnTop:async()=>{},onBatch:f=>batch=f,onClosing:()=>{},ack:async()=>{},quit:async()=>{}};
  w.addEventListener('error',e=>errors.push(e.error));
  w.eval(bundle);await tick();
  const q=s=>w.document.querySelector(s),el=n=>q('[data-el="'+n+'"]');
  return {w,q,el,saved,errors,batch:b=>batch(b)};
}

test('rows get per-level classes; level colours are editable and persisted; log-only dark theme toggles and persists',async()=>{
  const h=await setup(),{w,q,el,saved,errors}=h;
  try{
    const port=el('port');port.value='COM5';port.dispatchEvent(new w.Event('change',{bubbles:true}));q('[data-action="connect"]').click();await tick();
    const id=q('.su-tab-wrap.active [data-session]').dataset.session;
    h.batch({events:[{id,type:'record',record:rec(1,'10-26 02:29:30 W/WEB_MODULE qw_gnss: auto connect failed\r\r\n10-26 02:29:30 E/gnss-ble qw_gnss: ble failed\r\r\n10-26 02:28:00 I/VOLTMETER ok\r\r\n')}],states:[]});
    await tick(700);
    const rows=[...w.document.querySelectorAll('.su-log-row')];
    assert.equal(rows.length,3,'no blank rows between lines');
    assert(rows[0].classList.contains('lv-w')&&!rows[0].classList.contains('err'),'W row is a warning, not an error');
    assert(rows[1].classList.contains('lv-e'));assert(rows[2].classList.contains('lv-i'));
    assert(rows[0].querySelector('.su-token-tag.w'),'W/TAG token is tagged w');
    // Defaults on the root, W amber vs E red.
    const root=q('#sui2');assert.equal(root.style.getPropertyValue('--lv-w'),'#e08600');assert.equal(root.style.getPropertyValue('--lv-e'),'#e0342a');
    // Colour pickers in 显示选项.
    const pick=q('[data-level-color="w"]');assert(pick&&pick.type==='color');assert.equal(w.document.querySelectorAll('[data-level-color]').length,5);
    pick.value='#ff9900';pick.dispatchEvent(new w.Event('input',{bubbles:true}));
    assert.equal(root.style.getPropertyValue('--lv-w'),'#ff9900');
    // Dark toggle affects only the log panel.
    const tg=el('log-theme');assert.equal(tg.getAttribute('aria-pressed'),'false');tg.click();
    assert(root.classList.contains('su-log-dark'));assert.equal(tg.getAttribute('aria-pressed'),'true');assert.equal(tg.textContent.trim(),'深色');
    await tick(900);
    const last=saved.at(-1).ui;assert.equal(last.logTheme,'dark');assert.equal(last.levelColors.w,'#ff9900');assert.equal(last.levelColors.e,'#e0342a');
    q('[data-action="reset-level-colors"]').click();assert.equal(root.style.getPropertyValue('--lv-w'),'#e08600');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
});

test('stored log theme and colours are restored at startup; CSS keeps dark log opaque and scoped to the log',async()=>{
  const h=await setup({logTheme:'dark',levelColors:{w:'#123456'}}),{q,w,errors}=h;
  try{
    assert(q('#sui2').classList.contains('su-log-dark'));
    assert.equal(q('#sui2').style.getPropertyValue('--lv-w'),'#123456');
    assert.equal(q('[data-level-color="w"]').value,'#123456');
    assert.deepEqual(errors,[]);
  }finally{w.close();}
  const css=await fs.readFile('src/renderer/app.css','utf8');
  assert.match(css,/#sui2\.su-log-dark \.su-log-screen\{\s*background:#12161f;/,'dark log is an opaque surface');
  assert(!/\.su-log-dark (?!\.su-log|\.su-token|\.su-pause)/.test(css),'dark rules only target the log');
});

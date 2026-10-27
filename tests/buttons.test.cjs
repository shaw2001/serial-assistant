'use strict';
// 0.4.1 regression: `.su-btn:hover{background:var(--field-hover)}` also matched the
// red 断开 and blue primary buttons; the shorthand dropped their gradient and left
// white text on near-white. Check the stylesheet and the computed hover look.
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs');
const css=fs.readFileSync('src/renderer/app.css','utf8').replace(/\/\*[\s\S]*?\*\//g,'');
const rules=[...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(m=>({sel:m[1].trim(),body:m[2]}));
test('generic button hover never repaints filled (danger/primary) buttons',()=>{
  for(const r of rules){
    for(const sel of r.sel.split(',')){
      if(!/:hover/.test(sel)||!/background\s*:/.test(r.body))continue;
      if(/\.su-btn(?![\w-])/.test(sel)&&!/\.su-(danger|primary|send-button|connect|command-send)/.test(sel.replace(/:not\([^)]*\)/g,''))){
        for(const k of ['su-danger','su-primary','su-send-button','su-connect'])assert(sel.includes(':not(.'+k+')'),'hover rule "'+sel.trim()+'" would repaint .'+k);
      }
    }
  }
});
test('filled buttons have a hover rule that keeps white text and only adjusts brightness',()=>{
  const r=rules.find(x=>/:is\([^)]*\.su-danger[^)]*\):hover/.test(x.sel));
  assert(r,'missing filled-button hover rule');assert.match(r.body,/color:#fff/);assert.match(r.body,/filter:brightness/);assert.doesNotMatch(r.body,/background/);
});
test('title-bar logo is the app icon embedded as a data: PNG, not a file reference',()=>{
  const html=fs.readFileSync('src/renderer/index.html','utf8');
  assert.doesNotMatch(html,/src="logo\.svg"|href="logo\.svg"/);
  const m=html.match(/<img class="su-logo"[^>]*src="data:image\/png;base64,([A-Za-z0-9+/=]+)"/);assert(m,'logo must be a data: PNG');
  const png=Buffer.from(m[1],'base64');assert.equal(png.subarray(1,4).toString(),'PNG');assert.equal(png.readUInt32BE(16),60);assert.equal(png.readUInt32BE(20),60);
  assert(png.equals(fs.readFileSync('assets/icon/logo_60.png')),'logo must match assets/icon/logo_60.png');
  assert.doesNotMatch(html,/class="su-logo"[^>]*><svg/);
});

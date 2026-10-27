// Display-only cleanup of device text. Raw bytes (HEX view, 保存日志, 落盘)
// are never changed. Shells such as Zephyr/nRF, RT-Thread and Linux send
// VT100/ANSI colour and cursor sequences plus backspace echo; shown raw they
// look like garbage ("[1;32m", "[J", invisible ESC/BS characters).
const CONTROL=/[\x00-\x08\x0b-\x1f\x7f\x9b]/;
export function cleanText(text){
  const t=String(text??'');
  if(!CONTROL.test(t))return t;
  const stripped=t
    .replace(/\x1b\[[0-?]*[ -\/]*[@-~]/g,'')       // CSI: colours, cursor moves, erase line
    .replace(/\x9b[0-?]*[ -\/]*[@-~]/g,'')          // 8-bit CSI
    .replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?/g,'') // OSC: window titles
    .replace(/\x1b[()][0-9A-Za-z]/g,'')             // charset selection
    .replace(/\x1b[@-_]/g,'')                       // other 2-byte escapes
    .replace(/\x1b/g,'');
  const out=[];
  for(const ch of stripped){
    if(ch==='\b'||ch==='\x7f'){out.pop();continue;}  // echo of line editing
    if(ch==='\t'){out.push(ch);continue;}
    if(ch<' ')continue;                               // BEL, NUL and other C0 controls
    out.push(ch);
  }
  return out.join('');
}

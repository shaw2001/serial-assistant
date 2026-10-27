// Log level detection and the user-configurable level colours.
export const LEVELS=['v','d','i','w','e'];
export const LEVEL_NAMES={v:'V 详细',d:'D 调试',i:'I 信息',w:'W 警告',e:'E 错误'};
export const LEVEL_DEFAULTS={v:'#8a93a0',d:'#2f80ed',i:'#1f9d48',w:'#e08600',e:'#e0342a'};
const zephyr={err:'e',wrn:'w',inf:'i',dbg:'d'};
// Level prefix near the start of a line: "W/TAG", "[W/TAG]", "<wrn>", "E (123) tag:".
const prefix=/^(?:[\d\s:.,\-\/\[\]()]{0,40}?)(?:\[([VDIWE])\/|([VDIWE])\/[\w.$-]|<(err|wrn|inf|dbg)>|([VDIWE]) \(\d+\) )/;
export function logLevel(text){
  const head=String(text||'').slice(0,96),m=prefix.exec(head);
  if(m){const l=(m[1]||m[2]||m[4]);return l?l.toLowerCase():zephyr[m[3]];}
  if(/\b(ERROR|FAIL(?:ED)?)\b/i.test(text))return 'e';
  if(/\bWARN/i.test(text))return 'w';
  return '';
}
export function normalizeLevelColors(c){const out={};for(const k of LEVELS){const v=c&&c[k];out[k]=/^#[0-9a-f]{6}$/i.test(v||'')?v.toLowerCase():LEVEL_DEFAULTS[k];}return out;}
export function normalizeLogTheme(t){return t==='dark'?'dark':'light';}

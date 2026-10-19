// J-Link RTT session settings. The native side runs SEGGER's J-Link Commander
// (JLink.exe, default: send + receive over J-Link's RTT Telnet server) or
// JLinkRTTLogger.exe (receive only); this module builds, validates and renders them.
export const RTT_DEFAULTS=Object.freeze({dir:'C:\\Program Files\\SEGGER\\JLink_V794i',device:'nRF54L15_M33',iface:'SWD',speed:4000,channel:0,addrMode:'auto',address:'',ranges:'',serial:'',backend:'commander',telnetPort:19021});
const KEYS=Object.keys(RTT_DEFAULTS);
export function normalizeRTT(r){const out={...RTT_DEFAULTS};for(const k of KEYS)if(r&&r[k]!==undefined&&r[k]!==null)out[k]=r[k];out.speed=Number(out.speed);out.channel=Number(out.channel);out.telnetPort=Number(out.telnetPort)||19021;if(!out.backend)out.backend='commander';return out;}
export const rttReadOnlyOptions=r=>normalizeRTT(r).backend==='logger';
export function validateRTT(r){
  if(!String(r.dir||'').trim())return '请填写 J-Link 软件路径（SEGGER J-Link 安装目录，例如 '+RTT_DEFAULTS.dir+'）。';
  if(r.dir.length>260||/["\r\n\t\0]/.test(r.dir))return 'J-Link 软件路径无效。';
  if(!/^[A-Za-z0-9][A-Za-z0-9_.+/-]{0,63}$/.test(r.device))return '目标芯片名称无效：请填写 J-Link 设备库中的名称，例如 '+RTT_DEFAULTS.device+'。';
  if(!['SWD','JTAG'].includes(r.iface))return '接口只能是 SWD 或 JTAG。';
  if(!Number.isInteger(r.speed)||r.speed<1||r.speed>100000)return '速度应为 1–100000 kHz。';
  if(!Number.isInteger(r.channel)||r.channel<0||r.channel>255)return 'RTT 通道应为 0–255。';
  if(r.addrMode==='address'&&!/^0[xX][0-9a-fA-F]{1,8}$/.test(r.address))return 'RTT 控制块地址应为十六进制，例如 0x20000000。';
  if(r.addrMode==='ranges'&&!/^0[xX][0-9a-fA-F]{1,8} 0[xX][0-9a-fA-F]{1,8}(, ?0[xX][0-9a-fA-F]{1,8} 0[xX][0-9a-fA-F]{1,8}){0,7}$/.test(r.ranges))return 'RTT 搜索范围格式应为“起始地址 长度”，多段用逗号分隔，例如 0x20000000 0x40000。';
  if(!['auto','address','ranges'].includes(r.addrMode))return 'RTT 控制块地址设置无效。';
  if(!['commander','logger'].includes(r.backend))return 'RTT 后端无效。';
  if(!Number.isInteger(r.telnetPort)||r.telnetPort<1024||r.telnetPort>65535)return 'RTT Telnet 端口应为 1024–65535。';
  if(r.serial&&!/^[0-9]{1,12}$/.test(r.serial))return 'J-Link 序列号只能是数字，留空表示自动选择。';
  return '';
}
export function rttSummary(r){return 'J-Link RTT · '+r.device+' · '+r.iface+' '+r.speed+' kHz · 通道 '+r.channel+(r.backend==='logger'?' · 只读':'')+(r.addrMode==='address'?' · 控制块 '+r.address:r.addrMode==='ranges'?' · 搜索范围 '+r.ranges:'');}
export function rttLabel(r,taken=[]){const base='RTT · '+r.device+(r.channel?' · 通道 '+r.channel:'');let label=base,i=2;while(taken.includes(label))label=base+' #'+i++;return label;}
const q=a=>/[\s"]/.test(a)?'"'+a.replace(/"/g,'\\"')+'"':a;
// Mirrors core.RTTOptions.LoggerArgs; the output file is a temporary file chosen at connect time.
export function rttCommand(r){const dir=String(r.dir||'').replace(/[\\/]+$/,'');const a=['-Device',r.device,'-If',r.iface,'-Speed',String(r.speed)];if(r.serial)a.push('-USB',r.serial);if(r.backend==='logger'){if(r.addrMode==='address')a.push('-RTTAddress',r.address);if(r.addrMode==='ranges')a.push('-RTTSearchRanges',r.ranges);a.push('-RTTChannel',String(r.channel));return [q(dir+'\\JLinkRTTLogger.exe'),...a.map(q),'<临时输出文件>'].join(' ');}a.push('-AutoConnect','1','-ExitOnError','1','-NoGui','1','-RTTTelnetPort',String(r.telnetPort||19021),'-CommandFile');return [q(dir+'\\JLink.exe'),...a.map(q),'<临时命令文件>'].join(' ')+' · RTT Telnet 127.0.0.1:'+(r.telnetPort||19021)+(r.addrMode==='address'?' · exec SetRTTAddr '+r.address:r.addrMode==='ranges'?' · exec SetRTTSearchRanges '+r.ranges:'');}
export function rttFieldsHTML(r,esc,disabled=false){const d=disabled?' disabled':'';return '<div class="su-dialog-grid su-rtt-grid">'+
  '<label class="full">J-Link 软件路径<span class="su-path-row"><input data-rtt="dir" maxlength="260" spellcheck="false" placeholder="'+esc(RTT_DEFAULTS.dir)+'" value="'+esc(r.dir)+'"'+d+'><button type="button" data-action="pick-jlink"'+d+'>浏览…</button><button type="button" data-action="check-jlink">检测</button></span></label>'+
  '<label class="full">目标芯片（J-Link 设备名）<input data-rtt="device" maxlength="64" spellcheck="false" list="su-rtt-devices" value="'+esc(r.device)+'"'+d+'><datalist id="su-rtt-devices"><option value="nRF54L15_M33"><option value="nRF54L10_M33"><option value="nRF54L05_M33"><option value="nRF5340_xxAA_APP"><option value="nRF52840_xxAA"><option value="nRF52832_xxAA"></datalist></label>'+
  '<label>接口<select data-rtt="iface"'+d+'><option value="SWD"'+(r.iface==='SWD'?' selected':'')+'>SWD</option><option value="JTAG"'+(r.iface==='JTAG'?' selected':'')+'>JTAG</option></select></label>'+
  '<label>速度（kHz）<input data-rtt="speed" inputmode="numeric" maxlength="6" value="'+esc(String(r.speed))+'"'+d+'></label>'+
  '<label class="full">RTT 后端<select data-rtt="backend"'+d+'><option value="commander"'+(r.backend!=='logger'?' selected':'')+'>J-Link Commander + RTT Telnet（可收发）</option><option value="logger"'+(r.backend==='logger'?' selected':'')+'>JLinkRTTLogger（只读）</option></select></label>'+
  '<label>RTT 通道（收发）<input data-rtt="channel" inputmode="numeric" maxlength="3" value="'+esc(String(r.channel))+'"'+d+'></label>'+
  '<label data-rtt-backend="commander"'+(r.backend==='logger'?' hidden':'')+'>RTT Telnet 端口<input data-rtt="telnetPort" inputmode="numeric" maxlength="5" title="被占用时自动改用下一个空闲端口" value="'+esc(String(r.telnetPort||19021))+'"'+d+'></label>'+
  '<label>J-Link 序列号<input data-rtt="serial" inputmode="numeric" maxlength="12" placeholder="留空自动选择" value="'+esc(r.serial||'')+'"'+d+'></label>'+
  '<label class="full">RTT 控制块地址<select data-rtt="addrMode"'+d+'><option value="auto"'+(r.addrMode==='auto'?' selected':'')+'>自动搜索</option><option value="address"'+(r.addrMode==='address'?' selected':'')+'>指定地址</option><option value="ranges"'+(r.addrMode==='ranges'?' selected':'')+'>指定搜索范围</option></select></label>'+
  '<label class="full" data-rtt-for="address"'+(r.addrMode==='address'?'':' hidden')+'>控制块地址<input data-rtt="address" maxlength="10" spellcheck="false" placeholder="例如 0x20000000" value="'+esc(r.address||'')+'"'+d+'></label>'+
  '<label class="full" data-rtt-for="ranges"'+(r.addrMode==='ranges'?'':' hidden')+'>搜索范围<input data-rtt="ranges" maxlength="200" spellcheck="false" placeholder="起始地址 长度，例如 0x20000000 0x40000" value="'+esc(r.ranges||'')+'"'+d+'></label>'+
  '</div><p class="su-detail" data-jlink-status role="status"></p><p class="su-detail su-rtt-cmd">将运行：<code data-rtt-cmd>'+esc(rttCommand(r))+'</code></p><p class="su-detail" data-rtt-note>'+rttNote(r)+'</p>';}
export function rttNote(r){return r.backend==='logger'?'只读：通过 SEGGER JLinkRTTLogger 接收 RTT 上行通道 '+r.channel+'，与串口数据一样显示、搜索和保存；不能发送。':'可收发：通过 J-Link Commander 连接目标，经 J-Link 的 RTT Telnet 服务接收上行通道 '+r.channel+'；发送区和快捷指令写入 RTT 下行通道 '+r.channel+'（文本/HEX、编码和结尾符与串口相同）。Telnet 端口被占用时自动换用空闲端口。';}
export function readRTTFields(scope){const v=k=>scope.querySelector('[data-rtt="'+k+'"]')?.value??'';const num=k=>{const t=String(v(k)).trim();return /^\d+$/.test(t)?Number(t):NaN;};return {dir:v('dir').trim().replace(/^"(.*)"$/,'$1'),device:v('device').trim(),iface:v('iface'),speed:num('speed'),channel:num('channel'),addrMode:v('addrMode'),address:v('address').trim(),ranges:v('ranges').trim().replace(/\s+/g,' '),serial:v('serial').trim(),backend:v('backend')||'commander',telnetPort:num('telnetPort')};}

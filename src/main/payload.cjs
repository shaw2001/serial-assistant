'use strict';
const iconv = require('iconv-lite');
const MAX_PAYLOAD = 65536;
function payload(config) {
  if (!config || typeof config.content !== 'string') throw new Error('发送内容必须为字符串。');
  if (!['text', 'hex'].includes(config.mode)) throw new Error('无效的发送格式。');
  if (!['utf-8', 'ascii', 'gbk'].includes(config.encoding)) throw new Error('不支持的发送编码。');
  if (!['none', 'cr', 'lf', 'crlf'].includes(config.ending)) throw new Error('无效的结尾符。');
  if (!config.content.length) throw new Error('请输入发送内容。');
  if (config.content.length > MAX_PAYLOAD * 3) throw new Error('单次发送上限为 64 KiB。');
  let data;
  if (config.mode === 'hex') {
    const clean = config.content.replace(/\s/g, '');
    if (!clean) throw new Error('请输入 HEX 字节。');
    if (/[^\da-f]/i.test(clean)) throw new Error('HEX 只能包含 0–9、A–F 和空白。');
    if (clean.length % 2) throw new Error('HEX 字节不完整，请补齐最后一个半字节。');
    data = Buffer.from(clean, 'hex');
  } else {
    if (config.encoding === 'ascii' && /[^\x00-\x7f]/.test(config.content)) throw new Error('ASCII 无法表示中文，请使用 UTF-8 或 GBK。');
    data = iconv.encode(config.content, config.encoding);
    if (config.encoding === 'gbk' && iconv.decode(data, 'gbk') !== config.content) throw new Error('内容包含 GBK 无法表示的字符，请使用 UTF-8。');
  }
  const ending = {none:[], cr:[13], lf:[10], crlf:[13,10]}[config.ending];
  data = Buffer.concat([data, Buffer.from(ending)]);
  if (data.length > MAX_PAYLOAD) throw new Error('单次发送上限为 64 KiB。');
  return data;
}
function serialOptions(input) {
  if (!input || typeof input.path !== 'string' || input.path.length > 256) throw new Error('请输入串口名称。');
  const path = input.path.trim();
  if (!/^(COM\d+|\\\\\.\\COM\d+|\/dev\/[a-zA-Z0-9_./-]+)$/i.test(path) || path.includes('..')) throw new Error('端口名称应为 COM5 或 /dev/ttyUSB0 等系统串口路径。');
  if (!Number.isInteger(input.baudRate) || input.baudRate < 1 || input.baudRate > 12000000) throw new Error('波特率应为 1–12000000 的整数。');
  if (![5,6,7,8].includes(input.dataBits) || ![1,1.5,2].includes(input.stopBits)) throw new Error('串口数据位或停止位无效。');
  if (!['none','even','odd','mark','space'].includes(input.parity) || !['none','rtscts','xonxoff'].includes(input.flow)) throw new Error('串口校验或流控无效。');
  return {path, baudRate:input.baudRate, dataBits:input.dataBits, stopBits:input.stopBits, parity:input.parity, rtscts:input.flow==='rtscts', xon:input.flow==='xonxoff', xoff:input.flow==='xonxoff', autoOpen:false, highWaterMark:65536};
}
module.exports = {payload, serialOptions, MAX_PAYLOAD};

'use strict';
const test=require('node:test'),assert=require('node:assert/strict');
test('cleanText strips ANSI/VT100 and applies backspace echo; plain text untouched',async()=>{
  const {cleanText}=await import('../src/renderer/display.mjs');
  assert.equal(cleanText('plain 中文 ␍␊'),'plain 中文 ␍␊');
  assert.equal(cleanText('\x1b[1;32muart:~$ \x1b[m'),'uart:~$ ');
  assert.equal(cleanText('\x1b[Jhelp\x1b[2K\x1b[8D'),'help');
  assert.equal(cleanText('hel\x08 \x08lo'),'helo');
  assert.equal(cleanText('a\x07b\x00c\td'),'abc\td');
  assert.equal(cleanText('\x1b]0;title\x07msh />'),'msh />');
});

const test=require('node:test'),assert=require('node:assert/strict');
test('RTT prompt parser accepts fragmented colored redraws and never invents commands from unrelated logs',async()=>{
  const {CompletionReply}=await import('../src/renderer/completion.mjs');
  const r=new CompletionReply();const feed=s=>r.feed(Buffer.from(s).toString('base64'));
  feed('background ERROR help\r\nhelp - help\r\nheap - heap\r\nhelp - duplicate\r\n\x1b[32mmsh /');assert.equal(r.result(),null);
  feed('>\x1b[0mhe');assert.deepEqual(r.result(),{line:'he',candidates:['help','heap']});
  const none=new CompletionReply();none.feed(Buffer.from('sensor=20\r\nhelp - unrelated log').toString('base64'));assert.equal(none.result(),null);
  const empty=new CompletionReply();empty.feed(Buffer.from('\nps  help  help\nmsh >').toString('base64'));assert.deepEqual(empty.result(),{line:'',candidates:['ps','help']});
});

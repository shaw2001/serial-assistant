'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('retirement skips absent tags, preserves v0.1.8 and unrelated artifacts, and is repeatable',async()=>{
 const versions=[0,1,2,4,5,6,7].map(n=>'v0.1.'+n),releases=new Map(versions.map((v,i)=>[v,{id:100+i,tag_name:v}])),tags=new Set(versions),deleted=[];
 const artifacts=[{id:1,name:'SerialAssistant-v0.1.7-Standalone-Windows-x64'},{id:2,name:'SerialAssistant-v0.1.8-Standalone-Windows-x64'},{id:3,name:'unrelated-report'}];
 const fixed={id:200,tag_name:'v0.1.8',draft:false,prerelease:false,assets:[{name:'SerialAssistant-0.1.8-win-x64.exe',state:'uploaded'},{name:'SHA256SUMS.txt',state:'uploaded'}]};
 const fetch=async(url,o)=>{
 const path=new URL(url).pathname.split('/serial-assistant/')[1];let status=200,value;
 if(o.method==='GET'){
  if(path==='releases/tags/v0.1.8')value=fixed;
  else if(path.startsWith('releases/tags/')){value=releases.get(path.slice(14));if(!value)status=404}
  else if(path.startsWith('git/ref/tags/')){if(tags.has(path.slice(13)))value={};else status=404}
  else if(path==='actions/artifacts')value={artifacts};
  else if(path==='releases')value=[...releases.values(),fixed];
  else throw Error('unexpected route '+path);
 }else{
  deleted.push(path);status=204;
  if(path.startsWith('releases/')){const found=[...releases].find(([_,r])=>r.id===Number(path.slice(9)));assert(found,'must not delete fixed release');releases.delete(found[0])}
  else if(path.startsWith('git/refs/tags/')){if(!tags.delete(path.slice(14)))status=422}
  else if(path==='actions/artifacts/1'){artifacts.splice(artifacts.findIndex(a=>a.id===1),1)}
  else throw Error('unexpected deletion '+path);
 }
 return{status,ok:status>=200&&status<300,json:async()=>value};
 };
 const code=fs.readFileSync('scripts/retire-vulnerable-releases.cjs','utf8');
 for(let n=0;n<2;n++){
  const process={env:{GH_REPO:'shaw2001/serial-assistant',GH_TOKEN:'audit-dummy'}};
  await vm.runInNewContext(code,{fetch,AbortSignal,process,console:{log(){},error(){}}});assert.equal(process.exitCode,undefined);
 }
 assert.equal(releases.size,0);assert.equal(tags.size,0);assert.equal(deleted.length,15);assert.deepEqual(artifacts.map(a=>a.id),[2,3]);
});

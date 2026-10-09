'use strict';
const fs=require('node:fs'),path=require('node:path'),{spawn}=require('node:child_process');
async function main(){
 const version=require('../package.json').version,dir=path.resolve(__dirname,'../release'),exe=path.join(dir,`SerialAssistant-${version}-win-x64.exe`),report=path.join(dir,'desktop-validation.json');
 fs.rmSync(report,{force:true});fs.rmSync(report+'.trace',{force:true});
 const child=spawn(exe,['--smoke-test',report],{stdio:'ignore',windowsHide:false});
 let timedOut=false;
 const code=await new Promise((resolve,reject)=>{const timer=setTimeout(()=>{timedOut=true;child.kill();resolve(-1)},35000);child.once('error',e=>{clearTimeout(timer);reject(e)});child.once('exit',code=>{clearTimeout(timer);resolve(code)})});
 if(fs.existsSync(report+'.trace')){const lines=fs.readFileSync(report+'.trace','utf8').split('\n');console.log(lines.slice(0,50).join('\n'));if(lines.length>50)console.log(lines.slice(-30).join('\n'))}
 if(timedOut||code!==0||!fs.existsSync(report))throw Error(`Windows security smoke failed: exit=${code}, timeout=${timedOut}`);
 const data=JSON.parse(fs.readFileSync(report,'utf8'));
 if(data.version!==version||!data.fontReady||!data.layoutOK||!data.unauthorizedRejected||!(data.blockedNavigations>=1))throw Error('Windows security, font or layout gate failed');
 console.log('Windows security validation:',JSON.stringify(data));
}
main().catch(e=>{console.error(e.message);process.exitCode=1});

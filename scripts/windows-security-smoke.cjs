'use strict';
const fs=require('node:fs'),path=require('node:path'),{spawn}=require('node:child_process');
const version=require('../package.json').version,dir=path.resolve(__dirname,'../release'),exe=path.join(dir,`SerialAssistant-${version}-win-x64.exe`),report=path.join(dir,'desktop-validation.json');
function trace(){
 if(!fs.existsSync(report+'.trace'))return;
 const fd=fs.openSync(report+'.trace','r');
 try{const buffer=Buffer.alloc(32768),n=fs.readSync(fd,buffer,0,buffer.length,0);console.log(buffer.subarray(0,n).toString('utf8'))}finally{fs.closeSync(fd)}
}
async function main(){
 fs.rmSync(report,{force:true});fs.rmSync(report+'.trace',{force:true});
 console.log('Starting isolated Windows security probe');
 const child=spawn(exe,['--smoke-test',report],{stdio:'ignore',windowsHide:false});
 child.unref();
 const heartbeat=setInterval(()=>{console.log('Waiting for native security probe');trace()},10000);
 const timer=setTimeout(()=>{console.error('Native security probe exceeded 35 seconds');trace();try{child.kill()}finally{process.exit(1)}},35000);
 const code=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve)});
 clearTimeout(timer);clearInterval(heartbeat);trace();
 if(code!==0||!fs.existsSync(report))throw Error(`Windows security smoke failed: exit=${code}`);
 const data=JSON.parse(fs.readFileSync(report,'utf8'));
 if(data.version!==version||!data.fontReady||!data.layoutOK||!data.unauthorizedRejected||!(data.blockedNavigations>=1))throw Error('Windows security, font or layout gate failed');
 console.log('Windows security validation:',JSON.stringify(data));
}
main().then(()=>process.exit(0),e=>{console.error(e.message);process.exit(1)});

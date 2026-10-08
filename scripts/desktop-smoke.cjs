'use strict';
const {spawn}=require('node:child_process');
const path=require('node:path');
const child=spawn(require('electron'),[path.resolve(__dirname,'..'),'--smoke-test'],{stdio:'inherit',windowsHide:false});
const timer=setTimeout(()=>{child.kill();process.exitCode=1;},30000);
child.on('error',error=>{clearTimeout(timer);console.error(error);process.exitCode=1;});
child.on('exit',code=>{clearTimeout(timer);process.exitCode=code===0?0:1;});

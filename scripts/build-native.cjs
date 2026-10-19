'use strict';
const fs=require('node:fs'),path=require('node:path'),{execFileSync}=require('node:child_process');
const root=path.resolve(__dirname,'..'),native=path.join(root,'native');
// One version source: package.json feeds the EXE metadata, the artifact name and the -X main.version flag.
const version=require('../package.json').version,artifactName=`SerialAssistant-${version}-win-x64.exe`;
if(!/^\d+\.\d+\.\d+$/.test(version))throw new Error('package.json version must look like 1.2.3');
execFileSync(process.execPath,['scripts/build.cjs'],{cwd:root,stdio:'inherit'});
for(const name of ['index.html','app.css','app.js','OPPOSans-Regular.woff2'])fs.copyFileSync(path.join(root,'dist',name),path.join(native,'web',name));
fs.copyFileSync(path.join(root,'THIRD_PARTY_NOTICES.md'),path.join(native,'web','THIRD_PARTY_NOTICES.txt'));
fs.mkdirSync(path.join(root,'release'),{recursive:true});
execFileSync('go',['run','github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.5.0','-64','-manifest','cmd/serial-assistant/app.manifest','-file-version',`${version}.0`,'-product-version',version,'-product-name','SerialAssistant','-description','串口助手','-original-name',artifactName,'-propagate-ver-strings','-o','cmd/serial-assistant/resource_windows_amd64.syso','versioninfo.json'],{cwd:native,stdio:'inherit'});
const artifact=path.join(root,'release',artifactName);
execFileSync('go',['build','-buildvcs=false','-trimpath','-ldflags',`-s -w -H windowsgui -X main.version=${version}`,'-o',artifact,'./cmd/serial-assistant'],{cwd:native,stdio:'inherit',env:{...process.env,GOOS:'windows',GOARCH:'amd64',CGO_ENABLED:'0'}});
console.log('Single-file Windows EXE: '+(fs.statSync(artifact).size/1048576).toFixed(2)+' MiB');

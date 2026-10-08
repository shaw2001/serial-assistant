'use strict';
const fs=require('node:fs/promises');
const path=require('node:path');
const crypto=require('node:crypto');
const vm=require('node:vm');
const {build}=require('esbuild');
const FONT_URL='https://code.oppo.com/content/dam/oppo/common/fonts/font2/new-font/OPPOSansOS2-5000-Regular.woff2';
const FONT_HASH='54c935e46cce719f92db964976cbeb5c1da081b42114ef5e8866d64f2e5a19bb';
async function main(){
  const root=path.resolve(__dirname,'..'),font=path.join(root,'assets/OPPOSans-Regular.woff2');
  let data;try{data=await fs.readFile(font);}catch{
    console.log('Downloading the original OPPO Sans webfont from OPPO…');
    const response=await fetch(FONT_URL,{signal:AbortSignal.timeout(120000)});if(!response.ok)throw new Error('OPPO font download failed: '+response.status);data=Buffer.from(await response.arrayBuffer());await fs.mkdir(path.dirname(font),{recursive:true});
  }
  if(crypto.createHash('sha256').update(data).digest('hex')!==FONT_HASH)throw new Error('OPPO Sans checksum differs; review the upstream font before updating the pinned hash.');
  await fs.writeFile(font,data);await fs.mkdir(path.join(root,'dist'),{recursive:true});
  for(const file of await fs.readdir(path.join(root,'src/main')))if(file.endsWith('.cjs'))new vm.Script(await fs.readFile(path.join(root,'src/main',file),'utf8'),{filename:file});
  await build({entryPoints:[path.join(root,'src/renderer/app.mjs')],outfile:path.join(root,'dist/app.js'),bundle:true,platform:'browser',target:'chrome130',minify:true,legalComments:'none'});
  await Promise.all(['index.html','app.css'].map(file=>fs.copyFile(path.join(root,'src/renderer',file),path.join(root,'dist',file))));
  await fs.copyFile(font,path.join(root,'dist/OPPOSans-Regular.woff2'));
  console.log('Built v'+require('../package.json').version+'; original OPPO Sans verified; all runtime assets are local.');
}
main().catch(error=>{console.error(error.message);process.exitCode=1;});

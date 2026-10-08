'use strict';
const {app,BrowserWindow,ipcMain,dialog,shell,Menu}=require('electron');
const path=require('node:path');
const fsp=require('node:fs/promises');
const crypto=require('node:crypto');
const {fileURLToPath}=require('node:url');
const iconv=require('iconv-lite');
const {SerialService}=require('./serial-service.cjs');
const {ConfigStore,validate}=require('./config.cjs');
const {payload}=require('./payload.cjs');
let window,service,store,quitting=false,closing=false,batchInFlight=false;
const smoke=process.argv.includes('--smoke-test');
if(smoke){const os=require('node:os'),fs=require('node:fs');app.setPath('userData',fs.mkdtempSync(path.join(os.tmpdir(),'serial-assistant-smoke-')));}
const index=path.resolve(__dirname,'../../dist/index.html');
function handle(channel,fn){ipcMain.handle(channel,async(event,...args)=>{
  try{if(!window||event.sender!==window.webContents||event.senderFrame!==window.webContents.mainFrame||fileURLToPath(event.senderFrame.url)!==index)throw new Error('不允许的调用来源。');return{ok:true,value:await fn(...args)};}
  catch(error){return{ok:false,error:error.message||String(error)};}
});}
async function saveDocument(options,writer){const result=await dialog.showSaveDialog(window,options);if(result.canceled)return{canceled:true};const temp=result.filePath+'.'+crypto.randomBytes(5).toString('hex')+'.tmp';let file;try{file=await fsp.open(temp,'wx');await writer(file);await file.close();file=null;await fsp.rename(temp,result.filePath);return{canceled:false,path:result.filePath};}catch(error){if(file)await file.close().catch(()=>{});await fsp.unlink(temp).catch(()=>{});throw error;}}
function registerIPC(){
  handle('app:initialize',async()=>({...await store.load(),version:app.getVersion(),platform:process.platform,logsDirectory:service.directory}));
  handle('serial:list',()=>service.list());
  handle('serial:ack',()=>{batchInFlight=false;service.flushEvents();});
  handle('serial:open',(id,config)=>service.open(id,config));
  handle('serial:close',id=>service.close(id));handle('serial:remove',id=>service.remove(id));
  handle('serial:send',(id,config)=>service.send(id,config));
  handle('serial:preview',config=>({bytes:[...payload(config)]}));
  handle('serial:periodic-start',(id,config,interval)=>service.startPeriodic(id,config,interval));
  handle('serial:periodic-stop',id=>service.stopPeriodic(id));
  handle('serial:capture',(id,on)=>service.setCapture(id,on));handle('serial:clear',id=>service.clear(id));
  handle('config:save',config=>store.save(config));
  handle('config:export',config=>{const content=JSON.stringify(validate(config),null,2)+'\n';return saveDocument({title:'导出配置方案',defaultPath:'serial-assistant-profile.json',filters:[{name:'JSON 配置',extensions:['json']}]},f=>f.writeFile(content));});
  handle('config:import',async()=>{const result=await dialog.showOpenDialog(window,{title:'导入配置方案',properties:['openFile'],filters:[{name:'JSON 配置',extensions:['json']}]});if(result.canceled)return null;const file=result.filePaths[0],stat=await fsp.stat(file);if(stat.size>2*1024*1024)throw new Error('配置文件不能超过 2 MiB。');return validate(JSON.parse(await fsp.readFile(file,'utf8')));});
  handle('files:export-log',async options=>{
    const {id,scope,format,encoding='utf-8',ids=[]}=options||{};
    if(!['all','cache','visible'].includes(scope)||!['txt','json','jsonl'].includes(format)||!['utf-8','ascii','gbk'].includes(encoding)||!Array.isArray(ids)||ids.length>10000||!ids.every(Number.isInteger))throw new Error('日志导出参数无效。');
    const s=service.get(id),name=s.config.path.replace(/[^a-z0-9_-]/gi,'_');
    return saveDocument({title:'导出 '+s.config.path+' 日志',defaultPath:'serial-'+name+'-'+new Date().toISOString().slice(0,10)+'.'+format,filters:[{name:'会话日志',extensions:[format]}]},async file=>{
      const meta={schema:1,application:'SerialAssistant',version:app.getVersion(),port:s.config.path,config:s.config,scope,exportedAt:new Date().toISOString()};
      if(format==='json')await file.writeFile(JSON.stringify(meta).slice(0,-1)+',"records":[\n');
      else if(format==='jsonl')await file.writeFile(JSON.stringify({type:'metadata',...meta})+'\n');
      else await file.writeFile('# 串口助手 '+app.getVersion()+' · '+s.config.path+'\n# 主机记录时间；TX 表示已提交驱动，设备应答请查看 RX。\n');
      let first=true,decoder=iconv.getDecoder(encoding);
      for await(const r of service.records(id,scope,ids)){
        if(format==='json'){await file.writeFile((first?'':',\n')+JSON.stringify(r));first=false;}
        else if(format==='jsonl')await file.writeFile(JSON.stringify(r)+'\n');
        else{const data=Buffer.from(r.dataBase64,'base64');const text=r.direction==='SYS'?r.text:r.direction==='RX'?decoder.write(data):iconv.decode(data,encoding);await file.writeFile(r.time+'  '+r.direction+'  '+text.replace(/\0/g,'␀')+(text.endsWith('\n')?'':'\n'));}
      }
      if(format==='json')await file.writeFile('\n]}\n');
      if(format==='txt'){const rest=decoder.end();if(rest)await file.writeFile(rest+'\n');}
    });
  });
  handle('files:show-logs',async()=>{await fsp.mkdir(service.directory,{recursive:true});const error=await shell.openPath(service.directory);if(error)throw new Error(error);});
  handle('app:top',on=>{if(typeof on!=='boolean')throw new Error('无效设置。');window.setAlwaysOnTop(on);});
  handle('app:quit',()=>app.quit());
}
function createWindow(){
  window=new BrowserWindow({width:1220,height:860,minWidth:760,minHeight:620,title:'串口助手 · v'+app.getVersion(),backgroundColor:'#ffffff',webPreferences:{preload:path.join(__dirname,'preload.cjs'),nodeIntegration:false,contextIsolation:true,sandbox:true}});
  Menu.setApplicationMenu(null);window.webContents.setWindowOpenHandler(()=>({action:'deny'}));
  window.webContents.on('will-navigate',event=>event.preventDefault());
  window.webContents.session.setPermissionRequestHandler((_webContents,_permission,callback)=>callback(false));
  window.on('close',event=>{if(quitting)return;event.preventDefault();if(closing)return;closing=true;window.webContents.send('app:closing');setTimeout(()=>app.quit(),2000).unref();});
  window.on('closed',()=>{window=null;});window.loadFile(index);
  if(smoke){
    const timer=setTimeout(()=>{console.error('Desktop smoke test timed out');app.exit(1);},20000);
    window.webContents.on('render-process-gone',(_e,details)=>{console.error(details);app.exit(1);});
    window.webContents.on('did-finish-load',async()=>{
      try{for(let i=0;i<100;i++){const ready=await window.webContents.executeJavaScript("document.querySelectorAll('[role=tab]').length===1 && document.querySelector('[data-el=version]').textContent==='v0.1.0' && document.querySelector('[data-el=connect]').disabled && document.querySelector('[data-el=logs]').textContent.includes('选择串口')");if(ready){clearTimeout(timer);console.log('PASS: native desktop loaded, preload IPC initialized, local renderer booted; v0.1.0');app.exit(0);return;}await new Promise(r=>setTimeout(r,100));}throw new Error('Renderer did not initialize');}catch(error){console.error(error);app.exit(1);}
    });
  }
}
if(!app.requestSingleInstanceLock())app.quit();else{
  app.on('second-instance',()=>{if(window){if(window.isMinimized())window.restore();window.focus();}});
  app.whenReady().then(()=>{const directory=path.join(app.getPath('userData'),'logs');store=new ConfigStore(path.join(app.getPath('userData'),'config.json'));service=new SerialService({directory,canDeliver:()=>!batchInFlight&&window&&!window.isDestroyed(),onBatch:batch=>{if(window&&!window.isDestroyed()){batchInFlight=true;window.webContents.send('serial:batch',batch);}}});registerIPC();createWindow();});
  app.on('before-quit',event=>{if(quitting)return;event.preventDefault();quitting=true;const work=Promise.allSettled([service?.shutdown(),store?.pending]);const timeout=setTimeout(()=>app.exit(0),10000);timeout.unref();work.finally(()=>{clearTimeout(timeout);app.quit();});});
  app.on('window-all-closed',()=>app.quit());
}

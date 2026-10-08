'use strict';
const {contextBridge,ipcRenderer}=require('electron');
const invoke=(channel,...args)=>ipcRenderer.invoke(channel,...args).then(result=>{if(!result.ok)throw new Error(result.error);return result.value;});
const subscribe=(channel,callback)=>{const listener=(_event,data)=>callback(data);ipcRenderer.on(channel,listener);return()=>ipcRenderer.removeListener(channel,listener);};
contextBridge.exposeInMainWorld('serialAPI',Object.freeze({
  initialize:()=>invoke('app:initialize'),
  listPorts:()=>invoke('serial:list'),
  open:(id,options)=>invoke('serial:open',id,options),
  close:id=>invoke('serial:close',id),
  remove:id=>invoke('serial:remove',id),
  send:(id,config)=>invoke('serial:send',id,config),
  preview:config=>invoke('serial:preview',config),
  startPeriodic:(id,config,interval)=>invoke('serial:periodic-start',id,config,interval),
  stopPeriodic:id=>invoke('serial:periodic-stop',id),
  setCapture:(id,on)=>invoke('serial:capture',id,on),
  clear:id=>invoke('serial:clear',id),
  exportLog:options=>invoke('files:export-log',options),
  saveConfig:config=>invoke('config:save',config),
  importConfig:()=>invoke('config:import'),
  exportConfig:config=>invoke('config:export',config),
  showLogs:()=>invoke('files:show-logs'),
  setAlwaysOnTop:on=>invoke('app:top',on),
  quit:()=>invoke('app:quit'),
  onBatch:callback=>subscribe('serial:batch',callback),
  onClosing:callback=>subscribe('app:closing',callback),
  ack:()=>invoke('serial:ack')
}));

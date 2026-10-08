'use strict';
const readline=require('node:readline');
const {SerialService}=require('../src/main/serial-service.cjs');
const service=new SerialService({directory:process.argv[4]});
const options=path=>({path,baudRate:115200,dataBits:8,stopBits:1,parity:'none',flow:'none'});
const reply=value=>process.stdout.write(JSON.stringify(value)+'\n');
async function main(){
  await service.open('session-1',options(process.argv[2]));await service.open('session-2',options(process.argv[3]));reply({ready:true});
  const lines=readline.createInterface({input:process.stdin});
  for await(const line of lines){const c=JSON.parse(line);try{
    if(c.op==='send')reply({result:await service.send(c.id,c.config)});
    else if(c.op==='view'){const s=service.get(c.id);reply({state:service.snapshot(s),rxHex:Buffer.concat(s.records.filter(r=>r.direction==='RX').map(r=>Buffer.from(r.dataBase64,'base64'))).toString('hex')});}
    else if(c.op==='periodic')reply({state:service.startPeriodic(c.id,c.config,c.interval)});
    else if(c.op==='stop'){service.stopPeriodic(c.id);reply({ok:true});}
    else if(c.op==='close')reply({state:await service.close(c.id)});
    else if(c.op==='open')reply({state:await service.open(c.id,options(c.path))});
    else if(c.op==='export'){const records=[];for await(const r of service.records(c.id,'all'))records.push(r);reply({records});}
    else if(c.op==='quit'){await service.shutdown();reply({ok:true});lines.close();break;}
  }catch(e){reply({error:e.message});}}
}
main().catch(async e=>{reply({error:e.message});await service.shutdown();process.exitCode=1;});

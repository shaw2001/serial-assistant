// Parse only an explicit RTT prompt redraw; arbitrary logs are never commands.
export class CompletionReply {
  constructor(encoding='utf-8'){this.decoder=new TextDecoder(encoding==='ascii'?'windows-1252':encoding);this.text='';}
  feed(base64){this.text+=this.decoder.decode(Uint8Array.from(atob(base64),c=>c.charCodeAt(0)),{stream:true});this.text=this.text.slice(-65536);}
  result(){
    const text=this.text.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g,'').replace(/\x08/g,'');
    const lines=text.replace(/\r\n/g,'\n').replace(/\r/g,'\n').split('\n');
    const match=/^(?:msh|finsh|tshell)\s*(?:\/[^>\r\n]*)?>\s?(.*)$/.exec(lines.at(-1));
    if(!match||/[\x00-\x1f\x7f]/.test(match[1]))return null;
    const candidates=[];
    for(const row of lines.slice(0,-1)){
      const command=/^\s*([\w./-]+)\s+-\s+/.exec(row);
      if(command)candidates.push(command[1]);
      else if(/^\s*[\w./-]+(?:\s{2,}[\w./-]+)+\s*$/.test(row))candidates.push(...row.trim().split(/\s+/));
    }
    const prefix=match[1];return{line:prefix,candidates:[...new Set(candidates)].filter(c=>!prefix||c.startsWith(prefix)).slice(0,100)};
  }
}

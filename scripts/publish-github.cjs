'use strict';
// Run locally after `gh auth login`. Credentials remain with the official gh CLI.
const fs=require('node:fs/promises'),path=require('node:path'),os=require('node:os'),{execFileSync}=require('node:child_process');
const repository='shaw2001/serial-assistant',root=path.resolve(__dirname,'..');
const gh=args=>execFileSync('gh',args,{cwd:root,encoding:'utf8',maxBuffer:10*1024*1024});
async function main(){
  gh(['auth','status']);const tmp=await fs.mkdtemp(path.join(os.tmpdir(),'serial-assistant-publish-'));
  async function api(endpoint,method='GET',body){const args=['api',endpoint,'--method',method];if(body){const file=path.join(tmp,'request.json');await fs.writeFile(file,JSON.stringify(body));args.push('--input',file);}return JSON.parse(gh(args));}
  try{
    const user=await api('user');if(user.login!==repository.split('/')[0])throw new Error('Please authenticate gh as '+repository.split('/')[0]);
    const repo=await api('repos/'+repository);if(repo.archived)throw new Error('Repository is archived');const branch=repo.default_branch;
    const ref=await api('repos/'+repository+'/git/ref/heads/'+encodeURIComponent(branch)),parent=ref.object.sha;
    const commit=await api('repos/'+repository+'/git/commits/'+parent);
    const files=[];async function collect(dir){for(const entry of await fs.readdir(path.join(root,dir),{withFileTypes:true})){const relative=path.posix.join(dir,entry.name);if(entry.isDirectory()){if(['node_modules','dist','release','.git','assets'].includes(relative))continue;await collect(relative);}else if(entry.isFile()&&!(dir==='native/web'&&!['assets.go','bridge.js'].includes(entry.name))&&!/\.(log|jsonl|syso|exe|pyc|woff2)$/.test(relative)){files.push({path:relative,mode:'100644',type:'blob',content:await fs.readFile(path.join(root,relative),'utf8')});}}}
    await collect('');
    const tree=await api('repos/'+repository+'/git/trees','POST',{base_tree:commit.tree.sha,tree:files});
    const created=await api('repos/'+repository+'/git/commits','POST',{message:'Implement serial assistant MVP v0.1.2',tree:tree.sha,parents:[parent]});
    await api('repos/'+repository+'/git/refs/heads/'+encodeURIComponent(branch),'PATCH',{sha:created.sha,force:false});
    console.log('Uploaded source commit:',created.sha);
    let run;async function discover(attempts){for(let i=0;i<attempts;i++){await new Promise(r=>setTimeout(r,3000));const runs=JSON.parse(gh(['run','list','--repo',repository,'--workflow','release.yml','--json','databaseId,headSha,status','--limit','10']));run=runs.find(r=>r.headSha===created.sha);if(run)return;}}
    await discover(10);if(!run){gh(['workflow','run','release.yml','--repo',repository,'--ref',branch]);await discover(20);}console.log('Windows Release workflow started.');
    if(!run)throw new Error('Workflow was dispatched but run was not discovered. Check repository Actions.');
    execFileSync('gh',['run','watch',String(run.databaseId),'--repo',repository,'--exit-status'],{cwd:root,stdio:'inherit'});
    const release=await api('repos/'+repository+'/releases/tags/v0.1.2');if(release.draft||release.assets.length<2)throw new Error('Release is incomplete');console.log('Published:',release.html_url);
  }finally{await fs.rm(tmp,{recursive:true,force:true});}
}
main().catch(error=>{console.error(error.message);process.exitCode=1;});

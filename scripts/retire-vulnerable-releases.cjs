'use strict';
// One-time retirement authorized for v0.1.0–v0.1.7. Never delete commit history.
const repository='shaw2001/serial-assistant';
const retired=new Set(['v0.1.0','v0.1.1','v0.1.2','v0.1.3','v0.1.4','v0.1.5','v0.1.6','v0.1.7']);
async function api(route,method='GET',missingOK=false){
 const response=await fetch(`https://api.github.com/repos/${repository}/${route}`,{method,headers:{Authorization:`Bearer ${process.env.GH_TOKEN}`,Accept:'application/vnd.github+json','X-GitHub-Api-Version':'2022-11-28'},signal:AbortSignal.timeout(30000),redirect:'error'});
 if(missingOK&&response.status===404)return null;
 if(!response.ok)throw new Error(`${method} ${route}: HTTP ${response.status}`);
 return response.status===204?null:response.json();
}
async function main(){
 if(process.env.GH_REPO!==repository||!process.env.GH_TOKEN)throw new Error('Expected repository and authenticated GitHub Actions context required');
 const fixed=await api('releases/tags/v0.1.8');
 if(fixed.draft||fixed.prerelease||!fixed.assets.some(a=>a.name==='SerialAssistant-0.1.8-win-x64.exe'&&a.state==='uploaded')||!fixed.assets.some(a=>a.name==='SHA256SUMS.txt'&&a.state==='uploaded'))throw new Error('Verified replacement release is not available');
 for(const tag of retired){const release=await api(`releases/tags/${tag}`,'GET',true);if(release){await api(`releases/${release.id}`,'DELETE');console.log('Removed release and assets:',tag)}await api(`git/refs/tags/${tag}`,'DELETE',true)}
 const artifacts=[];
 for(let page=1;;page++){const data=await api(`actions/artifacts?per_page=100&page=${page}`);artifacts.push(...data.artifacts);if(data.artifacts.length<100)break}
 for(const artifact of artifacts){if(/^SerialAssistant-v?0\.1\.[0-7](?:-|$)/.test(artifact.name)){await api(`actions/artifacts/${artifact.id}`,'DELETE',true);console.log('Removed retired build artifact:',artifact.name)}}
 const remaining=await api('releases?per_page=100');if(remaining.some(r=>retired.has(r.tag_name)))throw new Error('A retired release is still present');
 console.log('Retirement complete; v0.1.8 and source commit history retained.');
}
main().catch(e=>{console.error(e.message);process.exitCode=1});

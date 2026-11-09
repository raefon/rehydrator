import { useEffect, useState } from 'react';
import { recentLogs } from './api';

type Entry = { time: string; level: string; message: string; fields: Record<string,string> };
export default function Logs() {
 const [entries,setEntries]=useState<Entry[]>([]), [scope,setScope]=useState(''), [error,setError]=useState(''), [search,setSearch]=useState(''), [level,setLevel]=useState('all');
 useEffect(()=>{const controller=new AbortController();let timer:ReturnType<typeof setTimeout>;
 async function poll(){try{const result=await recentLogs(controller.signal);if(!controller.signal.aborted){setEntries(result.entries);setScope(result.scope);setError('');}}catch(e){if(!controller.signal.aborted)setError((e as Error).message);}if(!controller.signal.aborted)timer=setTimeout(poll,5000);}
 void poll();return ()=>{controller.abort();clearTimeout(timer);};},[]);
 return <section><div className="sectionhead"><h2>Recent logs</h2><div className="toolbar"><input type="search" aria-label="Search logs" placeholder="Search logs…" value={search} onChange={e=>setSearch(e.target.value)}/><select aria-label="Log level" value={level} onChange={e=>setLevel(e.target.value)}><option value="all">All levels</option><option>INFO</option><option>WARN</option><option>ERROR</option></select></div></div><p className="note">{scope} Updates every 5 seconds. Split deployments show gateway logs here; controller logs remain available through Kubernetes.</p>{error&&<p role="alert">{error}</p>}<div className="tablewrap"><table className="log-table"><thead><tr><th>Time</th><th>Level</th><th>Message</th></tr></thead><tbody>{entries.filter(e=>(level==='all'||e.level===level)&&JSON.stringify(e).toLowerCase().includes(search.toLowerCase())).slice().reverse().map((e,i)=><tr key={`${e.time}-${i}`}><td>{new Date(e.time).toLocaleString()}</td><td>{e.level}</td><td>{e.message}<small style={{display:'block'}}>{Object.entries(e.fields).map(([k,v])=>`${k}=${v}`).join(' · ')}</small></td></tr>)}</tbody></table></div>{!entries.length&&<p className="note">No recent logs.</p>}</section>;
}

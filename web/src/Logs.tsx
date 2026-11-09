import { useEffect, useRef, useState } from 'react';
import { recentLogs, type LogEntry } from './api';

export default function Logs() {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [sources, setSources] = useState<string[]>([]);
  const [scope, setScope] = useState('Connecting to log feed…');
  const [error, setError] = useState('');
  const [loaded, setLoaded] = useState(false);
  const [search, setSearch] = useState('');
  const [source, setSource] = useState('all');
  const [level, setLevel] = useState('all');
  const [frozen, setFrozen] = useState<LogEntry[] | null>(null);
  const [follow, setFollow] = useState(true);
  const feed = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const result = await recentLogs(controller.signal);
        if (!controller.signal.aborted) {
          setEntries(result.entries); setSources(result.sources); setScope(result.scope);
          setError(''); setLoaded(true);
        }
      } catch (e) {
        if (!controller.signal.aborted) setError((e as Error).message);
      }
      if (!controller.signal.aborted) timer = setTimeout(poll, 2000);
    }
    void poll();
    return () => { controller.abort(); clearTimeout(timer); };
  }, []);

  const displayed = frozen ?? entries;
  const filtered = displayed.filter(e =>
    (source === 'all' || e.source === source) && (level === 'all' || e.level === level) &&
    `${e.time} ${e.source} ${e.level} ${e.message} ${Object.entries(e.fields).map(([k,v]) => `${k}=${v}`).join(' ')}`.toLowerCase().includes(search.toLowerCase())
  );
  useEffect(() => {
    if (follow && feed.current) feed.current.scrollTop = feed.current.scrollHeight;
  }, [displayed, follow, source, level, search]);

  return <section aria-label="Operational logs">
    <div className="log-controls">
      <div className="log-sources" role="group" aria-label="Log source">
        {['all', ...Array.from(new Set(['Gateway', 'Core', ...sources]))].map(s =>
          <button key={s} className="secondary" aria-pressed={source === s} onClick={() => setSource(s)}>{s === 'all' ? 'All sources' : s}</button>
        )}
      </div>
      <input type="search" aria-label="Search logs" placeholder="Search message, job, or field…" value={search} onChange={e => setSearch(e.target.value)} />
      <select aria-label="Log level" value={level} onChange={e => setLevel(e.target.value)}>
        <option value="all">All levels</option>{['DEBUG','INFO','WARN','ERROR'].map(l => <option key={l}>{l}</option>)}
      </select>
      <button className="secondary" onClick={() => setFrozen(frozen === null ? entries.slice() : null)}>{frozen === null ? 'Pause' : 'Resume'}</button>
      <span className={`pill ${error ? 'amber' : ''}`} role="status">{error ? 'Disconnected' : frozen !== null ? 'Ⅱ Paused' : loaded ? '● Live' : 'Connecting…'}</span>
    </div>
    {error && <p role="alert">{error} · Retrying automatically. Displaying the last received logs.</p>}
    <div className="log-terminal">
      <div className="log-window-bar"><span><span className="log-window-dots" aria-hidden="true">●●●</span>rehydrator — logs — follow</span><label><input type="checkbox" checked={follow} onChange={e => setFollow(e.target.checked)} /> Follow newest</label></div>
      <div ref={feed} className="log-output" tabIndex={0} aria-label="Log output, oldest first" onScroll={() => {
        const el = feed.current;
        if (el && el.scrollHeight - el.scrollTop - el.clientHeight > 40) setFollow(false);
      }}>
        {filtered.map((e,i) => <div className="log-line" key={`${e.time}-${e.source}-${i}`}><time dateTime={e.time}>{new Date(e.time).toLocaleTimeString(undefined, {hour12:false})}</time>{' '}<span className="log-source">[{e.source}]</span>{' '}<span className={`log-level log-level-${e.level.toLowerCase()}`}>{e.level}</span>{' '}{e.message}<span className="log-fields">{Object.entries(e.fields).map(([k,v]) => `  ${k}=${v}`).join('')}</span></div>)}
        {!filtered.length && <p className="log-empty">{!loaded ? 'Waiting for logs…' : !displayed.length ? 'No recent logs.' : 'No matching logs. Adjust your filters.'}</p>}
      </div>
      <div className="log-window-footer"><span>{filtered.length} entries shown · {frozen !== null ? 'View paused' : 'Updates every 2s'}</span><span>Oldest → newest</span></div>
    </div>
    <p className="note">Reporting: {sources.join(' + ') || 'Connecting…'} · {scope} Core = Rehydrator controller. Combined = Gateway and Core in the same process.</p>
  </section>;
}

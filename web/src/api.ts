import type { Snapshot, RecoveryDetail } from './model';
let authorization = '';
export function login(user: string, password: string) {
  const bytes = new TextEncoder().encode(`${user}:${password}`);
  authorization = `Basic ${btoa(Array.from(bytes, byte => String.fromCharCode(byte)).join(''))}`;
}
export function logout() { authorization = ''; }
async function request(path: string, init: RequestInit = {}) {
  const response = await fetch(`/dashboard/api/${path}`, { ...init, headers: { "X-Rehydrator-UI": "1", ...(authorization ? { Authorization: authorization } : {}), ...init.headers }, cache: 'no-store', redirect: 'error' });
  if (!response.ok) { if (response.status === 401) { const auth = await fetch('/dashboard/api/auth', { cache: 'no-store', signal: init.signal }).then(r => r.json()); if (auth.mode === 'oidc') { window.location.assign('/auth/login'); throw new Error('Redirecting to sign in'); } throw new Error('Authentication required'); } throw new Error((await response.text()).trim() || `Gateway request failed (${response.status})`); }
  return response.json();
}
export function snapshot(signal?: AbortSignal): Promise<Snapshot> { return request('snapshot', { signal }); }
export function addDownload(body: FormData) { return request('jobs', { method: 'POST', body }); }

export function recoveryLookup(id: string, signal?: AbortSignal): Promise<RecoveryDetail> { return request(`recovery/lookup?id=${encodeURIComponent(id)}`, { signal }); }
export function recoveryAction(id: string, action: 'check' | 'rearm'): Promise<{message: string}> { return request(`recovery/${action}?id=${encodeURIComponent(id)}`, { method: 'POST' }); }

export function recentLogs(signal?: AbortSignal): Promise<{entries: {time:string;level:string;message:string;fields:Record<string,string>}[];scope:string}> { return request('logs',{signal}); }
export function pruneFailed(ids:string[]): Promise<{results:{id:string;deleted:boolean;error?:string}[]}> { return request('jobs/prune',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({ids,confirm:true})}); }

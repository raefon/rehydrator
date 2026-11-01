import type { Snapshot } from './model';
let authorization = '';
export function login(user: string, password: string) {
  const bytes = new TextEncoder().encode(`${user}:${password}`);
  authorization = `Basic ${btoa(Array.from(bytes, byte => String.fromCharCode(byte)).join(''))}`;
}
export function logout() { authorization = ''; }
async function request(path: string, init: RequestInit = {}) {
  const response = await fetch(`/dashboard/api/${path}`, { ...init, headers: { "X-Rehydrator-UI": "1", ...(authorization ? { Authorization: authorization } : {}), ...init.headers }, cache: 'no-store', redirect: 'error' });
  if (!response.ok) { if (response.status === 401) { const auth = await fetch('/dashboard/api/auth', { cache: 'no-store', signal: init.signal }).then(r => r.json()); if (auth.mode === 'oidc') { window.location.assign('/auth/login'); throw new Error('Redirecting to sign in'); } throw new Error('Authentication required'); } throw new Error(`Gateway request failed (${response.status})`); }
  return response.json();
}
export function snapshot(signal?: AbortSignal): Promise<Snapshot> { return request('snapshot', { signal }); }
export function addDownload(body: FormData) { return request('jobs', { method: 'POST', body }); }

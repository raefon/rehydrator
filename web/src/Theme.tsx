import { useEffect, useState } from 'react';

type Preference = 'system' | 'light' | 'dark';
const key = 'rehydrator-theme';
function readPreference(): Preference {
  try { const saved = localStorage.getItem(key); if (saved === 'light' || saved === 'dark') return saved; } catch { /* Storage can be unavailable in private/locked-down browsers. */ }
  return 'system';
}
function apply(preference: Preference) {
  const dark = preference === 'dark' || (preference === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', dark ? '#1e1e1e' : '#ffffff');
}
// Apply before React renders to avoid flashing the opposite palette.
export function initializeTheme() { apply(readPreference()); }
export default function Theme() {
  const [preference, setPreference] = useState<Preference>(readPreference);
  useEffect(() => {
    apply(preference);
    const media = matchMedia('(prefers-color-scheme: dark)');
    const changed = () => { if (preference === 'system') apply('system'); };
    media.addEventListener('change', changed);
    return () => media.removeEventListener('change', changed);
  }, [preference]);
  return <label className="theme-picker">Theme<select aria-label="Color theme" value={preference} onChange={e => {
    const next = e.target.value as Preference;
    try { localStorage.setItem(key, next); } catch { /* The in-memory choice still works. */ }
    apply(next); setPreference(next);
  }}><option value="system">System</option><option value="light">Light</option><option value="dark">Dark</option></select></label>;
}

import type { Job } from './model';

type Field = 'name' | 'state' | 'status' | 'protocol' | 'category' | 'id' | 'provider';
const fields = new Set(['name', 'state', 'status', 'protocol', 'category', 'id', 'provider']);
function matches(value: string, pattern: string, exact: boolean): boolean {
  value = value.toLowerCase(); pattern = pattern.toLowerCase();
  if (!pattern.includes('*') && !pattern.includes('?')) return exact ? value === pattern : value.includes(pattern);
  // Glob matching without user-supplied regular expressions or regex backtracking.
  let i = 0, j = 0, star = -1, checkpoint = 0;
  while (i < value.length) {
    if (j < pattern.length && (pattern[j] === '?' || pattern[j] === value[i])) { i++; j++; }
    else if (pattern[j] === '*') { star = j++; checkpoint = i; }
    else if (star !== -1) { j = star + 1; i = ++checkpoint; }
    else return false;
  }
  while (pattern[j] === '*') j++;
  return j === pattern.length;
}
function fieldValue(job: Job, field: Field): string {
  if (field === 'state' || field === 'status') return job.status;
  if (field === 'provider') return !job.provider ? 'Not observed yet' : !job.provider.found
    ? 'Placement absent' : job.provider.state || 'State not supplied';
  return job[field];
}
export function compileDownloadSearch(query: string): { matches: (job: Job) => boolean; error: string } {
  const filters: { field: Field; value: string }[] = [], words: string[] = [];
  const tokens = query.match(/(?:[^\s"]|"[^"]*")+/g) || [];
  if ((query.match(/"/g) || []).length % 2) return { matches: () => false, error: 'Close the quoted search value.' };
  for (const token of tokens) {
    const split = token.indexOf('=');
    if (split < 0) { words.push(token.replace(/"/g, '')); continue; }
    const field = token.slice(0, split).toLowerCase(), value = token.slice(split + 1).replace(/"/g, '');
    if (!fields.has(field)) return { matches: () => false, error: `Unknown filter "${field}". Use name, state, protocol, category, id or provider.` };
    if (!value) return { matches: () => false, error: `Enter a value for ${field}=.` };
    filters.push({ field: field as Field, value });
  }
  const name = words.join(' ');
  return { error: '', matches: job => (!name || matches(job.name, name, false)) &&
    filters.every(filter => matches(fieldValue(job, filter.field), filter.value, filter.field !== 'name')) };
}

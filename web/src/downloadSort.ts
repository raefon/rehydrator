import type { Job } from './model';

export type DownloadSortKey = 'name' | 'protocol' | 'provider' | 'status' | 'attempts' | 'size';
export type SortDirection = 'asc' | 'desc';
export const downloadColumns: { key: DownloadSortKey; label: string }[] = [
  { key: 'name', label: 'Name / destination' },
  { key: 'protocol', label: 'Protocol' },
  { key: 'provider', label: 'TorBox' },
  { key: 'status', label: 'Rehydrator' },
  { key: 'attempts', label: 'Attempts' },
  { key: 'size', label: 'Catalog size' },
];
const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
function value(job: Job, key: DownloadSortKey): string | number {
  switch (key) {
    case 'name': return job.name;
    case 'protocol': return job.protocol;
    case 'provider': return !job.provider ? 'Not observed yet' : !job.provider.found
      ? 'Placement absent' : job.provider.state || 'State not supplied';
    case 'status': return job.status;
    case 'attempts': return job.attempts;
    case 'size': return job.files.reduce((sum, file) => sum + file.size, 0);
  }
}
export function sortDownloads(jobs: Job[], key: DownloadSortKey, direction: SortDirection): Job[] {
  return [...jobs].sort((a, b) => {
    const left = value(a, key), right = value(b, key);
    const compared = typeof left === 'number' && typeof right === 'number'
      ? left - right : collator.compare(String(left), String(right));
    // A fixed tie-break prevents equal states jumping around on refresh.
    return compared ? compared * (direction === 'asc' ? 1 : -1)
      : collator.compare(a.name, b.name) || a.id.localeCompare(b.id);
  });
}

export type Protocol = 'torrent' | 'usenet';
export interface CatalogFile { name: string; size: number; path: string }
export interface Job { id: string; name: string; category: string; protocol: Protocol; status: string; files: CatalogFile[]; hidden: boolean; attempts: number; createdAt: string }
export interface Snapshot { authMode: string; tenant: string; role: string; categories: string[]; jobs: Job[] }

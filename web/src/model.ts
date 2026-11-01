export type Protocol = 'torrent' | 'usenet';
export interface CatalogFile { name: string; size: number; path: string }
export interface Job { provider: ProviderStatus | null; id: string; name: string; category: string; protocol: Protocol; status: string; files: CatalogFile[]; hidden: boolean; attempts: number; createdAt: string }
export interface Snapshot { authMode: string; tenant: string; role: string; categories: string[]; jobs: Job[] }
export interface RecoveryDetail { job: Job; infohash: string; providerId: number; sourceKind: string; sourceRetained: boolean; missingChecks: number; rearmAvailable: boolean; reason: string; database: { nextCheckAt: string; verifyNextAt: string; updatedAt: string; library: {mediaType: string; arrId: number; state: string; path: string}[] } }
export interface ProviderStatus { providerId: number; checkedAt: string; lastAttemptAt: string; checkFailed: boolean; found: boolean; state: string; progress: number | null; speed: number | null; size: number | null; downloaded: number | null; finished: boolean | null; present: boolean | null; cached: boolean | null; ready: boolean }

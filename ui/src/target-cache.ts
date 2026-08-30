import { emptyTargets, normalizeTargets, type ProtocolTargetRecord, type TargetsState } from './model'

const TARGET_CACHE_KEY = 'raylea.subscription-hub.protocol-targets.v1'
const TARGET_CACHE_VERSION = 1
const TARGET_CACHE_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000

interface CachedTargetScope {
  cached_at: number
  items: ProtocolTargetRecord[]
}

interface TargetCacheSnapshot {
  version: number
  groups?: CachedTargetScope
  private_users?: CachedTargetScope
}

export interface TargetCacheStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

export function readCachedProtocolTargets(
  storage: TargetCacheStorage | null = browserStorage(),
  now = Date.now(),
): TargetsState {
  if (!storage) return emptyTargets()
  const snapshot = readSnapshot(storage, now)
  return targetsFromSnapshot(snapshot)
}

export function mergeAndCacheProtocolTargets(
  live: TargetsState,
  storage: TargetCacheStorage | null = browserStorage(),
  now = Date.now(),
): TargetsState {
  if (!storage) return live
  const snapshot = readSnapshot(storage, now)
  const protocolFailed = live.issues.some((issue) => issue.scope === 'protocol')
  const groupsFailed = protocolFailed || live.issues.some((issue) => issue.scope === 'groups')
  const privateUsersFailed = protocolFailed || live.issues.some((issue) => issue.scope === 'private_users')

  if (!groupsFailed) snapshot.groups = { cached_at: now, items: live.groups.map((item) => ({ ...item })) }
  if (!privateUsersFailed) snapshot.private_users = { cached_at: now, items: live.private_users.map((item) => ({ ...item })) }
  writeSnapshot(storage, snapshot)

  const cached = targetsFromSnapshot(snapshot)
  return {
    ...live,
    loaded: live.loaded || cached.loaded,
    groups: groupsFailed ? cached.groups : live.groups,
    private_users: privateUsersFailed ? cached.private_users : live.private_users,
  }
}

function targetsFromSnapshot(snapshot: TargetCacheSnapshot): TargetsState {
  const groups = snapshot.groups?.items ?? []
  const privateUsers = snapshot.private_users?.items ?? []
  const loaded = Boolean(snapshot.groups || snapshot.private_users)
  return {
    loaded,
    available: Boolean(snapshot.groups && snapshot.private_users),
    groups: groups.map((item) => ({ ...item })),
    private_users: privateUsers.map((item) => ({ ...item })),
    issues: [],
  }
}

function readSnapshot(storage: TargetCacheStorage, now: number): TargetCacheSnapshot {
  try {
    const parsed = JSON.parse(storage.getItem(TARGET_CACHE_KEY) || '{}') as Partial<TargetCacheSnapshot>
    if (parsed.version !== TARGET_CACHE_VERSION) return { version: TARGET_CACHE_VERSION }
    return {
      version: TARGET_CACHE_VERSION,
      groups: normalizeScope(parsed.groups, 'group', now),
      private_users: normalizeScope(parsed.private_users, 'private', now),
    }
  } catch {
    return { version: TARGET_CACHE_VERSION }
  }
}

function normalizeScope(value: unknown, targetType: 'group' | 'private', now: number): CachedTargetScope | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  const source = value as Partial<CachedTargetScope>
  const cachedAt = Number(source.cached_at)
  if (!Number.isFinite(cachedAt) || cachedAt <= 0 || now - cachedAt > TARGET_CACHE_MAX_AGE_MS) return undefined
  const normalized = targetType === 'group'
    ? normalizeTargets({ groups: source.items }).groups
    : normalizeTargets({ private_users: source.items }).private_users
  return { cached_at: cachedAt, items: normalized }
}

function writeSnapshot(storage: TargetCacheStorage, snapshot: TargetCacheSnapshot): void {
  try {
    storage.setItem(TARGET_CACHE_KEY, JSON.stringify(snapshot))
  } catch {
    // Live protocol data remains available when browser storage is unavailable.
  }
}

function browserStorage(): TargetCacheStorage | null {
  if (typeof window === 'undefined') return null
  try {
    return window.localStorage
  } catch {
    return null
  }
}

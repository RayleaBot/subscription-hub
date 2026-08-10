const AVATAR_CACHE_KEY = 'raylea.subscription-hub.avatar-cache.v1'
const AVATAR_CACHE_VERSION = 1
const AVATAR_CACHE_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000
const AVATAR_CACHE_MAX_ENTRIES = 160
const AVATAR_CACHE_MAX_CHARS = 4_000_000

interface AvatarCacheEntry {
  data_url: string
  cached_at: number
}

interface AvatarCacheSnapshot {
  version: number
  entries: Record<string, AvatarCacheEntry>
}

export interface AvatarCacheStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

export function readCachedAvatarDataURLs(
  sources: Iterable<string>,
  storage: AvatarCacheStorage | null = browserStorage(),
  now = Date.now(),
): Map<string, string> {
  if (!storage) return new Map()
  const requested = new Set([...sources].filter(isRemoteAvatarSource))
  if (requested.size === 0) return new Map()

  const entries = readValidEntries(storage, now)
  const result = new Map<string, string>()
  for (const source of requested) {
    const entry = entries[source]
    if (entry) result.set(source, entry.data_url)
  }
  return result
}

export function storeAvatarDataURLs(
  values: Iterable<readonly [string, string]>,
  storage: AvatarCacheStorage | null = browserStorage(),
  now = Date.now(),
): void {
  if (!storage) return
  const entries = readValidEntries(storage, now)
  for (const [source, dataURL] of values) {
    if (isRemoteAvatarSource(source) && isAvatarDataURL(dataURL)) {
      entries[source] = { data_url: dataURL, cached_at: now }
    }
  }

  const newestFirst = Object.entries(entries).sort((left, right) => right[1].cached_at - left[1].cached_at)
  const limited = Object.fromEntries(newestFirst.slice(0, AVATAR_CACHE_MAX_ENTRIES))
  let serialized = serialize(limited)
  while (serialized.length > AVATAR_CACHE_MAX_CHARS) {
    const oldest = Object.entries(limited).sort((left, right) => left[1].cached_at - right[1].cached_at)[0]
    if (!oldest) return
    delete limited[oldest[0]]
    serialized = serialize(limited)
  }

  try {
    storage.setItem(AVATAR_CACHE_KEY, serialized)
  } catch {
    // Avatar rendering remains functional when browser storage is unavailable or full.
  }
}

function readValidEntries(storage: AvatarCacheStorage, now: number): Record<string, AvatarCacheEntry> {
  try {
    const parsed = JSON.parse(storage.getItem(AVATAR_CACHE_KEY) || '{}') as Partial<AvatarCacheSnapshot>
    if (parsed.version !== AVATAR_CACHE_VERSION || !parsed.entries || typeof parsed.entries !== 'object') return {}
    const result: Record<string, AvatarCacheEntry> = {}
    for (const [source, value] of Object.entries(parsed.entries)) {
      if (!value || !isRemoteAvatarSource(source) || !isAvatarDataURL(value.data_url)) continue
      const cachedAt = Number(value.cached_at)
      if (!Number.isFinite(cachedAt) || cachedAt <= 0 || now - cachedAt > AVATAR_CACHE_MAX_AGE_MS) continue
      result[source] = { data_url: value.data_url, cached_at: cachedAt }
    }
    return result
  } catch {
    return {}
  }
}

function serialize(entries: Record<string, AvatarCacheEntry>): string {
  return JSON.stringify({ version: AVATAR_CACHE_VERSION, entries } satisfies AvatarCacheSnapshot)
}

function browserStorage(): AvatarCacheStorage | null {
  if (typeof window === 'undefined') return null
  try {
    return window.localStorage
  } catch {
    return null
  }
}

function isRemoteAvatarSource(value: string): boolean {
  return /^https:\/\//i.test(value)
}

function isAvatarDataURL(value: string): boolean {
  return /^data:image\/[a-z0-9.+-]+;base64,/i.test(value)
}

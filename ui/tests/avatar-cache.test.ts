import { describe, expect, it } from 'vitest'

import { readCachedAvatarDataURLs, storeAvatarDataURLs, type AvatarCacheStorage } from '../src/avatar-cache'

class MemoryStorage implements AvatarCacheStorage {
  private readonly values = new Map<string, string>()

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
  }
}

describe('avatar cache', () => {
  it('renders cached avatars immediately and accepts a background refresh', () => {
    const storage = new MemoryStorage()
    const source = 'https://i2.hdslb.com/bfs/face/example.jpg'
    const cached = 'data:image/jpeg;base64,AA=='
    const refreshed = 'data:image/jpeg;base64,BB=='

    storeAvatarDataURLs([[source, cached]], storage, 1_000)
    expect(readCachedAvatarDataURLs([source], storage, 1_001).get(source)).toBe(cached)

    storeAvatarDataURLs([[source, refreshed]], storage, 2_000)
    expect(readCachedAvatarDataURLs([source], storage, 2_001).get(source)).toBe(refreshed)
  })

  it('ignores expired and unrelated entries', () => {
    const storage = new MemoryStorage()
    const source = 'https://q1.qlogo.cn/g?b=qq&nk=10000&s=640'
    storeAvatarDataURLs([[source, 'data:image/png;base64,AA==']], storage, 1_000)

    const eightDaysLater = 1_000 + 8 * 24 * 60 * 60 * 1_000
    expect(readCachedAvatarDataURLs([source], storage, eightDaysLater)).toEqual(new Map())
    expect(readCachedAvatarDataURLs(['https://example.com/other.png'], storage, 1_001)).toEqual(new Map())
  })
})

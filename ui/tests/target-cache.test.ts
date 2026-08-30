import { describe, expect, it } from 'vitest'

import { normalizeTargets } from '../src/model'
import { mergeAndCacheProtocolTargets, readCachedProtocolTargets, type TargetCacheStorage } from '../src/target-cache'

class MemoryStorage implements TargetCacheStorage {
  private readonly values = new Map<string, string>()

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
  }
}

describe('protocol target cache', () => {
  it('serves the last successful group and friend lists on the next page load', () => {
    const storage = new MemoryStorage()
    mergeAndCacheProtocolTargets(normalizeTargets({
      available: true,
      groups: [{ target_id: '200', target_name: '测试群聊' }],
      private_users: [{ target_id: '300', nickname: '测试用户' }],
    }), storage, 1_000)

    const cached = readCachedProtocolTargets(storage, 1_001)

    expect(cached.available).toBe(true)
    expect(cached.groups[0]?.target_name).toBe('测试群聊')
    expect(cached.private_users[0]?.nickname).toBe('测试用户')
  })

  it('refreshes successful scopes and retains a cached scope that timed out', () => {
    const storage = new MemoryStorage()
    mergeAndCacheProtocolTargets(normalizeTargets({
      available: true,
      groups: [{ target_id: '200', target_name: '旧群聊' }],
      private_users: [{ target_id: '300', nickname: '缓存用户' }],
    }), storage, 1_000)

    const merged = mergeAndCacheProtocolTargets(normalizeTargets({
      available: false,
      groups: [{ target_id: '201', target_name: '新群聊' }],
      private_users: [],
      issues: [{ scope: 'private_users', message: '私聊对象列表读取超时' }],
    }), storage, 2_000)

    expect(merged.groups.map((item) => item.target_id)).toEqual(['201'])
    expect(merged.private_users.map((item) => item.target_id)).toEqual(['300'])
    expect(merged.issues[0]?.scope).toBe('private_users')
  })

  it('expires stale target metadata', () => {
    const storage = new MemoryStorage()
    mergeAndCacheProtocolTargets(normalizeTargets({
      available: true,
      groups: [{ target_id: '200', target_name: '测试群聊' }],
      private_users: [],
    }), storage, 1_000)

    const eightDaysLater = 1_000 + 8 * 24 * 60 * 60 * 1_000
    expect(readCachedProtocolTargets(storage, eightDaysLater).loaded).toBe(false)
  })
})

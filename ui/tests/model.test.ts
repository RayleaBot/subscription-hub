import { describe, expect, it } from 'vitest'

import { normalizeSettings, validateSubscription } from '../src/model'

describe('subscription settings model', () => {
  it('drops invalid sources and reports missing targets', () => {
    expect(normalizeSettings({ subscriptions: [{ platform: 'unknown', uid: '1', target_id: '2' }] }).subscriptions).toEqual([])
    const item = normalizeSettings({ subscriptions: [{ platform: 'bilibili', uid: '1', name: 'UP', target_id: '2' }] }).subscriptions[0]
    expect(validateSubscription(item!)).toEqual([])
  })
})

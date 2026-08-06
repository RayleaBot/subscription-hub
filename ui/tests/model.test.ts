import { describe, expect, it } from 'vitest'

import {
  buildIdentityRequests,
  buildRowsFromSettings,
  buildSettingsPayload,
  cloneRow,
  createRowContext,
  emptyTargets,
  normalizeSettings,
  normalizeTargets,
  targetMap,
  validateRows,
} from '../src/model'

describe('subscription settings model', () => {
  it('clones reactive-style row proxies without structured clone failures', () => {
    const settings = normalizeSettings({
      subscriptions: [{ platform: 'bilibili', uid: '100', name: 'UP', target_type: 'group', target_id: '200' }],
    })
    const [row] = buildRowsFromSettings(settings)
    const proxy = new Proxy(row!, {})

    const copy = cloneRow(proxy)

    expect(copy).not.toBe(proxy)
    expect(copy.targets).not.toBe(proxy.targets)
    expect(copy.targets[0]?.services).not.toBe(proxy.targets[0]?.services)
    expect(copy._editSnapshot).toBeNull()
  })

  it('groups targets for one platform account and splits them back on save', () => {
    const settings = normalizeSettings({
      enabled: true,
      subscriptions: [
        { id: 'one', platform: 'bilibili', uid: '100', name: 'UP', target_type: 'group', target_id: '200', services: ['video'] },
        { id: 'two', platform: 'bilibili', uid: '100', name: 'UP', target_type: 'private', target_id: '300', services: ['live'] },
      ],
    })
    const rows = buildRowsFromSettings(settings)
    expect(rows).toHaveLength(1)
    expect(rows[0]?.targets).toHaveLength(2)
    expect(rows[0]?.service_mode).toBe('mixed')

    const targets = normalizeTargets({
      available: true,
      groups: [{ target_id: '200', target_name: '测试群' }],
      private_users: [{ target_id: '300', nickname: '测试用户' }],
    })
    const payload = buildSettingsPayload(settings, rows, targetMap(targets))
    expect(payload.subscriptions.map((item) => item.id)).toEqual(['one', 'two'])
    expect(payload.subscriptions.map((item) => item.target_name)).toEqual(['测试群', '测试用户'])
  })

  it('keeps equal subject IDs separate across platforms', () => {
    const rows = buildRowsFromSettings(normalizeSettings({
      subscriptions: [
        { platform: 'bilibili', uid: '100', name: 'UP', target_type: 'group', target_id: '200' },
        { platform: 'weibo', uid: '100', name: '博主', target_type: 'group', target_id: '200' },
      ],
    }))
    expect(rows.map((row) => row.platform)).toEqual(['bilibili', 'weibo'])
  })

  it('keeps saved targets usable when the live protocol target list is unavailable', () => {
    const settings = normalizeSettings({
      subscriptions: [{ platform: 'bilibili', uid: '100', name: 'UP', target_type: 'group', target_id: '200' }],
    })
    const rows = buildRowsFromSettings(settings)
    const context = createRowContext(emptyTargets(), rows, new Map())
    expect(context.targetsLoaded).toBe(true)
    expect(validateRows(rows, context)).toEqual([])
  })

  it('builds one identity request per target and subscriber pair', () => {
    const settings = normalizeSettings({
      subscriptions: [{
        platform: 'bilibili', uid: '100', name: 'UP', target_type: 'group', target_id: '200', subscribers: [{ id: '300' }, { id: '300' }],
      }],
    })
    expect(buildIdentityRequests(buildRowsFromSettings(settings))).toEqual([
      { target_type: 'group', target_id: '200', user_id: '300' },
    ])
  })
})

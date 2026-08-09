import { describe, expect, it } from 'vitest'

import {
  buildIdentityRequests,
  buildRowsFromSettings,
  buildSettingsPayload,
  cloneRow,
  collectSubscriberIdentities,
  createRowContext,
  displayAvatarURL,
  emptyTargets,
  identityKey,
  normalizeSettings,
  normalizeTargets,
  targetMap,
  validateRows,
  validateSettings,
} from '../src/model'

describe('subscription settings model', () => {
  it('defaults delivery freshness to 30 minutes and preserves valid changes', () => {
    expect(normalizeSettings({}).delivery_max_age_minutes).toBe(30)
    const settings = normalizeSettings({ delivery_max_age_minutes: 90 })
    expect(settings.delivery_max_age_minutes).toBe(90)
    expect(validateSettings(settings)).toEqual([])
    expect(buildSettingsPayload(settings, [], new Map()).delivery_max_age_minutes).toBe(90)
    expect(validateSettings({ delivery_max_age_minutes: 0 })).not.toEqual([])
    expect(validateSettings({ delivery_max_age_minutes: 1441 })).not.toEqual([])
  })

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

  it('preserves resolved subscriber identity metadata when settings are saved', () => {
    const settings = normalizeSettings({
      enabled: true,
      subscriptions: [{
        platform: 'bilibili',
        uid: '100',
        name: 'UP',
        target_type: 'group',
        target_id: '200',
        subscribers: [{
          id: '300',
          nickname: 'subscriber',
          group_nickname: 'group subscriber',
          role: 'member',
          role_label: 'Member',
          avatar_url: 'https://q1.qlogo.cn/g?b=qq&nk=300&s=640',
        }],
      }],
    })
    const identities = collectSubscriberIdentities(settings)

    expect(identities.get(identityKey('group', '200', '300'))).toMatchObject({
      id: '300',
      nickname: 'subscriber',
      group_nickname: 'group subscriber',
      role: 'member',
      role_label: 'Member',
    })
    expect(buildSettingsPayload(settings, buildRowsFromSettings(settings), new Map(), identities).subscriptions[0]?.subscribers[0]).toMatchObject({
      id: '300',
      nickname: 'subscriber',
      group_nickname: 'group subscriber',
      role: 'member',
      role_label: 'Member',
    })
  })

  it('only exposes CSP-safe avatar URLs to image elements', () => {
    const source = 'https://i2.hdslb.com/bfs/face/example.jpg'
    expect(displayAvatarURL(source, new Map())).toBe('')
    expect(displayAvatarURL(source, new Map([[source, 'data:image/jpeg;base64,AA==']]))).toBe('data:image/jpeg;base64,AA==')
    expect(displayAvatarURL('data:image/png;base64,AA==', new Map())).toBe('data:image/png;base64,AA==')
  })
})

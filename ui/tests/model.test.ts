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
  identityValue,
  normalizeSettings,
  normalizeResolverSettings,
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

  it('defaults parsing off per target and enables only explicitly configured platforms', () => {
    const defaults = normalizeSettings({})
    expect(defaults.resolver.targets).toEqual([])
    expect(defaults.resolver.cooldowns).toMatchObject({ same_link_enabled: true, same_link_seconds: 10, same_platform_enabled: false })
    expect(defaults.resolver.media).toMatchObject({ live_record_seconds: 30, video_size_limit_mb: 70, media_concurrency: 1 })

    const settings = normalizeSettings({
      resolver: {
        targets: [{ target_type: 'group', target_id: '200', target_name: '测试群', bilibili: true }],
      },
    })
    expect(settings.resolver.targets).toEqual([{
      target_type: 'group', target_id: '200', target_name: '测试群', bilibili: true, weibo: false, douyin: false,
    }])
    expect(buildSettingsPayload(settings, [], new Map()).resolver.targets[0]?.bilibili).toBe(true)
  })

  it('normalizes resolver strategy values to supported bounds', () => {
    const resolver = normalizeResolverSettings({
      cooldowns: { same_link_seconds: 0, same_platform_seconds: 9000 },
      media: { live_record_seconds: 500, media_concurrency: 0, video_codec: 'vp9', image_forward_threshold: -1 },
    })
    expect(resolver.cooldowns.same_link_seconds).toBe(1)
    expect(resolver.cooldowns.same_platform_seconds).toBe(3600)
    expect(resolver.media.live_record_seconds).toBe(50)
    expect(resolver.media.media_concurrency).toBe(1)
    expect(resolver.media.video_codec).toBe('auto')
    expect(resolver.media.image_forward_threshold).toBe(0)
  })

  it('clones reactive-style resolver proxies without structured clone failures', () => {
    const source = normalizeResolverSettings({
      targets: [{ target_type: 'group', target_id: '200', bilibili: true }],
      cooldowns: { same_link_seconds: 20 },
    })
    const proxy = new Proxy(source, {})

    const copy = normalizeResolverSettings(proxy)

    expect(copy).not.toBe(proxy)
    expect(copy.targets).not.toBe(proxy.targets)
    expect(copy).toEqual(source)
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

  it('prefers douyin unique_id as the display identity and persists it', () => {
    const settings = normalizeSettings({
      subscriptions: [{
        platform: 'douyin', uid: 'MS4wLjABAAAAone', unique_id: 'douyin_id', name: '测试用户', target_type: 'group', target_id: '200',
      }],
    })
    const [row] = buildRowsFromSettings(settings)
    expect(row?.unique_id).toBe('douyin_id')
    expect(identityValue(row!)).toBe('douyin_id')
    const payload = buildSettingsPayload(settings, [row!], new Map())
    expect(payload.subscriptions[0]?.unique_id).toBe('douyin_id')
  })

  it('falls back to sec_uid when douyin unique_id is absent', () => {
    const settings = normalizeSettings({
      subscriptions: [{ platform: 'douyin', uid: 'MS4wLjABAAAAone', name: '测试用户', target_type: 'group', target_id: '200' }],
    })
    const [row] = buildRowsFromSettings(settings)
    expect(row?.unique_id).toBe('')
    expect(identityValue(row!)).toBe('MS4wLjABAAAAone')
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
          base_role: 'member',
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
      base_role: 'member',
      role: 'member',
      role_label: 'Member',
    })
    expect(buildSettingsPayload(settings, buildRowsFromSettings(settings), new Map(), identities).subscriptions[0]?.subscribers[0]).toMatchObject({
      id: '300',
      nickname: 'subscriber',
      group_nickname: 'group subscriber',
      base_role: 'member',
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

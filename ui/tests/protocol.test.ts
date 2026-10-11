import { describe, expect, it, vi } from 'vitest'
import { loadProtocolTargets, resolveProtocolIdentities } from '../src/protocol'
import { allTargets, buildRowsFromSettings, buildSettingsPayload, normalizeSettings, normalizeTargets, targetMap } from '../src/model'
import type { PluginUIClient } from '@rayleabot/plugin-ui'

describe('adapter scoped subscription targets', () => {
  it('keeps the same group on two bots separate through save', async () => {
    const apiRequest = vi.fn(async (_method: string, path: string) => path === '/api/adapters'
      ? { adapters: [{ id: 'bot-a', protocol: 'onebot11', enabled: true }, { id: 'bot-b', protocol: 'onebot11', enabled: true }] }
      : { available: true, groups: [{ target_id: '10001', target_name: 'Group' }], private_users: [], issues: [] })
    const client = { apiRequest } as unknown as Pick<PluginUIClient, 'apiRequest'>
    const targets = normalizeTargets(await loadProtocolTargets(client))
    expect(allTargets(targets).map((target) => target.key)).toEqual(['bot-a:group:10001', 'bot-b:group:10001'])
    const settings = normalizeSettings({ subscriptions: ['bot-a', 'bot-b'].map((source_adapter) => ({
      id: source_adapter, platform: 'weibo', uid: '42', name: 'Fixture', source_adapter, source_protocol: 'onebot11',
      target_type: 'group', target_id: '10001', services: ['all'], subscribers: [],
    })) })
    const saved = buildSettingsPayload(settings, buildRowsFromSettings(settings), targetMap(targets))
    expect(saved.subscriptions.map(({ id, source_adapter }) => ({ id, source_adapter }))).toEqual([
      { id: 'bot-a', source_adapter: 'bot-a' }, { id: 'bot-b', source_adapter: 'bot-b' },
    ])
  })

  it('queries members only through the stored bot', async () => {
    const apiRequest = vi.fn(async () => ({ items: [{ target_type: 'group', target_id: '10001', user_id: '20002', nickname: 'Member' }], issues: [] }))
    const client = { apiRequest } as unknown as Pick<PluginUIClient, 'apiRequest'>
    const response = await resolveProtocolIdentities(client, [{ source_adapter: 'bot-b', source_protocol: 'onebot11', target_type: 'group', target_id: '10001', user_id: '20002' }])
    expect(apiRequest).toHaveBeenCalledExactlyOnceWith('POST', '/api/adapters/bot-b/onebot11/identities/resolve', { items: [{ target_type: 'group', target_id: '10001', user_id: '20002' }] })
    expect(response.items[0]?.source_adapter).toBe('bot-b')
  })

  it('rejects saved subscriptions without a route', () => {
    expect(() => normalizeSettings({ subscriptions: [{ platform: 'weibo', uid: '42', target_type: 'group', target_id: '10001' }] })).toThrow()
  })
})

import { describe, expect, it, vi } from 'vitest'
import { loadProtocolTargets, resolveProtocolIdentities } from '../src/protocol'
import { allTargets, buildIdentityRequests, buildRowsFromSettings, buildSettingsPayload, changeTargetBot, createRowContext, emptyTargets, normalizeSettings, normalizeTargets, targetMap, validateRows } from '../src/model'
import type { PluginUIClient } from '@rayleabot/plugin-ui'

describe('account scoped subscription targets', () => {
  it('deduplicates instances of the same account and resolves its current transport after replacement', async () => {
    let adapters = [
      { id: 'old-a', protocol: 'onebot11', enabled: true, state: 'connected', identity: { id: '1001', name: 'Bot A' } },
      { id: 'alias-a', protocol: 'onebot11', enabled: true, state: 'connected', identity: { id: '1001', name: 'Bot A' } },
      { id: 'bot-b', protocol: 'onebot11', enabled: true, state: 'connected', identity: { id: '1002', name: 'Bot B' } },
    ]
    const apiRequest = vi.fn(async (method: string, path: string) => {
      if (path === '/api/adapters') return { adapters }
      if (method === 'POST') return { items: [{ target_type: 'group', target_id: '10001', user_id: '20002', nickname: 'Member' }], issues: [] }
      return { available: true, groups: [{ target_id: '10001', target_name: 'Group' }], private_users: [], issues: [] }
    })
    const client = { apiRequest } as unknown as Pick<PluginUIClient, 'apiRequest'>
    const targets = normalizeTargets(await loadProtocolTargets(client))
    expect(targets.bots.map((bot) => bot.bot_id)).toEqual(['1001', '1002'])
    expect(allTargets(targets).map((target) => target.key)).toEqual(['onebot11:1001:group:10001', 'onebot11:1002:group:10001'])
    expect(apiRequest).not.toHaveBeenCalledWith('GET', '/api/adapters/alias-a/onebot11/targets')
    const settings = normalizeSettings({ subscriptions: ['1001', '1002'].map((bot_id) => ({
      id: ` legacy-${bot_id} `, platform: 'weibo', uid: '42', name: 'Fixture', source_adapter: 'obsolete', bot_id, source_protocol: 'onebot11',
      target_type: 'group', target_id: '10001', services: ['all'], subscribers: [],
    })) })
    const saved = buildSettingsPayload(settings, buildRowsFromSettings(settings), targetMap(targets))
    expect(saved.subscriptions.map(({ id, bot_id }) => ({ id, bot_id }))).toEqual([
      { id: ' legacy-1001 ', bot_id: '1001' }, { id: ' legacy-1002 ', bot_id: '1002' },
    ])
    expect(JSON.stringify(saved)).not.toContain('source_adapter')
    adapters = [{ ...adapters[0]!, id: 'replacement' }]
    const response = await resolveProtocolIdentities(client, [{ bot_id: '1001', source_protocol: 'onebot11', target_type: 'group', target_id: '10001', user_id: '20002' }])
    expect(apiRequest).toHaveBeenLastCalledWith('POST', '/api/adapters/replacement/onebot11/identities/resolve', { items: [{ target_type: 'group', target_id: '10001', user_id: '20002' }] })
    expect(response.items[0]?.bot_id).toBe('1001')
  })

  it('preserves pending legacy records through save and rebinds without changing their IDs', () => {
    const settings = normalizeSettings({ subscriptions: ['old-a', 'old-b'].map((id) => ({
      id, source_adapter: id, source_protocol: 'onebot11', platform: 'weibo', uid: '42', target_type: 'group', target_id: '10001',
      enabled: id === 'old-a', services: id === 'old-a' ? ['video'] : ['post'], subscribers: [{ id: '300', nickname: id }],
    })), resolver: { targets: ['old-a', 'old-b'].map((source_adapter) => ({ source_adapter, source_protocol: 'onebot11', target_type: 'group', target_id: '10001', bilibili: true })) } })
    const rows = buildRowsFromSettings(settings)
    expect(rows[0]!.targets[0]!.key).not.toBe(rows[0]!.targets[1]!.key)
    expect(validateRows(rows, createRowContext(emptyTargets(), rows, new Map()))).toEqual([])
    expect(buildIdentityRequests(rows)).toEqual([])
    const saved = buildSettingsPayload(settings, rows, new Map())
    expect(saved.subscriptions).toHaveLength(2)
    expect(saved.subscriptions).toEqual(settings.subscriptions)
    expect(saved.resolver.targets).toHaveLength(2)
    expect(saved.subscriptions.every((item) => item.needs_bot_selection)).toBe(true)
    changeTargetBot(rows[0]!.targets[0]!, { source_protocol: 'onebot11', bot_id: '1001' })
    const rebound = buildSettingsPayload(settings, rows, new Map())
    expect(rebound.subscriptions[0]).toMatchObject({ id: 'old-a', bot_id: '1001', enabled: true })
    expect(rebound.subscriptions[0]?.needs_bot_selection).toBeUndefined()
    expect(rebound.subscriptions[1]?.needs_bot_selection).toBe(true)
    expect(JSON.stringify(rebound)).not.toContain('source_adapter')
  })
})

import { describe, expect, it, vi } from 'vitest'
import { loadProtocolTargets, resolveProtocolIdentities } from '../src/protocol'
import { allTargets, buildIdentityRequests, buildRowsFromSettings, buildSettingsPayload, changeTargetBot, createRowContext, emptyTargets, normalizeSettings, normalizeTargets, targetMap, validateRows } from '../src/model'
import type { BotAccount, PluginUIClient } from '@rayleabot/plugin-ui'

describe('account scoped subscription targets', () => {
  it('queries only the selected account and refreshes its current connection', async () => {
    let bot: BotAccount = { source_protocol: 'onebot11', bot_id: '1001', nickname: 'Bot A', state: 'online', adapter_id: 'old-a', last_seen_at: '' }
    const apiRequest = vi.fn(async (method: string) => {
      if (method === 'POST') return { items: [{ target_type: 'group', target_id: '10001', user_id: '20002', nickname: 'Member' }], issues: [] }
      return { available: true, groups: [{ target_id: '10001', target_name: 'Group' }], private_users: [], issues: [] }
    })
    const host = { client: { apiRequest } as unknown as PluginUIClient, refreshBot: vi.fn(async () => bot) }
    const targets = normalizeTargets(await loadProtocolTargets(host))
    expect(targets.bots.map((account) => account.bot_id)).toEqual(['1001'])
    expect(allTargets(targets).map((target) => target.key)).toEqual(['onebot11:1001:group:10001'])
    expect(apiRequest).toHaveBeenCalledExactlyOnceWith('GET', '/api/adapters/old-a/onebot11/targets')
    expect(host.refreshBot).toHaveBeenCalledTimes(2)
    bot = { ...bot, adapter_id: 'replacement' }
    const response = await resolveProtocolIdentities(host, [{ bot_id: '1001', source_protocol: 'onebot11', target_type: 'group', target_id: '10001', user_id: '20002' }])
    expect(apiRequest).toHaveBeenLastCalledWith('POST', '/api/adapters/replacement/onebot11/identities/resolve', { items: [{ target_type: 'group', target_id: '10001', user_id: '20002' }] })
    expect(response.items[0]?.bot_id).toBe('1001')
    await expect(resolveProtocolIdentities(host, [{ bot_id: '1002', source_protocol: 'onebot11', target_type: 'group', target_id: '10001', user_id: '20002' }])).rejects.toThrow('当前机器人账号')
  })

  it('rejects offline queries and discards results when the connection changes', async () => {
    const bot: BotAccount = { source_protocol: 'onebot11', bot_id: '1001', state: 'offline', last_seen_at: '' }
    const apiRequest = vi.fn(async () => ({ available: true, groups: [], private_users: [], issues: [] }))
    const refreshBot = vi.fn(async () => bot)
    const host = { client: { apiRequest } as unknown as PluginUIClient, refreshBot }
    await expect(loadProtocolTargets(host)).rejects.toThrow('账号离线')
    expect(apiRequest).not.toHaveBeenCalled()
    refreshBot.mockResolvedValueOnce({ ...bot, state: 'online', adapter_id: 'a' }).mockResolvedValueOnce({ ...bot, state: 'online', adapter_id: 'b' })
    await expect(loadProtocolTargets(host)).rejects.toThrow('连接已变化')
    refreshBot.mockResolvedValue({ ...bot, source_protocol: 'qqofficial' })
    await expect(loadProtocolTargets(host)).rejects.toThrow('OneBot11')
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

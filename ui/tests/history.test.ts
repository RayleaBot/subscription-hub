import { describe, expect, it, vi } from 'vitest'
import { PluginUIError, type PluginUIClient } from '@rayleabot/plugin-ui'
import { canAssignHistory, loadAssignmentAccounts, loadHistory, type HistoryRecord } from '../src/history'

function record(id: string, bot_id: string, source_protocol = 'onebot11'): HistoryRecord {
  return { id, kind: 'subscription', bot_id, source_protocol, label: id, target_type: 'group', target_id: '200', record: {} as HistoryRecord['record'] }
}

describe('global history directory checks', () => {
  it('uses single-account lookups and only treats 404 as absent', async () => {
    const invokeAction = vi.fn(async () => ({ total: 5, items: [record('missing', ''), record('qq', '', 'qqofficial'), record('a', 'a'), record('b', 'a'), record('removed', 'removed')] }))
    const apiRequest = vi.fn(async (_: string, path: string) => {
      if (path.endsWith('/removed')) throw new PluginUIError('platform.resource_not_found', 'missing', 404)
      return { bot_id: 'a' }
    })
    const client = { invokeAction, apiRequest } as unknown as PluginUIClient
    const result = await loadHistory(client)
    expect(result.total).toBe(5)
    expect(result.items.map(({ id, state }) => ({ id, state }))).toEqual([{ id: 'missing', state: 'missing' }, { id: 'qq', state: 'missing' }, { id: 'removed', state: 'removed' }])
    expect(result.items.map(canAssignHistory)).toEqual([true, false, false])
    expect(apiRequest).toHaveBeenCalledTimes(2)
    expect(apiRequest).toHaveBeenCalledWith('GET', '/api/bot-accounts/onebot11/a')
    apiRequest.mockRejectedValue(new PluginUIError('platform.internal_error', 'unavailable', 500))
    await expect(loadHistory(client)).rejects.toThrow('unavailable')
  })

  it('loads every directory page, including accounts without a connection', async () => {
    const apiRequest = vi.fn().mockResolvedValueOnce({ items: [{ bot_id: 'a', state: 'offline' }], next_cursor: '100' }).mockResolvedValueOnce({ items: [{ bot_id: 'b', state: 'no_connection' }] })
    const result = await loadAssignmentAccounts({ apiRequest } as unknown as PluginUIClient)
    expect(result.map(bot => bot.bot_id)).toEqual(['a', 'b'])
    expect(apiRequest).toHaveBeenLastCalledWith('GET', '/api/bot-accounts?source_protocol=onebot11&limit=100&cursor=100')
  })
})

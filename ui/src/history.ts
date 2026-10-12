import { PluginUIError, type BotAccount, type PluginUIClient } from '@rayleabot/plugin-ui'
import { botKey, type ResolverTargetSettings, type Subscription } from './model'

type HistoryClient = Pick<PluginUIClient, 'apiRequest' | 'invokeAction'>
export interface HistoryRecord {
  kind: 'subscription' | 'resolver'
  id: string
  source_protocol: string
  bot_id: string
  source_adapter?: string
  label: string
  target_type: string
  target_id: string
  record: Subscription | ResolverTargetSettings
}
export interface HistoryResponse { items: HistoryRecord[]; total: number }
export type HistoryEntry = HistoryRecord & { state: 'missing' | 'removed' | 'unsupported' }

export function accountPath(protocol: string, id: string) {
  return `/api/bot-accounts/${encodeURIComponent(protocol)}/${encodeURIComponent(id)}`
}

export async function loadHistory(client: HistoryClient): Promise<{ items: HistoryEntry[]; total: number }> {
  const result = await client.invokeAction('history.list', {}) as unknown as HistoryResponse
  const accounts = new Map<string, Promise<boolean>>()
  const items = await Promise.all(result.items.map(async (item): Promise<HistoryEntry | null> => {
    if (!item.source_protocol || !item.bot_id) return { ...item, state: 'missing' }
    const key = botKey(item)
    if (!accounts.has(key)) {
      accounts.set(key, client.apiRequest('GET', accountPath(item.source_protocol, item.bot_id))
        .then(() => true)
        .catch((error: unknown) => {
          if (error instanceof PluginUIError && error.status === 404) return false
          throw error
        }))
    }
    if (!await accounts.get(key)) return { ...item, state: 'removed' }
    return item.source_protocol !== 'onebot11' ? { ...item, state: 'unsupported' } : null
  }))
  return { items: items.filter((item): item is HistoryEntry => item !== null), total: result.total }
}

export async function loadAssignmentAccounts(client: HistoryClient): Promise<BotAccount[]> {
  const items: BotAccount[] = []
  let cursor = ''
  do {
    const result = await client.apiRequest<{ items: BotAccount[]; next_cursor?: string }>('GET', `/api/bot-accounts?source_protocol=onebot11&limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`)
    items.push(...result.items)
    cursor = result.next_cursor ?? ''
  } while (cursor)
  return items
}

export function canAssignHistory(item: HistoryEntry) {
  return item.source_protocol === 'onebot11' && !item.bot_id
}

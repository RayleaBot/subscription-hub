import type { PluginUIClient } from '@rayleabot/plugin-ui'

import { botKey, identityKey, type BotAccount, type IdentityResolveItem, type IdentityResolveResponse, type ProtocolTargetRecord } from './model'

type ManagementClient = Pick<PluginUIClient, 'apiRequest'>

interface AdapterDescriptor {
  id: string
  protocol: string
  enabled: boolean
  state: string
  identity?: { id: string; name: string }
}

interface ProtocolTargetsResponse {
  bots: BotAccount[]
  available: boolean
  groups: ProtocolTargetRecord[]
  private_users: ProtocolTargetRecord[]
  issues: IdentityResolveResponse['issues']
}

type WireTargetsResponse = Omit<ProtocolTargetsResponse, 'bots' | 'groups' | 'private_users'> & {
  groups: Omit<ProtocolTargetRecord, 'bot_id' | 'source_protocol'>[]
  private_users: Omit<ProtocolTargetRecord, 'bot_id' | 'source_protocol'>[]
}
type WireIdentityResponse = Omit<IdentityResolveResponse, 'items'> & {
  items: Omit<IdentityResolveItem, 'bot_id' | 'source_protocol'>[]
}

const identityBatchSize = 100

function adapterPath(adapterID: string, suffix: string) {
  return '/api/adapters/' + encodeURIComponent(adapterID) + '/onebot11/' + suffix
}

// The host directory API still uses instances. Resolve its temporary transport
// on every fetch; never save it as a target or identity key.
async function accountTransports(client: ManagementClient): Promise<Map<string, AdapterDescriptor>> {
  const { adapters } = await client.apiRequest<{ adapters: AdapterDescriptor[] }>('GET', '/api/adapters')
  const accounts = new Map<string, AdapterDescriptor>()
  for (const adapter of adapters) {
    if (adapter.protocol !== 'onebot11' || !adapter.enabled || !adapter.identity?.id) continue
    const key = botKey({ source_protocol: adapter.protocol, bot_id: adapter.identity.id })
    const previous = accounts.get(key)
    if (!previous || (previous.state !== 'connected' && adapter.state === 'connected')) accounts.set(key, adapter)
  }
  return accounts
}

function botAccount(adapter: AdapterDescriptor): BotAccount {
  return { source_protocol: adapter.protocol, bot_id: adapter.identity!.id, bot_name: adapter.identity!.name }
}

export async function loadProtocolTargets(client: ManagementClient): Promise<ProtocolTargetsResponse> {
  const accounts = [...(await accountTransports(client)).values()]
  const merged: ProtocolTargetsResponse = { bots: accounts.map(botAccount), available: false, groups: [], private_users: [], issues: [] }
  if (accounts.length === 0) {
    merged.issues.push({ scope: 'protocol', message: '暂无可用机器人' })
    return merged
  }
  const responses = await Promise.all(accounts.map(async (adapter) => ({
    account: botAccount(adapter),
    response: await client.apiRequest<WireTargetsResponse>('GET', adapterPath(adapter.id, 'targets')),
  })))
  for (const { account, response } of responses) {
    merged.available ||= response.available
    merged.groups.push(...response.groups.map((group) => ({ ...group, ...account })))
    merged.private_users.push(...response.private_users.map((user) => ({ ...user, ...account })))
    merged.issues.push(...response.issues)
  }
  return merged
}

export async function resolveProtocolIdentities(client: ManagementClient, items: IdentityResolveItem[]): Promise<IdentityResolveResponse> {
  const accounts = await accountTransports(client)
  const groups = new Map<string, IdentityResolveItem[]>()
  for (const item of items) {
    const key = botKey(item)
    if (!accounts.has(key)) throw new Error('机器人暂不可用')
    const group = groups.get(key) ?? []
    if (!group.some((existing) => identityKey(existing.source_protocol, existing.bot_id, existing.target_type, existing.target_id, existing.user_id) === identityKey(item.source_protocol, item.bot_id, item.target_type, item.target_id, item.user_id))) group.push(item)
    groups.set(key, group)
  }
  const result: IdentityResolveResponse = { items: [], issues: [] }
  for (const [key, pending] of groups) {
    const adapter = accounts.get(key)!
    const account = botAccount(adapter)
    for (let start = 0; start < pending.length; start += identityBatchSize) {
      const batch = pending.slice(start, start + identityBatchSize).map(({ target_type, target_id, user_id }) => ({ target_type, target_id, user_id }))
      const response = await client.apiRequest<WireIdentityResponse>('POST', adapterPath(adapter.id, 'identities/resolve'), { items: batch })
      result.items.push(...response.items.map((item) => ({ ...item, ...account })))
      result.issues.push(...response.issues)
    }
  }
  return result
}

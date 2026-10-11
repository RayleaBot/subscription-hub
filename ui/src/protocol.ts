import type { PluginUIClient } from '@rayleabot/plugin-ui'

import { identityKey, type IdentityResolveItem, type IdentityResolveResponse, type ProtocolTargetRecord } from './model'

type ManagementClient = Pick<PluginUIClient, 'apiRequest'>

interface AdapterDescriptor {
  id: string
  protocol: string
  enabled: boolean
}

interface ProtocolTargetsResponse {
  available: boolean
  groups: ProtocolTargetRecord[]
  private_users: ProtocolTargetRecord[]
  issues: IdentityResolveResponse['issues']
}

type WireTargetsResponse = Omit<ProtocolTargetsResponse, 'groups' | 'private_users'> & {
  groups: Omit<ProtocolTargetRecord, 'source_adapter' | 'source_protocol'>[]
  private_users: Omit<ProtocolTargetRecord, 'source_adapter' | 'source_protocol'>[]
}
type WireIdentityResponse = Omit<IdentityResolveResponse, 'items'> & {
  items: Omit<IdentityResolveItem, 'source_adapter' | 'source_protocol'>[]
}

const identityBatchSize = 100

function adapterPath(adapterID: string, suffix: string) {
  return '/api/adapters/' + encodeURIComponent(adapterID) + '/onebot11/' + suffix
}

export async function loadProtocolTargets(client: ManagementClient): Promise<ProtocolTargetsResponse> {
  const { adapters } = await client.apiRequest<{ adapters: AdapterDescriptor[] }>('GET', '/api/adapters')
  const adapterIDs = adapters.filter((adapter) => adapter.protocol === 'onebot11' && adapter.enabled).map((adapter) => adapter.id)
  const merged: ProtocolTargetsResponse = { available: false, groups: [], private_users: [], issues: [] }
  if (adapterIDs.length === 0) {
    merged.issues.push({ message: '尚未启用 OneBot11 连接' })
    return merged
  }
  const responses = await Promise.all(adapterIDs.map(async (source_adapter) => ({
    source_adapter,
    response: await client.apiRequest<WireTargetsResponse>('GET', adapterPath(source_adapter, 'targets')),
  })))
  for (const { source_adapter, response } of responses) {
    merged.available ||= response.available
    const route = { source_adapter, source_protocol: 'onebot11' as const }
    merged.groups.push(...response.groups.map((group) => ({ ...group, ...route })))
    merged.private_users.push(...response.private_users.map((user) => ({ ...user, ...route })))
    merged.issues.push(...response.issues)
  }
  return merged
}

export async function resolveProtocolIdentities(client: ManagementClient, items: IdentityResolveItem[]): Promise<IdentityResolveResponse> {
  const groups = new Map<string, IdentityResolveItem[]>()
  for (const item of items) {
    if (item.source_protocol !== 'onebot11' || !item.source_adapter) throw new Error('订阅人资料缺少所属连接。')
    const group = groups.get(item.source_adapter) ?? []
    if (!group.some((existing) => identityKey(existing.source_adapter, existing.target_type, existing.target_id, existing.user_id) === identityKey(item.source_adapter, item.target_type, item.target_id, item.user_id))) group.push(item)
    groups.set(item.source_adapter, group)
  }
  const result: IdentityResolveResponse = { items: [], issues: [] }
  for (const [source_adapter, pending] of groups) {
    for (let start = 0; start < pending.length; start += identityBatchSize) {
      const batch = pending.slice(start, start + identityBatchSize).map(({ target_type, target_id, user_id }) => ({ target_type, target_id, user_id }))
      const response = await client.apiRequest<WireIdentityResponse>('POST', adapterPath(source_adapter, 'identities/resolve'), { items: batch })
      result.items.push(...response.items.map((item) => ({ ...item, source_adapter, source_protocol: 'onebot11' as const })))
      result.issues.push(...response.issues)
    }
  }
  return result
}

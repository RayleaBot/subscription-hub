import type { PluginUIClient } from '@rayleabot/plugin-ui'

import type { IdentityResolveItem, IdentityResolveResponse } from './model'

type ManagementClient = Pick<PluginUIClient, 'apiRequest'>

interface AdapterDescriptor {
  id: string
  protocol: string
  enabled: boolean
}

interface ProtocolTargetsResponse {
  available: boolean
  groups: Array<{ target_id: string }>
  private_users: Array<{ target_id: string }>
  issues: IdentityResolveResponse['issues']
}

const identityBatchSize = 100
const noConnectionIssue = { message: '尚未启用 OneBot11 连接' }

async function oneBotAdapterIDs(client: ManagementClient): Promise<string[]> {
  const response = await client.apiRequest<{ adapters: AdapterDescriptor[] }>('GET', '/api/adapters')
  return response.adapters.filter((adapter) => adapter.protocol === 'onebot11' && adapter.enabled).map((adapter) => adapter.id)
}

function adapterPath(adapterID: string, suffix: string) {
  return `/api/adapters/${encodeURIComponent(adapterID)}/onebot11/${suffix}`
}

// loadProtocolTargets merges the push targets of every enabled OneBot11 connection.
export async function loadProtocolTargets(client: ManagementClient): Promise<ProtocolTargetsResponse> {
  const adapterIDs = await oneBotAdapterIDs(client)
  const merged: ProtocolTargetsResponse = { available: false, groups: [], private_users: [], issues: [] }
  if (adapterIDs.length === 0) {
    merged.issues.push(noConnectionIssue)
    return merged
  }
  const responses = await Promise.all(adapterIDs.map((adapterID) => client.apiRequest<ProtocolTargetsResponse>('GET', adapterPath(adapterID, 'targets'))))
  const seenGroups = new Set<string>()
  const seenUsers = new Set<string>()
  for (const response of responses) {
    merged.available ||= response.available
    for (const group of response.groups) {
      if (!seenGroups.has(group.target_id)) {
        seenGroups.add(group.target_id)
        merged.groups.push(group)
      }
    }
    for (const user of response.private_users) {
      if (!seenUsers.has(user.target_id)) {
        seenUsers.add(user.target_id)
        merged.private_users.push(user)
      }
    }
    merged.issues.push(...response.issues)
  }
  return merged
}

function identityKey(item: IdentityResolveItem) {
  return `${item.target_type}:${item.target_id}:${item.user_id}`
}

// resolveProtocolIdentities asks each enabled OneBot11 connection in turn for the
// members still unresolved, in batches the API accepts. Issues are reported only
// when some member is resolved by no connection.
export async function resolveProtocolIdentities(client: ManagementClient, items: IdentityResolveItem[]): Promise<IdentityResolveResponse> {
  const adapterIDs = await oneBotAdapterIDs(client)
  const resolved: IdentityResolveResponse['items'] = []
  let issues: IdentityResolveResponse['issues'] = adapterIDs.length === 0 ? [noConnectionIssue] : []
  let pending = items
  for (const adapterID of adapterIDs) {
    if (pending.length === 0) break
    issues = []
    const found = new Set<string>()
    for (let start = 0; start < pending.length; start += identityBatchSize) {
      const batch = pending.slice(start, start + identityBatchSize).map(({ target_type, target_id, user_id }) => ({ target_type, target_id, user_id }))
      const response = await client.apiRequest<IdentityResolveResponse>('POST', adapterPath(adapterID, 'identities/resolve'), { items: batch })
      resolved.push(...response.items)
      issues.push(...response.issues)
      response.items.forEach((item) => found.add(identityKey(item)))
    }
    pending = pending.filter((item) => !found.has(identityKey(item)))
  }
  return { items: resolved, issues: pending.length > 0 ? issues : [] }
}

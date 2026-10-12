import type { BotAccount, usePluginHost } from '@rayleabot/plugin-ui'
import { botKey, identityKey, type IdentityResolveItem, type IdentityResolveResponse, type ProtocolTargetRecord } from './model'

type ProtocolHost = Pick<ReturnType<typeof usePluginHost>, 'client' | 'refreshBot'>

interface WireTargetsResponse {
  available: boolean
  groups: Omit<ProtocolTargetRecord, 'bot_id' | 'source_protocol'>[]
  private_users: Omit<ProtocolTargetRecord, 'bot_id' | 'source_protocol'>[]
  issues: IdentityResolveResponse['issues']
}
type WireIdentityResponse = Omit<IdentityResolveResponse, 'items'> & {
  items: Omit<IdentityResolveItem, 'bot_id' | 'source_protocol'>[]
}

function adapterPath(bot: BotAccount, suffix: string) {
  return '/api/adapters/' + encodeURIComponent(bot.adapter_id!) + '/onebot11/' + suffix
}

async function onlineBot(host: ProtocolHost): Promise<BotAccount> {
  const bot = await host.refreshBot()
  if (bot.source_protocol !== 'onebot11') throw new Error('当前功能仅支持 OneBot11')
  if (bot.state !== 'online' || !bot.adapter_id) throw new Error('账号离线或无连接，暂不可查询；已保存数据可继续编辑。')
  return bot
}

async function verifyConnection(host: ProtocolHost, queried: BotAccount) {
  const current = await onlineBot(host)
  if (botKey(current) !== botKey(queried) || current.adapter_id !== queried.adapter_id) {
    throw new Error('账号连接已变化，请刷新可选范围。')
  }
}

export async function loadProtocolTargets(host: ProtocolHost) {
  const bot = await onlineBot(host)
  const response = await host.client.apiRequest<WireTargetsResponse>('GET', adapterPath(bot, 'targets'))
  await verifyConnection(host, bot)
  const account = { source_protocol: bot.source_protocol, bot_id: bot.bot_id }
  return {
    ...response,
    groups: response.groups.map((group) => ({ ...group, ...account })),
    private_users: response.private_users.map((user) => ({ ...user, ...account })),
  }
}

export async function resolveProtocolIdentities(host: ProtocolHost, items: IdentityResolveItem[]): Promise<IdentityResolveResponse> {
  const bot = await onlineBot(host)
  const pending = new Map<string, IdentityResolveItem>()
  for (const item of items) {
    if (botKey(item) !== botKey(bot)) throw new Error('订阅人不属于当前机器人账号')
    pending.set(identityKey(item.source_protocol, item.bot_id, item.target_type, item.target_id, item.user_id), item)
  }
  const requests = [...pending.values()]
  const result: IdentityResolveResponse = { items: [], issues: [] }
  for (let start = 0; start < requests.length; start += 100) {
    const batch = requests.slice(start, start + 100).map(({ target_type, target_id, user_id }) => ({ target_type, target_id, user_id }))
    const response = await host.client.apiRequest<WireIdentityResponse>('POST', adapterPath(bot, 'identities/resolve'), { items: batch })
    await verifyConnection(host, bot)
    result.items.push(...response.items.map((item) => ({ ...item, source_protocol: bot.source_protocol, bot_id: bot.bot_id })))
    result.issues.push(...response.issues)
  }
  return result
}

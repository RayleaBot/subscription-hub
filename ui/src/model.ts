export type Platform = 'bilibili' | 'weibo' | 'douyin' | 'netease_music'

export interface Subscription {
  id: string
  platform: Platform
  uid: string
  name: string
  avatar_url?: string
  target_type: 'group' | 'private'
  target_id: string
  target_name?: string
  services: string[]
  subscribers: Array<{ id: string; nickname: string; role?: string }>
  enabled: boolean
}

export interface SubscriptionSettings {
  enabled: boolean
  subscriptions: Subscription[]
}

const platforms = new Set<Platform>(['bilibili', 'weibo', 'douyin', 'netease_music'])

export function normalizeSettings(value: Record<string, unknown>): SubscriptionSettings {
  const source = Array.isArray(value.subscriptions) ? value.subscriptions : []
  const seen = new Set<string>()
  const subscriptions: Subscription[] = []
  for (const raw of source) {
    const item = raw as Partial<Subscription>
    const platform = platforms.has(item.platform as Platform) ? item.platform as Platform : null
    const uid = String(item.uid ?? '').trim()
    const targetType = item.target_type === 'private' ? 'private' : 'group'
    const targetID = String(item.target_id ?? '').trim()
    if (!platform || !uid || !targetID) continue
    const id = String(item.id || `${platform}-${uid}-${targetType}-${targetID}`)
    if (seen.has(id)) continue
    seen.add(id)
    subscriptions.push({
      id,
      platform,
      uid,
      name: String(item.name || uid),
      avatar_url: String(item.avatar_url || '') || undefined,
      target_type: targetType,
      target_id: targetID,
      target_name: String(item.target_name || '') || undefined,
      services: Array.isArray(item.services) && item.services.length ? item.services.map(String) : ['all'],
      subscribers: Array.isArray(item.subscribers) ? item.subscribers : [],
      enabled: item.enabled !== false,
    })
  }
  return { enabled: value.enabled !== false, subscriptions }
}

export function validateSubscription(item: Subscription): string[] {
  const errors: string[] = []
  if (!item.uid.trim()) errors.push('账号 ID 不能为空。')
  if (!item.target_id.trim()) errors.push('推送目标 ID 不能为空。')
  if (!item.name.trim()) errors.push('显示名称不能为空。')
  return errors
}

export function createSubscription(index: number): Subscription {
  return {
    id: `draft-${Date.now()}-${index}`,
    platform: 'bilibili',
    uid: '',
    name: '',
    target_type: 'group',
    target_id: '',
    services: ['all'],
    subscribers: [],
    enabled: true,
  }
}

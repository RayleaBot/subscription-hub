export type Platform = 'bilibili' | 'weibo' | 'douyin' | 'netease_music'
export type TargetType = 'group' | 'private'
export type ResolveState = 'idle' | 'checking' | 'resolved' | 'error'

export const PLATFORM_OPTIONS = [
  { value: 'bilibili', label: 'Bilibili', subjectLabel: 'UID', inputPlaceholder: 'UID 或 Bilibili 用户名' },
  { value: 'weibo', label: '微博', subjectLabel: 'UID', inputPlaceholder: 'UID 或微博主页标识' },
  { value: 'douyin', label: '抖音', subjectLabel: '抖音号', inputPlaceholder: '抖音号或主页标识' },
  { value: 'netease_music', label: '网易云音乐', subjectLabel: 'ID', inputPlaceholder: '歌曲、歌单、专辑或音乐人 ID' },
] as const satisfies ReadonlyArray<{
  value: Platform
  label: string
  subjectLabel: string
  inputPlaceholder: string
}>

export const PLATFORM_SERVICE_LABELS: Record<Platform, Record<string, string>> = {
  bilibili: { all: '全部', live: '直播', video: '视频', image_text: '图文', article: '文章', repost: '转发' },
  weibo: { all: '全部', post: '微博', image: '图片', video: '视频', repost: '转发' },
  douyin: { all: '全部', video: '视频', image_text: '图文', live: '直播' },
  netease_music: { all: '全部', song: '歌曲', album: '专辑', playlist: '歌单', artist: '音乐人' },
}

export interface Subscriber {
  id: string
  nickname?: string
  group_nickname?: string
  title?: string
  role?: string
  role_label?: string
  avatar_url?: string
}

export interface Subscription {
  id: string
  platform: Platform
  uid: string
  name: string
  avatar_url?: string
  target_type: TargetType
  target_id: string
  target_name?: string
  services: string[]
  subscribers: Subscriber[]
  enabled: boolean
}

export interface SubscriptionSettings {
  enabled: boolean
  subscriptions: Subscription[]
}

export interface RowTarget {
  key: string
  subscription_id: string
  target_type: TargetType
  target_id: string
  target_name: string
  services: string[]
}

export interface ResolveCandidate {
  uid: string
  name: string
  avatar_url?: string
}

export interface SubscriptionRow {
  row_id: string
  platform: Platform
  uid: string
  name: string
  avatar_url: string
  query: string
  resolved: boolean
  resolve_state: ResolveState
  resolve_message: string
  candidates: ResolveCandidate[]
  enabled: boolean
  services: string[]
  service_mode: 'common' | 'mixed'
  target_mode: TargetType
  targets: RowTarget[]
  subscriber_ids: string[]
  edit_mode: boolean
  _editSnapshot: SubscriptionRowSnapshot | null
}

export type SubscriptionRowSnapshot = Omit<SubscriptionRow, '_editSnapshot'>

export interface ProtocolTargetRecord {
  target_type?: TargetType
  target_id: string
  target_name?: string
  nickname?: string
  avatar_url?: string
}

export interface ProtocolIssue {
  scope?: string
  message: string
}

export interface TargetsState {
  loaded: boolean
  available: boolean
  groups: ProtocolTargetRecord[]
  private_users: ProtocolTargetRecord[]
  issues: ProtocolIssue[]
}

export interface LiveTarget {
  key: string
  target_type: TargetType
  target_id: string
  label: string
  avatar_url: string
}

export interface RowContext {
  targets: TargetsState
  targetMap: Map<string, LiveTarget>
  targetsLoaded: boolean
  subscriberAvatars: Map<string, string>
  avatarDataURLs: Map<string, string>
}

export interface IdentityResolveItem {
  target_type: TargetType
  target_id: string
  user_id: string
  nickname?: string
  group_nickname?: string
  title?: string
  role?: string
  role_label?: string
  avatar_url?: string
}

export interface IdentityResolveResponse {
  items: IdentityResolveItem[]
  issues: ProtocolIssue[]
}

const platformSet = new Set<Platform>(PLATFORM_OPTIONS.map((item) => item.value))
const numericPattern = /^[0-9]+$/

export function normalizeSettings(value: Record<string, unknown>): SubscriptionSettings {
  const source = Array.isArray(value.subscriptions) ? value.subscriptions : []
  return {
    enabled: value.enabled !== false,
    subscriptions: source.map(normalizeSubscription).filter((item): item is Subscription => item !== null),
  }
}

export function normalizeSubscription(value: unknown): Subscription | null {
  if (!isRecord(value)) return null
  const platform = normalizePlatform(value.platform)
  const uid = safeSubjectId(value.uid, platform)
  const targetType = value.target_type === 'private' ? 'private' : value.target_type === 'group' ? 'group' : null
  const targetID = trim(value.target_id)
  if (!uid || !targetType || !numericPattern.test(targetID)) return null
  return {
    id: trim(value.id) || `${platform}-${uid}-${targetType}-${targetID}`,
    platform,
    uid,
    name: trim(value.name) || uid,
    avatar_url: trim(value.avatar_url) || undefined,
    target_type: targetType,
    target_id: targetID,
    target_name: trim(value.target_name) || undefined,
    services: normalizeServices(value.services, platform),
    subscribers: Array.isArray(value.subscribers)
      ? value.subscribers.map(normalizeSubscriber).filter((item): item is Subscriber => item !== null)
      : [],
    enabled: value.enabled !== false,
  }
}

export function createBlankRow(rowID: string): SubscriptionRow {
  return {
    row_id: rowID,
    platform: 'bilibili',
    uid: '',
    name: '',
    avatar_url: '',
    query: '',
    resolved: false,
    resolve_state: 'idle',
    resolve_message: '',
    candidates: [],
    enabled: true,
    services: ['all'],
    service_mode: 'common',
    target_mode: 'group',
    targets: [],
    subscriber_ids: [],
    edit_mode: true,
    _editSnapshot: null,
  }
}

export function buildRowsFromSettings(settings: SubscriptionSettings): SubscriptionRow[] {
  const grouped = new Map<string, SubscriptionRow>()
  for (const subscription of settings.subscriptions) {
    const groupKey = `${subscription.platform}:${subscription.uid}`
    let row = grouped.get(groupKey)
    if (!row) {
      row = {
        row_id: `${subscription.platform}-${subscription.uid}`,
        platform: subscription.platform,
        uid: subscription.uid,
        name: subscription.name || subscription.uid,
        avatar_url: subscription.avatar_url || '',
        query: subscription.name || subscription.uid,
        resolved: true,
        resolve_state: 'resolved',
        resolve_message: '',
        candidates: [],
        enabled: false,
        services: normalizeServices(subscription.services, subscription.platform),
        service_mode: 'common',
        target_mode: subscription.target_type,
        targets: [],
        subscriber_ids: [],
        edit_mode: false,
        _editSnapshot: null,
      }
      grouped.set(groupKey, row)
    }
    row.enabled ||= subscription.enabled
    row.avatar_url ||= subscription.avatar_url || ''
    row.targets.push({
      key: targetKey(subscription.target_type, subscription.target_id),
      subscription_id: subscription.id,
      target_type: subscription.target_type,
      target_id: subscription.target_id,
      target_name: subscription.target_name || '',
      services: normalizeServices(subscription.services, subscription.platform),
    })
    row.subscriber_ids.push(...subscription.subscribers.map((subscriber) => subscriber.id).filter(Boolean))
  }

  const rows = [...grouped.values()]
  for (const row of rows) {
    row.subscriber_ids = unique(row.subscriber_ids)
    const serviceKeys = unique(row.targets.map((target) => servicesKey(target.services, row.platform)))
    if (serviceKeys.length > 1) row.service_mode = 'mixed'
    else if (row.targets[0]) row.services = [...row.targets[0].services]
  }
  return rows
}

export function cloneRow(row: SubscriptionRow): SubscriptionRow {
  return { ...cloneRowSnapshot(row), _editSnapshot: null }
}

export function rowSnapshot(row: SubscriptionRow): SubscriptionRowSnapshot {
  const clone = cloneRow(row)
  const { _editSnapshot: _ignored, ...snapshot } = clone
  return snapshot
}

export function restoreRow(row: SubscriptionRow, snapshot: SubscriptionRowSnapshot): void {
  const rowID = row.row_id
  Object.assign(row, cloneRowSnapshot(snapshot), { row_id: rowID, edit_mode: false, _editSnapshot: null })
}

function cloneRowSnapshot(row: SubscriptionRowSnapshot): SubscriptionRowSnapshot {
  return {
    ...row,
    candidates: row.candidates.map((candidate) => ({ ...candidate })),
    services: [...row.services],
    targets: row.targets.map((target) => ({ ...target, services: [...target.services] })),
    subscriber_ids: [...row.subscriber_ids],
  }
}

export function buildSettingsPayload(
  settings: Pick<SubscriptionSettings, 'enabled'>,
  rows: SubscriptionRow[],
  targetsByKey: Map<string, LiveTarget>,
  subscriberIdentities: Map<string, Subscriber> = new Map(),
): SubscriptionSettings {
  const subscriptions: Subscription[] = []
  for (const row of rows) {
    for (const target of row.targets) {
      const live = targetsByKey.get(target.key)
      subscriptions.push({
        id: target.subscription_id || `${row.platform}-${row.uid}-${target.target_type}-${target.target_id}`,
        platform: row.platform,
        uid: row.uid,
        name: row.name,
        avatar_url: row.avatar_url || undefined,
        target_type: target.target_type,
        target_id: target.target_id,
        target_name: live?.label || target.target_name || undefined,
        services: normalizeServices(row.service_mode === 'mixed' ? target.services : row.services, row.platform),
        subscribers: row.subscriber_ids.map((id) => {
          const identity = subscriberIdentities.get(identityKey(target.target_type, target.target_id, id))
          return identity ? { ...identity, id } : { id }
        }),
        enabled: row.enabled,
      })
    }
  }
  return { enabled: settings.enabled !== false, subscriptions }
}

export function normalizeTargets(payload: unknown): TargetsState {
  const source = isRecord(payload) ? payload : {}
  return {
    loaded: true,
    available: source.available === true,
    groups: Array.isArray(source.groups) ? source.groups.filter(isTargetRecord) : [],
    private_users: Array.isArray(source.private_users) ? source.private_users.filter(isTargetRecord) : [],
    issues: Array.isArray(source.issues)
      ? source.issues.filter(isRecord).map((issue) => ({ scope: trim(issue.scope) || undefined, message: trim(issue.message) })).filter((issue) => issue.message)
      : [],
  }
}

export function emptyTargets(): TargetsState {
  return { loaded: false, available: false, groups: [], private_users: [], issues: [] }
}

export function allTargets(state: TargetsState): LiveTarget[] {
  return [
    ...state.groups.map((target) => ({
      key: targetKey('group', target.target_id),
      target_type: 'group' as const,
      target_id: trim(target.target_id),
      label: trim(target.target_name) || trim(target.target_id),
      avatar_url: trim(target.avatar_url) || deriveTargetAvatarURL('group', target.target_id),
    })),
    ...state.private_users.map((target) => ({
      key: targetKey('private', target.target_id),
      target_type: 'private' as const,
      target_id: trim(target.target_id),
      label: trim(target.nickname) || trim(target.target_name) || trim(target.target_id),
      avatar_url: trim(target.avatar_url) || deriveTargetAvatarURL('private', target.target_id),
    })),
  ]
}

export function targetMap(state: TargetsState): Map<string, LiveTarget> {
  return new Map(allTargets(state).map((target) => [target.key, target]))
}

export function knownTargetsState(state: TargetsState, rows: SubscriptionRow[]): TargetsState {
  const groups = [...state.groups]
  const privateUsers = [...state.private_users]
  const seen = new Set(targetMap(state).keys())
  let savedCount = 0
  const issueScopes = new Set(state.issues.map((issue) => issue.scope).filter(Boolean))
  const fallbackAllowed = (targetType: TargetType) => {
    if (!state.loaded) return true
    if (state.available) return false
    if (!issueScopes.size || issueScopes.has('protocol')) return true
    return targetType === 'group' ? issueScopes.has('groups') : issueScopes.has('private_users')
  }

  for (const row of rows) {
    for (const target of row.targets) {
      if (!target.target_id || seen.has(target.key) || !fallbackAllowed(target.target_type)) continue
      if (target.target_type === 'group') {
        groups.push({ target_type: 'group', target_id: target.target_id, target_name: target.target_name || target.target_id })
      } else {
        privateUsers.push({ target_type: 'private', target_id: target.target_id, nickname: target.target_name || target.target_id })
      }
      seen.add(target.key)
      savedCount += 1
    }
  }
  return { ...state, loaded: state.loaded || savedCount > 0, groups, private_users: privateUsers }
}

export function createRowContext(
  targets: TargetsState,
  rows: SubscriptionRow[],
  subscriberAvatars: Map<string, string>,
  avatarDataURLs: Map<string, string> = new Map(),
): RowContext {
  const known = knownTargetsState(targets, rows)
  return { targets: known, targetMap: targetMap(known), targetsLoaded: known.loaded, subscriberAvatars, avatarDataURLs }
}

export function validateRow(row: SubscriptionRow, context: RowContext): string[] {
  const errors: string[] = []
  if (!row.resolved || !safeSubjectId(row.uid, row.platform) || !row.name.trim()) {
    errors.push(`${platformLabel(row.platform)} ${subjectLabel(row.platform)} 未完成`)
  }
  if (!context.targetsLoaded) errors.push('推送对象未载入')
  if (!row.targets.length) errors.push('请选择推送对象')
  for (const target of row.targets) {
    if (!context.targetMap.has(target.key)) errors.push(`${targetDisplay(target, context.targetMap)} 不在协议对象列表中`)
    if (row.service_mode === 'mixed' && !hasServiceSelection(target.services)) {
      errors.push(`${targetDisplay(target, context.targetMap)} 请选择推送类型`)
    }
  }
  if (row.service_mode !== 'mixed' && !hasServiceSelection(row.services)) errors.push('请选择推送类型')
  for (const id of row.subscriber_ids) {
    if (!numericPattern.test(id.trim())) errors.push(`订阅人 QQ 不合法：${id}`)
  }
  return unique(errors)
}

export function validateRows(rows: SubscriptionRow[], context: RowContext): string[] {
  return rows.flatMap((row) => validateRow(row, context))
}

export function normalizePlatform(value: unknown): Platform {
  const platform = trim(value) as Platform
  return platformSet.has(platform) ? platform : 'bilibili'
}

export function platformMeta(platform: Platform) {
  return PLATFORM_OPTIONS.find((item) => item.value === platform) ?? PLATFORM_OPTIONS[0]
}

export function platformLabel(platform: Platform): string {
  return platformMeta(platform).label
}

export function subjectLabel(platform: Platform): string {
  return platformMeta(platform).subjectLabel
}

export function inputPlaceholder(platform: Platform): string {
  return platformMeta(platform).inputPlaceholder
}

export function safeSubjectId(value: unknown, platform: Platform): string {
  const text = trim(value)
  if (platform === 'bilibili') return numericPattern.test(text) ? text : ''
  return [...text]
    .filter((char) => /[\p{L}\p{N}_.-]/u.test(char))
    .join('')
    .replace(/^[_.-]+|[_.-]+$/g, '')
    .slice(0, 96)
}

export function serviceLabels(platform: Platform): Record<string, string> {
  return PLATFORM_SERVICE_LABELS[platform]
}

export function serviceOrder(platform: Platform): string[] {
  return Object.keys(serviceLabels(platform))
}

export function serviceTypes(platform: Platform): string[] {
  return serviceOrder(platform).filter((service) => service !== 'all')
}

export function serviceLabel(service: string, platform: Platform): string {
  return serviceLabels(platform)[service] || service
}

export function normalizeServices(value: unknown, platform: Platform): string[] {
  const order = serviceOrder(platform)
  const types = serviceTypes(platform)
  const services = unique(Array.isArray(value) ? value.map(String) : ['all']).filter((item) => order.includes(item))
  if (!services.length || services.includes('all')) return ['all']
  const selected = types.filter((service) => services.includes(service))
  return selected.length === types.length ? ['all'] : selected
}

export function serviceCheckboxValues(value: string[], platform: Platform): Set<string> {
  if (value.length === 0) return new Set()
  const services = normalizeServices(value, platform)
  return services.includes('all') ? new Set(serviceOrder(platform)) : new Set(services)
}

export function hasServiceSelection(value: string[]): boolean {
  return value.length > 0
}

export function servicesKey(services: string[], platform: Platform): string {
  return normalizeServices(services, platform).join(',')
}

export function servicesText(services: string[], platform: Platform): string {
  return normalizeServices(services, platform).map((service) => serviceLabel(service, platform)).join('、')
}

export function targetKey(targetType: TargetType, targetID: string): string {
  return `${targetType}:${trim(targetID)}`
}

export function currentTargetsForMode(state: TargetsState, mode: TargetType): LiveTarget[] {
  return allTargets(state).filter((target) => target.target_type === mode)
}

export function targetDisplay(target: RowTarget, map: Map<string, LiveTarget>): string {
  const live = map.get(target.key)
  return `${target.target_type === 'group' ? '群聊' : '私聊'} ${live?.label || target.target_name || target.target_id}`
}

export function targetAvatar(target: RowTarget, map: Map<string, LiveTarget>): string {
  return map.get(target.key)?.avatar_url || deriveTargetAvatarURL(target.target_type, target.target_id)
}

export function deriveTargetAvatarURL(targetType: TargetType, targetID: string): string {
  const id = trim(targetID)
  if (!numericPattern.test(id)) return ''
  return targetType === 'group'
    ? `https://p.qlogo.cn/gh/${encodeURIComponent(id)}/${encodeURIComponent(id)}/100`
    : `https://q1.qlogo.cn/g?b=qq&nk=${encodeURIComponent(id)}&s=640`
}

export function subscriberAvatarURL(avatars: Map<string, string>, userID: string): string {
  const id = trim(userID)
  return avatars.get(id) || `https://q1.qlogo.cn/g?b=qq&nk=${encodeURIComponent(id)}&s=640`
}

export function displayAvatarURL(source: string, dataURLs: Map<string, string>): string {
  const value = trim(source)
  if (!value) return ''
  if (value.startsWith('data:image/') || value.startsWith('./') || value.startsWith('/')) return value
  return dataURLs.get(value) || ''
}

export function collectSubscriberAvatars(settings: SubscriptionSettings): Map<string, string> {
  const avatars = new Map<string, string>()
  for (const subscription of settings.subscriptions) {
    for (const subscriber of subscription.subscribers) {
      if (subscriber.id && subscriber.avatar_url) avatars.set(subscriber.id, subscriber.avatar_url)
    }
  }
  return avatars
}

export function collectSubscriberIdentities(settings: SubscriptionSettings): Map<string, Subscriber> {
  const identities = new Map<string, Subscriber>()
  for (const subscription of settings.subscriptions) {
    for (const subscriber of subscription.subscribers) {
      identities.set(
        identityKey(subscription.target_type, subscription.target_id, subscriber.id),
        { ...subscriber },
      )
    }
  }
  return identities
}

export function buildIdentityRequests(rows: SubscriptionRow[]): IdentityResolveItem[] {
  const items: IdentityResolveItem[] = []
  const seen = new Set<string>()
  for (const row of rows) {
    for (const target of row.targets) {
      for (const userID of row.subscriber_ids) {
        const key = identityKey(target.target_type, target.target_id, userID)
        if (seen.has(key)) continue
        seen.add(key)
        items.push({ target_type: target.target_type, target_id: target.target_id, user_id: userID })
      }
    }
  }
  return items
}

export function identityKey(targetType: TargetType, targetID: string, userID: string): string {
  return `${targetType}:${targetID}:${userID}`
}

export function isNumericID(value: string): boolean {
  return numericPattern.test(value.trim())
}

export function trim(value: unknown): string {
  return String(value ?? '').trim()
}

export function unique(values: string[]): string[] {
  return [...new Set(values.map(trim).filter(Boolean))]
}

function normalizeSubscriber(value: unknown): Subscriber | null {
  if (!isRecord(value)) return null
  const id = trim(value.id)
  if (!numericPattern.test(id)) return null
  return {
    id,
    nickname: trim(value.nickname) || undefined,
    group_nickname: trim(value.group_nickname) || undefined,
    title: trim(value.title) || undefined,
    role: trim(value.role) || undefined,
    role_label: trim(value.role_label) || undefined,
    avatar_url: trim(value.avatar_url) || undefined,
  }
}

function isTargetRecord(value: unknown): value is ProtocolTargetRecord {
  return isRecord(value) && typeof value.target_id === 'string' && value.target_id.trim().length > 0
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

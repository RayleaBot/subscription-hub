<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { Alert as AAlert } from 'ant-design-vue'
import { usePluginHost } from '@rayleabot/plugin-ui'

import SubscriptionCard from './components/SubscriptionCard.vue'
import {
  buildIdentityRequests,
  buildRowsFromSettings,
  buildSettingsPayload,
  cloneRow,
  collectSubscriberAvatars,
  createBlankRow,
  createRowContext,
  emptyTargets,
  identityKey,
  normalizePlatform,
  normalizeServices,
  normalizeSettings,
  normalizeTargets,
  platformLabel,
  restoreRow,
  rowSnapshot,
  serviceCheckboxValues,
  serviceTypes,
  servicesKey,
  targetDisplay,
  unique,
  validateRow,
  validateRows,
  type IdentityResolveResponse,
  type LiveTarget,
  type Platform,
  type ResolveCandidate,
  type SubscriptionRow,
  type SubscriptionSettings,
  type TargetType,
  type TargetsState,
} from './model'

const host = usePluginHost()
const defaultSettings: SubscriptionSettings = { enabled: true, subscriptions: [] }
const settings = ref<SubscriptionSettings>(structuredClone(defaultSettings))
const rows = ref<SubscriptionRow[]>([])
const targets = ref<TargetsState>(emptyTargets())
const subscriberAvatars = ref(new Map<string, string>())
const savedSnapshot = ref('')
const loaded = ref(false)
const status = ref('正在等待宿主初始化…')
const statusIsError = ref(false)
const saving = ref(false)
const checking = ref(false)
const targetsLoading = ref(false)
const resolvingRows = ref(new Set<string>())
const search = ref('')
const statusFilter = ref<'all' | 'enabled' | 'disabled'>('all')
const serviceFilter = ref('all')
const listRef = ref<HTMLElement | null>(null)
const resolveTimers = new Map<string, number>()
const resolveVersions = new Map<string, number>()
let rowCounter = 0

const hostErrorMessage = computed(() => host.error.value?.message ?? '')
const context = computed(() => createRowContext(targets.value, rows.value, subscriberAvatars.value))
const errors = computed(() => validateRows(rows.value, context.value))
const contentSnapshot = computed(() => JSON.stringify(buildSettingsPayload(settings.value, rows.value, new Map())))
const isDirty = computed(() => loaded.value && contentSnapshot.value !== savedSnapshot.value)
const visibleRows = computed(() => rows.value.filter((row) => rowVisible(row)))
const knownTargetCount = computed(() => context.value.targets.groups.length + context.value.targets.private_users.length)
const targetMetric = computed(() => {
  if (targets.value.available) return `${targets.value.groups.length} 群聊 / ${targets.value.private_users.length} 私聊`
  return knownTargetCount.value ? `${knownTargetCount.value} 个可用` : '未载入'
})
const dirtyStateText = computed(() => {
  if (!loaded.value) return '等待载入'
  if (saving.value) return '正在保存'
  return isDirty.value ? '设置有修改' : '设置已同步'
})

void host.ready
  .then((init) => {
    applySettings(init.config, true)
    setStatus('设置已载入')
    void reloadTargets()
  })
  .catch((error: unknown) => {
    setStatus(errorMessage(error, '插件页面连接失败'), true)
  })

function applySettings(value: Record<string, unknown>, markSaved: boolean) {
  const normalized = normalizeSettings(value)
  settings.value = normalized
  rows.value = buildRowsFromSettings(normalized)
  subscriberAvatars.value = collectSubscriberAvatars(normalized)
  loaded.value = true
  if (markSaved) savedSnapshot.value = signature(normalized, rows.value)
}

function signature(currentSettings: SubscriptionSettings, currentRows: SubscriptionRow[]): string {
  return JSON.stringify(buildSettingsPayload(currentSettings, currentRows, new Map()))
}

function setStatus(message: string, isError = false) {
  status.value = message
  statusIsError.value = isError
}

function nextRowID(): string {
  rowCounter += 1
  return `row-${Date.now()}-${rowCounter}`
}

function rowVisible(row: SubscriptionRow): boolean {
  const query = search.value.trim().toLowerCase()
  if (query) {
    const targetText = row.targets.flatMap((target) => [
      target.target_id,
      target.target_name,
      context.value.targetMap.get(target.key)?.label ?? '',
    ])
    const text = [row.uid, row.name, row.query, ...targetText, ...row.subscriber_ids, platformLabel(row.platform)].join(' ').toLowerCase()
    if (!text.includes(query)) return false
  }
  if (statusFilter.value === 'enabled' && !row.enabled) return false
  if (statusFilter.value === 'disabled' && row.enabled) return false
  if (serviceFilter.value !== 'all') {
    const services = row.service_mode === 'mixed' ? row.targets.flatMap((target) => target.services) : row.services
    if (!services.includes('all') && !services.includes(serviceFilter.value)) return false
  }
  return true
}

async function reloadTargets() {
  if (targetsLoading.value) return
  targetsLoading.value = true
  setStatus('正在刷新推送对象…')
  try {
    const payload = await host.client.request<Record<string, unknown>>('protocol.targets.reload', undefined, 5_000)
    targets.value = normalizeTargets(payload)
    const issue = targets.value.issues[0]?.message
    const liveCount = targets.value.groups.length + targets.value.private_users.length
    const knownCount = context.value.targets.groups.length + context.value.targets.private_users.length
    if (targets.value.available) setStatus('推送对象已刷新')
    else if (liveCount > 0) setStatus(`部分推送对象已刷新${issue ? `：${issue}` : ''}`)
    else if (knownCount > 0) setStatus(`${issue ? `未拉到新对象：${issue}` : '未拉到新对象'}，已保留 ${knownCount} 个已保存对象`)
    else setStatus(issue || '推送对象不可用', true)
  } catch (error) {
    targets.value = normalizeTargets({
      available: false,
      groups: [],
      private_users: [],
      issues: [{ scope: 'protocol', message: errorMessage(error, '推送对象不可用') }],
    })
    const knownCount = context.value.targets.groups.length + context.value.targets.private_users.length
    setStatus(knownCount > 0 ? `未拉到新对象，已保留 ${knownCount} 个已保存对象` : errorMessage(error, '推送对象不可用'), knownCount === 0)
  } finally {
    targetsLoading.value = false
  }
}

function addSubscription() {
  const row = createBlankRow(nextRowID())
  rows.value.unshift(row)
  void scrollToRow(row)
}

function beginEdit(row: SubscriptionRow) {
  row._editSnapshot = rowSnapshot(row)
  row.edit_mode = true
  void scrollToRow(row)
}

function cancelEdit(row: SubscriptionRow) {
  clearResolveTimer(row.row_id)
  if (row._editSnapshot) restoreRow(row, row._editSnapshot)
}

function finishEdit(row: SubscriptionRow) {
  const rowErrors = validateRow(row, context.value)
  if (rowErrors.length > 0) {
    setStatus(rowErrors[0]!, true)
    return
  }
  row._editSnapshot = null
  row.edit_mode = false
  setStatus('订阅项已完成编辑')
}

function duplicateRow(row: SubscriptionRow) {
  const copy = cloneRow(row)
  copy.row_id = nextRowID()
  copy.targets = copy.targets.map((target) => ({ ...target, subscription_id: '' }))
  copy.edit_mode = true
  rows.value.push(copy)
  void scrollToRow(copy)
}

function removeRow(row: SubscriptionRow) {
  clearResolveTimer(row.row_id)
  rows.value = rows.value.filter((item) => item !== row)
}

async function scrollToRow(row: SubscriptionRow) {
  await nextTick()
  const selector = `[data-row-id="${CSS.escape(row.row_id)}"]`
  listRef.value?.querySelector(selector)?.scrollIntoView({ block: 'center', behavior: 'smooth' })
}

function changePlatform(row: SubscriptionRow, platformValue: Platform) {
  clearResolveTimer(row.row_id)
  row.platform = normalizePlatform(platformValue)
  row.uid = ''
  row.name = ''
  row.avatar_url = ''
  row.query = ''
  row.resolved = false
  row.resolve_state = 'idle'
  row.resolve_message = ''
  row.candidates = []
  row.services = ['all']
  row.service_mode = 'common'
  row.targets.forEach((target) => { target.services = ['all'] })
}

function scheduleResolve(row: SubscriptionRow, immediate = false) {
  clearResolveTimer(row.row_id)
  if (!row.query.trim()) {
    row.resolve_state = 'error'
    row.resolve_message = `请填写${platformLabel(row.platform)}对象。`
    return
  }
  const run = () => { void resolveUser(row) }
  if (immediate) run()
  else resolveTimers.set(row.row_id, window.setTimeout(run, 700))
}

function clearResolveTimer(rowID: string) {
  const timer = resolveTimers.get(rowID)
  if (timer !== undefined) window.clearTimeout(timer)
  resolveTimers.delete(rowID)
}

async function resolveUser(row: SubscriptionRow) {
  clearResolveTimer(row.row_id)
  const query = row.query.trim()
  if (!query) {
    row.resolve_state = 'error'
    row.resolve_message = `请填写${platformLabel(row.platform)}对象。`
    return
  }
  const version = (resolveVersions.get(row.row_id) ?? 0) + 1
  resolveVersions.set(row.row_id, version)
  setResolving(row.row_id, true)
  row.resolve_state = 'checking'
  row.resolve_message = `正在校验${platformLabel(row.platform)}对象…`
  row.candidates = []
  row.resolved = false
  try {
    const result = await host.client.invokeAction('subscription.resolve_user', { platform: row.platform, query })
    if (resolveVersions.get(row.row_id) !== version || row.query.trim() !== query) return
    const candidate = isCandidate(result.user) ? result.user : null
    if (result.exact === true && candidate) {
      applyCandidate(row, candidate)
      row.resolve_message = `${platformLabel(row.platform)}对象已校验。`
      setStatus(`${platformLabel(row.platform)}对象已校验`)
    } else {
      row.resolve_state = 'error'
      row.resolve_message = String(result.message || `请选择一个候选${platformLabel(row.platform)}对象后保存。`)
      row.candidates = Array.isArray(result.candidates) ? result.candidates.filter(isCandidate) : []
    }
  } catch (error) {
    if (resolveVersions.get(row.row_id) !== version) return
    row.resolve_state = 'error'
    row.resolve_message = errorMessage(error, `${platformLabel(row.platform)}对象校验失败。`)
    row.candidates = []
    setStatus(row.resolve_message, true)
  } finally {
    setResolving(row.row_id, false)
  }
}

function applyCandidate(row: SubscriptionRow, candidate: ResolveCandidate) {
  row.uid = candidate.uid.trim()
  row.name = candidate.name.trim()
  row.avatar_url = candidate.avatar_url?.trim() || ''
  row.query = row.name || row.uid
  row.resolved = Boolean(row.uid && row.name)
  row.resolve_state = row.resolved ? 'resolved' : 'error'
  row.candidates = []
}

function chooseCandidate(row: SubscriptionRow, candidate: ResolveCandidate) {
  applyCandidate(row, candidate)
  row.resolve_message = `${platformLabel(row.platform)}对象已校验。`
  setStatus(`${platformLabel(row.platform)}对象已校验`)
}

function setResolving(rowID: string, active: boolean) {
  const next = new Set(resolvingRows.value)
  if (active) next.add(rowID)
  else next.delete(rowID)
  resolvingRows.value = next
}

function setTargetMode(row: SubscriptionRow, mode: TargetType) {
  row.target_mode = mode
}

function toggleTarget(row: SubscriptionRow, liveTarget: LiveTarget) {
  const existing = row.targets.findIndex((target) => target.key === liveTarget.key)
  if (existing >= 0) {
    row.targets.splice(existing, 1)
    return
  }
  row.targets.push({
    key: liveTarget.key,
    subscription_id: '',
    target_type: liveTarget.target_type,
    target_id: liveTarget.target_id,
    target_name: liveTarget.label,
    services: normalizeServices(row.service_mode === 'mixed' ? ['all'] : row.services, row.platform),
  })
}

function removeTarget(row: SubscriptionRow, key: string) {
  row.targets = row.targets.filter((target) => target.key !== key)
}

function toggleService(row: SubscriptionRow, targetKey: string, service: string, checked: boolean) {
  const current = targetKey === 'common'
    ? row.services
    : row.targets.find((target) => target.key === targetKey)?.services ?? []
  const next = changedServices(current, row.platform, service, checked)
  if (targetKey === 'common') {
    row.service_mode = 'common'
    row.services = next
    row.targets.forEach((target) => { target.services = [...next] })
    return
  }
  const target = row.targets.find((item) => item.key === targetKey)
  if (!target) return
  target.services = next
  const serviceKeys = unique(row.targets.map((item) => servicesKey(item.services, row.platform)))
  row.service_mode = serviceKeys.length > 1 ? 'mixed' : 'common'
  if (row.service_mode === 'common' && row.targets[0]) row.services = [...row.targets[0].services]
}

function changedServices(current: string[], platform: Platform, service: string, checked: boolean): string[] {
  if (service === 'all') return checked ? ['all'] : []
  const types = serviceTypes(platform)
  const selected = serviceCheckboxValues(current, platform)
  selected.delete('all')
  if (checked) selected.add(service)
  else selected.delete(service)
  const values = types.filter((item) => selected.has(item))
  return values.length === types.length ? ['all'] : values
}

function addSubscriber(row: SubscriptionRow, id: string) {
  row.subscriber_ids = unique([...row.subscriber_ids, id])
}

function removeSubscriber(row: SubscriptionRow, id: string) {
  row.subscriber_ids = row.subscriber_ids.filter((item) => item !== id)
}

async function reloadSettings() {
  setStatus('正在重新载入设置…')
  try {
    const response = await host.client.reloadSettings()
    applySettings(response.config, true)
    setStatus('设置已同步')
  } catch (error) {
    setStatus(errorMessage(error, '重新载入设置失败'), true)
  }
}

function resetSettings() {
  settings.value = structuredClone(defaultSettings)
  rows.value = []
  subscriberAvatars.value = new Map()
  setStatus('已恢复默认设置，保存后生效')
}

async function saveSettings() {
  if (errors.value.length > 0) {
    setStatus(errors.value[0]!, true)
    return
  }
  saving.value = true
  try {
    const identityRequests = buildIdentityRequests(rows.value)
    if (identityRequests.length > 0) {
      setStatus('正在刷新订阅人身份…')
      const resolved = await host.client.request<IdentityResolveResponse>('protocol.identities.resolve', { items: identityRequests })
      const received = new Set(resolved.items.map((item) => identityKey(item.target_type, item.target_id, item.user_id)))
      const missing = identityRequests.filter((item) => !received.has(identityKey(item.target_type, item.target_id, item.user_id)))
      if (resolved.issues.length > 0 || missing.length > 0) {
        throw new Error(resolved.issues[0]?.message || '订阅人身份刷新失败')
      }
      const avatars = new Map(subscriberAvatars.value)
      resolved.items.forEach((item) => { if (item.avatar_url) avatars.set(item.user_id, item.avatar_url) })
      subscriberAvatars.value = avatars
    }

    setStatus('正在保存设置…')
    const payload = buildSettingsPayload(settings.value, rows.value, context.value.targetMap)
    const response = await host.client.saveSettings(payload as unknown as Record<string, unknown>)
    applySettings(response.config, true)
    setStatus('设置已同步')
  } catch (error) {
    setStatus(errorMessage(error, '保存设置失败'), true)
  } finally {
    saving.value = false
  }
}

async function checkNow() {
  checking.value = true
  setStatus('正在检查订阅…')
  try {
    const result = await host.client.invokeAction('subscription.check_now')
    if (result.skipped === 'disabled') setStatus('订阅中心未启用')
    else if (result.skipped === 'no_bilibili_subscriptions') setStatus('没有可检查的 Bilibili 订阅')
    else {
      const checked = Number(result.checked || 0)
      const sent = Number(result.sent || 0)
      const actionErrors = Array.isArray(result.errors) ? result.errors.filter(Boolean).map(String) : []
      setStatus(`订阅检查完成：检查 ${checked} 个订阅账号，推送 ${sent} 条更新${actionErrors[0] ? `；${actionErrors[0]}` : ''}`)
    }
  } catch (error) {
    setStatus(errorMessage(error, '订阅检查失败'), true)
  } finally {
    checking.value = false
  }
}

function openPreview() {
  try {
    host.client.send('render_template.open', { template_id: 'plugin.raylea.subscription-hub.bilibili-update' })
  } catch (error) {
    setStatus(errorMessage(error, '无法打开卡片预览'), true)
  }
}

function isCandidate(value: unknown): value is ResolveCandidate {
  if (!value || typeof value !== 'object') return false
  const candidate = value as Partial<ResolveCandidate>
  return typeof candidate.uid === 'string' && typeof candidate.name === 'string'
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback
}
</script>

<template>
  <a class="skip-link" href="#main-content">跳到主要内容</a>
  <main id="main-content" class="page-shell">
    <header class="page-header">
      <div>
        <p class="eyebrow" translate="no">raylea.subscription-hub</p>
        <h1>订阅设置</h1>
        <p>按平台账号管理推送对象</p>
      </div>
      <div class="status-pill" :class="{ 'is-error': statusIsError }" aria-live="polite">{{ status }}</div>
    </header>

    <AAlert v-if="hostErrorMessage" class="host-alert" type="error" :message="hostErrorMessage" show-icon />

    <section class="status-strip" aria-label="订阅中心状态">
      <label class="switch-row" for="enabled-input">
        <input id="enabled-input" v-model="settings.enabled" name="enabled" type="checkbox" autocomplete="off" />
        <span><strong>订阅中心</strong><small>{{ settings.enabled ? '启用' : '停用' }}</small></span>
      </label>
      <div class="strip-metric"><span>订阅</span><strong>{{ rows.length }} / {{ settings.subscriptions.length }}</strong></div>
      <div class="strip-metric"><span>推送对象</span><strong>{{ targetMetric }}</strong></div>
      <div class="strip-metric"><span>保存</span><strong>{{ errors.length === 0 ? '可保存' : '需处理' }}</strong></div>
      <button type="button" class="button button--small" :disabled="targetsLoading" @click="reloadTargets">{{ targetsLoading ? '刷新中…' : '刷新对象' }}</button>
    </section>

    <section class="panel">
      <div class="section-title section-title--inline">
        <div><h2>订阅管理</h2><p>同一平台账号的群聊和私聊目标合并在同一卡片编辑。</p></div>
        <button type="button" class="button button--primary-accent" @click="addSubscription">添加订阅</button>
      </div>

      <div class="toolbar" role="search">
        <label class="field field--search" for="subscription-search-input">
          <span>搜索</span>
          <input id="subscription-search-input" v-model="search" name="subscription_search" type="search" autocomplete="off" placeholder="搜索平台、账号、推送对象或订阅人…" />
        </label>
        <label class="field" for="status-filter-input">
          <span>状态</span>
          <select id="status-filter-input" v-model="statusFilter" name="status_filter" autocomplete="off">
            <option value="all">全部</option><option value="enabled">启用</option><option value="disabled">停用</option>
          </select>
        </label>
        <label class="field" for="service-filter-input">
          <span>类型</span>
          <select id="service-filter-input" v-model="serviceFilter" name="service_filter" autocomplete="off">
            <option value="all">全部类型</option><option value="live">直播</option><option value="video">视频</option><option value="image_text">图文</option><option value="article">文章</option><option value="repost">转发</option><option value="post">微博</option><option value="image">图片</option><option value="song">歌曲</option><option value="album">专辑</option><option value="playlist">歌单</option><option value="artist">音乐人</option>
          </select>
        </label>
      </div>

      <div ref="listRef" class="subscription-list" aria-live="polite">
        <SubscriptionCard
          v-for="row in visibleRows"
          :key="row.row_id"
          :row="row"
          :context="context"
          :resolving="resolvingRows.has(row.row_id)"
          @edit="beginEdit(row)"
          @cancel="cancelEdit(row)"
          @finish="finishEdit(row)"
          @duplicate="duplicateRow(row)"
          @remove="removeRow(row)"
          @resolve="scheduleResolve(row, true)"
          @choose-candidate="chooseCandidate(row, $event)"
          @target-mode="setTargetMode(row, $event)"
          @toggle-target="toggleTarget(row, $event)"
          @remove-target="removeTarget(row, $event)"
          @toggle-service="(targetKey, service, checked) => toggleService(row, targetKey, service, checked)"
          @add-subscriber="addSubscriber(row, $event)"
          @invalid-subscriber="setStatus('订阅人 QQ 号不正确', true)"
          @remove-subscriber="removeSubscriber(row, $event)"
          @platform-change="changePlatform(row, $event)"
          @query-change="scheduleResolve(row)"
          @query-composition-start="clearResolveTimer(row.row_id)"
        />
        <div v-if="visibleRows.length === 0" class="empty-state"><p>没有匹配的订阅</p><p>可添加订阅或调整筛选条件</p></div>
      </div>
    </section>

    <footer class="actions-bar">
      <div class="dirty-state" aria-live="polite">{{ dirtyStateText }}</div>
      <div class="footer-buttons">
        <button type="button" class="button" :disabled="saving" @click="reloadSettings">重新载入</button>
        <button type="button" class="button" :disabled="saving" @click="resetSettings">恢复默认</button>
        <button type="button" class="button" :disabled="checking || saving" @click="checkNow">{{ checking ? '检查中…' : '立即检查' }}</button>
        <button type="button" class="button" @click="openPreview">打开卡片预览</button>
        <button type="button" class="button button--primary" :disabled="!loaded || errors.length > 0 || saving" @click="saveSettings">{{ saving ? '保存中…' : '保存设置' }}</button>
      </div>
    </footer>
  </main>
</template>

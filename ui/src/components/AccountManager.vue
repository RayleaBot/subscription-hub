<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Alert as AAlert, Button as AButton, Input as AInput, InputNumber as AInputNumber, InputPassword as AInputPassword, Modal as AModal, Select as ASelect, SelectOption as ASelectOption, Switch as ASwitch, message as feedback } from 'ant-design-vue'
import type { usePluginHost } from '@rayleabot/plugin-ui'

import AvatarBadge from './AvatarBadge.vue'
import PlatformLogo from './PlatformLogo.vue'
import UiIcon from './UiIcon.vue'
import { encodeQRCode } from '../qrcode'

interface PlatformEntry {
  id: string
  name: string
}

interface AccountEntry {
  platform: string
  account_id: string
  label: string
  enabled: boolean
  uid?: string
  unique_id?: string
  nickname?: string
  avatar_url?: string
  credential_state: string
  credential_checked_at?: string
  credential_last_error?: string
  updated_at: string
}

interface AccountDraft {
  account_id: string
  label: string
  enabled: boolean
  cookie: string
}

interface QRSession {
  platform: string
  login_id: string
  qrcode_url: string
  state: string
  expires_at: string
  error: string
}

const props = defineProps<{ host: ReturnType<typeof usePluginHost> }>()
const host = props.host
const [feedbackApi, FeedbackContext] = feedback.useMessage()
const accounts = ref<AccountEntry[]>([])
const platforms = ref<PlatformEntry[]>([])
const loading = ref(true)
const busy = ref<Record<string, boolean>>({})
const status = ref('')
const statusIsError = ref(false)
const avatarDataURLs = ref(new Map<string, string>())

const editingKey = ref('')
const draft = ref<AccountDraft>({ account_id: '', label: '', enabled: true, cookie: '' })
const adding = ref(false)
const addPlatform = ref('')
const addDraft = ref<AccountDraft>({ account_id: '', label: '', enabled: true, cookie: '' })
const deleting = ref<AccountEntry | null>(null)
const query = ref('')
const page = ref(1)
const pageSize = 20
const platformFilter = ref('all')
const stateFilter = ref('all')
const settingsOpen = ref(false)
const addMode = ref<'qr' | 'manual'>('qr')
const editingAccount = computed(() => accounts.value.find(account => accountKey(account) === editingKey.value))
const filteredAccounts = computed(() => {
  const text = query.value.trim().toLocaleLowerCase()
  return accounts.value.filter(account => {
    if (platformFilter.value !== 'all' && account.platform !== platformFilter.value) return false
    if (stateFilter.value === 'attention' && account.credential_state === 'valid' && !account.credential_last_error) return false
    if (stateFilter.value === 'valid' && account.credential_state !== 'valid') return false
    if (stateFilter.value === 'disabled' && account.enabled) return false
    return !text || [platformName(account.platform), account.account_id, account.label, account.nickname, account.uid, account.unique_id].join(' ').toLocaleLowerCase().includes(text)
  }).sort((a, b) => {
    const attention = (item: AccountEntry) => Number(item.credential_state === 'invalid' || Boolean(item.credential_last_error))
    return attention(b) - attention(a) || (a.label || a.account_id).localeCompare(b.label || b.account_id, 'zh-CN') || accountKey(a).localeCompare(accountKey(b))
  })
})
const total = computed(() => filteredAccounts.value.length)
const visibleAccounts = computed(() => filteredAccounts.value.slice((page.value - 1) * pageSize, page.value * pageSize))
const hasFilters = computed(() => Boolean(query.value || platformFilter.value !== 'all' || stateFilter.value !== 'all'))
function clearFilters() { query.value = ''; platformFilter.value = 'all'; stateFilter.value = 'all' }

const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const accountSettings = ref({
  account_check_interval_minutes: Number(host.config.value.account_check_interval_minutes ?? 360),
  account_browser_mode: String(host.config.value.account_browser_mode ?? 'auto'),
  account_browser_remote_debugging_url: String(host.config.value.account_browser_remote_debugging_url ?? ''),
})
const qr = ref<QRSession | null>(null)
const qrImage = ref('')
let qrTimer: number | undefined
let qrGeneration = 0
let listGeneration = 0
let disposed = false
const qrStarting = ref(false)

const knownPlatformNames = computed(() => new Map(platforms.value.map((entry) => [entry.id, entry.name])))

void host.ready
  .then(async (init) => {
    accountSettings.value = {
      account_check_interval_minutes: Number(init.config.account_check_interval_minutes ?? 360),
      account_browser_mode: String(init.config.account_browser_mode ?? 'auto'),
      account_browser_remote_debugging_url: String(init.config.account_browser_remote_debugging_url ?? ''),
    }
    await refresh()
  })
  .catch((error: unknown) => {
    setStatus(errorMessage(error, '插件账号页面连接失败'), true)
    loading.value = false
  })

watch([query, platformFilter, stateFilter], () => { page.value = 1 })

onBeforeUnmount(() => {
  disposed = true
  listGeneration++
  feedbackApi.destroy()
  void cancelQR()
})

function accountKey(account: { platform: string; account_id: string }): string {
  return `${account.platform}:${account.account_id}`
}

function platformName(id: string): string {
  return knownPlatformNames.value.get(id) ?? id
}

function setStatus(message: string, isError = false) {
  status.value = isError ? message : ''
  statusIsError.value = isError
  if (message && !isError) void feedbackApi.success({ key: 'account-feedback', content: message, duration: 3 })
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message
  return fallback
}

async function invoke(action: string, payload: Record<string, unknown> = {}): Promise<Record<string, unknown>> {
  return host.client.invokeAction(action, payload)
}

async function refresh() {
  const generation = ++listGeneration
  loading.value = true
  try {
    // Read each server page before local filtering so filters and counts cover every account.
    const collected: AccountEntry[] = []
    let offset = 0
    let expected = 0
    do {
      const result = await invoke('account.list', { offset, limit: 100 })
      if (disposed || generation !== listGeneration) return
      if (typeof result.error === 'string' && result.error) throw new Error(String(result.message ?? result.error))
      const batch = Array.isArray(result.accounts) ? result.accounts as AccountEntry[] : []
      expected = Number(result.total ?? batch.length)
      if (batch.length === 0 && offset < expected) throw new Error('账号列表不完整，请刷新重试。')
      collected.push(...batch)
      offset += batch.length
      platforms.value = Array.isArray(result.platforms) ? result.platforms as PlatformEntry[] : []
    } while (offset < expected)
    accounts.value = [...new Map(collected.map(account => [accountKey(account), account])).values()]
    page.value = Math.min(page.value, pageCount.value)
    if (!addPlatform.value && platforms.value.length > 0) addPlatform.value = platforms.value[0].id
    loading.value = false
    if (statusIsError.value) setStatus('')
    await refreshAvatars(generation)
  } catch (error: unknown) {
    if (!disposed && generation === listGeneration) setStatus(errorMessage(error, '账号列表载入失败'), true)
  } finally {
    if (!disposed && generation === listGeneration) loading.value = false
  }
}

async function refreshAvatars(generation: number) {
  const urls = [...new Set(accounts.value.map((account) => account.avatar_url ?? '').filter((url) => url && !avatarDataURLs.value.has(url)))]
  if (urls.length === 0) return
  try {
    for (let offset = 0; offset < urls.length; offset += 4) {
      if (disposed || generation !== listGeneration) return
      const result = await invoke('subscription.resolve_avatars', { urls: urls.slice(offset, offset + 4) })
      if (disposed || generation !== listGeneration) return
      const items = Array.isArray(result.items) ? result.items as Array<{ source_url: string; data_url: string }> : []
      const next = new Map(avatarDataURLs.value)
      for (const item of items) next.set(item.source_url, item.data_url)
      avatarDataURLs.value = next
    }
  } catch {
    // Avatars are decorative; failures fall back to the text badge.
  }
}

function avatarURL(account: AccountEntry): string {
  const source = account.avatar_url ?? ''
  return source ? avatarDataURLs.value.get(source) ?? '' : ''
}

function credentialLabel(account: AccountEntry): string {
  switch (account.credential_state) {
    case 'valid': return '凭据有效'
    case 'invalid': return '凭据失效'
    default: return '待检查'
  }
}

function beginEdit(account: AccountEntry) {
  setStatus('')
  editingKey.value = accountKey(account)
  draft.value = { account_id: account.account_id, label: account.label, enabled: account.enabled, cookie: '' }
}

function cancelEdit() {
  editingKey.value = ''
}

function setBusy(key: string, value: boolean) {
  busy.value = { ...busy.value, [key]: value }
}

function isBusy(key: string): boolean {
  return busy.value[key] === true
}

async function saveEdit(account: AccountEntry) {
  const key = accountKey(account)
  setBusy(key, true)
  try {
    const result = await invoke('account.upsert', {
      platform: account.platform,
      account_id: account.account_id,
      label: draft.value.label.trim() || account.account_id,
      enabled: draft.value.enabled,
      cookie: draft.value.cookie.trim(),
    })
    if (typeof result.error === 'string' && result.error) {
      setStatus(String(result.message ?? result.error), true)
      return
    }
    editingKey.value = ''
    await refresh()
    setStatus('账号已保存')
  } catch (error: unknown) {
    setStatus(errorMessage(error, '账号保存失败'), true)
  } finally {
    setBusy(key, false)
  }
}

function openAdd() {
  setStatus('')
  adding.value = true
  addMode.value = 'qr'
  if (platformFilter.value !== 'all') addPlatform.value = platformFilter.value
  addDraft.value = { account_id: '', label: '', enabled: true, cookie: '' }
}

async function saveNew() {
  const accountID = addDraft.value.account_id.trim()
  const cookie = addDraft.value.cookie.trim()
  if (!accountID || !cookie || !addPlatform.value) {
    setStatus('新增账号需要平台、账号 ID 和 CK。', true)
    return
  }
  setBusy('__add__', true)
  try {
    const result = await invoke('account.upsert', {
      platform: addPlatform.value,
      account_id: accountID,
      label: addDraft.value.label.trim() || accountID,
      enabled: addDraft.value.enabled,
      cookie,
      create_only: true,
    })
    if (typeof result.error === 'string' && result.error) {
      setStatus(String(result.message ?? result.error), true)
      return
    }
    adding.value = false
    await refresh()
    setStatus('账号已添加')
  } catch (error: unknown) {
    setStatus(errorMessage(error, '账号保存失败'), true)
  } finally {
    setBusy('__add__', false)
  }
}

async function removeAccount(account: AccountEntry) {
  const key = accountKey(account)
  setBusy(key, true)
  try {
    const result = await invoke('account.delete', { platform: account.platform, account_id: account.account_id })
    if (typeof result.error === 'string' && result.error) {
      setStatus(String(result.message ?? result.error), true)
      return
    }
    deleting.value = null
    await refresh()
    setStatus('账号已删除')
  } catch (error: unknown) {
    setStatus(errorMessage(error, '账号删除失败'), true)
  } finally {
    setBusy(key, false)
  }
}

async function validateAccount(account: AccountEntry) {
  const key = accountKey(account)
  setBusy(key, true)
  try {
    const result = await invoke('account.validate', { platform: account.platform, account_id: account.account_id })
    if (typeof result.error === 'string' && result.error) {
      setStatus(String(result.message ?? result.error), true)
      return
    }
    await refresh()
    const updated = accounts.value.find((item) => accountKey(item) === key)
    setStatus(updated?.credential_last_error || 'CK 检查完成', Boolean(updated?.credential_last_error))
  } catch (error: unknown) {
    setStatus(errorMessage(error, 'CK 检查失败'), true)
  } finally {
    setBusy(key, false)
  }
}

async function saveAccountSettings() {
  const interval = accountSettings.value.account_check_interval_minutes
  if (!Number.isInteger(interval) || (interval !== 0 && (interval < 15 || interval > 10080))) {
    setStatus('检查间隔应为 15～10080 分钟，填 0 关闭自动检查。', true)
    return
  }
  const endpoint = accountSettings.value.account_browser_remote_debugging_url.trim()
  if (accountSettings.value.account_browser_mode === 'remote_cdp' && !endpoint) {
    setStatus('远程浏览器模式需要填写调试地址。', true)
    return
  }
  if (endpoint) {
    try {
      const parsed = new URL(endpoint)
      if (!['http:', 'https:', 'ws:', 'wss:'].includes(parsed.protocol) || parsed.username || parsed.password || parsed.hash
        || !(/^(localhost|127(?:\.\d{1,3}){3}|\[::1\])$/i.test(parsed.hostname))) throw new Error()
    } catch {
      setStatus('调试地址必须是无用户名和密码的本机回环 HTTP(S) 或 WS(S) 地址。', true)
      return
    }
  }
  setBusy('__settings__', true)
  try {
    await host.client.saveSettings({ ...accountSettings.value, account_browser_remote_debugging_url: endpoint })
    setStatus('账号设置已保存')
    settingsOpen.value = false
  } catch (error) {
    setStatus(errorMessage(error, '账号设置保存失败'), true)
  } finally {
    setBusy('__settings__', false)
  }
}

async function movePage(next: number) {
  page.value = Math.max(1, Math.min(pageCount.value, next))
}

function checkedAtText(value?: string): string {
  if (!value) return '尚未检查'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? '尚未检查' : parsed.toLocaleString('zh-CN')
}

async function releaseQR(session: Pick<QRSession, 'platform' | 'login_id'> | null) {
  if (!session?.login_id) return
  try { await invoke('account.qr_cancel', { platform: session.platform, login_id: session.login_id }) } catch {
    // The host enforces the session deadline if the page loses its connection.
  }
}

async function startQR(platform: string) {
  if (qrStarting.value || disposed) return
  qrStarting.value = true
  await cancelQR()
  const generation = qrGeneration
  try {
    if (disposed) return
    const result = await invoke('account.qr_create', { platform })
    const created = { platform, login_id: String(result.login_id ?? '') }
    if (disposed || generation !== qrGeneration) { await releaseQR(created); return }
    if (typeof result.error === 'string' && result.error) {
      setStatus(String(result.message ?? result.error), true)
      return
    }
    adding.value = false
    qr.value = {
      ...created,
      qrcode_url: String(result.qrcode_url ?? ''),
      state: String(result.state ?? 'pending_scan'),
      expires_at: String(result.expires_at ?? ''),
      error: '',
    }
    qrImage.value = qr.value.qrcode_url ? encodeQRCode(qr.value.qrcode_url) : ''
    scheduleQRPoll(generation)
  } catch (error: unknown) {
    if (!disposed && generation === qrGeneration) {
      setStatus(errorMessage(error, '扫码登录创建失败'), true)
      await cancelQR()
    }
  } finally {
    qrStarting.value = false
  }
}

function scheduleQRPoll(generation: number) {
  if (!disposed && generation === qrGeneration) qrTimer = window.setTimeout(() => { void pollQR(generation) }, 2000)
}

async function pollQR(generation: number) {
  const session = qr.value
  if (!session?.login_id || disposed || generation !== qrGeneration) return
  try {
    const result = await invoke('account.qr_poll', { platform: session.platform, login_id: session.login_id })
    if (disposed || generation !== qrGeneration || qr.value !== session) return
    if (typeof result.error === 'string' && result.error) throw new Error(String(result.message ?? result.error))
    const state = String(result.state ?? '')
    if (!['pending_scan', 'pending_confirm', 'verification_required', 'succeeded', 'expired', 'failed'].includes(state)) throw new Error('扫码状态无法识别，请重新扫码。')
    session.state = state
    if (state === 'succeeded') {
      await cancelQR()
      if (disposed) return
      await refresh()
      setStatus('扫码登录成功，账号已保存')
    } else if (state === 'expired' || state === 'failed') {
      session.error = '二维码已过期或登录被阻断，请重新扫码。'
      await releaseQR(session)
    } else {
      scheduleQRPoll(generation)
    }
  } catch (error: unknown) {
    if (disposed || generation !== qrGeneration || qr.value !== session) return
    session.error = errorMessage(error, '扫码状态读取失败')
    session.state = 'failed'
    await releaseQR(session)
  }
}

async function cancelQR() {
  ++qrGeneration
  stopQRPolling()
  const session = qr.value
  qr.value = null
  qrImage.value = ''
  await releaseQR(session)
}

function stopQRPolling() {
  if (qrTimer !== undefined) {
    window.clearTimeout(qrTimer)
    qrTimer = undefined
  }
}

function qrStateText(state: string): string {
  switch (state) {
    case 'pending_confirm': return '已扫码，请在客户端确认'
    case 'verification_required': return '请在登录浏览器完成短信、滑块或验证码验证'
    case 'succeeded': return '登录成功'
    case 'expired': return '二维码已过期'
    case 'failed': return '扫码登录失败'
    default: return '等待扫码'
  }
}
</script>

<template>
  <FeedbackContext />
  <section class="accounts-workspace" aria-label="账号管理">
    <div class="workspace-toolbar">
      <label class="search-control"><UiIcon name="search" /><input v-model="query" type="search" aria-label="搜索账号" placeholder="搜索昵称、备注或账号 ID" /></label>
      <div class="toolbar-actions">
        <button type="button" class="button icon-button" :disabled="loading" title="刷新账号" aria-label="刷新账号" @click="refresh"><UiIcon name="refresh" :class="{ 'is-spinning': loading }" /></button>
        <button type="button" class="button" @click="settingsOpen = true"><UiIcon name="settings" />账号设置</button>
        <button type="button" class="button button--primary" :disabled="!platforms.length" @click="openAdd"><UiIcon name="plus" />添加账号</button>
      </div>
    </div>
    <div class="filter-bar">
      <div class="filter-chips" role="group" aria-label="平台筛选">
        <button type="button" class="filter-chip" :aria-pressed="platformFilter === 'all'" @click="platformFilter = 'all'">全部平台<span class="filter-count">{{ accounts.length }}</span></button>
        <button v-for="platform in platforms" :key="platform.id" type="button" class="filter-chip" :aria-pressed="platformFilter === platform.id" @click="platformFilter = platform.id"><PlatformLogo :platform="platform.id" :size="20" />{{ platform.name }}</button>
      </div>
      <div class="filter-chips filter-chips--secondary" role="group" aria-label="账号状态筛选">
        <button v-for="item in [{ id: 'all', label: '全部状态' }, { id: 'attention', label: '需处理' }, { id: 'valid', label: '有效' }, { id: 'disabled', label: '已停用' }]" :key="item.id" type="button" class="filter-chip" :aria-pressed="stateFilter === item.id" @click="stateFilter = item.id">{{ item.label }}</button>
      </div>
    </div>
    <div class="collection-caption"><span>{{ total }} 个账号<span v-if="loading"> · 正在刷新…</span></span><span>订阅与解析共用 · 异常账号优先</span></div>
    <AAlert v-if="status" class="host-alert" type="error" :message="status" show-icon closable @close="status = ''" />
    <div v-if="loading && !accounts.length" class="account-grid" role="status" aria-label="正在载入账号">
      <div v-for="item in 3" :key="item" class="account-skeleton"><div class="skeleton skeleton--avatar"></div><div class="skeleton"></div><div class="skeleton skeleton--short"></div></div>
    </div>
    <TransitionGroup v-else name="cards" tag="div" class="account-grid" :aria-busy="loading">
      <article v-for="account in visibleAccounts" :key="accountKey(account)" class="account-card" :class="{ 'account-card--disabled': !account.enabled }">
        <div class="account-card__platform"><span><PlatformLogo :platform="account.platform" :size="28" />{{ platformName(account.platform) }}</span><span class="account-enabled"><span class="status-dot" :class="{ 'status-dot--active': account.enabled }"></span>{{ account.enabled ? '已启用' : '已停用' }}</span></div>
        <div class="account-card__identity">
          <AvatarBadge :url="avatarURL(account)" :label="account.nickname || account.label || account.account_id" size="up" />
          <div class="account-card__meta"><h2 :title="account.label || account.account_id">{{ account.label || account.account_id }}</h2><span v-if="account.nickname && account.nickname !== account.label">{{ account.nickname }}</span><small :title="account.account_id">ID {{ account.account_id }}</small></div>
        </div>
        <div class="account-health" :class="`account-health--${account.credential_state}`"><UiIcon :name="account.credential_state === 'valid' ? 'shield' : account.credential_state === 'invalid' ? 'inbox' : 'clock'" :size="16" /><strong>{{ credentialLabel(account) }}</strong><span v-if="!account.credential_last_error">{{ account.credential_state === 'valid' ? account.enabled ? '可用于订阅与解析' : '启用后可使用' : '检查后更新状态' }}</span></div>
        <p v-if="account.credential_last_error" class="account-card__error">{{ account.credential_last_error }}</p>
        <div class="account-card__time"><UiIcon name="clock" :size="14" /><span>{{ account.credential_checked_at ? '检查于 ' : '' }}{{ checkedAtText(account.credential_checked_at) }}</span></div>
        <div class="account-card__actions">
          <button type="button" class="button button--small" :disabled="isBusy(accountKey(account))" @click="validateAccount(account)"><UiIcon name="refresh" :size="15" :class="{ 'is-spinning': isBusy(accountKey(account)) }" />{{ isBusy(accountKey(account)) ? '检查中…' : '检查 CK' }}</button>
          <button v-if="account.credential_state === 'invalid'" type="button" class="button button--small" :disabled="qrStarting" @click="startQR(account.platform)"><UiIcon name="qr" :size="15" />重新登录</button>
          <div class="account-card__utilities"><button type="button" class="button icon-button" title="编辑账号" :aria-label="`编辑 ${account.label || account.account_id}`" :disabled="isBusy(accountKey(account))" @click="beginEdit(account)"><UiIcon name="edit" /></button><button type="button" class="button icon-button danger-action" title="删除账号" :aria-label="`删除 ${account.label || account.account_id}`" :disabled="isBusy(accountKey(account))" @click="deleting = account"><UiIcon name="trash" /></button></div>
        </div>
      </article>
    </TransitionGroup>
    <section v-if="!loading && !total" class="empty-state" role="status"><UiIcon name="inbox" :size="32" /><h2>{{ hasFilters ? '没有匹配的账号' : '连接你的第一个账号' }}</h2><p>{{ hasFilters ? '试试其他关键词，或清除筛选条件。' : '扫码登录后，即可用于平台订阅与链接解析。' }}</p><button type="button" class="button" @click="hasFilters ? clearFilters() : openAdd()">{{ hasFilters ? '清除筛选' : '添加账号' }}</button></section>
    <nav v-if="pageCount > 1" class="account-pagination" aria-label="账号分页"><button type="button" class="button" :disabled="page <= 1" @click="movePage(page - 1)">上一页</button><span>{{ page }} / {{ pageCount }} 页</span><button type="button" class="button" :disabled="page >= pageCount" @click="movePage(page + 1)">下一页</button></nav>
  </section>

  <AModal :footer="null" :open="settingsOpen" title="账号设置" centered :width="440" class="hub-dialog" @cancel="settingsOpen = false">
    <form class="panel-form" @submit.prevent="saveAccountSettings">
      <p class="panel-description">自动维护账号状态，选择扫码登录使用的浏览器。</p>
      <label class="panel-field"><span>自动检查间隔</span><AInputNumber v-model:value="accountSettings.account_check_interval_minutes" :min="0" :max="10080" :precision="0" addon-after="分钟" /><small>15～10080 分钟；填 0 关闭自动检查。</small></label>
      <label class="panel-field"><span>登录浏览器</span><ASelect v-model:value="accountSettings.account_browser_mode"><ASelectOption value="auto">自动选择</ASelectOption><ASelectOption value="visible">显示浏览器</ASelectOption><ASelectOption value="headless">无界面浏览器</ASelectOption><ASelectOption value="remote_cdp">远程调试浏览器</ASelectOption></ASelect></label>
      <label v-if="accountSettings.account_browser_mode === 'remote_cdp'" class="panel-field"><span>本机浏览器调试地址</span><AInput v-model:value="accountSettings.account_browser_remote_debugging_url" placeholder="http://127.0.0.1:9222" /><small>连接已启动的本机浏览器。</small></label>
      <AAlert v-if="statusIsError" type="error" :message="status" show-icon />
      <AButton html-type="submit" type="primary" :loading="isBusy('__settings__')">保存账号设置</AButton>
    </form>
  </AModal>
  <AModal :footer="null" :open="Boolean(editingAccount)" title="编辑账号" centered :width="440" class="hub-dialog" @cancel="cancelEdit">
    <form v-if="editingAccount" class="panel-form" @submit.prevent="saveEdit(editingAccount)">
      <div class="panel-identity"><PlatformLogo :platform="editingAccount.platform" :size="32" /><div><strong>{{ platformName(editingAccount.platform) }}</strong><small>ID {{ editingAccount.account_id }}</small><small v-if="editingAccount.uid">UID {{ editingAccount.uid }}</small><small v-if="editingAccount.unique_id">抖音号 {{ editingAccount.unique_id }}</small></div></div>
      <label class="panel-field"><span>备注</span><AInput v-model:value="draft.label" placeholder="账号备注" /></label>
      <label class="panel-field"><span>更新 CK</span><AInputPassword v-model:value="draft.cookie" autocomplete="new-password" placeholder="留空保留当前 CK" /><small>已有凭据不会回显；仅在需要更新时填写。</small></label>
      <label class="switch-row"><span>启用账号</span><ASwitch v-model:checked="draft.enabled" /></label>
      <AAlert v-if="statusIsError" type="error" :message="status" show-icon />
      <AButton html-type="submit" type="primary" :loading="isBusy(accountKey(editingAccount))">保存账号</AButton>
    </form>
  </AModal>
  <AModal centered :open="deleting !== null" title="删除账号" ok-text="删除" cancel-text="取消" :ok-button-props="{ danger: true }" :confirm-loading="deleting ? isBusy(accountKey(deleting)) : false" @ok="deleting && removeAccount(deleting)" @cancel="deleting = null"><p v-if="deleting">删除 {{ platformName(deleting.platform) }} 账号“{{ deleting.label || deleting.account_id }}”及其本地凭据？此操作无法撤销。</p><AAlert v-if="statusIsError" type="error" :message="status" show-icon /></AModal>
  <AModal centered :open="adding" title="添加账号" :footer="null" @cancel="adding = false">
    <form class="panel-form" @submit.prevent="addMode === 'qr' ? startQR(addPlatform) : saveNew()">
      <div class="platform-choices" role="group" aria-label="选择账号平台"><button v-for="platform in platforms" :key="platform.id" type="button" :aria-pressed="addPlatform === platform.id" @click="addPlatform = platform.id"><PlatformLogo :platform="platform.id" :size="36" /><span>{{ platform.name }}</span></button></div>
      <div class="filter-chips" role="group" aria-label="登录方式"><button type="button" class="filter-chip" :aria-pressed="addMode === 'qr'" @click="addMode = 'qr'">扫码登录</button><button type="button" class="filter-chip" :aria-pressed="addMode === 'manual'" @click="addMode = 'manual'">手动填写 CK</button></div>
      <p v-if="addMode === 'qr'" class="panel-description">使用平台客户端扫码，成功后自动保存账号与凭据。</p>
      <template v-else><label class="panel-field"><span>账号 ID</span><AInput v-model:value="addDraft.account_id" placeholder="小写字母、数字、下划线、点或中划线" /></label><label class="panel-field"><span>备注</span><AInput v-model:value="addDraft.label" placeholder="留空使用账号 ID" /></label><label class="panel-field"><span>CK</span><AInputPassword v-model:value="addDraft.cookie" autocomplete="new-password" placeholder="粘贴平台 Cookie" /></label><label class="switch-row"><span>启用账号</span><ASwitch v-model:checked="addDraft.enabled" /></label></template>
      <AAlert v-if="statusIsError" type="error" :message="status" show-icon />
      <AButton html-type="submit" type="primary" :loading="addMode === 'qr' ? qrStarting : isBusy('__add__')" :disabled="!addPlatform">{{ addMode === 'qr' ? '扫码获取 CK' : '保存账号' }}</AButton>
    </form>
  </AModal>
  <AModal centered :open="qr !== null" title="扫码登录" :footer="null" @cancel="cancelQR"><div v-if="qr" class="qr-panel"><div class="panel-identity"><PlatformLogo :platform="qr.platform" :size="28" /><strong>{{ platformName(qr.platform) }}</strong></div><div class="qr-frame"><img v-if="qrImage" :src="qrImage" alt="登录二维码" width="200" height="200" /></div><strong role="status">{{ qrStateText(qr.state) }}</strong><p v-if="qr.error" class="account-card__error">{{ qr.error }}</p><small>使用 {{ platformName(qr.platform) }} 客户端扫码，成功后自动保存。</small><AButton v-if="qr.state === 'expired' || qr.state === 'failed'" :loading="qrStarting" @click="startQR(qr.platform)">重新扫码</AButton><AButton @click="cancelQR">取消扫码</AButton></div></AModal>
</template>
<style scoped>
.account-grid { position: relative; display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr)); gap: 18px; align-items: start; }
.account-card { min-width: 0; display: flex; flex-direction: column; padding: 20px; border: 1px solid var(--border); border-radius: 14px; background: var(--surface); transition: border-color 180ms ease-out; }
.account-card:hover { border-color: var(--border-strong); }
.account-card--disabled { background: var(--surface-soft); }
.account-card__platform { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 22px; }
.account-card__platform > span { display: inline-flex; align-items: center; gap: 8px; color: var(--muted); font-size: 12px; }
.account-enabled { white-space: nowrap; }
.status-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--muted); }.status-dot--active { background: var(--success); }
.account-card__identity { display: flex; gap: 12px; align-items: center; margin-bottom: 22px; }
.account-card__meta { min-width: 0; display: grid; gap: 2px; }.account-card__meta h2 { font-size: 16px; line-height: 1.5; }.account-card__meta :is(h2, span, small) { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }.account-card__meta :is(span, small) { color: var(--muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.account-health { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--muted); }.account-health strong { font-weight: 600; }.account-health > span { margin-left: auto; font-size: 11px; color: var(--muted); }.account-health--valid { color: var(--success); }.account-health--invalid { color: var(--danger); }
.account-card__error { margin: 10px 0 0; padding: 10px 12px; border-radius: 8px; color: var(--danger); background: var(--danger-soft); line-height: 1.65; font-size: 12px; overflow-wrap: anywhere; }
.account-card__time { display: flex; align-items: center; gap: 6px; color: var(--muted); font-size: 11px; margin: 12px 0 18px; font-variant-numeric: tabular-nums; }
.account-card__actions { display: flex; gap: 6px; align-items: center; border-top: 1px solid var(--border); padding-top: 14px; }.account-card__utilities { margin-left: auto; display: flex; gap: 2px; }.account-card__utilities .button { border-color: transparent; background: transparent; }
.account-pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 24px; color: var(--muted); font-size: 12px; }
.account-skeleton { padding: 24px; background: var(--surface); border: 1px solid var(--border); border-radius: 14px; min-height: 240px; }.skeleton { height: 14px; width: 80%; margin: 20px 0; background: var(--surface-strong); border-radius: 6px; }.skeleton--avatar { width: 48px; height: 48px; border-radius: 50%; }.skeleton--short { width: 50%; }
.qr-panel { display: flex; flex-direction: column; align-items: center; gap: 18px; padding: 12px 0; text-align: center; }.qr-panel small { color: var(--muted); }.qr-frame { padding: 16px; border-radius: 12px; background: white; }.qr-frame img { display: block; }
@media (max-width: 600px) { .account-grid { gap: 12px; }.account-card { padding: 18px; }.account-card__platform { margin-bottom: 18px; } }
</style>

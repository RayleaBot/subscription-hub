<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Alert as AAlert, Button as AButton, Input as AInput, InputNumber as AInputNumber, Modal as AModal, Select as ASelect, SelectOption as ASelectOption, Switch as ASwitch, Tag as ATag } from 'ant-design-vue'
import type { usePluginHost } from '@rayleabot/plugin-ui'

import AvatarBadge from './AvatarBadge.vue'
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
const total = ref(0)
const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const accountSettings = ref({
  account_check_interval_minutes: Number(host.config.value.account_check_interval_minutes ?? 360),
  account_browser_mode: String(host.config.value.account_browser_mode ?? 'auto'),
  account_browser_remote_debugging_url: String(host.config.value.account_browser_remote_debugging_url ?? ''),
})
const qr = ref<QRSession | null>(null)
const qrImage = ref('')
let qrTimer: number | undefined
let searchTimer: number | undefined
let qrGeneration = 0
let listGeneration = 0
let disposed = false
const qrStarting = ref(false)

const knownPlatformNames = computed(() => new Map(platforms.value.map((entry) => [entry.id, entry.name])))

void host.ready
  .then(async () => {
    await refresh()
  })
  .catch((error: unknown) => {
    setStatus(errorMessage(error, '插件账号页面连接失败'), true)
    loading.value = false
  })

watch(query, () => {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => { page.value = 1; void refresh() }, 250)
})

onBeforeUnmount(() => {
  disposed = true
  listGeneration++
  if (searchTimer !== undefined) window.clearTimeout(searchTimer)
  void cancelQR()
})

function accountKey(account: { platform: string; account_id: string }): string {
  return `${account.platform}:${account.account_id}`
}

function platformName(id: string): string {
  return knownPlatformNames.value.get(id) ?? id
}

function setStatus(message: string, isError = false) {
  status.value = message
  statusIsError.value = isError
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
    const result = await invoke('account.list', { query: query.value.trim(), offset: (page.value - 1) * pageSize, limit: pageSize })
    if (disposed || generation !== listGeneration) return
    if (typeof result.error === 'string' && result.error) {
      setStatus(String(result.message ?? result.error), true)
      return
    }
    accounts.value = Array.isArray(result.accounts) ? result.accounts as AccountEntry[] : []
    total.value = Number(result.total ?? accounts.value.length)
    if (page.value > pageCount.value) { page.value = pageCount.value; await refresh(); return }
    platforms.value = Array.isArray(result.platforms) ? result.platforms as PlatformEntry[] : []
    if (!addPlatform.value && platforms.value.length > 0) addPlatform.value = platforms.value[0].id
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
    case 'valid': return '检查有效'
    case 'invalid': return 'CK 已失效'
    default: return '待检查'
  }
}

function credentialColor(account: AccountEntry): string {
  switch (account.credential_state) {
    case 'valid': return 'green'
    case 'invalid': return 'red'
    default: return 'default'
  }
}

function beginEdit(account: AccountEntry) {
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
  adding.value = true
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
    setStatus(updated?.credential_last_error || 'CK 检查完成')
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
  } catch (error) {
    setStatus(errorMessage(error, '账号设置保存失败'), true)
  } finally {
    setBusy('__settings__', false)
  }
}

async function movePage(next: number) {
  page.value = Math.max(1, Math.min(pageCount.value, next))
  await refresh()
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
  <AAlert v-if="status" class="host-alert" :type="statusIsError ? 'error' : 'info'" :message="status" show-icon />
  <section class="account-toolbar">
    <div>
      <strong>三方账号</strong>
      <small>管理订阅检查与链接解析使用的平台账号。</small>
    </div>
    <div class="footer-buttons">
      <AButton size="small" :disabled="loading" @click="refresh">刷新</AButton>
      <AButton size="small" type="primary" @click="openAdd">添加账号</AButton>
    </div>
  </section>

  <details class="account-settings">
    <summary>账号设置</summary>
    <div class="account-editor account-editor--dialog">
      <label><span>自动检查间隔（分钟，0 表示关闭）</span><AInputNumber v-model:value="accountSettings.account_check_interval_minutes" :min="0" :max="10080" :precision="0" /></label>
      <label><span>登录浏览器模式</span><ASelect v-model:value="accountSettings.account_browser_mode">
        <ASelectOption value="auto">自动选择</ASelectOption><ASelectOption value="visible">显示浏览器</ASelectOption><ASelectOption value="headless">无界面浏览器</ASelectOption><ASelectOption value="remote_cdp">远程调试浏览器</ASelectOption>
      </ASelect></label>
      <label><span>本机浏览器调试地址</span><AInput v-model:value="accountSettings.account_browser_remote_debugging_url" placeholder="http://127.0.0.1:9222" /></label>
      <AButton :loading="isBusy('__settings__')" @click="saveAccountSettings">保存账号设置</AButton>
    </div>
  </details>
  <AInput v-model:value="query" aria-label="搜索账号" placeholder="搜索平台、账号 ID、备注或昵称" allow-clear />
  <section v-if="loading" class="empty" role="status">正在载入账号…</section>
  <section v-else-if="accounts.length === 0" class="empty" role="status">{{ query ? '没有匹配的账号。' : '当前没有账号，先添加账号或扫码登录。' }}</section>

  <section v-for="platform in platforms" :key="platform.id" class="platform-section">
    <div class="platform-section__head">
      <strong>{{ platform.name }}</strong>
      <div class="footer-buttons">
        <AButton size="small" :loading="qrStarting" @click="startQR(platform.id)">扫码获取 CK</AButton>
      </div>
    </div>

    <div class="account-grid">
      <article
        v-for="account in accounts.filter((entry) => entry.platform === platform.id)"
        :key="accountKey(account)"
        class="account-card"
        :class="{ 'account-card--editing': editingKey === accountKey(account) }"
      >
        <div class="account-card__head">
          <AvatarBadge :url="avatarURL(account)" :label="account.label || account.account_id" size="up" />
          <div class="account-card__meta">
            <strong>{{ account.label || account.account_id }}</strong>
            <small>{{ platformName(account.platform) }} · ID {{ account.account_id }}</small>
            <small v-if="account.nickname || account.uid">{{ account.nickname || '未命名' }} · UID {{ account.uid || '—' }}<template v-if="account.unique_id"> · 抖音号 {{ account.unique_id }}</template></small>
          </div>
          <div class="account-card__badges">
            <ATag :color="account.enabled ? 'blue' : 'default'">{{ account.enabled ? '启用' : '停用' }}</ATag>
            <ATag :color="credentialColor(account)">{{ credentialLabel(account) }}</ATag>
          </div>
        </div>

        <template v-if="editingKey === accountKey(account)">
          <div class="account-editor">
            <label>
              <span>备注</span>
              <AInput v-model:value="draft.label" size="small" placeholder="账号备注" />
            </label>
            <label>
              <span>CK</span>
              <AInput v-model:value="draft.cookie" size="small" placeholder="留空时保留当前 CK" />
            </label>
            <label class="switch-row">
              <span>启用</span>
              <ASwitch v-model:checked="draft.enabled" size="small" />
            </label>
          </div>
          <div class="account-card__actions">
            <AButton size="small" :disabled="isBusy(accountKey(account))" @click="cancelEdit">取消</AButton>
            <AButton size="small" type="primary" :loading="isBusy(accountKey(account))" @click="saveEdit(account)">保存</AButton>
          </div>
        </template>
        <template v-else>
          <small>最近检查：{{ checkedAtText(account.credential_checked_at) }}</small>
          <p v-if="account.credential_last_error" class="account-card__error">{{ account.credential_last_error }}</p>
          <div class="account-card__actions">
            <AButton size="small" :loading="isBusy(accountKey(account))" @click="validateAccount(account)">检查 CK</AButton>
            <AButton size="small" @click="beginEdit(account)">编辑</AButton>
            <AButton size="small" danger :loading="isBusy(accountKey(account))" @click="deleting = account">删除</AButton>
          </div>
        </template>
      </article>
    </div>
  </section>

  <nav class="account-pagination" aria-label="账号分页">
    <AButton :disabled="loading || page <= 1" @click="movePage(page - 1)">上一页</AButton>
    <span>共 {{ total }} 个账号 · {{ page }} / {{ pageCount }} 页</span>
    <AButton :disabled="loading || page >= pageCount" @click="movePage(page + 1)">下一页</AButton>
  </nav>
  <AModal :open="deleting !== null" title="删除账号" ok-text="删除" :confirm-loading="deleting ? isBusy(accountKey(deleting)) : false" @ok="deleting && removeAccount(deleting)" @cancel="deleting = null">
    <p v-if="deleting">确认删除 {{ platformName(deleting.platform) }} 账号“{{ deleting.label || deleting.account_id }}”及其本地凭据？此操作无法撤销。</p>
  </AModal>
  <AModal :open="adding" title="添加账号" :confirm-loading="isBusy('__add__')" @ok="saveNew" @cancel="adding = false">
    <div class="account-editor account-editor--dialog">
      <label>
        <span>平台</span>
        <ASelect v-model:value="addPlatform" size="small">
          <ASelectOption v-for="platform in platforms" :key="platform.id" :value="platform.id">{{ platform.name }}</ASelectOption>
        </ASelect>
      </label>
      <label>
        <span>账号 ID</span>
        <AInput v-model:value="addDraft.account_id" size="small" placeholder="小写字母、数字、下划线、点或中划线" />
      </label>
      <label>
        <span>备注</span>
        <AInput v-model:value="addDraft.label" size="small" placeholder="留空使用账号 ID" />
      </label>
      <label>
        <span>CK</span>
        <AInput v-model:value="addDraft.cookie" size="small" placeholder="粘贴平台 Cookie" />
      </label>
      <label class="switch-row">
        <span>启用</span>
        <ASwitch v-model:checked="addDraft.enabled" size="small" />
      </label>
    </div>
  </AModal>

  <AModal :open="qr !== null" title="扫码获取 CK" :footer="null" @cancel="cancelQR">
    <div v-if="qr" class="qr-panel">
      <img v-if="qrImage" :src="qrImage" alt="登录二维码" width="168" height="168" />
      <div>
        <p role="status">{{ qrStateText(qr.state) }}</p>
        <p v-if="qr.error" class="account-card__error">{{ qr.error }}</p>
        <small>使用 {{ platformName(qr.platform) }} 客户端扫码，登录成功后凭据会自动保存。</small>
      </div>
      <AButton v-if="qr.state === 'expired' || qr.state === 'failed'" size="small" :loading="qrStarting" @click="startQR(qr.platform)">重新扫码</AButton>
      <AButton size="small" @click="cancelQR">取消扫码</AButton>
    </div>
  </AModal>
</template>

<style scoped>
.account-settings { margin-bottom: 16px; }
.account-settings summary { cursor: pointer; padding: 8px 0; }
.account-pagination { display: flex; align-items: center; justify-content: center; gap: 12px; margin: 16px 0; }

.account-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin: 8px 0 16px;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface-soft, transparent);
}

.account-toolbar small,
.platform-section__empty {
  display: block;
  margin-top: 4px;
  color: var(--muted);
  font-size: 12px;
}

.platform-section {
  margin-bottom: 24px;
}

.platform-section__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}

.account-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 12px;
}

.account-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-height: 168px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface, transparent);
}

.account-card--editing {
  border-color: var(--color-primary, var(--border));
}

.account-card__head {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.account-card__meta {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
}

.account-card__meta small {
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-card__badges {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
}

.account-card__error {
  margin: 0;
  color: var(--color-error, #cf1322);
  font-size: 12px;
}

.account-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: auto;
}

.account-editor {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.account-editor--dialog label {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.account-editor label > span:first-child {
  color: var(--muted);
  font-size: 12px;
}

.switch-row {
  flex-direction: row !important;
  align-items: center;
  justify-content: space-between;
}

.qr-panel {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  text-align: center;
}

.empty {
  padding: 32px 0;
  color: var(--muted);
  text-align: center;
}
</style>

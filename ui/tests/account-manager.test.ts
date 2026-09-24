import { createApp, nextTick, type App as VueApp } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ useHost: vi.fn(), invoke: vi.fn(), save: vi.fn(), toast: vi.fn(), destroyToast: vi.fn() }))
vi.mock('@rayleabot/plugin-ui', () => ({ usePluginHost: mocks.useHost }))
vi.mock('ant-design-vue', () => ({
  message: { useMessage: () => [{ success: mocks.toast, destroy: mocks.destroyToast }, { template: '<span />' }] },
  Alert: { props: ['message'], template: '<div role="status">{{ message }}</div>' },
  Button: { props: ['disabled', 'loading', 'htmlType'], template: '<button :type="htmlType || \'button\'" :disabled="disabled || loading"><slot /></button>' },
  Input: { props: ['value'], emits: ['update:value'], template: '<input :value="value" @input="$emit(\'update:value\', $event.target.value)" />' },
  InputPassword: { props: ['value'], emits: ['update:value'], template: '<input type="password" :value="value" @input="$emit(\'update:value\', $event.target.value)" />' },
  InputNumber: { props: ['value'], emits: ['update:value'], template: '<input type="number" :value="value" @input="$emit(\'update:value\', Number($event.target.value))" />' },
  Select: { props: ['value'], emits: ['update:value'], template: '<select :value="value" @change="$emit(\'update:value\', $event.target.value)"><slot /></select>' },
  SelectOption: { props: ['value'], template: '<option :value="value"><slot /></option>' },
  Switch: { props: ['checked'], emits: ['update:checked'], template: '<input type="checkbox" :checked="checked" @change="$emit(\'update:checked\', $event.target.checked)" />' },
  Tag: { template: '<span><slot /></span>' },
  Modal: { props: ['open', 'title', 'footer'], emits: ['ok', 'cancel'], template: '<section v-if="open" role="dialog"><strong>{{ title }}</strong><slot /><button v-if="footer !== null" @click="$emit(\'ok\')">确认</button><button @click="$emit(\'cancel\')">关闭</button></section>' },
}))

import App from '../src/App.vue'

let app: VueApp | undefined
let root: HTMLDivElement
let records: Array<Record<string, unknown>>
let total: number

async function settle() { for (let i = 0; i < 20; i++) { await Promise.resolve(); await nextTick() }; await vi.advanceTimersByTimeAsync(40); await nextTick() }
function button(label: string) {
  const found = [...root.querySelectorAll('button')].find(item => item.textContent?.trim() === label || item.getAttribute('aria-label') === label)
  if (!found) throw new Error(`button missing: ${label}`)
  return found
}
async function mount() { root = document.createElement('div'); app = createApp(App); document.body.append(root); app.mount(root); await settle() }

beforeEach(() => {
  vi.useFakeTimers()
  mocks.useHost.mockReset(); mocks.invoke.mockReset(); mocks.save.mockReset(); mocks.toast.mockReset(); mocks.destroyToast.mockReset()
  records = [{ platform: 'bilibili', account_id: 'primary', label: '主账号', enabled: true, credential_state: 'valid', updated_at: '2026-09-12T00:00:00Z' }]
  total = 1
  const config = { enabled: true, subscriptions: [], resolver: { targets: [] }, account_check_interval_minutes: 360, account_browser_mode: 'auto', account_browser_remote_debugging_url: '' }
  const init = { config, page: { id: 'accounts', label: '账号管理' }, theme: { mode: 'light', tokens: {} } }
  mocks.useHost.mockReturnValue({ client: { invokeAction: mocks.invoke, saveSettings: mocks.save, request: vi.fn() }, ready: Promise.resolve(init), init: { value: init }, config: { value: config }, error: { value: null } })
  mocks.save.mockResolvedValue({ config })
  mocks.invoke.mockImplementation(async (action: string, payload: Record<string, unknown>) => {
    if (action === 'account.list') return { accounts: records.slice(Number(payload.offset), Number(payload.offset) + Number(payload.limit)), platforms: [{ id: 'bilibili', name: 'Bilibili' }], total }
    if (action === 'subscription.resolve_avatars') return { items: (payload.urls as string[]).map(source_url => ({ source_url, data_url: 'data:image/png;base64,Zml4dHVyZQ==' })) }
    if (action === 'account.upsert') { records = records.map(record => record.account_id === payload.account_id && record.platform === payload.platform ? { ...record, label: payload.label, enabled: payload.enabled } : record); return { saved: true } }
    if (action === 'account.delete') { records = []; total = 0; return { deleted: true } }
    if (action === 'account.qr_create') return { login_id: 'qr-fixture', qrcode_url: 'https://example.test/scan', state: 'pending_scan', expires_at: '2030-01-01T00:00:00Z' }
    if (action === 'account.qr_poll') return { state: 'pending_scan' }
    if (action === 'account.qr_cancel') return { cancelled: true }
    throw new Error(`unexpected action ${action}`)
  })
})
afterEach(() => { app?.unmount(); app = undefined; root?.remove(); vi.useRealTimers(); vi.restoreAllMocks() })

describe('account management through the parent host', () => {
  it('loads through one host connection and confirms deletion inside the page', async () => {
    const nativeConfirm = vi.spyOn(window, 'confirm')
    await mount()
    expect(mocks.useHost).toHaveBeenCalledTimes(1)
    expect(root.textContent).toContain('主账号')
    button('删除 主账号').click(); await settle()
    expect(root.querySelector('[role="dialog"]')).not.toBeNull()
    expect(mocks.invoke.mock.calls.some(([action]) => action === 'account.delete')).toBe(false)
    button('确认').click(); await settle()
    expect(nativeConfirm).not.toHaveBeenCalled()
    expect(mocks.invoke).toHaveBeenCalledWith('account.delete', { platform: 'bilibili', account_id: 'primary' })
    expect(root.querySelector('.account-card')).toBeNull()
  })

  it('filters the complete multi-page collection and keeps pagination local', async () => {
    const base = records[0]!
    records = Array.from({ length: 105 }, (_, i) => ({ ...base, account_id: `account-${i}`, label: `账号 ${i}`, avatar_url: i < 5 ? `https://i0.hdslb.com/bfs/face/${i}.jpg` : '', platform: i === 104 ? 'douyin' : 'bilibili', credential_state: i === 104 ? 'invalid' : 'valid' }))
    total = records.length
    await mount()
    expect(mocks.invoke).toHaveBeenCalledWith('account.list', { offset: 0, limit: 100 })
    expect(mocks.invoke).toHaveBeenCalledWith('account.list', { offset: 100, limit: 100 })
    expect(mocks.invoke.mock.calls.filter(([action]) => action === 'subscription.resolve_avatars').map(([, payload]) => payload.urls.length)).toEqual([4, 1])
    expect(root.querySelectorAll('.account-card')).toHaveLength(20)
    button('下一页').click(); await settle()
    expect(mocks.invoke.mock.calls.filter(([action]) => action === 'account.list')).toHaveLength(2)
    button('需处理').click(); await settle()
    expect(root.querySelectorAll('.account-card')).toHaveLength(1)
    expect(root.querySelector('.account-card')?.textContent).toContain('账号 104')
    button('全部状态').click(); await settle()
    const search = root.querySelector<HTMLInputElement>('[aria-label="搜索账号"]')!
    search.value = '账号 104'; search.dispatchEvent(new Event('input')); await settle()
    expect(root.querySelectorAll('.account-card')).toHaveLength(1)
    expect(root.querySelector('.account-card')?.textContent).toContain('账号 104')
  })

  it('saves account check settings including disabling automatic checks', async () => {
    await mount()
    button('账号设置').click(); await settle()
    const interval = root.querySelector<HTMLInputElement>('input[type="number"]')!
    interval.value = '0'; interval.dispatchEvent(new Event('input')); await nextTick()
    button('保存账号设置').click(); await settle()
    expect(mocks.save).toHaveBeenCalledWith({ account_check_interval_minutes: 0, account_browser_mode: 'auto', account_browser_remote_debugging_url: '' })
  })

  it('edits an account in a centered dialog and keeps stored credentials hidden', async () => {
    await mount()
    button('编辑 主账号').click(); await settle()
    const dialog = root.querySelector<HTMLElement>('[role="dialog"]')!
    expect(dialog).not.toBeNull()
    const inputs = dialog.querySelectorAll<HTMLInputElement>('input')
    const note = inputs[0]!
    const cookie = dialog.querySelector<HTMLInputElement>('input[type="password"]')!
    expect(cookie.value).toBe('')
    note.value = '新的备注'; note.dispatchEvent(new Event('input')); await settle()
    button('保存账号').click(); await settle()
    expect(mocks.invoke).toHaveBeenCalledWith('account.upsert', { platform: 'bilibili', account_id: 'primary', label: '新的备注', enabled: true, cookie: '' })
    expect(root.querySelector('[role="dialog"]')).toBeNull()
    expect(root.querySelector('.account-card')?.textContent).toContain('新的备注')
  })

  it('retains visible accounts and reports a refresh failure instead of replacing the collection', async () => {
    await mount()
    const original = mocks.invoke.getMockImplementation()!
    mocks.invoke.mockImplementation((action: string, payload: Record<string, unknown>) => action === 'account.list' ? Promise.reject(new Error('读取失败，请重试')) : original(action, payload))
    button('刷新账号').click(); await settle()
    expect(root.querySelectorAll('.account-card')).toHaveLength(1)
    expect(root.textContent).toContain('读取失败，请重试')
  })

  it('announces QR success with an expiring toast instead of a persistent page banner', async () => {
    await mount()
    const original = mocks.invoke.getMockImplementation()!
    mocks.invoke.mockImplementation((action: string, payload: Record<string, unknown>) => action === 'account.qr_poll' ? Promise.resolve({ state: 'succeeded' }) : original(action, payload))
    button('添加账号').click(); await settle()
    button('扫码获取 CK').click(); await settle()
    await vi.advanceTimersByTimeAsync(2000); await settle()
    expect(mocks.toast).toHaveBeenCalledWith({ key: 'account-feedback', content: '扫码登录成功，账号已保存', duration: 3 })
    expect(root.querySelector('.qr-panel')).toBeNull()
    expect(root.textContent).not.toContain('扫码登录成功，账号已保存')
    app!.unmount(); app = undefined
    expect(mocks.destroyToast).toHaveBeenCalled()
  })

  it('cancels a QR session that finishes creating after the page is gone', async () => {
    await mount()
    let finish!: (result: Record<string, unknown>) => void
    const original = mocks.invoke.getMockImplementation()!
    mocks.invoke.mockImplementation((action: string, payload: Record<string, unknown>) => action === 'account.qr_create' ? new Promise(resolve => { finish = resolve }) : original(action, payload))
    button('添加账号').click(); await settle()
    button('扫码获取 CK').click(); await settle()
    app!.unmount(); app = undefined
    finish({ login_id: 'late-session', qrcode_url: 'https://example.test/scan' }); await settle()
    expect(mocks.invoke).toHaveBeenCalledWith('account.qr_cancel', { platform: 'bilibili', login_id: 'late-session' })
    await vi.advanceTimersByTimeAsync(5000)
    expect(mocks.invoke.mock.calls.some(([action]) => action === 'account.qr_poll')).toBe(false)
  })

  it('waits for the current poll and ignores its response after cancellation', async () => {
    await mount()
    let finish!: (result: Record<string, unknown>) => void
    const original = mocks.invoke.getMockImplementation()!
    mocks.invoke.mockImplementation((action: string, payload: Record<string, unknown>) => action === 'account.qr_poll' ? new Promise(resolve => { finish = resolve }) : original(action, payload))
    button('添加账号').click(); await settle()
    button('扫码获取 CK').click(); await settle()
    await vi.advanceTimersByTimeAsync(8000); await settle()
    expect(mocks.invoke.mock.calls.filter(([action]) => action === 'account.qr_poll')).toHaveLength(1)
    button('取消扫码').click(); await settle()
    const reads = mocks.invoke.mock.calls.filter(([action]) => action === 'account.list').length
    finish({ state: 'succeeded' }); await settle()
    expect(root.querySelector('.qr-panel')).toBeNull()
    expect(mocks.invoke.mock.calls.filter(([action]) => action === 'account.list')).toHaveLength(reads)
  })
})

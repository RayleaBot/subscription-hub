import { createApp, nextTick, type App as VueApp } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ useHost: vi.fn(), invoke: vi.fn(), save: vi.fn() }))
vi.mock('@rayleabot/plugin-ui', () => ({ usePluginHost: mocks.useHost }))
vi.mock('ant-design-vue', () => ({
  Alert: { props: ['message'], template: '<div role="status">{{ message }}</div>' },
  Button: { props: ['disabled', 'loading'], template: '<button :disabled="disabled || loading"><slot /></button>' },
  Input: { props: ['value'], emits: ['update:value'], template: '<input :value="value" @input="$emit(\'update:value\', $event.target.value)" />' },
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

async function settle() { for (let i = 0; i < 20; i++) { await Promise.resolve(); await nextTick() } }
function button(label: string) {
  const found = [...root.querySelectorAll('button')].find(item => item.textContent?.trim() === label)
  if (!found) throw new Error(`button missing: ${label}`)
  return found
}
async function mount() { root = document.createElement('div'); app = createApp(App); app.mount(root); await settle() }

beforeEach(() => {
  vi.useFakeTimers()
  mocks.useHost.mockReset(); mocks.invoke.mockReset(); mocks.save.mockReset()
  records = [{ platform: 'bilibili', account_id: 'primary', label: '主账号', enabled: true, credential_state: 'valid', updated_at: '2026-09-12T00:00:00Z' }]
  total = 1
  const config = { enabled: true, subscriptions: [], resolver: { targets: [] }, account_check_interval_minutes: 360, account_browser_mode: 'auto', account_browser_remote_debugging_url: '' }
  const init = { config, page: { id: 'accounts', label: '账号管理' }, theme: { mode: 'light', tokens: {} } }
  mocks.useHost.mockReturnValue({ client: { invokeAction: mocks.invoke, saveSettings: mocks.save, request: vi.fn() }, ready: Promise.resolve(init), init: { value: init }, config: { value: config }, error: { value: null } })
  mocks.save.mockResolvedValue({ config })
  mocks.invoke.mockImplementation(async (action: string, payload: Record<string, unknown>) => {
    if (action === 'account.list') return { accounts: records, platforms: [{ id: 'bilibili', name: 'Bilibili' }], total }
    if (action === 'subscription.resolve_avatars') return { items: (payload.urls as string[]).map(source_url => ({ source_url, data_url: 'data:image/png;base64,Zml4dHVyZQ==' })) }
    if (action === 'account.delete') { records = []; total = 0; return { deleted: true } }
    if (action === 'account.qr_create') return { login_id: 'qr-fixture', qrcode_url: 'https://example.test/scan', state: 'pending_scan', expires_at: '2030-01-01T00:00:00Z' }
    if (action === 'account.qr_poll') return { state: 'pending_scan' }
    if (action === 'account.qr_cancel') return { cancelled: true }
    throw new Error(`unexpected action ${action}`)
  })
})
afterEach(() => { app?.unmount(); app = undefined; vi.useRealTimers(); vi.restoreAllMocks() })

describe('account management through the parent host', () => {
  it('loads through one host connection and confirms deletion inside the page', async () => {
    const nativeConfirm = vi.spyOn(window, 'confirm')
    await mount()
    expect(mocks.useHost).toHaveBeenCalledTimes(1)
    expect(root.textContent).toContain('主账号')
    button('删除').click(); await settle()
    expect(root.querySelector('[role="dialog"]')).not.toBeNull()
    expect(mocks.invoke.mock.calls.some(([action]) => action === 'account.delete')).toBe(false)
    button('确认').click(); await settle()
    expect(nativeConfirm).not.toHaveBeenCalled()
    expect(mocks.invoke).toHaveBeenCalledWith('account.delete', { platform: 'bilibili', account_id: 'primary' })
    expect(root.querySelector('.account-card')).toBeNull()
  })

  it('loads avatars in batches of four and supports account pagination and search', async () => {
    records = Array.from({ length: 5 }, (_, i) => ({ ...records[0], account_id: `account-${i}`, avatar_url: `https://i0.hdslb.com/bfs/face/${i}.jpg` }))
    total = 21
    await mount()
    expect(mocks.invoke.mock.calls.filter(([action]) => action === 'subscription.resolve_avatars').map(([, payload]) => payload.urls.length)).toEqual([4, 1])
    button('下一页').click(); await settle()
    expect(mocks.invoke).toHaveBeenCalledWith('account.list', { query: '', offset: 20, limit: 20 })
    const search = root.querySelector<HTMLInputElement>('[aria-label="搜索账号"]')!
    search.value = '主账号'; search.dispatchEvent(new Event('input'))
    await vi.advanceTimersByTimeAsync(250); await settle()
    expect(mocks.invoke).toHaveBeenCalledWith('account.list', { query: '主账号', offset: 0, limit: 20 })
  })

  it('saves account check settings including disabling automatic checks', async () => {
    await mount()
    const interval = root.querySelector<HTMLInputElement>('input[type="number"]')!
    interval.value = '0'; interval.dispatchEvent(new Event('input')); await nextTick()
    button('保存账号设置').click(); await settle()
    expect(mocks.save).toHaveBeenCalledWith({ account_check_interval_minutes: 0, account_browser_mode: 'auto', account_browser_remote_debugging_url: '' })
  })

  it('cancels a QR session that finishes creating after the page is gone', async () => {
    await mount()
    let finish!: (result: Record<string, unknown>) => void
    const original = mocks.invoke.getMockImplementation()!
    mocks.invoke.mockImplementation((action: string, payload: Record<string, unknown>) => action === 'account.qr_create' ? new Promise(resolve => { finish = resolve }) : original(action, payload))
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

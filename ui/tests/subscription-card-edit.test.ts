import { createApp, nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const host = vi.hoisted(() => {
  const init = {
    config: {
      enabled: true,
      subscriptions: [{ bot_id: '1001', source_protocol: 'onebot11' as const,
        id: 'bilibili-100-group-200',
        platform: 'bilibili',
        uid: '100',
        name: '测试 UP',
        target_type: 'group',
        target_id: '200',
        target_name: '测试群聊',
        services: ['video'],
        subscribers: [],
        enabled: true,
      }],
      resolver: { targets: [] },
    },
    page: { id: 'subscriptions', label: '订阅设置' },
    theme: { mode: 'light', tokens: {} },
  }
  const client = {
    apiRequest: vi.fn(async (method: string, path: string) => {
      if (method === 'GET' && path === '/api/adapters') {
        return { adapters: [{ id: 'onebot', protocol: 'onebot11', enabled: true, state: 'connected', identity: { id: '1001', name: '机器人' } }] }
      }
      if (method === 'GET' && path === '/api/adapters/onebot/onebot11/targets') {
        return {
          available: true,
          groups: [{ bot_id: '1001', source_protocol: 'onebot11' as const,  target_id: '200', target_name: '测试群聊' }],
          private_users: [],
          issues: [],
        }
      }
      throw new Error(`unexpected request: ${method} ${path}`)
    }),
    invokeAction: vi.fn(async (action: string, payload: Record<string, unknown>) => {
      if (action === 'subscription.settings' || action === 'subscription.save') return { settings: init.config }
      if (action !== 'subscription.resolve_avatars') throw new Error(`unexpected action: ${action}`)
      const urls = Array.isArray(payload.urls) ? payload.urls.map(String) : []
      return { items: urls.map((source_url) => ({ source_url, data_url: 'data:image/png;base64,dGVzdA==' })) }
    }),
  }
  return {
    bot: { value: { source_protocol: 'onebot11', bot_id: '1001', state: 'online', adapter_id: 'onebot' } },
    refreshBot: vi.fn(async () => ({ source_protocol: 'onebot11', bot_id: '1001', state: 'online', adapter_id: 'onebot' })),
    client,
    ready: Promise.resolve(init),
    init: { value: init },
    error: { value: null },
  }
})

vi.mock('@rayleabot/plugin-ui', () => ({
  usePluginHost: () => host,
}))
vi.mock('ant-design-vue', () => ({ Alert: { template: '<div />' }, Modal: { props: ['open'], template: '<section v-if="open"><slot /></section>' } }))

import App from '../src/App.vue'

describe('subscription card editing', () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn()
    host.client.apiRequest.mockClear()
    host.client.invokeAction.mockClear()
    host.bot.value.state = 'online'
    host.refreshBot.mockResolvedValue({ ...host.bot.value })
  })

  it('keeps the active card visible when its service stops matching the current filter', async () => {
    const root = document.createElement('div')
    const app = createApp(App)
    app.mount(root)
    await settle()

    const serviceFilter = root.querySelector<HTMLSelectElement>('#service-filter-input')
    if (!serviceFilter) throw new Error('service filter not found')
    serviceFilter.value = 'video'
    serviceFilter.dispatchEvent(new Event('change'))
    await nextTick()

    findButton(root, '编辑').click()
    await nextTick()

    const videoOption = [...root.querySelectorAll<HTMLLabelElement>('.inline-checks label')]
      .find((label) => label.textContent?.trim() === '视频')
    const videoCheckbox = videoOption?.querySelector<HTMLInputElement>('input')
    if (!videoCheckbox) throw new Error('video service checkbox not found')
    videoCheckbox.click()
    await nextTick()

    expect(root.querySelector('.sub-card--editing')).not.toBeNull()
    expect(root.textContent).toContain('完成')
    app.unmount()
  })

  it('saves existing data offline through the account action without submitting account or global fields', async () => {
    host.bot.value.state = 'offline'
    host.refreshBot.mockResolvedValue({ ...host.bot.value })
    const root = document.createElement('div')
    const app = createApp(App)
    app.mount(root)
    await settle()
    const enabled = root.querySelector<HTMLInputElement>('input[aria-label="启用订阅"]')!
    enabled.checked = false
    enabled.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    findButton(root, '保存设置').click()
    await settle()
    const save = host.client.invokeAction.mock.calls.find(([action]) => action === 'subscription.save')
    expect(save).toBeDefined()
    expect(Object.keys(save![1])).toEqual(['subscriptions'])
    const records = save![1].subscriptions as Record<string, unknown>[]
    expect(records).toHaveLength(1)
    expect(records[0]).toMatchObject({ id: 'bilibili-100-group-200', enabled: false })
    expect(records[0]).not.toHaveProperty('bot_id')
    expect(records[0]).not.toHaveProperty('source_protocol')
    expect(host.client.apiRequest).not.toHaveBeenCalled()
    app.unmount()
  })
})

async function settle() {
  for (let index = 0; index < 8; index += 1) {
    await Promise.resolve()
    await nextTick()
  }
}

function findButton(root: HTMLElement, text: string): HTMLButtonElement {
  const button = [...root.querySelectorAll('button')].find((item) => item.textContent?.trim() === text)
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

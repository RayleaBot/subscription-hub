import { createApp, nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const host = vi.hoisted(() => {
  const init = {
    config: {
      enabled: true,
      subscriptions: [{
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
    request: vi.fn(async (type: string) => {
      if (type === 'protocol.targets.reload') {
        return {
          available: true,
          groups: [{ target_id: '200', target_name: '测试群聊' }],
          private_users: [],
        }
      }
      throw new Error(`unexpected request: ${type}`)
    }),
    invokeAction: vi.fn(async (action: string, payload: Record<string, unknown>) => {
      if (action !== 'subscription.resolve_avatars') throw new Error(`unexpected action: ${action}`)
      const urls = Array.isArray(payload.urls) ? payload.urls.map(String) : []
      return { items: urls.map((source_url) => ({ source_url, data_url: 'data:image/png;base64,dGVzdA==' })) }
    }),
    send: vi.fn(),
  }
  return {
    client,
    ready: Promise.resolve(init),
    init: { value: init },
    error: { value: null },
  }
})

vi.mock('@rayleabot/plugin-ui', () => ({
  usePluginHost: () => host,
}))
vi.mock('ant-design-vue', () => ({ Alert: { template: '<div />' } }))

import App from '../src/App.vue'

describe('subscription card editing', () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn()
    host.client.request.mockClear()
    host.client.invokeAction.mockClear()
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

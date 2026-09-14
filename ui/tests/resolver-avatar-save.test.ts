import { createApp, nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const host = vi.hoisted(() => {
  const avatarDataURL = 'data:image/png;base64,dGVzdA=='
  const initialConfig = {
    enabled: true,
    subscriptions: [],
    resolver: {
      targets: [],
    },
  }
  const init = {
    config: initialConfig,
    page: { id: 'resolver-groups', label: '群聊解析' },
    theme: { mode: 'light', tokens: {} },
  }
  const client = {
    apiRequest: vi.fn(async (method: string, path: string) => {
      if (method === 'GET' && path === '/api/adapters') {
        return { adapters: [{ id: 'onebot', protocol: 'onebot11', enabled: true }] }
      }
      if (method === 'GET' && path === '/api/adapters/onebot/onebot11/targets') {
        return {
          available: true,
          groups: [{ target_id: '200', target_name: '测试群聊' }],
          private_users: [],
          issues: [],
        }
      }
      throw new Error(`unexpected request: ${method} ${path}`)
    }),
    invokeAction: vi.fn(async (action: string, payload: Record<string, unknown>) => {
      if (action !== 'subscription.resolve_avatars') throw new Error(`unexpected action: ${action}`)
      const urls = Array.isArray(payload.urls) ? payload.urls.map(String) : []
      return { items: urls.map((source_url) => ({ source_url, data_url: avatarDataURL })) }
    }),
    saveSettings: vi.fn(async (config: Record<string, unknown>) => ({ config })),
  }
  return {
    avatarDataURL,
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

describe('resolver avatar lifecycle', () => {
  beforeEach(() => {
    host.client.apiRequest.mockClear()
    host.client.invokeAction.mockClear()
    host.client.saveSettings.mockClear()
  })

  it('keeps hydrated candidate avatars after saving unchanged settings', async () => {
    const root = document.createElement('div')
    const app = createApp(App)
    app.mount(root)
    await settle()

    findButton(root, '添加群聊').click()
    await settle()

    const avatarBeforeSave = root.querySelector<HTMLImageElement>('.target-choice-card img')
    expect(avatarBeforeSave?.src).toBe(host.avatarDataURL)

    findButton(root, '保存设置').click()
    await settle()

    const avatarAfterSave = root.querySelector<HTMLImageElement>('.target-choice-card img')
    expect(avatarAfterSave?.src).toBe(host.avatarDataURL)
    expect(host.client.saveSettings).toHaveBeenCalledOnce()
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

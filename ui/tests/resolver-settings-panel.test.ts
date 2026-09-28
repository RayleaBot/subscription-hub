import { createApp, h, nextTick, reactive } from 'vue'
import { describe, expect, it, vi } from 'vitest'

vi.mock('ant-design-vue', () => ({ Modal: { props: ['open'], template: '<section v-if="open" role="dialog"><slot /></section>' } }))

import ResolverSettingsPanel from '../src/components/ResolverSettingsPanel.vue'
import { deriveTargetAvatarURL, emptyTargets, normalizeResolverSettings, normalizeTargets, type ResolverSettings } from '../src/model'

const avatarDataURL = 'data:image/png;base64,dGVzdA=='

describe('resolver settings panel', () => {
  it('mounts each resolver page with reactive settings', () => {
    const settings = reactive(normalizeResolverSettings({}))

    for (const [view, title] of [
      ['targets', '链接解析'],
      ['strategy', '防抖与媒体策略'],
    ] as const) {
      const root = document.createElement('div')
      const app = createApp(ResolverSettingsPanel, {
        modelValue: settings,
        avatarDataUrls: new Map(),
        targets: emptyTargets(),
        view,
      })

      expect(() => app.mount(root)).not.toThrow()
      expect(root.textContent).toContain(title)
      app.unmount()
    }
  })

  it('hides the empty state until the host settings have loaded', async () => {
    const root = document.createElement('div')
    const props = reactive({
      modelValue: normalizeResolverSettings({}),
      avatarDataUrls: new Map<string, string>(),
      targets: emptyTargets(),
      view: 'targets' as const,
      loaded: false,
    })
    const app = createApp({ render: () => h(ResolverSettingsPanel, props) })
    app.mount(root)

    expect(root.querySelector('.resolver-empty')).toBeNull()

    props.modelValue = normalizeResolverSettings({ targets: [{ target_type: 'group', target_id: '200', target_name: '测试群聊', bilibili: true }] })
    props.loaded = true
    await settle()
    expect(root.querySelectorAll('.resolver-target-card')).toHaveLength(1)
    expect(root.querySelector('.resolver-empty')).toBeNull()

    props.modelValue = normalizeResolverSettings({})
    await settle()
    expect(root.querySelectorAll('.resolver-target-card:not(.cards-leave-active)')).toHaveLength(0)
    expect(root.querySelector('.resolver-empty')?.textContent).toContain('尚未添加解析对象')
    app.unmount()
  })

  it('adds a protocol object by clicking its avatar card and keeps unselected objects out of management', async () => {
    const root = document.createElement('div')
    let updated: ResolverSettings | undefined
    const avatarSource = deriveTargetAvatarURL('group', '200')
    const app = createApp(ResolverSettingsPanel, {
      modelValue: reactive(normalizeResolverSettings({})),
      avatarDataUrls: new Map([[avatarSource, avatarDataURL]]),
      targets: normalizeTargets({
        available: true,
        groups: [{ target_id: '200', target_name: '测试群聊' }],
      }),
      view: 'targets',
      'onUpdate:modelValue': (value: ResolverSettings) => { updated = value },
    })
    app.mount(root)

    expect(root.querySelectorAll('.resolver-target-card')).toHaveLength(0)
    expect(root.textContent).toContain('尚未添加解析对象')
    expect(root.textContent).not.toContain('测试群聊')

    findButton(root, '添加对象').click()
    await nextTick()
    expect(root.textContent).toContain('测试群聊')
    expect(root.textContent).toContain('群号 200')
    expect(root.querySelector('.target-choice-card img')?.getAttribute('src')).toBe(avatarDataURL)
    expect(root.querySelectorAll('.resolver-target-card')).toHaveLength(0)

    const targetCard = root.querySelector<HTMLButtonElement>('.target-choice-card')
    if (!targetCard) throw new Error('target choice card not found')
    targetCard.click()
    await nextTick()
    expect(root.querySelectorAll('.resolver-target-card')).toHaveLength(1)
    expect(root.querySelector('.resolver-target-card img')?.getAttribute('src')).toBe(avatarDataURL)
    expect(root.querySelectorAll('.resolver-target-card input[type="checkbox"]')).toHaveLength(3)
    expect(updated?.targets).toEqual([expect.objectContaining({ target_type: 'group', target_id: '200', bilibili: false })])
    app.unmount()
  })

  it('shows targets enabled from chat without exposing unrelated protocol objects', () => {
    const root = document.createElement('div')
    const app = createApp(ResolverSettingsPanel, {
      modelValue: reactive(normalizeResolverSettings({
        targets: [{ target_type: 'private', target_id: '300', target_name: '已开启用户', weibo: true }],
      })),
      avatarDataUrls: new Map(),
      targets: normalizeTargets({
        available: true,
        private_users: [
          { target_id: '300', nickname: '已开启用户' },
          { target_id: '301', nickname: '未添加用户' },
        ],
      }),
      view: 'targets',
    })
    app.mount(root)

    expect(root.querySelectorAll('.resolver-target-card')).toHaveLength(1)
    expect(root.textContent).toContain('已开启用户')
    expect(root.textContent).toContain('QQ 300')
    expect(root.textContent).not.toContain('未添加用户')
    app.unmount()
  })

  it('lists groups and private chats together and filters them by scope', async () => {
    const root = document.createElement('div')
    const app = createApp(ResolverSettingsPanel, {
      modelValue: reactive(normalizeResolverSettings({
        targets: [
          { target_type: 'group', target_id: '200', target_name: '测试群聊', bilibili: true },
          { target_type: 'private', target_id: '300', target_name: '测试用户', weibo: true },
        ],
      })),
      avatarDataUrls: new Map(),
      targets: emptyTargets(),
      view: 'targets',
    })
    app.mount(root)

    expect(root.querySelectorAll('.resolver-target-card')).toHaveLength(2)
    expect(root.textContent).toContain('群号 200')
    expect(root.textContent).toContain('QQ 300')

    findScopeTab(root, '私聊').click()
    await settle()
    expect(stayingCards(root)).toHaveLength(1)
    expect(stayingCards(root)[0]?.textContent).toContain('QQ 300')

    findScopeTab(root, '群聊').click()
    await settle()
    expect(stayingCards(root)).toHaveLength(1)
    expect(stayingCards(root)[0]?.textContent).toContain('群号 200')
    app.unmount()
  })

  it('toggles the super admin whitelist as part of the resolver settings', async () => {
    const root = document.createElement('div')
    let updated: ResolverSettings | undefined
    const app = createApp(ResolverSettingsPanel, {
      modelValue: reactive(normalizeResolverSettings({})),
      avatarDataUrls: new Map(),
      targets: emptyTargets(),
      view: 'targets',
      'onUpdate:modelValue': (value: ResolverSettings) => { updated = value },
    })
    app.mount(root)

    const toggle = root.querySelector<HTMLInputElement>('input[aria-label="超级管理员白名单"]')
    if (!toggle) throw new Error('whitelist toggle not found')
    expect(toggle.checked).toBe(false)
    expect(root.querySelector('.admin-rule')?.textContent).toContain('已关闭')

    toggle.checked = true
    toggle.dispatchEvent(new Event('change'))
    await nextTick()
    expect(updated?.super_admin_whitelist).toBe(true)
    expect(root.querySelector('.admin-rule')?.textContent).toContain('已开启')
    app.unmount()
  })
})

function findScopeTab(root: HTMLElement, text: string): HTMLButtonElement {
  const button = [...root.querySelectorAll<HTMLButtonElement>('.scope-tab')].find((item) => item.textContent?.trim().startsWith(text))
  if (!button) throw new Error(`scope tab not found: ${text}`)
  return button
}

function findButton(root: HTMLElement, text: string): HTMLButtonElement {
  const button = [...root.querySelectorAll('button')].find((item) => item.textContent?.trim() === text)
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

function stayingCards(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>('.resolver-target-card:not(.cards-leave-active)')]
}

async function settle() {
  for (let index = 0; index < 4; index += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
  }
}

import { createApp, nextTick, reactive } from 'vue'
import { describe, expect, it, vi } from 'vitest'

vi.mock('ant-design-vue', () => ({ Modal: { props: ['open'], template: '<section v-if="open" role="dialog"><slot /></section>' } }))

import ResolverSettingsPanel from '../src/components/ResolverSettingsPanel.vue'
import { deriveTargetAvatarURL, emptyTargets, normalizeResolverSettings, normalizeTargets, type ResolverSettings } from '../src/model'

const avatarDataURL = 'data:image/png;base64,dGVzdA=='

describe('resolver settings panel', () => {
  it('mounts each resolver page with reactive settings', () => {
    const settings = reactive(normalizeResolverSettings({}))

    for (const [view, title] of [
      ['group', '群聊解析'],
      ['private', '用户解析'],
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
      view: 'group',
      'onUpdate:modelValue': (value: ResolverSettings) => { updated = value },
    })
    app.mount(root)

    expect(root.querySelectorAll('.resolver-target-card')).toHaveLength(0)
    expect(root.textContent).toContain('尚未添加群聊')
    expect(root.textContent).not.toContain('测试群聊')

    findButton(root, '添加群聊').click()
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
      view: 'private',
    })
    app.mount(root)

    expect(root.querySelectorAll('.resolver-target-card')).toHaveLength(1)
    expect(root.textContent).toContain('已开启用户')
    expect(root.textContent).toContain('QQ 300')
    expect(root.textContent).not.toContain('未添加用户')
    app.unmount()
  })
})

function findButton(root: HTMLElement, text: string): HTMLButtonElement {
  const button = [...root.querySelectorAll('button')].find((item) => item.textContent?.trim() === text)
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

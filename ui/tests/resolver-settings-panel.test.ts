import { createApp, nextTick, reactive } from 'vue'
import { describe, expect, it } from 'vitest'

import ResolverSettingsPanel from '../src/components/ResolverSettingsPanel.vue'
import { emptyTargets, normalizeResolverSettings, normalizeTargets, type ResolverSettings } from '../src/model'

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
        targets: emptyTargets(),
        view,
      })

      expect(() => app.mount(root)).not.toThrow()
      expect(root.textContent).toContain(title)
      app.unmount()
    }
  })

  it('keeps protocol objects out of the management table until they are added', async () => {
    const root = document.createElement('div')
    let updated: ResolverSettings | undefined
    const app = createApp(ResolverSettingsPanel, {
      modelValue: reactive(normalizeResolverSettings({})),
      targets: normalizeTargets({
        available: true,
        groups: [{ target_id: '200', target_name: '测试群聊' }],
      }),
      view: 'group',
      'onUpdate:modelValue': (value: ResolverSettings) => { updated = value },
    })
    app.mount(root)

    expect(root.querySelectorAll('.target-row')).toHaveLength(0)
    expect(root.textContent).toContain('尚未添加群聊')
    expect(root.textContent).not.toContain('测试群聊')

    findButton(root, '添加群聊').click()
    await nextTick()
    expect(root.textContent).toContain('测试群聊')
    expect(root.querySelectorAll('.target-row')).toHaveLength(0)

    findButton(root, '添加').click()
    await nextTick()
    expect(root.querySelectorAll('.target-row')).toHaveLength(1)
    expect(root.querySelectorAll('.target-row input[type="checkbox"]')).toHaveLength(3)
    expect(updated?.targets).toEqual([expect.objectContaining({ target_type: 'group', target_id: '200', bilibili: false })])
    app.unmount()
  })

  it('shows targets enabled from chat without exposing unrelated protocol objects', () => {
    const root = document.createElement('div')
    const app = createApp(ResolverSettingsPanel, {
      modelValue: reactive(normalizeResolverSettings({
        targets: [{ target_type: 'private', target_id: '300', target_name: '已开启用户', weibo: true }],
      })),
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

    expect(root.querySelectorAll('.target-row')).toHaveLength(1)
    expect(root.textContent).toContain('已开启用户')
    expect(root.textContent).not.toContain('未添加用户')
    app.unmount()
  })
})

function findButton(root: HTMLElement, text: string): HTMLButtonElement {
  const button = [...root.querySelectorAll('button')].find((item) => item.textContent?.trim() === text)
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

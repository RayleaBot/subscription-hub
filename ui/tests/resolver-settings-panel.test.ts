import { createApp, reactive } from 'vue'
import { describe, expect, it } from 'vitest'

import ResolverSettingsPanel from '../src/components/ResolverSettingsPanel.vue'
import { emptyTargets, normalizeResolverSettings } from '../src/model'

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
})

import { createApp, defineComponent, h, nextTick, onBeforeUnmount } from 'vue'
import { expect, it, vi } from 'vitest'
import { usePluginHost } from '@rayleabot/plugin-ui'
import type { PluginUIBridgeClient } from '@rayleabot/plugin-ui'

it('keeps the channel open for child resource cancellation during unmount', async () => {
  const operations: string[] = []
  const client = {
    on: () => () => undefined,
    connect: async () => ({ config: {}, theme: { mode: 'light', tokens: {} } }),
    observeDocumentHeight: vi.fn(),
    close: () => { operations.push('closed') },
  } as unknown as PluginUIBridgeClient
  const Child = defineComponent({ setup() { onBeforeUnmount(() => { operations.push('cancelled') }); return () => h('div') } })
  const Parent = defineComponent({ setup() { usePluginHost(client); return () => h(Child) } })
  const app = createApp(Parent)
  app.mount(document.createElement('div'))
  await nextTick()
  app.unmount()
  expect(operations).toEqual(['cancelled', 'closed'])
})

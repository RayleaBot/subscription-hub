<script setup lang="ts">
import { ref } from 'vue'
import { Alert as AAlert, Modal as AModal } from 'ant-design-vue'
import type { BotAccount, usePluginHost } from '@rayleabot/plugin-ui'
import { accountPath, canAssignHistory, loadAssignmentAccounts, loadHistory, type HistoryEntry } from '../history'
import { platformLabel, servicesText, type Subscription, type ResolverTargetSettings } from '../model'

const props = defineProps<{ host: ReturnType<typeof usePluginHost> }>()
const open = ref(false)
const busy = ref(false)
const error = ref('')
const loaded = ref(false)
const items = ref<HistoryEntry[]>([])
const total = ref(0)
const accounts = ref<BotAccount[]>([])
const selected = ref<Record<string, string>>({})
const deleting = ref<HistoryEntry | null>(null)
const key = (item: HistoryEntry) => `${item.kind}:${item.id}`

async function refresh() {
  busy.value = true
  error.value = ''
  try {
    await props.host.ready
    const [history, directory] = await Promise.all([loadHistory(props.host.client), loadAssignmentAccounts(props.host.client)])
    items.value = history.items
    total.value = history.total
    accounts.value = directory
    loaded.value = true
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '历史数据载入失败'
  } finally { busy.value = false }
}

async function show() {
  open.value = true
  await refresh()
}

async function mutate(item: HistoryEntry, assign: boolean) {
  busy.value = true
  error.value = ''
  try {
    const payload: Record<string, unknown> = { kind: item.kind, id: item.id }
    if (assign) {
      const bot = await props.host.client.apiRequest<BotAccount>('GET', accountPath('onebot11', selected.value[key(item)]!))
      payload.bot = { source_protocol: bot.source_protocol, bot_id: bot.bot_id }
    }
    await props.host.client.invokeAction(assign ? 'history.assign' : 'history.delete', payload)
    deleting.value = null
    await refresh()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '历史数据操作失败'
  } finally { busy.value = false }
}

function summary(item: HistoryEntry) {
  if (item.kind === 'subscription') {
    const record = item.record as Subscription
    return `${platformLabel(record.platform)} · ${record.uid} · ${servicesText(record.services, record.platform)} · ${record.enabled ? '已启用' : '已停用'}`
  }
  const record = item.record as ResolverTargetSettings
  return (['bilibili', 'weibo', 'douyin'] as const).map(platform => `${platformLabel(platform)}：${record[platform] ? '开' : '关'}`).join(' · ')
}

function subscriberText(item: HistoryEntry) {
  const record = item.record as Subscription
  return record.subscribers?.map(person => person.nickname ? `${person.nickname}（${person.id}）` : person.id).join('、') || '无'
}
</script>

<template>
  <button type="button" class="button" @click="show">历史数据与归属</button>
  <AModal centered :open="open" :footer="null" title="历史数据与归属" :width="800" class="hub-dialog" @cancel="open = false">
    <div class="history-workspace">
      <p class="panel-description">缺少账号的 OneBot11 记录可选择归属。QQ 官方记录及完整账号记录不可改挂。</p>
      <div class="collection-caption"><span>{{ loaded ? `待处理 ${items.length} 条 / 插件共 ${total} 条` : '正在载入…' }}</span><button type="button" class="button button--small" :disabled="busy" @click="refresh">刷新</button></div>
      <AAlert v-if="error" type="error" :message="error" show-icon />
      <div v-if="loaded && !items.length && !error" class="empty-state">没有待处理的历史数据</div>
      <article v-for="item in items" :key="key(item)" class="history-record">
        <div class="history-record__heading"><strong>{{ item.kind === 'subscription' ? '订阅' : '解析' }} · {{ item.label || item.target_id }}</strong><span class="badge badge--warning">{{ item.state === 'missing' ? '待选择机器人' : item.state === 'removed' ? '账号不在目录' : '协议暂不支持' }}</span></div>
        <p>{{ item.target_type === 'group' ? '群聊' : '私聊' }} {{ item.target_id }} · {{ item.source_protocol || '协议未知' }}<template v-if="item.bot_id"> · {{ item.bot_id }}</template></p>
        <p class="panel-description">{{ summary(item) }}</p>
        <details><summary>查看记录</summary><p v-if="item.source_adapter">原连接：{{ item.source_adapter }}</p><p v-if="item.kind === 'subscription'">订阅人：{{ subscriberText(item) }}</p><p v-if="item.state === 'removed'">同一账号重新登录后可恢复访问。</p></details>
        <div class="history-record__actions">
          <template v-if="canAssignHistory(item)">
            <select v-model="selected[key(item)]" aria-label="归属机器人" :disabled="busy"><option :value="undefined" disabled>选择 OneBot11 账号</option><option v-for="bot in accounts" :key="bot.bot_id" :value="bot.bot_id">{{ bot.nickname || bot.bot_id }} · {{ bot.bot_id }}{{ bot.state === 'online' ? '' : bot.state === 'offline' ? ' · 离线' : ' · 无连接' }}</option></select>
            <button type="button" class="button" :disabled="busy || !selected[key(item)]" @click="mutate(item, true)">确认归属</button>
          </template>
          <button type="button" class="button danger-action" :disabled="busy" @click="deleting = item">删除</button>
        </div>
      </article>
    </div>
  </AModal>
  <AModal centered :open="!!deleting" title="删除历史记录" ok-text="删除" cancel-text="取消" :confirm-loading="busy" @cancel="deleting = null" @ok="deleting && mutate(deleting, false)">
    <p>删除这条{{ deleting?.kind === 'subscription' ? '订阅' : '解析' }}记录？</p>
    <AAlert v-if="error" type="error" :message="error" show-icon />
  </AModal>
</template>

<style scoped>
.history-workspace { display: grid; gap: 14px; }
.history-record { padding: 16px; border: 1px solid var(--border); border-radius: 12px; background: var(--surface); }
.history-record__heading, .history-record__actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.history-record__heading { justify-content: space-between; }
.history-record p { margin: 10px 0; overflow-wrap: anywhere; }
.history-record details { margin-bottom: 12px; }
.history-record select { min-width: 0; max-width: 100%; padding: 8px; color: var(--text); background: var(--surface); border: 1px solid var(--border); border-radius: 8px; }
@media (max-width: 600px) { .history-record select { min-height: 44px; } }
</style>

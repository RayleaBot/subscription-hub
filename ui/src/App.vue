<script setup lang="ts">
import { computed, ref } from 'vue'
import { Alert as AAlert, Button as AButton, Input as AInput, Select as ASelect, SelectOption as ASelectOption, Switch as ASwitch } from 'ant-design-vue'
import { usePluginHost } from '@rayleabot/plugin-ui'

import { createSubscription, normalizeSettings, type SubscriptionSettings, validateSubscription } from './model'

const host = usePluginHost()
const draft = ref<SubscriptionSettings>(normalizeSettings({}))
const status = ref('正在连接宿主…')
const busy = ref(false)
const errors = computed(() => draft.value.subscriptions.flatMap((item, index) => validateSubscription(item).map((error) => `第 ${index + 1} 项：${error}`)))

void host.ready.then((init) => {
  draft.value = normalizeSettings(init.config)
  status.value = '订阅配置已加载'
})

function addSubscription() {
  draft.value.subscriptions.unshift(createSubscription(draft.value.subscriptions.length))
}

async function reload() {
  const response = await host.client.reloadSettings()
  draft.value = normalizeSettings(response.config)
  status.value = '已重新读取配置'
}

async function save() {
  if (errors.value.length > 0) return
  busy.value = true
  try {
    const response = await host.client.saveSettings(structuredClone(draft.value) as unknown as Record<string, unknown>)
    draft.value = normalizeSettings(response.config)
    status.value = `已保存 ${draft.value.subscriptions.length} 项订阅`
  } finally {
    busy.value = false
  }
}

async function checkNow() {
  busy.value = true
  try {
    const result = await host.client.invokeAction('subscription.check_now')
    status.value = `检查完成：${result.checked ?? 0} 项，推送 ${result.pushed ?? 0} 项，失败 ${result.failed ?? 0} 项`
  } finally {
    busy.value = false
  }
}

async function resolveUser(index: number) {
  const item = draft.value.subscriptions[index]
  if (!item?.uid.trim()) return
  busy.value = true
  try {
    const result = await host.client.invokeAction('subscription.resolve_user', { platform: item.platform, query: item.uid })
    const user = result.user as { uid?: string; name?: string; avatar_url?: string } | undefined
    if (user) {
      item.uid = String(user.uid || item.uid)
      item.name = String(user.name || item.name || item.uid)
      item.avatar_url = String(user.avatar_url || '') || undefined
      status.value = `已解析账号：${item.name}`
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="page-shell">
    <header class="page-header">
      <div>
        <p class="eyebrow">RAYLEABOT · SUBSCRIPTION HUB</p>
        <h1>订阅中心</h1>
        <p>统一管理内容源、推送目标和启用状态。账号凭据仍由宿主管理，不会进入插件页面。</p>
      </div>
      <span class="status">{{ status }}</span>
    </header>

    <AAlert v-if="errors.length" type="warning" :message="errors[0]" :description="errors.slice(1).join('；')" show-icon />

    <section class="overview">
      <article><span>总订阅</span><strong>{{ draft.subscriptions.length }}</strong></article>
      <article><span>已启用</span><strong>{{ draft.subscriptions.filter((item) => item.enabled).length }}</strong></article>
      <article><span>全局状态</span><strong>{{ draft.enabled ? '运行中' : '已暂停' }}</strong></article>
      <label class="global-switch"><span>启用订阅检查</span><ASwitch v-model:checked="draft.enabled" /></label>
    </section>

    <section class="panel">
      <div class="section-heading">
        <div><h2>推送规则</h2><p>插件仅通过宿主 bridge 保存配置，不直接访问管理 API。</p></div>
        <AButton type="primary" ghost @click="addSubscription">新增订阅</AButton>
      </div>

      <div v-if="draft.subscriptions.length === 0" class="empty-state">尚未配置订阅。点击“新增订阅”开始。</div>
      <div class="subscription-list">
        <article v-for="(item, index) in draft.subscriptions" :key="item.id" class="subscription-card">
          <div class="card-head">
            <div class="identity">
              <span class="platform-mark">{{ item.platform.slice(0, 1).toUpperCase() }}</span>
              <div><strong>{{ item.name || '待填写账号' }}</strong><small>{{ item.uid || '无账号 ID' }}</small></div>
            </div>
            <div class="card-actions"><ASwitch v-model:checked="item.enabled" /><AButton danger type="text" @click="draft.subscriptions.splice(index, 1)">删除</AButton></div>
          </div>
          <div class="fields">
            <label><span>平台</span><ASelect v-model:value="item.platform"><ASelectOption value="bilibili">Bilibili</ASelectOption><ASelectOption value="weibo">微博</ASelectOption><ASelectOption value="douyin">抖音</ASelectOption><ASelectOption value="netease_music">网易云音乐</ASelectOption></ASelect></label>
            <label><span>账号 ID / 主页标识</span><AInput v-model:value="item.uid" /></label>
            <label><span>显示名称</span><AInput v-model:value="item.name" /></label>
            <label><span>目标类型</span><ASelect v-model:value="item.target_type"><ASelectOption value="group">群聊</ASelectOption><ASelectOption value="private">私聊</ASelectOption></ASelect></label>
            <label><span>推送目标 ID</span><AInput v-model:value="item.target_id" /></label>
          </div>
          <AButton size="small" :disabled="busy || !item.uid" @click="resolveUser(index)">解析账号</AButton>
        </article>
      </div>
    </section>

    <footer class="action-bar">
      <AButton @click="reload">重新读取</AButton>
      <AButton :loading="busy" @click="checkNow">立即检查</AButton>
      <AButton type="primary" :loading="busy" :disabled="errors.length > 0" @click="save">保存配置</AButton>
    </footer>
  </main>
</template>

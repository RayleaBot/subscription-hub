<script setup lang="ts">
import { computed, ref } from 'vue'

import AvatarBadge from './AvatarBadge.vue'
import {
  currentTargetsForMode,
  inputPlaceholder,
  isNumericID,
  PLATFORM_OPTIONS,
  platformLabel,
  serviceCheckboxValues,
  serviceLabel,
  serviceOrder,
  servicesText,
  subjectLabel,
  subscriberAvatarURL,
  targetAvatar,
  targetDisplay,
  validateRow,
  type LiveTarget,
  type Platform,
  type ResolveCandidate,
  type RowContext,
  type SubscriptionRow,
  type TargetType,
} from '../model'

const props = defineProps<{
  row: SubscriptionRow
  context: RowContext
  resolving: boolean
}>()

const emit = defineEmits<{
  edit: []
  cancel: []
  finish: []
  duplicate: []
  remove: []
  resolve: []
  chooseCandidate: [candidate: ResolveCandidate]
  targetMode: [mode: TargetType]
  toggleTarget: [target: LiveTarget]
  removeTarget: [key: string]
  toggleService: [targetKey: string, service: string, checked: boolean]
  addSubscriber: [id: string]
  invalidSubscriber: []
  removeSubscriber: [id: string]
  platformChange: [platform: Platform]
  queryChange: []
  queryCompositionStart: []
}>()

const subscriberInput = ref('')
const title = computed(() => props.row.name || props.row.uid || `未设置${platformLabel(props.row.platform)}对象`)
const subtitle = computed(() => props.row.uid
  ? `${platformLabel(props.row.platform)} · ${subjectLabel(props.row.platform)} ${props.row.uid}`
  : inputPlaceholder(props.row.platform))
const errors = computed(() => validateRow(props.row, props.context))
const availableTargets = computed(() => currentTargetsForMode(props.context.targets, props.row.target_mode))
const visibleTargets = computed(() => props.row.targets.slice(0, 5))
const visibleSubscribers = computed(() => props.row.subscriber_ids.slice(0, 5))

function serviceChecked(targetKey: string, service: string): boolean {
  const values = targetKey === 'common'
    ? props.row.services
    : props.row.targets.find((target) => target.key === targetKey)?.services ?? []
  return serviceCheckboxValues(values, props.row.platform).has(service)
}

function onQueryInput(event: Event) {
  const input = event.target as HTMLInputElement
  props.row.query = input.value
  props.row.resolved = false
  props.row.resolve_state = 'idle'
  props.row.resolve_message = ''
  props.row.candidates = []
  if (!(event as InputEvent).isComposing) emit('queryChange')
}

function addSubscriber() {
  const id = subscriberInput.value.trim()
  if (!isNumericID(id)) {
    emit('invalidSubscriber')
    return
  }
  emit('addSubscriber', id)
  subscriberInput.value = ''
}
</script>

<template>
  <article class="sub-card" :data-row-id="row.row_id" :class="{ 'sub-card--editing': row.edit_mode, 'sub-card--disabled': !row.enabled }">
    <div class="sub-card__head">
      <AvatarBadge :url="row.avatar_url" :label="title" size="up" />
      <div class="sub-card__meta">
        <strong>{{ title }}</strong>
        <small>{{ subtitle }}</small>
      </div>
      <div class="sub-card__status">
        <span v-if="row.resolved" class="badge">已校验</span>
        <span v-else-if="row.edit_mode" class="badge">{{ resolving ? '校验中' : '待校验' }}</span>
        <span class="badge" :class="errors.length ? 'badge--danger' : 'badge--success'">{{ errors.length ? '需处理' : '可保存' }}</span>
        <label v-if="!row.edit_mode" class="switch-row" title="启用">
          <input v-model="row.enabled" type="checkbox" aria-label="启用订阅" />
        </label>
      </div>
    </div>

    <div v-if="row.edit_mode" class="sub-card__body">
      <section class="sub-card__section">
        <div class="sub-card__section-title">订阅对象</div>
        <div class="up-input-line">
          <select
            class="platform-select"
            :value="row.platform"
            autocomplete="off"
            aria-label="平台"
            @change="emit('platformChange', ($event.target as HTMLSelectElement).value as Platform)"
          >
            <option v-for="option in PLATFORM_OPTIONS" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
          <input
            class="up-query-input"
            :value="row.query"
            type="text"
            autocomplete="off"
            :placeholder="inputPlaceholder(row.platform)"
            @input="onQueryInput"
            @compositionstart="emit('queryCompositionStart')"
            @compositionend="onQueryInput"
          />
          <button type="button" class="button button--small" :disabled="resolving" @click="emit('resolve')">校验</button>
        </div>
        <div v-if="row.resolve_message" class="row-note">{{ row.resolve_message }}</div>
        <div v-if="row.candidates.length" class="candidate-list">
          <button
            v-for="candidate in row.candidates"
            :key="`${candidate.uid}-${candidate.name}`"
            type="button"
            class="button candidate-button"
            @click="emit('chooseCandidate', candidate)"
          >
            <AvatarBadge :url="candidate.avatar_url" :label="candidate.name" size="candidate" />
            <span>{{ candidate.name }} · {{ subjectLabel(row.platform) }} {{ candidate.uid }}</span>
          </button>
        </div>

        <div v-if="row.service_mode === 'mixed'" class="target-service-editor">
          <span class="badge badge--warning">目标配置不同</span>
          <div v-for="target in row.targets" :key="target.key" class="target-service-line">
            <span class="row-note">{{ targetDisplay(target, context.targetMap) }}</span>
            <div class="inline-checks">
              <label v-for="service in serviceOrder(row.platform)" :key="service">
                <input
                  type="checkbox"
                  :checked="serviceChecked(target.key, service)"
                  @change="emit('toggleService', target.key, service, ($event.target as HTMLInputElement).checked)"
                />
                {{ serviceLabel(service, row.platform) }}
              </label>
            </div>
          </div>
        </div>
        <div v-else class="inline-checks" aria-label="推送类型">
          <label v-for="service in serviceOrder(row.platform)" :key="service">
            <input
              type="checkbox"
              :checked="serviceChecked('common', service)"
              @change="emit('toggleService', 'common', service, ($event.target as HTMLInputElement).checked)"
            />
            {{ serviceLabel(service, row.platform) }}
          </label>
        </div>
      </section>

      <section class="sub-card__section">
        <div class="sub-card__section-title">推送对象</div>
        <div class="mode-tabs" role="group" aria-label="推送对象类型">
          <button type="button" class="button button--small" :class="{ 'is-active': row.target_mode === 'group' }" @click="emit('targetMode', 'group')">群聊</button>
          <button type="button" class="button button--small" :class="{ 'is-active': row.target_mode === 'private' }" @click="emit('targetMode', 'private')">私聊</button>
        </div>
        <div class="target-select" role="listbox" aria-multiselectable="true" :aria-disabled="!context.targets.loaded" tabindex="0">
          <div class="target-options-list">
            <button
              v-for="target in availableTargets"
              :key="target.key"
              type="button"
              class="target-option"
              :class="{ 'is-selected': row.targets.some((item) => item.key === target.key) }"
              role="option"
              :aria-selected="row.targets.some((item) => item.key === target.key)"
              @click="emit('toggleTarget', target)"
            >
              <span class="target-option__mark" aria-hidden="true">{{ row.targets.some((item) => item.key === target.key) ? '✓' : '' }}</span>
              <span class="target-option__label">{{ target.label }}</span>
              <span class="target-option__id">{{ target.target_id }}</span>
            </button>
            <div v-if="availableTargets.length === 0" class="target-option-empty">没有可选对象</div>
          </div>
        </div>
        <div class="chip-list target-chip-list">
          <span v-for="target in row.targets" :key="target.key" class="chip" :class="{ 'badge--warning': !context.targetMap.has(target.key) }">
            <AvatarBadge :url="targetAvatar(target, context.targetMap)" :label="targetDisplay(target, context.targetMap)" size="candidate" />
            <span>{{ targetDisplay(target, context.targetMap) }}</span>
            <button type="button" aria-label="移除推送对象" @click="emit('removeTarget', target.key)">×</button>
          </span>
          <span v-if="row.targets.length === 0" class="chip">未选择推送对象</span>
        </div>
        <div v-if="context.targets.issues.length" class="target-note">{{ context.targets.issues.map((issue) => issue.message).join('；') }}</div>
      </section>

      <section class="sub-card__section">
        <div class="sub-card__section-title">订阅人</div>
        <div class="subscriber-line">
          <input v-model="subscriberInput" class="subscriber-input" type="text" inputmode="numeric" autocomplete="off" placeholder="QQ 号，留空为系统订阅" @keydown.enter.prevent="addSubscriber" />
          <button type="button" class="button button--small" @click="addSubscriber">添加</button>
        </div>
        <div class="chip-list subscriber-chip-list">
          <span v-for="id in row.subscriber_ids" :key="id" class="chip">
            <AvatarBadge :url="subscriberAvatarURL(context.subscriberAvatars, id)" :label="`QQ ${id}`" size="candidate" />
            <span>QQ {{ id }}</span>
            <button type="button" aria-label="移除订阅人" @click="emit('removeSubscriber', id)">×</button>
          </span>
          <span v-if="row.subscriber_ids.length === 0" class="chip chip--success">系统订阅</span>
        </div>
        <div class="row-note">只保存 QQ 号，昵称和群名片保存时刷新。</div>
      </section>

      <ul v-if="errors.length" class="validation-list">
        <li v-for="error in errors" :key="error">{{ error }}</li>
      </ul>
    </div>

    <div v-else class="sub-card__body">
      <section class="sub-card__section">
        <div class="sub-card__section-title">推送类型</div>
        <div class="sub-card__services">
          <span v-if="row.service_mode === 'mixed'" class="service-tag">目标配置不同</span>
          <span v-else v-for="service in servicesText(row.services, row.platform).split('、')" :key="service" class="service-tag">{{ service }}</span>
        </div>
      </section>

      <section class="sub-card__section">
        <div class="sub-card__section-title">推送对象</div>
        <div class="sub-card__targets-summary">
          <span v-if="visibleTargets.length" class="avatar-stack">
            <AvatarBadge v-for="target in visibleTargets" :key="target.key" :url="targetAvatar(target, context.targetMap)" :label="targetDisplay(target, context.targetMap)" size="target" />
            <span v-if="row.targets.length > visibleTargets.length" class="avatar-stack__overflow">+{{ row.targets.length - visibleTargets.length }}</span>
          </span>
          <span v-else class="sub-card__summary-label">无</span>
          <span class="sub-card__summary-label">{{ row.targets.length ? `${row.targets.length} 个推送对象` : '未选择推送对象' }}</span>
        </div>
      </section>

      <section class="sub-card__section">
        <div class="sub-card__section-title">订阅人</div>
        <div class="sub-card__subscribers-summary">
          <span v-if="visibleSubscribers.length" class="avatar-stack">
            <AvatarBadge v-for="id in visibleSubscribers" :key="id" :url="subscriberAvatarURL(context.subscriberAvatars, id)" :label="`QQ ${id}`" size="subscriber" />
            <span v-if="row.subscriber_ids.length > visibleSubscribers.length" class="avatar-stack__overflow">+{{ row.subscriber_ids.length - visibleSubscribers.length }}</span>
          </span>
          <span v-else class="chip chip--success">系统订阅</span>
          <span class="sub-card__summary-label">{{ row.subscriber_ids.length ? `${row.subscriber_ids.length} 位订阅人` : '系统订阅' }}</span>
        </div>
      </section>
    </div>

    <div class="sub-card__actions">
      <template v-if="row.edit_mode">
        <div class="button-group">
          <label class="switch-row" title="启用">
            <input v-model="row.enabled" type="checkbox" />
            <span>{{ row.enabled ? '已启用' : '已停用' }}</span>
          </label>
        </div>
        <div class="button-group">
          <button type="button" class="button button--primary button--small" @click="emit('finish')">完成</button>
          <button type="button" class="button button--ghost button--small" @click="emit('cancel')">取消</button>
          <button type="button" class="button button--small" @click="emit('duplicate')">复制</button>
          <button type="button" class="button button--small button--danger" @click="emit('remove')">删除</button>
        </div>
      </template>
      <template v-else>
        <button type="button" class="button button--primary button--small" @click="emit('edit')">编辑</button>
        <button type="button" class="button button--small" @click="emit('duplicate')">复制</button>
        <button type="button" class="button button--small button--danger" @click="emit('remove')">删除</button>
      </template>
    </div>
  </article>
</template>

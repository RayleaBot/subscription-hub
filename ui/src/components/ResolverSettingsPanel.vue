<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Modal as AModal } from 'ant-design-vue'
import PlatformLogo from './PlatformLogo.vue'
import UiIcon from './UiIcon.vue'

import {
  allTargets,
  deriveTargetAvatarURL,
  displayAvatarURL,
  normalizeResolverSettings,
  targetKey,
  type LiveTarget,
  type ResolverPlatform,
  type ResolverSettings,
  type TargetType,
  type TargetsState,
} from '../model'

type ResolverPage = 'targets' | 'strategy'
type ScopeFilter = 'all' | TargetType

const props = withDefaults(defineProps<{
  modelValue: ResolverSettings
  avatarDataUrls: Map<string, string>
  targets: TargetsState
  view: ResolverPage
  loaded?: boolean
}>(), { loaded: true })
const emit = defineEmits<{
  'update:modelValue': [value: ResolverSettings]
  'open-help': []
  'request-avatars': [sources: string[]]
}>()

const page = computed(() => props.view)
const manageSearch = ref('')
const pickerSearch = ref('')
const pickerOpen = ref(false)
const pickerType = ref<TargetType>('group')
const scopeFilter = ref<ScopeFilter>('all')
const draft = ref<ResolverSettings>(normalizeResolverSettings(props.modelValue))

const resolverPlatforms: ResolverPlatform[] = ['bilibili', 'weibo', 'douyin']
const scopeOrder: ScopeFilter[] = ['all', 'group', 'private']
const pickerTypes: TargetType[] = ['group', 'private']

watch(
  () => props.modelValue,
  (value) => {
    if (JSON.stringify(value) !== JSON.stringify(draft.value)) draft.value = normalizeResolverSettings(value)
  },
  { deep: true },
)

watch(
  draft,
  (value) => {
    if (JSON.stringify(value) !== JSON.stringify(props.modelValue)) emit('update:modelValue', normalizeResolverSettings(value))
  },
  { deep: true },
)

watch(page, () => {
  manageSearch.value = ''
  pickerSearch.value = ''
  pickerOpen.value = false
  scopeFilter.value = 'all'
})

const configured = computed(() => new Map(draft.value.targets.map((target) => [targetKey(target.target_type, target.target_id), target])))
const liveTargets = computed(() => new Map(allTargets(props.targets).map((target) => [target.key, target])))
const configuredTargets = computed<LiveTarget[]>(() => {
  if (page.value !== 'targets') return []
  return draft.value.targets.map((target) => {
    const key = targetKey(target.target_type, target.target_id)
    const live = liveTargets.value.get(key)
    return {
      key,
      target_type: target.target_type,
      target_id: target.target_id,
      label: live?.label || target.target_name || target.target_id,
      avatar_url: live?.avatar_url || deriveTargetAvatarURL(target.target_type, target.target_id),
    }
  })
})
const scopeCounts = computed<Record<ScopeFilter, number>>(() => {
  const counts = { all: 0, group: 0, private: 0 }
  for (const target of configuredTargets.value) {
    counts.all += 1
    counts[target.target_type] += 1
  }
  return counts
})
const scopeOptions = computed(() => scopeOrder.map((value) => ({ value, label: scopeLabel(value), count: scopeCounts.value[value] })))
const scopeIndex = computed(() => scopeOrder.indexOf(scopeFilter.value))
const pickerIndex = computed(() => pickerTypes.indexOf(pickerType.value))
const filteredConfiguredTargets = computed(() => {
  const query = manageSearch.value.trim().toLowerCase()
  return configuredTargets.value.filter((target) => (
    (scopeFilter.value === 'all' || target.target_type === scopeFilter.value)
    && (!query || targetMatches(target, query))
  ))
})
const candidateTargets = computed(() => {
  if (page.value !== 'targets') return []
  const query = pickerSearch.value.trim().toLowerCase()
  return allTargets(props.targets).filter((target) => (
    target.target_type === pickerType.value
    && !configured.value.has(target.key)
    && (!query || targetMatches(target, query))
  ))
})
const visibleConfiguredTargets = computed(() => filteredConfiguredTargets.value.slice(0, 100))
const visibleCandidateTargets = computed(() => candidateTargets.value.slice(0, 100))
const visibleAvatarSources = computed(() => {
  const targets = pickerOpen.value
    ? [...visibleConfiguredTargets.value, ...visibleCandidateTargets.value]
    : visibleConfiguredTargets.value
  return [...new Set(targets.map((target) => target.avatar_url).filter(Boolean))]
})
const pickerTypeLabel = computed(() => typeLabel(pickerType.value))
const pickerPlaceholder = computed(() => `输入${pickerType.value === 'group' ? '群名或群号' : '昵称或 QQ 号'}…`)
const emptyCopy = computed(() => {
  if (manageSearch.value) return { title: '没有匹配项', hint: '请调整搜索条件或切换会话类型。' }
  if (scopeFilter.value !== 'all' && scopeCounts.value.all > 0) return { title: `尚未添加${scopeLabel(scopeFilter.value)}`, hint: `点击“添加对象”从连接协议选择${scopeLabel(scopeFilter.value)}。` }
  return { title: '尚未添加解析对象', hint: '点击“添加对象”从连接协议选择群聊或私聊；在聊天中开启解析后也会自动加入。' }
})
const pageCopy = computed(() => {
  if (page.value === 'targets') return { title: '链接解析', description: '在一个列表里管理群聊与私聊的 B站、微博、抖音解析开关。' }
  return { title: '防抖与媒体策略', description: '管理解析冷却、直播录制、视频发送和平台清晰度策略。' }
})

watch(
  () => visibleAvatarSources.value.join('\n'),
  () => {
    if (visibleAvatarSources.value.length > 0) emit('request-avatars', visibleAvatarSources.value)
  },
  { immediate: true },
)

function scopeLabel(scope: ScopeFilter): string {
  if (scope === 'group') return '群聊'
  return scope === 'private' ? '私聊' : '全部'
}

function typeLabel(type: TargetType): string {
  return type === 'group' ? '群聊' : '私聊'
}

function targetMatches(target: LiveTarget, query: string): boolean {
  return `${target.label} ${target.target_id}`.toLowerCase().includes(query)
}

function avatarURL(target: LiveTarget): string {
  return displayAvatarURL(target.avatar_url, props.avatarDataUrls)
}

function avatarFallback(target: LiveTarget): string {
  return [...target.label.trim()][0] || (target.target_type === 'group' ? '群' : '用')
}

function targetNumberLabel(target: LiveTarget): string {
  return `${target.target_type === 'group' ? '群号' : 'QQ'} ${target.target_id}`
}

function resolverPlatformLabel(platform: ResolverPlatform): string {
  if (platform === 'bilibili') return 'B站'
  return platform === 'weibo' ? '微博' : '抖音'
}

function platformEnabled(target: LiveTarget, platform: ResolverPlatform): boolean {
  return configured.value.get(target.key)?.[platform] === true
}

function enabledCount(target: LiveTarget): number {
  return resolverPlatforms.filter((platform) => platformEnabled(target, platform)).length
}

function openPicker() {
  pickerType.value = scopeFilter.value === 'private' ? 'private' : 'group'
  pickerSearch.value = ''
  pickerOpen.value = true
}

function addTarget(target: LiveTarget) {
  if (configured.value.has(target.key)) return
  draft.value.targets.push({
    target_type: target.target_type,
    target_id: target.target_id,
    target_name: target.label,
    bilibili: false,
    weibo: false,
    douyin: false,
  })
}

function removeTarget(target: LiveTarget) {
  const index = draft.value.targets.findIndex((item) => targetKey(item.target_type, item.target_id) === target.key)
  if (index >= 0) draft.value.targets.splice(index, 1)
}

function setPlatform(target: LiveTarget, platform: ResolverPlatform, enabled: boolean) {
  let index = draft.value.targets.findIndex((item) => targetKey(item.target_type, item.target_id) === target.key)
  if (index < 0) {
    addTarget(target)
    index = draft.value.targets.length - 1
  }
  const current = draft.value.targets[index]!
  current.target_name = target.label
  current[platform] = enabled
}
</script>

<template>
  <section class="resolver-panel" aria-labelledby="resolver-settings-title">
    <h2 id="resolver-settings-title" class="sr-only">{{ pageCopy.title }}</h2>
    <div v-if="page === 'targets'" class="target-page">
      <section class="admin-rule" :class="{ 'is-on': draft.super_admin_whitelist }" aria-labelledby="admin-rule-title">
        <span class="admin-rule__icon" aria-hidden="true"><UiIcon name="shield" :size="22" /></span>
        <div class="admin-rule__copy">
          <h3 id="admin-rule-title">超级管理员白名单</h3>
          <p>开启后，超级管理员发送的 B站、微博、抖音链接不受下方群聊、私聊开关限制，全部解析；冷却与媒体策略仍然生效。聊天中可用 <code>开启超管解析</code> / <code>关闭超管解析</code> 切换。</p>
        </div>
        <label class="admin-rule__control">
          <span class="admin-rule__state" aria-live="polite">{{ draft.super_admin_whitelist ? '已开启' : '已关闭' }}</span>
          <span class="compact-switch compact-switch--large">
            <input v-model="draft.super_admin_whitelist" type="checkbox" aria-label="超级管理员白名单" />
            <span aria-hidden="true"></span>
          </span>
        </label>
      </section>

      <div class="target-toolbar">
        <label class="target-search">
          <span class="sr-only">搜索已添加的群聊或私聊</span>
          <UiIcon name="search" :size="16" />
          <input v-model="manageSearch" type="search" autocomplete="off" placeholder="搜索群名、昵称或号码…" />
        </label>
        <div class="scope-tabs" role="group" aria-label="会话类型筛选" :style="{ '--tab-count': scopeOptions.length, '--tab-index': scopeIndex }">
          <span class="scope-tabs__indicator" aria-hidden="true"></span>
          <button
            v-for="scope in scopeOptions"
            :key="scope.value"
            type="button"
            class="scope-tab"
            :aria-pressed="scopeFilter === scope.value"
            @click="scopeFilter = scope.value"
          >{{ scope.label }}<span class="scope-tab__count">{{ scope.count }}</span></button>
        </div>
        <div class="target-toolbar__actions">
          <button type="button" class="button" @click="$emit('open-help')">解析帮助</button>
          <button type="button" class="target-button target-button--primary" :aria-expanded="pickerOpen" @click="openPicker">
            <UiIcon name="plus" />添加对象
          </button>
        </div>
      </div>

      <AModal centered :footer="null" :open="pickerOpen" title="添加对象" :width="480" class="hub-dialog" @cancel="pickerOpen = false"><section class="target-picker" aria-labelledby="target-picker-title">
        <div class="target-picker-heading">
          <h3 id="target-picker-title">从连接协议添加{{ pickerTypeLabel }}</h3>
          <p>这里只列出尚未添加的对象；添加后才会出现在解析开关列表中。</p>
        </div>
        <div class="scope-tabs scope-tabs--picker" role="group" aria-label="添加对象类型" :style="{ '--tab-count': pickerTypes.length, '--tab-index': pickerIndex }">
          <span class="scope-tabs__indicator" aria-hidden="true"></span>
          <button v-for="type in pickerTypes" :key="type" type="button" class="scope-tab" :aria-pressed="pickerType === type" @click="pickerType = type">{{ typeLabel(type) }}</button>
        </div>
        <label class="picker-search">
          <span>搜索可添加{{ pickerTypeLabel }}</span>
          <input v-model="pickerSearch" type="search" autocomplete="off" :placeholder="pickerPlaceholder" />
        </label>
        <div class="candidate-grid">
          <TransitionGroup name="cards">
          <button
            v-for="target in visibleCandidateTargets"
            :key="target.key"
            type="button"
            class="target-choice-card"
            :aria-label="`添加${target.label}，${targetNumberLabel(target)}`"
            :title="`点击添加${target.label}`"
            @click="addTarget(target)"
          >
            <span class="target-avatar target-avatar--choice" aria-hidden="true">
              <img v-if="avatarURL(target)" :src="avatarURL(target)" alt="" />
              <span v-else>{{ avatarFallback(target) }}</span>
            </span>
            <span class="target-identity">
              <strong :title="target.label">{{ target.label }}</strong>
              <span>{{ targetNumberLabel(target) }}</span>
            </span>
            <UiIcon name="plus" :size="16" class="target-choice-card__plus" />
          </button>
          </TransitionGroup>
          <div v-if="visibleCandidateTargets.length === 0" class="picker-empty">
            <strong>{{ !targets.loaded ? `正在读取连接协议中的${pickerTypeLabel}…` : pickerSearch ? '没有匹配项' : `暂无可添加${pickerTypeLabel}` }}</strong>
            <span v-if="targets.loaded && !pickerSearch">{{ targets.available ? `连接协议中的${pickerTypeLabel}已全部添加。` : '请确认连接协议在线，并刷新可选范围。' }}</span>
          </div>
        </div>
        <p v-if="targets.issues.length" class="picker-issue">{{ targets.issues.map((issue) => issue.message).join('；') }}</p>
        <p v-if="candidateTargets.length > visibleCandidateTargets.length" class="result-limit">当前显示前 100 项，输入名称或号码可继续筛选。</p>
      </section></AModal>

      <div class="collection-caption">
        <span>{{ filteredConfiguredTargets.length }} 个{{ scopeFilter === 'all' ? '解析对象' : scopeLabel(scopeFilter) }}<template v-if="scopeFilter === 'all' && scopeCounts.all > 0">（{{ scopeCounts.group }} 群聊 / {{ scopeCounts.private }} 私聊）</template></span>
        <span>按平台独立开启解析</span>
      </div>
      <TransitionGroup name="cards" tag="div" class="resolver-target-grid" aria-label="群聊与私聊解析开关">
        <article v-for="target in visibleConfiguredTargets" :key="target.key" class="resolver-target-card" :class="{ 'is-active': enabledCount(target) > 0 }">
          <div class="resolver-target-card__head">
            <span class="target-avatar" aria-hidden="true">
              <img v-if="avatarURL(target)" :src="avatarURL(target)" alt="" />
              <span v-else>{{ avatarFallback(target) }}</span>
            </span>
            <div class="target-identity">
              <strong :title="target.label">{{ target.label }}</strong>
              <span><em class="type-chip" :class="`type-chip--${target.target_type}`">{{ typeLabel(target.target_type) }}</em>{{ targetNumberLabel(target) }}</span>
            </div>
            <button type="button" class="target-remove" :aria-label="`移除${target.label}`" @click="removeTarget(target)">移除</button>
          </div>
          <div class="resolver-platforms">
            <label v-for="platform in resolverPlatforms" :key="platform" class="platform-switch" :class="{ 'is-on': platformEnabled(target, platform) }">
              <span class="platform-switch__label"><PlatformLogo :platform="platform" :size="24" />{{ resolverPlatformLabel(platform) }}</span>
              <span class="compact-switch">
                <input
                  type="checkbox"
                  :checked="platformEnabled(target, platform)"
                  :aria-label="`${target.label} ${resolverPlatformLabel(platform)}解析`"
                  @change="setPlatform(target, platform, ($event.target as HTMLInputElement).checked)"
                />
                <span aria-hidden="true"></span>
              </span>
            </label>
          </div>
        </article>
      </TransitionGroup>
      <div v-if="loaded && visibleConfiguredTargets.length === 0" class="resolver-empty">
        <UiIcon name="inbox" :size="28" />
        <strong>{{ emptyCopy.title }}</strong>
        <span>{{ emptyCopy.hint }}</span>
      </div>
      <p v-if="filteredConfiguredTargets.length > visibleConfiguredTargets.length" class="result-limit">当前显示前 100 项，输入名称或号码可继续筛选。</p>
    </div>

    <div v-else class="strategy-page">
      <section class="strategy-section">
        <div class="strategy-title"><h3>冷却防抖</h3><p>同链接冷却默认开启 10 秒；同平台冷却按需开启。</p></div>
        <div class="setting-grid setting-grid--two">
          <label class="setting-toggle">
            <span><strong>同链接冷却</strong><small>同一会话内，相同链接在时间窗口内只解析一次。</small></span>
            <input v-model="draft.cooldowns.same_link_enabled" type="checkbox" />
          </label>
          <label class="number-field"><span>冷却时间</span><div><input v-model.number="draft.cooldowns.same_link_seconds" type="number" min="1" max="3600" /><em>秒</em></div></label>
          <label class="setting-toggle">
            <span><strong>同平台冷却</strong><small>同一会话内，限制同平台连续解析频率。</small></span>
            <input v-model="draft.cooldowns.same_platform_enabled" type="checkbox" />
          </label>
          <label class="number-field"><span>冷却时间</span><div><input v-model.number="draft.cooldowns.same_platform_seconds" type="number" min="1" max="3600" /><em>秒</em></div></label>
        </div>
      </section>

      <section class="strategy-section">
        <div class="strategy-title"><h3>通用媒体</h3><p>控制直播录制、媒体发送方式和 FFmpeg 并发。</p></div>
        <div class="setting-grid setting-grid--three">
          <label class="number-field"><span>直播录制</span><div><input v-model.number="draft.media.live_record_seconds" type="number" min="5" max="50" /><em>秒</em></div></label>
          <label class="number-field"><span>文件上传阈值</span><div><input v-model.number="draft.media.video_size_limit_mb" type="number" min="1" max="2048" /><em>MB</em></div></label>
          <label class="number-field"><span>媒体并发</span><div><input v-model.number="draft.media.media_concurrency" type="number" min="1" max="8" /><em>路</em></div></label>
          <label class="number-field"><span>图片转发阈值</span><div><input v-model.number="draft.media.image_forward_threshold" type="number" min="0" max="100" /><em>张</em></div></label>
          <label class="number-field"><span>合并转发分批</span><div><input v-model.number="draft.media.image_batch_size" type="number" min="1" max="100" /><em>张</em></div></label>
          <label class="select-field"><span>视频编码偏好</span><select v-model="draft.media.video_codec"><option value="auto">自动</option><option value="avc">AVC / H.264</option><option value="hevc">HEVC / H.265</option><option value="av1">AV1</option></select></label>
        </div>
        <div class="inline-toggles">
          <label><input v-model="draft.media.upload_oversize" type="checkbox" />B站、微博超限视频上传为群或私聊文件；抖音始终发送视频</label>
          <label><input v-model="draft.media.compatibility_transcode" type="checkbox" />统一转码为 H.264 + AAC</label>
        </div>
      </section>

      <section class="strategy-section">
        <div class="strategy-title"><h3><PlatformLogo platform="bilibili" :size="28" />B站视频</h3><p>普通视频与番剧分别控制时长和清晰度；番剧直链默认关闭。</p></div>
        <div class="setting-grid setting-grid--three">
          <label class="number-field"><span>普通视频最长</span><div><input v-model.number="draft.media.bilibili_max_duration_seconds" type="number" min="1" max="7200" /><em>秒</em></div></label>
          <label class="select-field"><span>普通视频清晰度</span><select v-model.number="draft.media.bilibili_resolution"><option :value="360">360p</option><option :value="480">480p</option><option :value="720">720p</option><option :value="1080">1080p</option><option :value="2160">2160p</option></select></label>
          <label class="number-field"><span>智能大小上限</span><div><input v-model.number="draft.media.bilibili_file_size_limit_mb" type="number" min="1" max="2048" /><em>MB</em></div></label>
          <label class="select-field"><span>最低清晰度</span><select v-model.number="draft.media.bilibili_min_resolution"><option :value="360">360p</option><option :value="480">480p</option><option :value="720">720p</option><option :value="1080">1080p</option><option :value="2160">2160p</option></select></label>
          <label class="select-field"><span>番剧清晰度</span><select v-model.number="draft.media.bilibili_bangumi_resolution"><option :value="360">360p</option><option :value="480">480p</option><option :value="720">720p</option><option :value="1080">1080p</option><option :value="2160">2160p</option></select></label>
          <label class="number-field"><span>番剧最长</span><div><input v-model.number="draft.media.bilibili_bangumi_max_seconds" type="number" min="1" max="10800" /><em>秒</em></div></label>
        </div>
        <div class="inline-toggles">
          <label><input v-model="draft.media.bilibili_smart_resolution" type="checkbox" />按文件大小智能降低普通视频清晰度</label>
          <label><input v-model="draft.media.bilibili_bangumi_direct" type="checkbox" />发送番剧视频直链媒体</label>
        </div>
      </section>

      <section class="strategy-section">
        <div class="strategy-title"><h3><PlatformLogo platform="douyin" :size="28" />抖音视频</h3><p>控制作品时长、目标清晰度和动态图片配乐。</p></div>
        <div class="setting-grid setting-grid--two">
          <label class="number-field"><span>视频最长</span><div><input v-model.number="draft.media.douyin_max_duration_seconds" type="number" min="1" max="7200" /><em>秒</em></div></label>
          <label class="select-field"><span>目标清晰度</span><select v-model.number="draft.media.douyin_resolution"><option :value="720">720p</option><option :value="1080">1080p</option></select></label>
        </div>
        <div class="inline-toggles"><label><input v-model="draft.media.douyin_merge_bgm" type="checkbox" />动态图片视频合并作品背景音乐</label></div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.resolver-panel { border: 0; border-radius: 0; background: transparent; box-shadow: none; }
.target-page, .strategy-page { padding: 0; }

/* Global rule: super admin whitelist */
.admin-rule {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
  padding: 18px 20px;
  border: 1px solid var(--border);
  border-radius: 14px;
  background: var(--surface);
  transition: border-color 220ms var(--motion-ease), background-color 220ms var(--motion-ease), box-shadow 220ms var(--motion-ease);
}
.admin-rule.is-on { border-color: var(--border-accent); background: color-mix(in srgb, var(--accent) 6%, var(--surface)); box-shadow: 0 0 0 4px color-mix(in srgb, var(--accent) 6%, transparent); }
.admin-rule__icon {
  display: grid;
  width: 44px;
  height: 44px;
  place-items: center;
  border-radius: 12px;
  color: var(--accent-strong);
  background: var(--accent-soft);
  transition: color 220ms ease-out, background-color 220ms ease-out, transform 320ms var(--motion-ease);
}
.admin-rule.is-on .admin-rule__icon { color: var(--on-accent); background: var(--accent); transform: rotate(-6deg) scale(1.04); }
.admin-rule__copy { min-width: 0; }
.admin-rule__copy h3 { margin: 0; color: var(--text); font-size: 15px; font-weight: 650; }
.admin-rule__copy p { margin: 5px 0 0; color: var(--muted); font-size: 13px; line-height: 1.6; }
.admin-rule__copy code { padding: 1px 6px; border-radius: 5px; background: var(--surface-strong); color: var(--text); font-family: "Cascadia Mono", "SFMono-Regular", ui-monospace, monospace; font-size: 12px; }
.admin-rule__control { display: flex; align-items: center; gap: 12px; cursor: pointer; }
.admin-rule__state { min-width: 3em; color: var(--muted); font-size: 13px; font-weight: 600; text-align: end; transition: color 200ms ease-out; }
.admin-rule.is-on .admin-rule__state { color: var(--accent-strong); }

/* Toolbar */
.target-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-bottom: 20px; }
.target-toolbar__actions { display: flex; flex-shrink: 0; align-items: center; gap: 8px; margin-left: auto; }
.target-search {
  display: flex;
  align-items: center;
  gap: 10px;
  width: min(380px, 100%);
  min-height: 40px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: 9px;
  color: var(--muted);
  background: var(--surface);
  transition: border-color 160ms ease-out, box-shadow 160ms ease-out;
}
.target-search:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent); }
.target-search input { width: 100%; min-width: 0; height: 38px; border: 0; outline: 0; background: transparent; color: var(--text); font: inherit; font-size: 13px; }
.target-search input:focus-visible { outline: 0; }

.scope-tabs {
  position: relative;
  display: grid;
  grid-template-columns: repeat(var(--tab-count, 3), minmax(0, 1fr));
  isolation: isolate;
  padding: 3px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface-soft);
}
.scope-tabs__indicator {
  position: absolute;
  z-index: 0;
  top: 3px;
  bottom: 3px;
  left: 3px;
  width: calc((100% - 6px) / var(--tab-count, 3));
  border-radius: 8px;
  background: var(--surface);
  box-shadow: 0 1px 3px rgb(15 23 42 / 10%), 0 0 0 1px var(--border);
  transform: translateX(calc(var(--tab-index, 0) * 100%));
  transition: transform 260ms var(--motion-ease);
}
.scope-tab {
  position: relative;
  z-index: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 32px;
  padding: 0 14px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--muted);
  font: inherit;
  font-size: 13px;
  white-space: nowrap;
  cursor: pointer;
  transition: color 180ms ease-out;
}
.scope-tab:hover { color: var(--text); }
.scope-tab[aria-pressed='true'] { color: var(--text); font-weight: 650; }
.scope-tab__count { display: inline-flex; min-width: 18px; height: 18px; align-items: center; justify-content: center; padding: 0 5px; border-radius: 999px; background: var(--surface-strong); color: var(--muted); font-size: 11px; font-weight: 600; font-variant-numeric: tabular-nums; transition: background-color 180ms ease-out, color 180ms ease-out; }
.scope-tab[aria-pressed='true'] .scope-tab__count { background: var(--accent-soft); color: var(--accent-strong); }
.scope-tabs--picker { width: 100%; margin-top: 16px; }
.scope-tabs--picker .scope-tab { min-height: 36px; }

.target-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 36px;
  padding: 0 14px;
  border: 1px solid var(--border);
  border-radius: 10px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: color 160ms ease-out, background-color 160ms ease-out, border-color 160ms ease-out, transform 160ms var(--motion-ease);
}
.target-button:hover { border-color: var(--accent); color: var(--accent-strong); }
.target-button:active { transform: translateY(1px); }
.target-button--primary { flex: 0 0 auto; color: var(--on-accent); border-color: var(--accent); background: var(--accent); }
.target-button--primary:hover { color: var(--on-accent); border-color: var(--accent-strong); background: var(--accent-strong); }

/* Picker */
.target-picker { padding: 0; margin: 0; border: 0; background: transparent; box-shadow: none; }
.target-picker-heading h3 { margin: 0; color: var(--text); font-size: 17px; }
.target-picker-heading p { max-width: 70ch; margin: 6px 0 0; color: var(--muted); line-height: 1.55; }
.picker-search { display: grid; gap: 7px; margin-top: 14px; color: var(--text); font-size: 13px; font-weight: 600; }
.picker-search input { min-width: 0; min-height: 40px; padding: 0 13px; border: 1px solid var(--border); border-radius: 10px; color: var(--text); background: var(--surface); font: inherit; font-size: 13px; }
.candidate-grid { position: relative; display: grid; grid-template-columns: 1fr; gap: 8px; margin-top: 14px; }
.target-choice-card {
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  min-width: 0;
  min-height: 68px;
  padding: 12px;
  border: 1px solid transparent;
  border-radius: 10px;
  color: var(--text);
  background: var(--surface-soft);
  font: inherit;
  text-align: start;
  cursor: pointer;
  transition: border-color 160ms ease-out, background-color 160ms ease-out, transform 160ms var(--motion-ease);
}
.target-choice-card:hover { border-color: var(--border-accent); background: color-mix(in srgb, var(--accent) 6%, var(--surface)); }
.target-choice-card:active { transform: scale(.985); }
.target-choice-card__plus { color: var(--muted); transition: color 160ms ease-out, transform 200ms var(--motion-ease); }
.target-choice-card:hover .target-choice-card__plus { color: var(--accent-strong); transform: rotate(90deg); }
.picker-empty { display: grid; grid-column: 1 / -1; gap: 5px; place-items: center; min-height: 126px; padding: 22px; border: 1px dashed var(--border); border-radius: 10px; color: var(--muted); background: var(--surface); text-align: center; }
.picker-empty strong { color: var(--text); }
.picker-issue { margin: 12px 0 0; color: var(--danger); font-size: 13px; line-height: 1.55; }

/* Target cards */
.resolver-target-grid { position: relative; display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr)); gap: 18px; }
.resolver-target-card {
  min-width: 0;
  padding: 20px;
  border: 1px solid var(--border);
  border-radius: 14px;
  background: var(--surface);
  transition: border-color 200ms ease-out, background-color 200ms ease-out;
}
.resolver-target-card:hover { border-color: var(--border-strong); }
.resolver-target-card.is-active { border-color: color-mix(in srgb, var(--accent) 24%, var(--border)); }
.resolver-target-card__head { display: grid; grid-template-columns: 48px minmax(0, 1fr) auto; align-items: center; gap: 12px; }
.target-avatar {
  display: grid;
  width: 48px;
  height: 48px;
  overflow: hidden;
  place-items: center;
  flex: 0 0 auto;
  border: 1px solid var(--border);
  border-radius: 50%;
  color: var(--accent-strong);
  background: color-mix(in srgb, var(--accent) 10%, var(--surface-soft));
  font-size: 17px;
  font-weight: 800;
}
.target-avatar--choice { width: 44px; height: 44px; }
.target-avatar img { width: 100%; height: 100%; object-fit: cover; }
.target-identity { min-width: 0; }
.target-identity strong,
.target-identity span { display: block; min-width: 0; }
.target-identity strong { display: -webkit-box; overflow: hidden; color: var(--text); line-height: 1.4; overflow-wrap: anywhere; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.target-identity span { display: flex; align-items: center; gap: 6px; overflow: hidden; margin-top: 4px; color: var(--muted); font-size: 13px; font-variant-numeric: tabular-nums; text-overflow: ellipsis; white-space: nowrap; }
.type-chip { display: inline-flex; flex-shrink: 0; align-items: center; padding: 1px 7px; border-radius: 999px; background: var(--surface-strong); color: var(--text); font-size: 11px; font-style: normal; font-weight: 600; letter-spacing: .02em; }
.type-chip--group { background: color-mix(in srgb, var(--accent) 12%, var(--surface)); color: var(--accent-strong); }
.type-chip--private { background: color-mix(in srgb, var(--warning) 12%, var(--surface)); color: var(--warning); }
.target-remove { min-height: 36px; padding: 0 9px; border: 0; border-radius: 8px; color: var(--danger); background: transparent; font: inherit; font-size: 13px; font-weight: 600; cursor: pointer; transition: background-color 160ms ease-out; }
.target-remove:hover { background: color-mix(in srgb, var(--danger) 9%, transparent); }
.resolver-platforms { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin-top: 22px; padding-top: 16px; border-top: 1px solid var(--border); }
.platform-switch { display: grid; min-width: 0; place-items: center; gap: 7px; padding: 8px 4px; border-radius: 10px; color: var(--muted); font-size: 12px; font-weight: 500; cursor: pointer; transition: background-color 200ms ease-out, color 200ms ease-out; }
.platform-switch:hover { background: var(--surface-soft); }
.platform-switch.is-on { color: var(--text); }
.platform-switch__label { display: flex; align-items: center; gap: 6px; }

/* Switches */
.compact-switch { position: relative; display: grid; place-items: center; }
.compact-switch input { position: absolute; opacity: 0; pointer-events: none; }
.compact-switch span {
  position: relative;
  width: 38px;
  height: 22px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--muted) 35%, var(--surface));
  cursor: pointer;
  transition: background-color 200ms ease-out;
}
.compact-switch span::after {
  position: absolute;
  top: 3px;
  inset-inline-start: 3px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #fff;
  box-shadow: 0 2px 6px rgba(24, 32, 51, 0.2);
  content: '';
  transition: transform 220ms var(--motion-ease), width 160ms ease-out;
}
.compact-switch span:active::after { width: 20px; }
.compact-switch input:checked + span { background: var(--accent); }
.compact-switch input:checked + span::after { transform: translateX(16px); }
.compact-switch input:checked + span:active::after { transform: translateX(12px); }
.compact-switch--large span { width: 46px; height: 26px; }
.compact-switch--large span::after { width: 20px; height: 20px; }
.compact-switch--large span:active::after { width: 24px; }
.compact-switch--large input:checked + span::after { transform: translateX(20px); }
.compact-switch--large input:checked + span:active::after { transform: translateX(16px); }
.compact-switch input:focus-visible + span,
.target-button:focus-visible,
.scope-tab:focus-visible,
.target-choice-card:focus-visible,
.target-remove:focus-visible,
input:focus-visible,
select:focus-visible { outline: 2px solid var(--muted); outline-offset: 2px; }

.resolver-empty { display: grid; gap: 6px; place-items: center; min-height: 200px; padding: 24px; border: 1px dashed var(--border); border-radius: 14px; color: var(--muted); text-align: center; }
.resolver-empty .ui-icon { margin-bottom: 4px; color: var(--muted); }
.resolver-empty strong { color: var(--text); }
.resolver-empty span { max-width: 48ch; font-size: 13px; line-height: 1.6; }
.result-limit { margin: 12px 0 0; color: var(--muted); font-size: 13px; }

/* Strategy page */
.strategy-page { max-width: 1120px; }
.strategy-section { display: grid; grid-template-columns: 220px minmax(0, 1fr); column-gap: 40px; padding: 24px 0; }
.strategy-section + .strategy-section { margin: 0; padding-top: 28px; border-top: 1px solid var(--border); }
.strategy-title { grid-column: 1; grid-row: 1 / 3; }
.strategy-title h3 { display: flex; align-items: center; gap: 8px; margin: 0; color: var(--text); font-size: 16px; }
.strategy-title p { margin: 8px 0 0; color: var(--muted); font-size: 12px; line-height: 1.6; }
.setting-grid { display: grid; grid-column: 2; gap: 20px; margin-top: 0; }
.setting-grid--two { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.setting-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.setting-toggle { display: flex; align-items: center; justify-content: space-between; gap: 18px; min-width: 0; }
.setting-toggle span { min-width: 0; }
.setting-toggle strong,
.setting-toggle small { display: block; }
.setting-toggle strong { color: var(--text); font-size: 13px; font-weight: 500; }
.setting-toggle small { margin-top: 4px; color: var(--muted); font-size: 12px; line-height: 1.45; }
.setting-toggle input,
.inline-toggles input { width: 18px; height: 18px; accent-color: var(--accent); }
.number-field,
.select-field { display: grid; gap: 7px; color: var(--text); font-size: 13px; font-weight: 500; }
.number-field div { position: relative; }
.number-field input,
.select-field select { min-width: 0; min-height: 38px; border: 1px solid var(--border); border-radius: 10px; color: var(--text); background: var(--surface); font: inherit; font-size: 14px; }
.number-field input { width: 100%; padding: 0 50px 0 12px; font-variant-numeric: tabular-nums; }
.number-field em { position: absolute; top: 50%; inset-inline-end: 12px; color: var(--muted); font-style: normal; transform: translateY(-50%); }
.select-field select { width: 100%; padding: 0 10px; }
.inline-toggles { display: flex; grid-column: 2; flex-direction: column; gap: 16px; margin-top: 18px; }
.inline-toggles label { display: inline-flex; align-items: center; gap: 8px; color: var(--text); font-size: 13px; font-weight: 400; }

@media (max-width: 860px) {
  .strategy-section { grid-template-columns: 1fr; gap: 20px; }
  .strategy-title, .setting-grid, .inline-toggles { grid-column: 1; grid-row: auto; }
  .setting-grid--three { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .target-search { width: 100%; flex: 1 1 100%; }
  .target-toolbar__actions { margin-left: 0; }
}

@media (max-width: 560px) {
  .setting-grid--two,
  .setting-grid--three { grid-template-columns: 1fr; }
  .admin-rule { grid-template-columns: auto minmax(0, 1fr); row-gap: 14px; padding: 16px; }
  .admin-rule__control { grid-column: 1 / -1; justify-content: space-between; }
  .scope-tabs { width: 100%; }
  .target-toolbar__actions { width: 100%; }
  .target-toolbar__actions > * { flex: 1 1 auto; }
  .resolver-target-card__head { grid-template-columns: 44px minmax(0, 1fr) auto; gap: 10px; }
  .target-avatar { width: 44px; height: 44px; }
  .platform-switch__label { gap: 4px; }
  .target-remove, .target-button, .scope-tab { min-height: 44px; }
  .compact-switch { min-height: 44px; }
}
</style>

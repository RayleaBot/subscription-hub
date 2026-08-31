<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import {
  allTargets,
  deriveTargetAvatarURL,
  displayAvatarURL,
  normalizeResolverSettings,
  targetKey,
  type LiveTarget,
  type ResolverPlatform,
  type ResolverSettings,
  type TargetsState,
} from '../model'

type ResolverPage = 'group' | 'private' | 'strategy'

const props = defineProps<{
  modelValue: ResolverSettings
  avatarDataUrls: Map<string, string>
  targets: TargetsState
  view: ResolverPage
}>()
const emit = defineEmits<{
  'update:modelValue': [value: ResolverSettings]
  'open-help': []
  'request-avatars': [sources: string[]]
}>()

const page = computed(() => props.view)
const manageSearch = ref('')
const pickerSearch = ref('')
const pickerOpen = ref(false)
const draft = ref<ResolverSettings>(normalizeResolverSettings(props.modelValue))

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
})

const configured = computed(() => new Map(draft.value.targets.map((target) => [targetKey(target.target_type, target.target_id), target])))
const liveTargets = computed(() => new Map(allTargets(props.targets).map((target) => [target.key, target])))
const configuredTargets = computed(() => {
  if (page.value === 'strategy') return []
  return draft.value.targets.filter((target) => target.target_type === page.value).map((target) => {
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
const filteredConfiguredTargets = computed(() => {
  if (page.value === 'strategy') return []
  const query = manageSearch.value.trim().toLowerCase()
  return configuredTargets.value.filter((target) => !query || targetMatches(target, query))
})
const candidateTargets = computed(() => {
  if (page.value === 'strategy') return []
  const query = pickerSearch.value.trim().toLowerCase()
  return allTargets(props.targets).filter((target) => (
    target.target_type === page.value
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
const targetTypeLabel = computed(() => page.value === 'group' ? '群聊' : '用户')
const pageCopy = computed(() => {
  if (page.value === 'group') return { title: '群聊解析', description: '从连接协议添加需要管理的群聊，再分别开启 B站、微博或抖音解析。' }
  if (page.value === 'private') return { title: '用户解析', description: '从连接协议添加需要管理的私聊用户，再分别开启 B站、微博或抖音解析。' }
  return { title: '防抖与媒体策略', description: '管理解析冷却、直播录制、视频发送和平台清晰度策略。' }
})

watch(
  () => visibleAvatarSources.value.join('\n'),
  () => {
    if (visibleAvatarSources.value.length > 0) emit('request-avatars', visibleAvatarSources.value)
  },
  { immediate: true },
)

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
    <div class="resolver-heading">
      <div>
        <h2 id="resolver-settings-title">{{ pageCopy.title }}</h2>
        <p>{{ pageCopy.description }}</p>
      </div>
      <button type="button" class="preview-help" @click="$emit('open-help')">预览解析帮助</button>
    </div>

    <div v-if="page !== 'strategy'" class="target-page">
      <div class="target-toolbar">
        <label v-if="configuredTargets.length" class="target-search">
          <span>筛选已添加{{ targetTypeLabel }}</span>
          <input v-model="manageSearch" type="search" autocomplete="off" :placeholder="`输入${page === 'group' ? '群名或群号' : '昵称或 QQ 号'}…`" />
        </label>
        <button type="button" class="target-button target-button--primary" :aria-expanded="pickerOpen" @click="pickerOpen = !pickerOpen">
          {{ pickerOpen ? '收起添加列表' : `添加${targetTypeLabel}` }}
        </button>
      </div>

      <section v-if="pickerOpen" class="target-picker" aria-labelledby="target-picker-title">
        <div class="target-picker-heading">
          <div>
            <h3 id="target-picker-title">从连接协议添加{{ targetTypeLabel }}</h3>
            <p>这里只列出尚未添加的对象；添加后才会出现在解析开关列表中。</p>
          </div>
        </div>
        <label class="picker-search">
          <span>搜索可添加{{ targetTypeLabel }}</span>
          <input v-model="pickerSearch" type="search" autocomplete="off" :placeholder="`输入${page === 'group' ? '群名或群号' : '昵称或 QQ 号'}…`" />
        </label>
        <div class="candidate-grid">
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
          </button>
          <div v-if="visibleCandidateTargets.length === 0" class="picker-empty">
            <strong>{{ !targets.loaded ? `正在读取连接协议中的${targetTypeLabel}…` : pickerSearch ? '没有匹配项' : `暂无可添加${targetTypeLabel}` }}</strong>
            <span v-if="targets.loaded && !pickerSearch">{{ targets.available ? `连接协议中的${targetTypeLabel}已全部添加。` : '请确认连接协议在线，并刷新可选范围。' }}</span>
          </div>
        </div>
        <p v-if="targets.issues.length" class="picker-issue">{{ targets.issues.map((issue) => issue.message).join('；') }}</p>
        <p v-if="candidateTargets.length > visibleCandidateTargets.length" class="result-limit">当前显示前 100 项，输入名称或号码可继续筛选。</p>
      </section>

      <div class="resolver-target-grid" :aria-label="`${page === 'group' ? '群聊' : '用户'}解析开关`">
        <article v-for="target in visibleConfiguredTargets" :key="target.key" class="resolver-target-card">
          <div class="resolver-target-card__head">
            <span class="target-avatar" aria-hidden="true">
              <img v-if="avatarURL(target)" :src="avatarURL(target)" alt="" />
              <span v-else>{{ avatarFallback(target) }}</span>
            </span>
            <div class="target-identity">
              <strong :title="target.label">{{ target.label }}</strong>
              <span>{{ targetNumberLabel(target) }}</span>
            </div>
            <button type="button" class="target-remove" :aria-label="`移除${target.label}`" @click="removeTarget(target)">移除</button>
          </div>
          <div class="resolver-platforms">
            <label v-for="platform in (['bilibili', 'weibo', 'douyin'] as ResolverPlatform[])" :key="platform" class="platform-switch">
              <span>{{ resolverPlatformLabel(platform) }}</span>
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
        <div v-if="visibleConfiguredTargets.length === 0" class="resolver-empty">
          <strong>{{ manageSearch ? '没有匹配项' : `尚未添加${targetTypeLabel}` }}</strong>
          <span>{{ manageSearch ? '请调整筛选条件。' : `点击“添加${targetTypeLabel}”从连接协议选择；在聊天中开启解析后也会自动加入。` }}</span>
        </div>
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
        <div class="strategy-title"><h3>B站视频</h3><p>普通视频与番剧分别控制时长和清晰度；番剧直链默认关闭。</p></div>
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
        <div class="strategy-title"><h3>抖音视频</h3><p>控制作品时长、目标清晰度和动态图片配乐。</p></div>
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
.resolver-panel {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 16px;
  background: var(--surface);
}

.resolver-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  padding: 24px 26px 20px;
}

.resolver-heading h2,
.strategy-title h3 { margin: 0; color: var(--text); }
.resolver-heading h2 { font-size: 24px; letter-spacing: -0.02em; }
.resolver-heading p,
.strategy-title p { margin: 7px 0 0; color: var(--muted); line-height: 1.6; }

.preview-help {
  flex: 0 0 auto;
  min-height: 40px;
  padding: 0 15px;
  border: 1px solid var(--border);
  border-radius: 10px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}

.target-page,
.strategy-page { padding: 22px 26px 26px; }

.target-toolbar {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
}

.target-search,
.picker-search {
  display: grid;
  gap: 7px;
  color: var(--text);
  font-size: 14px;
  font-weight: 700;
}

.target-search { width: min(620px, 100%); }
.picker-search { margin-top: 18px; }

.target-search input,
.picker-search input,
.number-field input,
.select-field select {
  min-width: 0;
  height: 42px;
  border: 1px solid var(--border);
  border-radius: 10px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
}

.target-search input,
.picker-search input { padding: 0 13px; font-size: 16px; }

.target-button {
  min-height: 38px;
  padding: 0 14px;
  border: 1px solid var(--border);
  border-radius: 10px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  font-weight: 750;
  cursor: pointer;
}
.target-button:hover { border-color: var(--accent); color: var(--accent-strong); }
.target-button--primary { flex: 0 0 auto; min-height: 42px; color: #fff; border-color: var(--accent); background: var(--accent); }
.target-button--primary:hover { color: #fff; border-color: var(--accent-strong); background: var(--accent-strong); }

.target-picker {
  margin-bottom: 22px;
  padding: 20px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface-soft);
}
.target-picker-heading h3 { margin: 0; color: var(--text); font-size: 17px; }
.target-picker-heading p { max-width: 70ch; margin: 6px 0 0; color: var(--muted); line-height: 1.55; }
.candidate-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 230px), 1fr));
  gap: 10px;
  margin-top: 14px;
}
.target-choice-card {
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr);
  align-items: center;
  gap: 12px;
  min-width: 0;
  min-height: 68px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  color: var(--text);
  background: var(--surface);
  font: inherit;
  text-align: start;
  cursor: pointer;
  transition: border-color 160ms ease-out, background 160ms ease-out, transform 160ms ease-out;
}
.target-choice-card:hover { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 5%, var(--surface)); transform: translateY(-1px); }
.picker-empty { display: grid; grid-column: 1 / -1; gap: 5px; place-items: center; min-height: 126px; padding: 22px; border: 1px dashed var(--border); border-radius: 10px; color: var(--muted); background: var(--surface); text-align: center; }
.picker-empty strong { color: var(--text); }
.picker-issue { margin: 12px 0 0; color: var(--danger); font-size: 13px; line-height: 1.55; }

.resolver-target-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
  gap: 14px;
}
.resolver-target-card { min-width: 0; padding: 16px; border: 1px solid var(--border); border-radius: 12px; background: var(--surface); }
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
.target-identity span { overflow: hidden; margin-top: 3px; color: var(--muted); font-size: 13px; font-variant-numeric: tabular-nums; text-overflow: ellipsis; white-space: nowrap; }
.target-remove { min-height: 36px; padding: 0 9px; border: 0; border-radius: 8px; color: var(--danger); background: transparent; font: inherit; font-size: 13px; font-weight: 750; cursor: pointer; }
.target-remove:hover { background: color-mix(in srgb, var(--danger) 9%, transparent); }
.resolver-platforms { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin-top: 15px; padding-top: 13px; border-top: 1px solid var(--border); }
.platform-switch { display: grid; min-width: 0; place-items: center; gap: 7px; color: var(--muted); font-size: 12px; font-weight: 750; }

.compact-switch { display: grid; place-items: center; }
.compact-switch input { position: absolute; opacity: 0; pointer-events: none; }
.compact-switch span {
  position: relative;
  width: 38px;
  height: 22px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--muted) 35%, var(--surface));
  cursor: pointer;
  transition: background 160ms ease-out;
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
  transition: transform 160ms ease-out;
}
.compact-switch input:checked + span { background: var(--accent); }
.compact-switch input:checked + span::after { transform: translateX(16px); }
.compact-switch input:focus-visible + span,
.preview-help:focus-visible,
.target-button:focus-visible,
.target-choice-card:focus-visible,
.target-remove:focus-visible,
input:focus-visible,
select:focus-visible { outline: 3px solid color-mix(in srgb, var(--accent) 24%, transparent); outline-offset: 2px; }

.resolver-empty { display: grid; grid-column: 1 / -1; gap: 5px; place-items: center; min-height: 160px; padding: 24px; border: 1px dashed var(--border); border-radius: 12px; color: var(--muted); text-align: center; }
.resolver-empty strong { color: var(--text); }
.result-limit { margin: 12px 0 0; color: var(--muted); font-size: 13px; }

.strategy-section + .strategy-section { margin-top: 30px; padding-top: 26px; border-top: 1px solid var(--border); }
.strategy-title h3 { font-size: 18px; }
.strategy-title p { font-size: 14px; }
.setting-grid { display: grid; gap: 16px 20px; margin-top: 19px; }
.setting-grid--two { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.setting-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }

.setting-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  min-width: 0;
}
.setting-toggle span { min-width: 0; }
.setting-toggle strong,
.setting-toggle small { display: block; }
.setting-toggle strong { color: var(--text); }
.setting-toggle small { margin-top: 4px; color: var(--muted); line-height: 1.45; }
.setting-toggle input,
.inline-toggles input { width: 18px; height: 18px; accent-color: var(--accent); }

.number-field,
.select-field { display: grid; gap: 7px; color: var(--text); font-size: 14px; font-weight: 700; }
.number-field div { position: relative; }
.number-field input { width: 100%; padding: 0 50px 0 12px; font-size: 16px; font-variant-numeric: tabular-nums; }
.number-field em { position: absolute; top: 50%; inset-inline-end: 12px; color: var(--muted); font-style: normal; transform: translateY(-50%); }
.select-field select { width: 100%; padding: 0 10px; font-size: 16px; }

.inline-toggles { display: flex; flex-wrap: wrap; gap: 12px 24px; margin-top: 18px; }
.inline-toggles label { display: inline-flex; align-items: center; gap: 8px; color: var(--text); font-size: 14px; font-weight: 700; }

@media (max-width: 820px) {
  .resolver-heading { align-items: stretch; flex-direction: column; }
  .preview-help { width: 100%; }
  .setting-grid--three { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 560px) {
  .resolver-heading,
  .target-page,
  .strategy-page { padding-inline: 18px; }
  .setting-grid--two,
  .setting-grid--three { grid-template-columns: 1fr; }
  .target-toolbar { align-items: stretch; flex-direction: column; }
  .target-button--primary { width: 100%; }
  .target-picker { padding: 16px; }
  .resolver-target-card__head { grid-template-columns: 44px minmax(0, 1fr) auto; gap: 10px; }
  .target-avatar { width: 44px; height: 44px; }
}
</style>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import {
  allTargets,
  targetKey,
  type LiveTarget,
  type ResolverPlatform,
  type ResolverSettings,
  type ResolverTargetSettings,
  type TargetType,
  type TargetsState,
} from '../model'

const props = defineProps<{
  modelValue: ResolverSettings
  targets: TargetsState
}>()
const emit = defineEmits<{
  'update:modelValue': [value: ResolverSettings]
  'open-help': []
}>()

type ResolverPage = TargetType | 'strategy'

const page = ref<ResolverPage>('group')
const search = ref('')
const draft = ref<ResolverSettings>(structuredClone(props.modelValue))

watch(
  () => props.modelValue,
  (value) => {
    if (JSON.stringify(value) !== JSON.stringify(draft.value)) draft.value = structuredClone(value)
  },
  { deep: true },
)

watch(
  draft,
  (value) => {
    if (JSON.stringify(value) !== JSON.stringify(props.modelValue)) emit('update:modelValue', structuredClone(value))
  },
  { deep: true },
)

const configured = computed(() => new Map(draft.value.targets.map((target) => [targetKey(target.target_type, target.target_id), target])))
const targetOptions = computed(() => {
  const result = new Map<string, LiveTarget>()
  for (const target of allTargets(props.targets)) result.set(target.key, target)
  for (const target of draft.value.targets) {
    const key = targetKey(target.target_type, target.target_id)
    if (!result.has(key)) {
      result.set(key, {
        key,
        target_type: target.target_type,
        target_id: target.target_id,
        label: target.target_name || target.target_id,
        avatar_url: '',
      })
    }
  }
  return [...result.values()]
})
const filteredTargets = computed(() => {
  if (page.value === 'strategy') return []
  const query = search.value.trim().toLowerCase()
  return targetOptions.value.filter((target) => {
    if (target.target_type !== page.value) return false
    return !query || `${target.label} ${target.target_id}`.toLowerCase().includes(query)
  })
})
const visibleTargets = computed(() => filteredTargets.value.slice(0, 100))
const enabledCounts = computed(() => ({
  group: draft.value.targets.filter((target) => target.target_type === 'group' && targetEnabled(target)).length,
  private: draft.value.targets.filter((target) => target.target_type === 'private' && targetEnabled(target)).length,
}))

function targetEnabled(target: ResolverTargetSettings): boolean {
  return target.bilibili || target.weibo || target.douyin
}

function platformEnabled(target: LiveTarget, platform: ResolverPlatform): boolean {
  return configured.value.get(target.key)?.[platform] === true
}

function setPlatform(target: LiveTarget, platform: ResolverPlatform, enabled: boolean) {
  const index = draft.value.targets.findIndex((item) => targetKey(item.target_type, item.target_id) === target.key)
  if (index < 0) {
    draft.value.targets.push({
      target_type: target.target_type,
      target_id: target.target_id,
      target_name: target.label,
      bilibili: false,
      weibo: false,
      douyin: false,
      [platform]: enabled,
    })
    return
  }
  const current = draft.value.targets[index]!
  current.target_name = target.label
  current[platform] = enabled
  if (!targetEnabled(current)) draft.value.targets.splice(index, 1)
}
</script>

<template>
  <section class="resolver-panel" aria-labelledby="resolver-settings-title">
    <div class="resolver-heading">
      <div>
        <h2 id="resolver-settings-title">链接解析</h2>
        <p>解析默认关闭。开关按群聊或用户独立保存，发送链接后先发预览卡片，再发送媒体。</p>
      </div>
      <button type="button" class="preview-help" @click="$emit('open-help')">预览解析帮助</button>
    </div>

    <div class="resolver-tabs" role="tablist" aria-label="解析设置页面">
      <button type="button" role="tab" :aria-selected="page === 'group'" :class="{ active: page === 'group' }" @click="page = 'group'">
        群聊 <span>{{ enabledCounts.group }}</span>
      </button>
      <button type="button" role="tab" :aria-selected="page === 'private'" :class="{ active: page === 'private' }" @click="page = 'private'">
        用户 <span>{{ enabledCounts.private }}</span>
      </button>
      <button type="button" role="tab" :aria-selected="page === 'strategy'" :class="{ active: page === 'strategy' }" @click="page = 'strategy'">
        防抖与媒体策略
      </button>
    </div>

    <div v-if="page !== 'strategy'" class="target-page" role="tabpanel">
      <label class="target-search">
        <span>筛选{{ page === 'group' ? '群聊' : '用户' }}</span>
        <input v-model="search" type="search" autocomplete="off" :placeholder="`输入${page === 'group' ? '群名或群号' : '昵称或 QQ 号'}…`" />
      </label>

      <div class="target-table" role="table" :aria-label="`${page === 'group' ? '群聊' : '用户'}解析开关`">
        <div class="target-table-head" role="row">
          <span role="columnheader">对象</span><span role="columnheader">B站</span><span role="columnheader">微博</span><span role="columnheader">抖音</span>
        </div>
        <div v-for="target in visibleTargets" :key="target.key" class="target-row" role="row">
          <div class="target-identity" role="cell">
            <strong :title="target.label">{{ target.label }}</strong>
            <span>{{ target.target_id }}</span>
          </div>
          <label v-for="platform in (['bilibili', 'weibo', 'douyin'] as ResolverPlatform[])" :key="platform" class="compact-switch" role="cell">
            <input
              type="checkbox"
              :checked="platformEnabled(target, platform)"
              :aria-label="`${target.label} ${platform === 'bilibili' ? 'B站' : platform === 'weibo' ? '微博' : '抖音'}解析`"
              @change="setPlatform(target, platform, ($event.target as HTMLInputElement).checked)"
            />
            <span aria-hidden="true"></span>
          </label>
        </div>
        <div v-if="visibleTargets.length === 0" class="resolver-empty">
          <strong>暂无可管理对象</strong>
          <span>{{ search ? '没有匹配项，请调整筛选条件。' : '刷新可选范围后，可在此逐项开启解析。' }}</span>
        </div>
      </div>
      <p v-if="filteredTargets.length > visibleTargets.length" class="result-limit">当前显示前 100 项，输入名称或号码可继续筛选。</p>
    </div>

    <div v-else class="strategy-page" role="tabpanel">
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
          <label class="number-field"><span>视频阈值</span><div><input v-model.number="draft.media.video_size_limit_mb" type="number" min="1" max="2048" /><em>MB</em></div></label>
          <label class="number-field"><span>媒体并发</span><div><input v-model.number="draft.media.media_concurrency" type="number" min="1" max="8" /><em>路</em></div></label>
          <label class="number-field"><span>图片转发阈值</span><div><input v-model.number="draft.media.image_forward_threshold" type="number" min="0" max="100" /><em>张</em></div></label>
          <label class="number-field"><span>合并转发分批</span><div><input v-model.number="draft.media.image_batch_size" type="number" min="1" max="100" /><em>张</em></div></label>
          <label class="select-field"><span>视频编码偏好</span><select v-model="draft.media.video_codec"><option value="auto">自动</option><option value="avc">AVC / H.264</option><option value="hevc">HEVC / H.265</option><option value="av1">AV1</option></select></label>
        </div>
        <div class="inline-toggles">
          <label><input v-model="draft.media.upload_oversize" type="checkbox" />超限视频上传为群或私聊文件</label>
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

.resolver-tabs {
  display: flex;
  gap: 4px;
  padding: 0 26px;
  border-bottom: 1px solid var(--border);
}

.resolver-tabs button {
  min-height: 46px;
  padding: 0 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--muted);
  background: transparent;
  font: inherit;
  font-weight: 750;
  cursor: pointer;
}

.resolver-tabs button.active { border-bottom-color: var(--accent); color: var(--accent-strong); }
.resolver-tabs button span { margin-inline-start: 5px; color: var(--muted); font-variant-numeric: tabular-nums; }

.target-page,
.strategy-page { padding: 22px 26px 26px; }

.target-search {
  display: grid;
  grid-template-columns: minmax(110px, 0.28fr) minmax(240px, 1fr);
  align-items: center;
  gap: 16px;
  max-width: 620px;
  margin-bottom: 18px;
  color: var(--text);
  font-weight: 700;
}

.target-search input,
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

.target-search input { padding: 0 13px; font-size: 16px; }

.target-table { overflow: hidden; border: 1px solid var(--border); border-radius: 12px; }
.target-table-head,
.target-row { display: grid; grid-template-columns: minmax(220px, 1fr) repeat(3, minmax(70px, 94px)); align-items: center; }
.target-table-head { min-height: 42px; color: var(--muted); background: var(--surface-soft); font-size: 13px; font-weight: 800; }
.target-table-head span { text-align: center; }
.target-table-head span:first-child { padding-inline: 18px; text-align: start; }
.target-row { min-height: 66px; border-top: 1px solid var(--border); }
.target-identity { min-width: 0; padding-inline: 18px; }
.target-identity strong,
.target-identity span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.target-identity strong { color: var(--text); }
.target-identity span { margin-top: 3px; color: var(--muted); font-size: 13px; font-variant-numeric: tabular-nums; }

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
.resolver-tabs button:focus-visible,
input:focus-visible,
select:focus-visible { outline: 3px solid color-mix(in srgb, var(--accent) 24%, transparent); outline-offset: 2px; }

.resolver-empty { display: grid; gap: 5px; place-items: center; min-height: 160px; padding: 24px; color: var(--muted); text-align: center; }
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
  .resolver-tabs { overflow-x: auto; }
  .resolver-tabs button { flex: 0 0 auto; }
  .target-table-head,
  .target-row { grid-template-columns: minmax(150px, 1fr) repeat(3, 62px); }
  .setting-grid--three { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 560px) {
  .resolver-heading,
  .target-page,
  .strategy-page { padding-inline: 18px; }
  .resolver-tabs { padding-inline: 12px; }
  .target-search,
  .setting-grid--two,
  .setting-grid--three { grid-template-columns: 1fr; }
  .target-table-head,
  .target-row { grid-template-columns: minmax(120px, 1fr) repeat(3, 52px); }
  .target-identity { padding-inline: 12px; }
}
</style>

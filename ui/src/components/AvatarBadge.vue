<script setup lang="ts">
import { computed, ref, watch } from 'vue'

const props = defineProps<{
  url?: string
  label: string
  size: 'up' | 'target' | 'subscriber' | 'candidate'
}>()

const failed = ref(false)
const fallbackText = computed(() => props.label.trim().slice(0, 1).toUpperCase() || '?')
const fallbackColor = computed(() => {
  let hash = 0
  for (const character of props.label || '?') hash = character.charCodeAt(0) + ((hash << 5) - hash)
  return `hsl(${Math.abs(hash) % 360} 72% 58%)`
})

watch(() => props.url, () => { failed.value = false })
</script>

<template>
  <span class="avatar" :class="`avatar--${props.size}`" :style="{ background: fallbackColor }" :aria-label="props.label">
    <img
      v-if="props.url && !failed"
      :src="props.url"
      :alt="props.label"
      loading="lazy"
      referrerpolicy="no-referrer"
      @error="failed = true"
    />
    <span class="avatar-fallback__text">{{ fallbackText }}</span>
  </span>
</template>

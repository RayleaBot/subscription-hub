<script setup lang="ts">
import { computed } from 'vue'
import bilibili from '../assets/platforms/bilibili.png'
import douyin from '../assets/platforms/douyin.png'
import weibo from '../assets/platforms/weibo.png'
import netease from '../assets/platforms/netease_music.png'
const props = withDefaults(defineProps<{ platform: string; size?: number }>(), { size: 24 })
const logos: Record<string, string> = { bilibili, douyin, weibo, netease_music: netease }
const logo = computed(() => logos[props.platform])
</script>
<template><img v-if="logo" :src="logo" alt="" :width="size" :height="size" class="platform-logo" /><span v-else class="platform-logo platform-logo--fallback" :style="{ width: `${size}px`, height: `${size}px` }" aria-hidden="true">{{ platform.slice(0, 1).toUpperCase() }}</span></template>
<style scoped>.platform-logo { display: inline-flex; flex-shrink: 0; object-fit: contain; border-radius: 6px; background: #fff; }.platform-logo--fallback { align-items: center; justify-content: center; color: #555; }</style>

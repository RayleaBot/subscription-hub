<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ConfigProvider, theme } from 'ant-design-vue'
import zhCN from 'ant-design-vue/es/locale/zh_CN'
const dark = ref(document.documentElement.dataset.theme === 'dark')
const colors = ref<Record<string, string>>({})
function syncTheme() {
  dark.value = document.documentElement.dataset.theme === 'dark'
  const css = getComputedStyle(document.documentElement)
  const read = (key: string, fallback: string) => css.getPropertyValue(key).trim() || fallback
  colors.value = {
    colorPrimary: read('--accent', dark.value ? '#a3c5b3' : '#476c5e'),
    colorTextLightSolid: read('--on-accent', dark.value ? '#18281f' : '#ffffff'),
    colorBgContainer: read('--raylea-color-surface', dark.value ? '#1e1e1e' : '#ffffff'),
    colorBgElevated: read('--raylea-color-surface', dark.value ? '#1e1e1e' : '#ffffff'),
    colorText: read('--raylea-color-text', dark.value ? '#eeeeee' : '#252525'),
    colorTextSecondary: read('--raylea-color-muted', dark.value ? '#adadad' : '#666666'),
    colorBorder: read('--raylea-color-border', dark.value ? '#3d3d3d' : '#e3e3e3'),
  }
}
const observer = new MutationObserver(syncTheme)
onMounted(() => { syncTheme(); observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'style'] }) })
onBeforeUnmount(() => observer.disconnect())
const config = computed(() => ({ algorithm: dark.value ? theme.darkAlgorithm : theme.defaultAlgorithm, token: { ...colors.value, borderRadius: 8, controlHeight: 38, fontFamily: '"Segoe UI", "Microsoft YaHei UI", sans-serif' } }))
</script>
<template><ConfigProvider :theme="config" :locale="zhCN"><slot /></ConfigProvider></template>

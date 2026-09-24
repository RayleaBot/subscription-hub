import { createApp, h } from 'vue'
import '@rayleabot/plugin-ui/theme.css'

import App from './App.vue'
import ThemeProvider from './components/ThemeProvider.vue'
import './styles.css'

createApp({ render: () => h(ThemeProvider, null, { default: () => h(App) }) }).mount('#app')

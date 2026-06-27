import { createApp } from 'vue'
import App from './App.vue'
import router from './router.js'
import { connectWS, initLayoutFlags, store } from './store.js'
import { getSettings } from './api.js'

connectWS()
initLayoutFlags()

// Settings (notably sidebarOpen, derived from DesktopSidebarOpen) must be applied
// before the first mount/render, not in App.vue's onMounted — otherwise the Sidebar's
// open/close <Transition> would see it flip post-mount and animate on every page load.
const settings = await getSettings()
store.settings = settings
store.wordWrap = settings.wordWrap
store.sidebarWidth = settings.sidebarWidth
if (store.isPushLayout) store.sidebarOpen = settings.desktopSidebarOpen

createApp(App).use(router).mount('#app')

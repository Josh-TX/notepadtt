import { createApp } from 'vue'
import App from './App.vue'
import { connectWS, initLayoutFlags } from './store.js'

initLayoutFlags()
connectWS()

createApp(App).mount('#app')

import { createRouter, createWebHashHistory } from 'vue-router'

// Route components are empty - navigation is handled in App.vue via useRoute
const Empty = { template: '' }

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/:pathMatch(.*)*', component: Empty },
  ],
})

export default router

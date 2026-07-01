<template>
  <div class="navbar-wrapper">
    <button v-show="!(store.isPushLayout && store.sidebarOpen)" class="icon-btn sidebar-btn" @click="onToggleSidebar" title="Toggle sidebar">
      <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
        <rect x="1" y="3" width="14" height="1.5" rx="0.5"/>
        <rect x="1" y="7" width="14" height="1.5" rx="0.5"/>
        <rect x="1" y="11" width="14" height="1.5" rx="0.5"/>
      </svg>
    </button>
    <nav class="navbar" :class="{ 'no-sidebar-btn': store.isPushLayout && store.sidebarOpen }">
      <router-link v-if="crumbs.length" class="crumb link" to="/">root</router-link>
      <span v-else class="crumb">root</span>
      <span class="sep" v-if="crumbs.length">/</span>
      <template v-for="(crumb, i) in crumbs" :key="crumb.path">
        <router-link
          v-if="i < crumbs.length - 1"
          class="crumb link"
          :to="'/' + crumb.path"
        >{{ crumb.name }}</router-link>
        <span v-else class="crumb">{{ crumb.name }}</span>
        <span class="sep" v-if="i < crumbs.length - 1">/</span>
      </template>
    </nav>
    <button class="icon-btn new-btn" @click="newFile" title="New file">+</button>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { store, toggleSidebar, setActiveFile, setFileVersion, showToast } from '../store.js'
import { createFile, updateDesktopSidebarOpen } from '../api.js'

const crumbs = computed(() => {
  const p = store.currentFolderPath
  if (!p) return []
  const parts = p.split('/')
  return parts.map((name, i) => ({
    name,
    path: parts.slice(0, i + 1).join('/'),
  }))
})

// Toggling the sidebar while in push-layout (desktop) mode persists the new open state
// as DesktopSidebarOpen, so a refresh restores it — on mobile (overlay layout) the
// toggle is purely visual and nothing is saved.
async function onToggleSidebar() {
  toggleSidebar()
  if (!store.isPushLayout) return
  const open = store.sidebarOpen
  try {
    await updateDesktopSidebarOpen(open)
  } catch (e) {
    toggleSidebar()
    showToast('Failed to save sidebar state', 'error')
  }
}

async function newFile() {
  const data = await createFile(store.currentFolderPath)
  store.fileContents[data.fileId] = data.content
  setFileVersion(data.fileId, data.versionId)
  setActiveFile(data.fileId)
}
</script>

<style scoped>
.navbar-wrapper {
  position: relative;
  background: #181818;
  flex-shrink: 0;
  border-bottom: 1px solid #2d2d2d;
}
.navbar {
  display: flex;
  align-items: center;
  gap: 4px;
  overflow-x: auto;
  white-space: nowrap;
  padding: 0 36px 0 38px;
  line-height: 34px;
}
.navbar.no-sidebar-btn {
  padding-left: 10px;
}
.icon-btn {
  position: absolute;
  top: 0;
  background: #181818;
  border: none;
  color: #aaa;
  cursor: pointer;
  padding: 0 10px;
  height: 34px;
  display: flex;
  align-items: center;
}
.sidebar-btn { left: 0; }
.new-btn { right: 0; font-size: 20px; }
.icon-btn:hover { color: #fff; background: #2a2d2e; }
.crumb { color: #ccc; }
.crumb.link {
  color: #9cdcfe;
  text-decoration: none;
  cursor: pointer;
}
.crumb.link:hover { text-decoration: underline; }
.sep { color: #777; }
</style>

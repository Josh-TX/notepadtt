<template>
  <Transition name="sidebar">
    <Sidebar v-if="store.sidebarOpen" class="sidebar" :style="sidebarWidthStyle" />
  </Transition>
  <Transition name="overlay">
    <div v-if="store.sidebarOpen" class="sidebar-overlay" @click="closeSidebar" />
  </Transition>
  <div class="main-layout" :class="{ 'sidebar-open': store.sidebarOpen }" :style="mainLayoutStyle">
    <Navbar />
    <TabBar />
    <Editor />
    <Footer />
  </div>
  <Transition name="toast">
    <div v-if="store.toast" class="toast" :class="store.toastType">
      <span>{{ store.toast }}</span>
      <button v-if="store.toastAction" class="toast-action" @click="onToastAction">{{ store.toastAction.label }}</button>
    </div>
  </Transition>
  <SearchModal />
  <HistoryModal />
  <TrashModal />
  <SettingsModal />
  <MoveFileModal />
</template>

<script setup>
import { watch, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { store, setCurrentFolder, setActiveFile, setFileVersion, getFilesInFolder, onTreeUpdate, closeSidebar, dismissToast, openSearchModal, showToast } from './store.js'
import { getFiles, getFile } from './api.js'
import Navbar from './components/Navbar.vue'
import TabBar from './components/TabBar.vue'
import Editor from './components/Editor.vue'
import Footer from './components/Footer.vue'
import Sidebar from './components/Sidebar.vue'
import SearchModal from './components/SearchModal.vue'
import HistoryModal from './components/HistoryModal.vue'
import TrashModal from './components/TrashModal.vue'
import SettingsModal from './components/SettingsModal.vue'
import MoveFileModal from './components/MoveFileModal.vue'

const route = useRoute()

// Only override the desktop-default 400px width with the live, resizable value when in
// push-layout mode — on mobile the overlay sidebar keeps its own 85vw/max-400px CSS rule,
// which an unconditional inline style would otherwise always win over.
const sidebarWidthStyle = computed(() => store.isPushLayout ? { width: store.sidebarWidth + 'px' } : null)
const mainLayoutStyle = computed(() => {
  if (!store.sidebarOpen || !store.isPushLayout) return null
  return {
    marginLeft: store.sidebarWidth + 'px',
    transition: store.sidebarDragging ? 'none' : undefined,
  }
})

function onToastAction() {
  const action = store.toastAction
  dismissToast()
  action?.handler()
}

function folderFromRoute() {
  const p = route.params.pathMatch
  if (!p) return ''
  const joined = Array.isArray(p) ? p.join('/') : p
  return joined.replace(/^\//, '').replace(/\/$/, '')
}

async function navigateToFolder(folderPath) {
  setCurrentFolder(folderPath)
  await pickActiveFile(folderPath)
}

async function pickActiveFile(folderPath) {
  const files = getFilesInFolder(folderPath)
  let fileId = store.pendingFileId
  if (fileId) {
    if (!files.find(f => f.fileId === fileId)) {
      setActiveFile(null) // tree not updated yet; onTreeUpdate will retry
      return
    }
    store.pendingFileId = null
  } else {
    fileId = files.length ? [...files].sort((a, b) => b.lastOpened - a.lastOpened)[0].fileId : null
  }
  if (!fileId) {
    setActiveFile(null)
    return
  }
  try {
    const data = await getFile(fileId)
    store.fileContents[fileId] = data.content
    setFileVersion(fileId, data.versionId)
    setActiveFile(fileId)
  } catch (e) {
    showToast(e.message, 'error')
    setActiveFile(null)
  }
}

onMounted(async () => {
  const tree = await getFiles()
  store.fileTree = tree
  await navigateToFolder(folderFromRoute())
})

watch(() => store.settings?.title, (title) => {
  if (title != null) document.title = title
})

// global Ctrl+F / Cmd+F: open the Search Modal instead of native browser find,
// unless the user has set ctrlFSearch to false (defaults to true before settings load)
function onKeydown(e) {
  if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== 'f') return
  if (store.settings?.ctrlFSearch ?? true) {
    e.preventDefault()
    openSearchModal()
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))

// tree updates from WS: re-pick active file in case folder contents changed
onTreeUpdate(async () => {
  const files = getFilesInFolder(store.currentFolderPath)
  if (store.pendingFileId && files.find(f => f.fileId === store.pendingFileId)) {
    await pickActiveFile(store.currentFolderPath)
  } else if (store.activeFileId && !files.find(f => f.fileId === store.activeFileId)) {
    await pickActiveFile(store.currentFolderPath)
  }
})

watch(() => route.params.pathMatch, async () => {
  await navigateToFolder(folderFromRoute())
})
</script>

<style scoped>
.sidebar {
  position: fixed;
  left: 0;
  top: 0;
  z-index: 100;
  width: 400px;
  height: 100%;
  background: #181818;
  overflow: hidden;
}
.sidebar-enter-active,
.sidebar-leave-active {
  transition: transform 150ms ease;
}
.sidebar-enter-from,
.sidebar-leave-to {
  transform: translateX(-100%);
}
.sidebar-overlay {
  display: none;
  position: fixed;
  inset: 0;
  z-index: 50;
  background: rgba(0, 0, 0, 0.4);
}
.overlay-enter-active,
.overlay-leave-active {
  transition: opacity 150ms ease;
}
.overlay-enter-from,
.overlay-leave-to {
  opacity: 0;
}
@media (max-width: 767px) {
  .sidebar-overlay {
    display: block;
  }
}
.main-layout {
  display: flex;
  flex-direction: column;
  min-width: 0;
  height: 100%;
  overflow: hidden;
}
@media (min-width: 768px) {
  .main-layout {
    transition: margin-left 150ms ease;
  }
  .main-layout.sidebar-open {
    margin-left: 400px;
  }
}
@media (max-width: 767px) {
  .sidebar {
    width: 85vw;
    max-width: 400px;
  }
}
.toast {
  position: fixed;
  bottom: 40px;
  left: 50%;
  transform: translateX(-50%);
  background: #d4d4d4;
  color: #1a1a1a;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 8px 18px;
  border-radius: 4px;
  font-size: 13px;
  z-index: 300;
  white-space: nowrap;
}
.toast.error {
  background: #7a1a1a;
  color: #fff;
  pointer-events: none;
}
.toast-action {
  flex-shrink: 0;
  background: transparent;
  border: none;
  color: #0a5fb4;
  font-weight: 600;
  font-size: 13px;
  cursor: pointer;
  padding: 0;
}
.toast-action:hover { color: #000; text-decoration: underline; }
.toast-enter-active,
.toast-leave-active {
  transition: opacity 200ms ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
}
</style>

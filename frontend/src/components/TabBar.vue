<template>
  <div class="tabbar">
    <button v-show="!(store.isPushLayout && store.sidebarOpen)" class="bar-btn" title="Toggle sidebar" @click="toggleSidebar">
      <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
        <rect x="1" y="3" width="14" height="1.5" rx="0.5"/>
        <rect x="1" y="7" width="14" height="1.5" rx="0.5"/>
        <rect x="1" y="11" width="14" height="1.5" rx="0.5"/>
      </svg>
    </button>
    <div
      class="tabs-scroll"
      ref="scrollEl"
      @dragover.prevent="onContainerDragOver"
      @drop.prevent="onDrop"
      @dragleave="onContainerDragLeave"
    >
      <div
        v-for="tab in store.tabs"
        :key="tab.fileId"
        class="tab"
        :class="{ active: tab.fileId === store.activeFileId, dragging: isDragging && tab.fileId === dragFileId, 'menu-open': menuTab && tab.fileId === menuTab.fileId }"
        :title="tab.absPath"
        :draggable="!isTouchDevice"
        @click="activateTab(tab.fileId)"
        @contextmenu.prevent="openMenu($event, tab)"
        @dragstart="onDragStart($event, tab)"
        @dragend="onDragEnd"
      >
        <span class="tab-name">{{ tab.name }}</span>
        <button class="tab-close" title="Close" @click.stop="closeTabById(tab.fileId)">✕</button>
      </div>
      <div v-if="isDragging && dropIndex !== null" class="drop-indicator" :style="indicatorStyle" />
    </div>
    <button class="bar-btn new-btn" title="New file" @click="newFile">+</button>
    <div class="bar-spacer" />
    <button class="bar-btn" title="Search files (Ctrl+Shift+F)" @click="openSearchModal">
      <svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="6.5" cy="6.5" r="4"/>
        <line x1="10" y1="10" x2="14" y2="14"/>
      </svg>
    </button>

    <ContextMenu
      v-if="menuTab"
      :x="menuX"
      :y="menuY"
      :items="menuItems"
      @close="menuTab = null"
    />
  </div>
</template>

<script setup>
import { computed, ref, watch, nextTick, onMounted } from 'vue'
import { store, activateTab, activateNewTab, closeTabById, reorderTab, toggleSidebar, openSearchModal, openMoveModal, deleteWithUndo, showToast } from '../store.js'
import { createFile, rename, duplicate, downloadUrl } from '../api.js'
import ContextMenu from './ContextMenu.vue'

const scrollEl = ref(null)
const isTouchDevice = ref(false)
onMounted(() => { isTouchDevice.value = window.matchMedia('(pointer: coarse)').matches })
const menuTab = ref(null)
const menuX = ref(0)
const menuY = ref(0)

watch([() => store.activeFileId, () => store.tabs], async () => {
  await nextTick()
  const el = scrollEl.value?.querySelector('.tab.active')
  el?.scrollIntoView({ inline: 'nearest', behavior: 'instant' })
})

async function newFile() {
  try {
    await activateNewTab(await createFile(''))
  } catch (e) {
    showToast(e.message, 'error')
  }
}

function openMenu(e, tab) {
  menuTab.value = tab
  menuX.value = e.clientX
  menuY.value = e.clientY
}

const menuItems = computed(() => {
  const tab = menuTab.value
  return [
    { label: 'Rename', action: () => doRename(tab) },
    { label: 'Move', action: () => openMoveModal({ type: 'file', path: tab.path, name: tab.name }) },
    { label: 'Delete', action: () => deleteWithUndo(tab.path, tab.name) },
    { label: 'Duplicate', action: () => doDuplicate(tab) },
    { label: 'Download', action: () => doDownload(tab) },
  ]
})

async function doRename(tab) {
  const newName = window.prompt('Rename file:', tab.name)
  if (!newName || newName === tab.name) return
  try {
    await rename(tab.path, newName)
  } catch (e) {
    showToast(e.message, 'error')
  }
}

async function doDuplicate(tab) {
  try {
    await activateNewTab(await duplicate(tab.path))
  } catch (e) {
    showToast(e.message, 'error')
  }
}

function doDownload(tab) {
  const a = document.createElement('a')
  a.href = downloadUrl(tab.path)
  a.download = tab.name
  a.click()
}

// --- Drag-to-reorder (desktop only via HTML5 drag-and-drop) ---

const dragFileId = ref(null)
const dropIndex = ref(null)
const isDragging = computed(() => dragFileId.value !== null)

const indicatorStyle = computed(() => {
  if (!scrollEl.value || dropIndex.value === null) return {}
  const tabs = scrollEl.value.querySelectorAll('.tab')
  let x
  if (tabs.length === 0) {
    x = 0
  } else if (dropIndex.value >= tabs.length) {
    const last = tabs[tabs.length - 1]
    x = last.offsetLeft + last.offsetWidth
  } else {
    x = tabs[dropIndex.value].offsetLeft
  }
  return { left: `${x}px` }
})

let scrollRafId = null
let scrollVelocity = 0
const SCROLL_ZONE = 60
const MAX_SCROLL_SPEED = 12

function onDragStart(e, file) {
  dragFileId.value = file.fileId
  e.dataTransfer.effectAllowed = 'move'
}

function onDragEnd() {
  dragFileId.value = null
  dropIndex.value = null
  stopAutoScroll()
}

function onContainerDragOver(e) {
  if (!dragFileId.value) return
  const tabs = Array.from(scrollEl.value.querySelectorAll('.tab'))
  if (tabs.length === 0) {
    dropIndex.value = 0
    autoScroll(e.clientX)
    return
  }
  let found = tabs.length
  for (let i = 0; i < tabs.length; i++) {
    const rect = tabs[i].getBoundingClientRect()
    if (e.clientX < rect.left + rect.width / 2) {
      found = i
      break
    }
  }
  dropIndex.value = found
  autoScroll(e.clientX)
}

function onContainerDragLeave(e) {
  if (!dragFileId.value) return
  if (!scrollEl.value?.contains(e.relatedTarget)) {
    dropIndex.value = null
    stopAutoScroll()
  }
}

async function onDrop() {
  if (dragFileId.value === null || dropIndex.value === null) return
  const fid = dragFileId.value
  const rawDrop = dropIndex.value
  dragFileId.value = null
  dropIndex.value = null
  stopAutoScroll()

  // Convert UI drop position to final position in the resulting array.
  // dropIndex is 0..N in the current array (with the dragged file present).
  // After removing the dragged file, the target index shifts if we drop after its current position.
  const currentIndex = store.tabs.findIndex(f => f.fileId === fid)
  const targetIndex = rawDrop > currentIndex ? rawDrop - 1 : rawDrop
  await reorderTab(fid, targetIndex)
}

function autoScroll(clientX) {
  if (!scrollEl.value) return
  const rect = scrollEl.value.getBoundingClientRect()
  if (clientX < rect.left + SCROLL_ZONE) {
    scrollVelocity = -MAX_SCROLL_SPEED * (1 - (clientX - rect.left) / SCROLL_ZONE)
  } else if (clientX > rect.right - SCROLL_ZONE) {
    scrollVelocity = MAX_SCROLL_SPEED * (1 - (rect.right - clientX) / SCROLL_ZONE)
  } else {
    scrollVelocity = 0
  }
  if (scrollVelocity !== 0 && scrollRafId === null) {
    scrollRafId = requestAnimationFrame(doScroll)
  }
}

function doScroll() {
  scrollRafId = null
  if (scrollVelocity !== 0 && dragFileId.value !== null) {
    scrollEl.value.scrollLeft += scrollVelocity
    scrollRafId = requestAnimationFrame(doScroll)
  }
}

function stopAutoScroll() {
  scrollVelocity = 0
  if (scrollRafId !== null) {
    cancelAnimationFrame(scrollRafId)
    scrollRafId = null
  }
}
</script>

<style scoped>
.tabbar {
  display: flex;
  align-items: stretch;
  min-height: 35px;
  background: #181818;
  border-bottom: 1px solid #252526;
  flex-shrink: 0;
}
.tabs-scroll {
  flex: 0 1 auto;
  position: relative;
  display: flex;
  align-items: stretch;
  overflow-x: auto;
  scrollbar-width: thin;
  scrollbar-color: #555 transparent;
  min-width: 0;
  margin: 0 0 -1px 0;
}
.tabs-scroll::-webkit-scrollbar { height: 3px; }
.tabs-scroll::-webkit-scrollbar-thumb { background: #555; }
.tab {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 6px 0 10px;
  cursor: pointer;
  color: #aaa;
  background: #181818;
  border-right: 1px solid #252526;
  border-top: 2px solid transparent;
  border-bottom: 1px solid #252526;
  white-space: nowrap;
  user-select: none;
}
.tab:hover, .tab.menu-open { background: #1e1e1e; color: #ddd; }
.tab-name { line-height: 35px; }
.tab-close {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  margin-right: -4px;
  padding: 0;
  border: none;
  border-radius: 3px;
  background: transparent;
  color: #999;
  font-size: 14px;
  line-height: 1;
  cursor: pointer;
}
.tab-close:hover { background: #3a3a3a; color: #fff; }
.tab.active {
  background: #1f1f1f;
  color: #fff;
  border-top-color: #0078D4;
  border-bottom-color: #1f1f1f;
}
.tab.dragging { opacity: 0.4; }
.drop-indicator {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 2px;
  background: #aaa;
  pointer-events: none;
}
.bar-btn {
  flex-shrink: 0;
  background: #181818;
  border: none;
  color: #aaa;
  cursor: pointer;
  padding: 0 10px;
  display: flex;
  align-items: center;
}
.bar-btn:hover { color: #fff; background: #2a2d2e; }
.new-btn { font-size: 20px; }
.bar-spacer { flex: 1; }
</style>

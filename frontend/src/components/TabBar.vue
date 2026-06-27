<template>
  <div class="tabbar">
    <div
      class="tabs-scroll"
      ref="scrollEl"
      @dragover.prevent="onContainerDragOver"
      @drop.prevent="onDrop"
      @dragleave="onContainerDragLeave"
    >
      <template v-if="tabFiles.length">
        <div
          v-for="file in tabFiles"
          :key="file.fileId"
          class="tab"
          :class="{ active: file.fileId === store.activeFileId, dragging: isDragging && file.fileId === dragFileId, 'menu-open': menuFile && file.fileId === menuFile.fileId }"
          :draggable="!isTouchDevice"
          @click="switchTab(file.fileId)"
          @contextmenu.prevent="openMenu($event, file)"
          @dragstart="onDragStart($event, file)"
          @dragend="onDragEnd"
        >
          <span class="tab-name">{{ file.name }}</span>
          <button v-if="showCloseIcon(file)" class="tab-close" title="Close" @click.stop="doClose(file)">✕</button>
        </div>
        <div v-if="isDragging && dropIndex !== null" class="drop-indicator" :style="indicatorStyle" />
      </template>
      <div v-else class="blank-hint">no text files in this folder</div>
    </div>

    <ContextMenu
      v-if="menuFile"
      :x="menuX"
      :y="menuY"
      :items="menuItems"
      @close="menuFile = null"
    />
  </div>
</template>

<script setup>
import { computed, ref, watch, nextTick, onMounted } from 'vue'
import { store, setActiveFile, setFileVersion, getFilesInFolder, openHistoryModal, openMoveModal, showToast } from '../store.js'
import { getFile, renameFile, deleteFile, duplicateFile, reorderFile } from '../api.js'
import { restoreAndOpen } from '../restore.js'
import ContextMenu from './ContextMenu.vue'

const scrollEl = ref(null)
const isTouchDevice = ref(false)
onMounted(() => { isTouchDevice.value = window.matchMedia('(pointer: coarse)').matches })
const menuFile = ref(null)
const menuX = ref(0)
const menuY = ref(0)

const tabFiles = computed(() => getFilesInFolder(store.currentFolderPath))

watch([() => store.activeFileId, tabFiles], async () => {
  await nextTick()
  const el = scrollEl.value?.querySelector('.tab.active')
  el?.scrollIntoView({ inline: 'nearest', behavior: 'instant' })
})

async function switchTab(fileId) {
  const data = await getFile(fileId)
  store.fileContents[fileId] = data.content
  setFileVersion(fileId, data.versionId)
  setActiveFile(fileId)
}

function openMenu(e, file) {
  menuFile.value = file
  menuX.value = e.clientX
  menuY.value = e.clientY
}

const menuItems = computed(() => {
  const file = menuFile.value
  return [
    { label: 'Rename', action: () => doRename(file) },
    { label: 'Move', action: () => doMove(file) },
    { label: 'Delete', action: () => doDelete(file) },
    { label: 'Duplicate', action: () => doDuplicate(file) },
    { label: 'Download', action: () => doDownload(file) },
    { label: 'History', action: () => doHistory(file) },
  ]
})

function doHistory(file) {
  menuFile.value = null
  openHistoryModal(file)
}

function doMove(file) {
  menuFile.value = null
  openMoveModal({ type: 'file', fileId: file.fileId, path: file.path, name: file.name })
}

async function doRename(file) {
  menuFile.value = null
  const newName = window.prompt('Rename file:', file.name)
  if (!newName || newName === file.name) return
  await renameFile(file.fileId, newName)
}

function getNextTabAfterDelete(file) {
  if (file.fileId !== store.activeFileId) return null
  const files = tabFiles.value
  const idx = files.findIndex(f => f.fileId === file.fileId)
  if (idx === -1 || files.length <= 1) return null
  return files[idx < files.length - 1 ? idx + 1 : idx - 1]
}

async function doDelete(file) {
  menuFile.value = null
  const next = getNextTabAfterDelete(file)
  await deleteFile(file.fileId)
  showToast(`Deleted "${file.name}"`, null, { label: 'UNDO', handler: () => restoreAndOpen(file.fileId) })
  if (next) await switchTab(next.fileId)
}

function showCloseIcon(file) {
  const mode = store.settings?.tabCloseIcon
  if (mode === 'visible') return true
  if (mode === 'new') return file.name.startsWith('new ')
  return false
}

async function doClose(file) {
  const next = getNextTabAfterDelete(file)
  await deleteFile(file.fileId)
  showToast(`Deleted "${file.name}"`, null, { label: 'UNDO', handler: () => restoreAndOpen(file.fileId) })
  if (next) await switchTab(next.fileId)
}

async function doDuplicate(file) {
  menuFile.value = null
  await duplicateFile(file.fileId)
}

function doDownload(file) {
  menuFile.value = null
  const content = store.fileContents[file.fileId] ?? ''
  const blob = new Blob([content], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = file.name
  a.click()
  URL.revokeObjectURL(url)
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
  const currentIndex = tabFiles.value.findIndex(f => f.fileId === fid)
  const targetIndex = rawDrop > currentIndex ? rawDrop - 1 : rawDrop
  await reorderFile(fid, targetIndex)
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
  flex: 1;
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
.blank-hint {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #555;
  font-style: italic;
}
</style>

<template>
  <div class="sidebar" :class="{ resizing: store.sidebarDragging }">
    <div class="sidebar-header">
      <button class="collapse-btn" @click="onCollapseClick" title="Collapse sidebar">
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="9,4 5,8 9,12"/>
          <polyline points="13,4 9,8 13,12"/>
        </svg>
      </button>
      <span class="sidebar-title">notepadtt</span>
    </div>
    <div
      class="sidebar-scroll"
      ref="treeScrollEl"
      @dragover.prevent="onTreeDragOver"
      @drop.prevent="onTreeDrop"
      @dragleave="onTreeDragLeave"
      @contextmenu="onScrollContextMenu"
    >
      <FileTree
        v-if="store.fileTree"
        :node="store.fileTree"
        :expanded="expanded"
        :menuFilePath="menuFile?.path"
        :menuFolderPath="menuFolder?.path"
        label="root"
        @toggle="toggleFolder"
        @open-file="openFile"
        @folder-menu="openFolderMenu"
        @file-menu="openFileMenu"
      />
    </div>
    <ContextMenu
      v-if="menuFolder"
      :x="menuX"
      :y="menuY"
      :items="folderMenuItems"
      @close="menuFolder = null"
    />
    <ContextMenu
      v-if="menuFile"
      :x="menuX"
      :y="menuY"
      :items="fileMenuItems"
      @close="menuFile = null"
    />

    <div
      v-if="store.isPushLayout && !isTouchDevice"
      class="resize-handle"
      :class="{ dragging: store.sidebarDragging }"
      :style="{ left: (store.sidebarWidth - 3) + 'px' }"
      @pointerdown="onResizeStart"
      @pointermove="onResizeMove"
      @pointerup="onResizeEnd"
    />
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { store, closeSidebar, saveSidebarWidth, openPath, activateNewTab, openMoveModal, deleteWithUndo, showToast, getFolderNode, folderOfPath } from '../store.js'
import { createFile, createFolder, rename, duplicate, move, downloadUrl } from '../api.js'
import FileTree from './FileTree.vue'
import ContextMenu from './ContextMenu.vue'

const expanded = ref({})
const menuFolder = ref(null)
const menuFile = ref(null)
const menuX = ref(0)
const menuY = ref(0)

const isTouchDevice = ref(false)
onMounted(() => { isTouchDevice.value = window.matchMedia('(pointer: coarse)').matches })

let dragStartX = 0
let preDragWidth = 0

function onResizeStart(e) {
  e.target.setPointerCapture(e.pointerId)
  dragStartX = e.clientX
  preDragWidth = store.sidebarWidth
  store.sidebarDragging = true
}

function onResizeMove(e) {
  if (!store.sidebarDragging) return
  const next = Math.round(preDragWidth + (e.clientX - dragStartX))
  store.sidebarWidth = Math.min(600, Math.max(100, next))
}

function onResizeEnd(e) {
  if (!store.sidebarDragging) return
  store.sidebarDragging = false
  e.target.releasePointerCapture(e.pointerId)
  saveSidebarWidth()
}

function onCollapseClick() {
  closeSidebar()
}

onMounted(() => {
  expanded.value[''] = true
  expandToPath(folderOfPath(activePath.value))
})

// Reveal the active tab's file in the tree.
const activePath = computed(() => store.tabs.find(t => t.fileId === store.activeFileId)?.path ?? '')
watch(activePath, (p) => expandToPath(folderOfPath(p)))

function expandToPath(path) {
  if (!path) return
  const parts = path.split('/')
  let cur = ''
  for (const part of parts) {
    cur = cur ? cur + '/' + part : part
    expanded.value[cur] = true
  }
}

function toggleFolder(path) {
  expanded.value[path] = !expanded.value[path]
}

// --- Drag-to-move within the FileTree (desktop only via HTML5 drag-and-drop) ---
// store.dragItem/store.dragOverPath are global (see store.js) since FileTree.vue's
// drag source rows live at arbitrary recursion depth and read/write them directly,
// rather than threading drag state through props/emits at every nesting level.

const treeScrollEl = ref(null)

const TREE_SCROLL_ZONE = 60
const TREE_MAX_SCROLL_SPEED = 12
const FOLDER_EXPAND_STILL_DELAY = 500    // ms of near-stationary hover before a collapsed folder auto-expands
const FOLDER_EXPAND_MOVING_DELAY = 1000  // ms of hover (with movement) before a collapsed folder auto-expands
const HOVER_STILL_EPSILON = 4            // px of movement still counted as "still"

let hoverPath = null
let hoverMoved = false
let hoverLast = { x: 0, y: 0 }
let hoverTimer = null
let treeScrollRafId = null
let treeScrollVelocity = 0
let lastValidPath = null
let dropHandled = false

watch(() => store.dragItem, async (val, oldVal) => {
  if (!val) {
    if (!dropHandled && oldVal && lastValidPath !== null) {
      const path = lastValidPath
      const item = oldVal
      const valid = !(item.type === 'folder' && (path === item.path || path.startsWith(item.path + '/')))
      if (valid && path !== folderOfPath(item.path)) {
        await handleDrop(item, path)
      }
    }
    dropHandled = false
    lastValidPath = null
    resetTreeDragVisual()
  }
})

function resetTreeDragVisual() {
  store.dragOverPath = null
  clearHoverTimer()
  stopTreeAutoScroll()
}

function clearHoverTimer() {
  if (hoverTimer) {
    clearTimeout(hoverTimer)
    hoverTimer = null
  }
  hoverPath = null
}

// Resolves which folder a dragged item is currently hovering over: a folder row's own
// path, a file row's parent path, or root ('') for anything else within the scroll area
// (the root row itself, or blank space below the last rendered row).
function resolveDropTargetPath(e) {
  const folderRow = e.target.closest?.('.folder-row')
  if (folderRow) return folderRow.dataset.path
  const fileRow = e.target.closest?.('.file-row')
  if (fileRow) return fileRow.dataset.parentPath
  return ''
}

// A folder can't be dropped onto itself or any of its own descendants (would create a
// cycle). Files have no invalid target.
function isValidDropTarget(path) {
  const item = store.dragItem
  if (!item) return false
  if (item.type === 'folder' && (path === item.path || path.startsWith(item.path + '/'))) return false
  return true
}

function onTreeDragOver(e) {
  if (!store.dragItem) return
  autoScrollTree(e.clientY)

  const path = resolveDropTargetPath(e)
  const valid = isValidDropTarget(path)
  store.dragOverPath = valid ? path : null
  if (valid) lastValidPath = path

  if (!valid) {
    clearHoverTimer()
    return
  }

  if (path !== hoverPath) {
    clearHoverTimer()
    hoverPath = path
    hoverMoved = false
    hoverLast = { x: e.clientX, y: e.clientY }
    if (!expanded.value[path]) {
      hoverTimer = setTimeout(() => onHoverStillElapsed(path), FOLDER_EXPAND_STILL_DELAY)
    }
    return
  }
  if (Math.abs(e.clientX - hoverLast.x) > HOVER_STILL_EPSILON || Math.abs(e.clientY - hoverLast.y) > HOVER_STILL_EPSILON) {
    hoverMoved = true
  }
  hoverLast = { x: e.clientX, y: e.clientY }
}

function onHoverStillElapsed(path) {
  if (hoverPath !== path || expanded.value[path]) return
  if (hoverMoved) {
    hoverTimer = setTimeout(() => onHoverMovingElapsed(path), FOLDER_EXPAND_MOVING_DELAY - FOLDER_EXPAND_STILL_DELAY)
    return
  }
  toggleFolder(path)
}

function onHoverMovingElapsed(path) {
  if (hoverPath === path && !expanded.value[path]) toggleFolder(path)
}

function autoScrollTree(clientY) {
  const el = treeScrollEl.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  if (clientY < rect.top + TREE_SCROLL_ZONE) {
    treeScrollVelocity = -TREE_MAX_SCROLL_SPEED * (1 - (clientY - rect.top) / TREE_SCROLL_ZONE)
  } else if (clientY > rect.bottom - TREE_SCROLL_ZONE) {
    treeScrollVelocity = TREE_MAX_SCROLL_SPEED * (1 - (rect.bottom - clientY) / TREE_SCROLL_ZONE)
  } else {
    treeScrollVelocity = 0
  }
  if (treeScrollVelocity !== 0 && treeScrollRafId === null) {
    treeScrollRafId = requestAnimationFrame(doTreeScroll)
  }
}

function doTreeScroll() {
  treeScrollRafId = null
  if (treeScrollVelocity !== 0 && store.dragItem) {
    treeScrollEl.value.scrollTop += treeScrollVelocity
    treeScrollRafId = requestAnimationFrame(doTreeScroll)
  }
}

function stopTreeAutoScroll() {
  treeScrollVelocity = 0
  if (treeScrollRafId !== null) {
    cancelAnimationFrame(treeScrollRafId)
    treeScrollRafId = null
  }
}

function onTreeDragLeave(e) {
  if (!store.dragItem) return
  if (e.relatedTarget && !treeScrollEl.value?.contains(e.relatedTarget)) {
    lastValidPath = null
    resetTreeDragVisual()
  }
}

async function onTreeDrop() {
  dropHandled = true
  const item = store.dragItem
  const targetPath = store.dragOverPath
  store.dragItem = null
  if (!item || targetPath === null) return
  if (targetPath === folderOfPath(item.path)) return // dropped onto its own current parent: no-op

  await handleDrop(item, targetPath)
}

function hasNameConflict(targetPath, name) {
  const node = getFolderNode(targetPath)
  if (!node) return false
  return node.folders.some(f => f.name === name) || node.files.some(f => f.name === name)
}

async function handleDrop(item, targetPath) {
  if (hasNameConflict(targetPath, item.name)) {
    showToast(`"${item.name}" already exists in ${targetPath || 'root'}`, 'error')
    return
  }
  try {
    await move(item.path, targetPath ? targetPath + '/' + item.name : item.name)
  } catch (err) {
    showToast(err.message, 'error')
  }
}

function openFile(file) {
  openPath(file.path)
  if (window.innerWidth < 768) closeSidebar()
}

// Right-click on the blank scroll area (not a tree row) acts as if the root row was
// right-clicked, since row clicks already handle their own contextmenu via bubbling.
function onScrollContextMenu(e) {
  if (e.target.closest('.tree-row') || !store.fileTree) return
  e.preventDefault()
  openFolderMenu(e, store.fileTree)
}

function openFolderMenu(e, folder) {
  menuFolder.value = folder
  menuX.value = e.clientX
  menuY.value = e.clientY
}

function openFileMenu(e, file) {
  menuFile.value = file
  menuX.value = e.clientX
  menuY.value = e.clientY
}

const folderMenuItems = computed(() => {
  const folder = menuFolder.value
  const items = [
    { label: 'New File', action: () => doNewFile(folder) },
    { label: 'New Folder', action: () => doNewFolder(folder) },
  ]
  if (folder?.path !== '') {
    items.push({ label: 'Rename Folder', action: () => doRenameFolder(folder) })
    items.push({ label: 'Move Folder', action: () => openMoveModal({ type: 'folder', path: folder.path, name: folder.name }) })
    items.push({ label: 'Delete Folder', action: () => doDeleteFolder(folder) })
  }
  return items
})

const fileMenuItems = computed(() => {
  const file = menuFile.value
  return [
    { label: 'Rename', action: () => doRenameFile(file) },
    { label: 'Move', action: () => openMoveModal({ type: 'file', path: file.path, name: file.name }) },
    { label: 'Delete', action: () => deleteWithUndo(file.path, file.name) },
    { label: 'Duplicate', action: () => doDuplicateFile(file) },
    { label: 'Download', action: () => doDownloadFile(file) },
  ]
})

async function doRename(item, label) {
  const newName = window.prompt(label, item.name)
  if (!newName || newName === item.name) return
  try {
    await rename(item.path, newName)
  } catch (e) {
    showToast(e.message, 'error')
  }
}
const doRenameFile = (file) => doRename(file, 'Rename file:')
const doRenameFolder = (folder) => doRename(folder, 'Rename folder:')

async function doDuplicateFile(file) {
  try {
    await activateNewTab(await duplicate(file.path))
  } catch (e) {
    showToast(e.message, 'error')
  }
}

function doDownloadFile(file) {
  const a = document.createElement('a')
  a.href = downloadUrl(file.path)
  a.download = file.name
  a.click()
}

// New files from the tree's folder menu are created in that folder (the TabBar "+" always
// creates in the data root).
async function doNewFile(folder) {
  try {
    await activateNewTab(await createFile(folder.path))
  } catch (e) {
    showToast(e.message, 'error')
  }
}

async function doNewFolder(folder) {
  const name = window.prompt('New folder name:')
  if (!name) return
  try {
    await createFolder(folder.path, name)
    expanded.value[folder.path] = true
  } catch (e) {
    showToast(e.message, 'error')
  }
}

// Symlinks and empty folders are removed without asking; anything else confirms first
// (there's still a short UNDO window afterwards).
async function doDeleteFolder(folder) {
  const empty = !folder.files.length && !folder.folders.length
  if (!folder.isLink && !empty &&
      !confirm(`Delete folder "${folder.name}" and everything in it?\n\nYou'll have a few seconds to undo.`)) return
  await deleteWithUndo(folder.path, folder.name)
}
</script>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: #181818;
  border-right: 1px solid #2d2d2d;
}
.sidebar.resizing {
  border-right: 1px solid #0078d4;
}
.sidebar-header {
  display: flex;
  align-items: center;
  gap: 4px;
  height: 35px;
  padding: 0 4px 0 0;
  border-bottom: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.collapse-btn {
  flex-shrink: 0;
  background: transparent;
  border: none;
  color: #aaa;
  cursor: pointer;
  padding: 0 10px;
  height: 100%;
  display: flex;
  align-items: center;
}
.collapse-btn:hover { color: #fff; background: #2a2d2e; }
.sidebar-title {
  font-weight: 700;
  color: #bbb;
  letter-spacing: 0.08em;
}
.sidebar-scroll {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  scrollbar-width: thin;
  scrollbar-color: #555 transparent;
  padding: 4px 0;
}
.resize-handle {
  position: fixed;
  top: 0;
  width: 6px;
  height: 100%;
  cursor: col-resize;
  z-index: 10;
  touch-action: none;
}
.resize-handle:hover {
  background: #2d2d2d;
}
.resize-handle.dragging {
  background: transparent;
}
</style>

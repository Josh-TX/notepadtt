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
    <div class="search-row" @click="openSearchModal" role="button" tabindex="0" @keydown.enter="openSearchModal">
      <svg class="search-icon" width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="6.5" cy="6.5" r="4"/>
        <line x1="10" y1="10" x2="14" y2="14"/>
      </svg>
      <span>Search</span>
    </div>
    <div class="trash-row" @click="openTrashModal" role="button" tabindex="0" @keydown.enter="openTrashModal">
      <svg class="trash-icon" width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="2,4 14,4"/>
        <path d="M3,4 L4,14 L12,14 L13,4"/>
        <path d="M6,4 V2 H10 V4"/>
      </svg>
      <span>Trash</span>
    </div>
    <div
      class="sidebar-scroll"
      ref="treeScrollEl"
      @dragover.prevent="onTreeDragOver"
      @drop.prevent="onTreeDrop"
      @dragleave="onTreeDragLeave"
    >
      <FileTree
        v-if="store.fileTree"
        :node="store.fileTree"
        :expanded="expanded"
        :menuFileId="menuFile?.fileId"
        :menuFolderPath="menuFolder?.path"
        label="root"
        @toggle="toggleFolder"
        @open-folder="openFolder"
        @open-file="openFile"
        @folder-menu="openFolderMenu"
        @file-menu="openFileMenu"
      />
    </div>
    <div class="sidebar-footer">
      <div class="settings-row" @click="openSettingsModal" role="button" tabindex="0" @keydown.enter="openSettingsModal">
        <svg class="settings-icon" width="15" height="15" viewBox="0 0 16 16" fill="currentColor" stroke="none">
          <path fill-rule="evenodd" d="M12.33 5.5L14.76 6.19L14.76 9.81L12.33 10.5L12.95 12.95L9.81 14.76L8 13L6.19 14.76L3.05 12.95L3.67 10.5L1.24 9.81L1.24 6.19L3.67 5.5L3.05 3.05L6.19 1.24L8 3L9.81 1.24L12.95 3.05Z M8 5.8A2.2 2.2 0 1 1 8 10.2A2.2 2.2 0 1 1 8 5.8Z"/>
        </svg>
        <span>Settings</span>
      </div>
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
import { useRouter } from 'vue-router'
import { store, closeSidebar, setActiveFile, setFileVersion, onTreeUpdate, openSearchModal, openHistoryModal, openTrashModal, openSettingsModal, openMoveModal, showToast, getFolderNode, folderOfPath, willBeTracked } from '../store.js'
import { createFile, createFolder, deleteFolder, renameFolder, getFile, renameFile, deleteFile, duplicateFile, updateSidebarWidth, updateDesktopSidebarOpen, moveFile, moveFolder } from '../api.js'
import { restoreAndOpen } from '../restore.js'
import FileTree from './FileTree.vue'
import ContextMenu from './ContextMenu.vue'

const router = useRouter()
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
  const next = preDragWidth + (e.clientX - dragStartX)
  store.sidebarWidth = Math.min(600, Math.max(100, next))
}

async function onResizeEnd(e) {
  if (!store.sidebarDragging) return
  store.sidebarDragging = false
  e.target.releasePointerCapture(e.pointerId)
  const width = store.sidebarWidth
  try {
    await updateSidebarWidth(width)
  } catch (err) {
    store.sidebarWidth = preDragWidth
    showToast('Failed to save sidebar width', 'error')
  }
}

// The collapse button only ever closes the sidebar, but while in push-layout (desktop)
// mode that close is also persisted as DesktopSidebarOpen, mirroring Navbar's toggle.
async function onCollapseClick() {
  closeSidebar()
  if (!store.isPushLayout) return
  try {
    await updateDesktopSidebarOpen(false)
  } catch (err) {
    store.sidebarOpen = true
    showToast('Failed to save sidebar state', 'error')
  }
}

// expand path to current folder on mount
onMounted(() => {
  expanded.value[''] = true
  expandToPath(store.currentFolderPath)
})

watch(() => store.currentFolderPath, (p) => {
  expandToPath(p)
})

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
        if (item.type === 'file') await handleFileDrop(item, path)
        else await handleFolderDrop(item, path)
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

  if (item.type === 'file') {
    await handleFileDrop(item, targetPath)
  } else {
    await handleFolderDrop(item, targetPath)
  }
}

function hasNameConflict(targetPath, name) {
  const node = getFolderNode(targetPath)
  if (!node) return false
  return node.folders.some(f => f.name === name) || node.files.some(f => f.name === name)
}

async function handleFileDrop(item, targetPath) {
  if (hasNameConflict(targetPath, item.name)) {
    showToast(`File named "${item.name}" already exists in ${targetPath || 'root'}`, 'error')
    return
  }
  const newPath = targetPath ? targetPath + '/' + item.name : item.name
  try {
    const data = await moveFile(item.fileId, newPath)
    if (targetPath === store.currentFolderPath) {
      store.fileContents[item.fileId] = data.content
      setFileVersion(item.fileId, data.versionId)
      setActiveFile(item.fileId)
    } else {
      store.pendingFileId = item.fileId
      router.push(targetPath ? '/' + targetPath : '/')
    }
  } catch (err) {
    showToast('Failed to move file', 'error')
  }
}

async function handleFolderDrop(item, targetPath) {
  if (hasNameConflict(targetPath, item.name)) {
    showToast(`A file or folder named "${item.name}" already exists there`, 'error')
    return
  }
  const newPath = targetPath ? targetPath + '/' + item.name : item.name
  try {
    await moveFolder(item.path, newPath)
    if (store.currentFolderPath === item.path || store.currentFolderPath.startsWith(item.path + '/')) {
      const newCurrentPath = newPath + store.currentFolderPath.slice(item.path.length)
      router.push(newCurrentPath ? '/' + newCurrentPath : '/')
    }
  } catch (err) {
    showToast('Failed to move folder', 'error')
  }
}

async function openFolder(path) {
  router.push('/' + path)
  if (window.innerWidth < 768) closeSidebar()
}

async function openFile(file) {
  const folderPath = file.path.includes('/')
    ? file.path.split('/').slice(0, -1).join('/')
    : ''
  if (folderPath === store.currentFolderPath) {
    const data = await getFile(file.fileId)
    store.fileContents[file.fileId] = data.content
    setFileVersion(file.fileId, data.versionId)
    setActiveFile(file.fileId)
  } else {
    store.pendingFileId = file.fileId
    router.push(folderPath ? '/' + folderPath : '/')
  }
  if (window.innerWidth < 768) closeSidebar()
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
    items.push({ label: 'Move Folder', action: () => doMoveFolder(folder) })
    items.push({ label: 'Delete Folder', action: () => doDeleteFolder(folder) })
  }
  return items
})

const fileMenuItems = computed(() => {
  const file = menuFile.value
  return [
    { label: 'Rename', action: () => doRenameFile(file) },
    { label: 'Move', action: () => doMoveFile(file) },
    { label: 'Delete', action: () => doDeleteFile(file) },
    { label: 'Duplicate', action: () => doDuplicateFile(file) },
    { label: 'Download', action: () => doDownloadFile(file) },
    { label: 'History', action: () => doHistoryFile(file) },
  ]
})

function doHistoryFile(file) {
  menuFile.value = null
  openHistoryModal(file)
}

function doMoveFile(file) {
  menuFile.value = null
  openMoveModal({ type: 'file', fileId: file.fileId, path: file.path, name: file.name })
}

function doMoveFolder(folder) {
  menuFolder.value = null
  openMoveModal({ type: 'folder', path: folder.path, name: folder.name })
}

async function doRenameFile(file) {
  menuFile.value = null
  const newName = window.prompt('Rename file:', file.name)
  if (!newName || newName === file.name) return
  if (!willBeTracked(newName)) {
    if (!confirm(`"${newName}" doesn't have a common text file extension, so it won't be tracked (no history, search, or version recovery). Enable "All Files" under Settings if you want it tracked.`)) return
  }
  await renameFile(file.fileId, newName)
}

async function doDeleteFile(file) {
  menuFile.value = null
  await deleteFile(file.fileId)
  showToast(`Deleted "${file.name}"`, null, { label: 'UNDO', handler: () => restoreAndOpen(file.fileId) })
}

async function doDuplicateFile(file) {
  menuFile.value = null
  await duplicateFile(file.fileId)
}

function doDownloadFile(file) {
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

async function doNewFile(folder) {
  menuFolder.value = null
  const data = await createFile(folder.path)
  if (folder.path === store.currentFolderPath) {
    store.fileContents[data.fileId] = data.content
    setFileVersion(data.fileId, data.versionId)
    setActiveFile(data.fileId)
  } else {
    store.pendingFileId = data.fileId
    router.push(folder.path ? '/' + folder.path : '/')
  }
}

async function doNewFolder(folder) {
  menuFolder.value = null
  const name = window.prompt('New folder name:')
  if (!name) return
  await createFolder(folder.path, name)
}

async function doRenameFolder(folder) {
  menuFolder.value = null
  const name = window.prompt('Rename folder:', folder.name)
  if (!name || name === folder.name) return
  await renameFolder(folder.path, name)
}

async function doDeleteFolder(folder) {
  menuFolder.value = null
  if (!confirm(`Delete folder "${folder.name}" and all its contents? Text files will be moved to trash.`)) return
  const result = await deleteFolder(folder.path)
  if (result?.untrackedCount > 0 || result?.symlinkCount > 0) {
    const parts = []
    if (result.untrackedCount > 0) {
      const n = result.untrackedCount
      parts.push(`${n} non-text file${n === 1 ? '' : 's'} that cannot be recovered after deletion`)
    }
    if (result.symlinkCount > 0) {
      const n = result.symlinkCount
      parts.push(`${n} symlink${n === 1 ? '' : 's'} (only the link will be removed; the linked content is untouched)`)
    }
    if (!confirm(`This folder contains ${parts.join(' and ')}. Delete anyway?`)) return
    await deleteFolder(folder.path, true)
  }
  // if current folder was inside deleted folder, navigate up
  if (store.currentFolderPath === folder.path || store.currentFolderPath.startsWith(folder.path + '/')) {
    const parent = folder.path.includes('/') ? folder.path.split('/').slice(0, -1).join('/') : ''
    router.push(parent ? '/' + parent : '/')
  }
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
.search-row, .trash-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  cursor: pointer;
  color: #ccc;
  user-select: none;
  border-bottom: 1px solid #2d2d2d;
  flex-shrink: 0;
  font-size: 13px;
}
.search-row:hover, .trash-row:hover { background: #2a2d2e; color: #fff; }
.search-icon, .trash-icon { flex-shrink: 0; color: #aaa; }
.search-row:hover .search-icon, .trash-row:hover .trash-icon { color: #fff; }
.sidebar-scroll {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  scrollbar-width: thin;
  scrollbar-color: #555 transparent;
  padding: 4px 0;
}
.sidebar-footer {
  border-top: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.settings-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  cursor: pointer;
  color: #ccc;
  user-select: none;
  font-size: 13px;
}
.settings-row:hover { background: #2a2d2e; color: #fff; }
.settings-icon { flex-shrink: 0; color: #aaa; }
.settings-row:hover .settings-icon { color: #fff; }
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

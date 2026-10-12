<template>
  <div class="tree-self" :class="label !== undefined ? dropClassFor(node.path ?? '') : {}">
    <div
      v-if="label !== undefined"
      class="tree-row folder-row"
      :class="{ 'menu-open': menuFolderPath === (node.path ?? '') }"
      :data-path="node.path ?? ''"
      @click="$emit('toggle', node.path ?? '')"
      @contextmenu.prevent="$emit('folder-menu', $event, node)"
    >
      <span class="tree-icon">
        <svg v-if="expanded[node.path ?? '']" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="4,6 8,10 12,6"/>
        </svg>
        <svg v-else width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="6,4 10,8 6,12"/>
        </svg>
      </span>
      <span class="tree-label">{{ label }}<span v-if="node.isLink" class="link-indicator" title="Symlink">🔗</span></span>
    </div>

    <ul v-if="label === undefined || expanded[node.path ?? '']" class="tree-list">
    <li v-for="folder in node.folders" :key="folder.path" :class="dropClassFor(folder.path)">
      <div
        class="tree-row folder-row"
        :class="{ 'menu-open': menuFolderPath === folder.path, dragging: dragItem?.type === 'folder' && dragItem.path === folder.path }"
        :data-path="folder.path"
        :draggable="dragEnabled"
        @click="$emit('toggle', folder.path)"
        @contextmenu.prevent="$emit('folder-menu', $event, folder)"
        @dragstart="onFolderDragStart($event, folder)"
        @dragend="onItemDragEnd"
      >
        <span class="tree-icon">
          <svg v-if="expanded[folder.path]" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="4,6 8,10 12,6"/>
          </svg>
          <svg v-else width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="6,4 10,8 6,12"/>
          </svg>
        </span>
        <span class="tree-label">{{ folder.name }}<span v-if="folder.isLink" class="link-indicator" title="Symlink">🔗</span></span>
      </div>
      <FileTree
        v-if="expanded[folder.path]"
        :node="folder"
        :expanded="expanded"
        :menuFilePath="menuFilePath"
        :menuFolderPath="menuFolderPath"
        @toggle="(p) => $emit('toggle', p)"
        @open-file="(f) => $emit('open-file', f)"
        @folder-menu="(e, f) => $emit('folder-menu', e, f)"
        @file-menu="(e, f) => $emit('file-menu', e, f)"
      />
    </li>
    <li v-for="file in node.files" :key="file.path">
      <div
        class="tree-row file-row"
        :class="{ active: activePath === file.path, 'menu-open': menuFilePath === file.path, dragging: dragItem?.type === 'file' && dragItem.path === file.path }"
        :data-parent-path="node.path ?? ''"
        :draggable="dragEnabled && store.renamingPath !== file.path"
        @click="store.renamingPath !== file.path && $emit('open-file', file)"
        @contextmenu.prevent="$emit('file-menu', $event, file)"
        @dragstart="onFileDragStart($event, file)"
        @dragend="onItemDragEnd"
      >
        <input
          v-if="store.renamingPath === file.path"
          class="rename-input"
          :value="file.name"
          spellcheck="false"
          @vue:mounted="({ el }) => { el.focus(); el.select() }"
          @click.stop
          @keydown.enter.prevent="commitRename($event, file)"
          @keydown.esc.prevent="cancelRename"
          @blur="commitRename($event, file)"
        />
        <span v-else class="tree-label">{{ file.name }}<span v-if="file.isLink" class="link-indicator" title="Symlink">🔗</span></span>
        <span class="file-size">{{ formatSize(file.size) }}</span>
      </div>
    </li>
    </ul>
  </div>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { store, folderOfPath, showToast } from '../store.js'
import { move } from '../api.js'

defineProps({
  node: Object,
  expanded: Object,
  label: String,
  menuFilePath: String,
  menuFolderPath: String,
})
defineEmits(['toggle', 'open-file', 'folder-menu', 'file-menu'])

// Up to 3 significant digits + unit, e.g. "123 B", "1.23 KB", "12.3 MB"
function formatSize(bytes) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = bytes ?? 0
  let i = 0
  while (n >= 1000 && i < units.length - 1) { n /= 1024; i++ }
  if (i === 0) return `${n} B`
  return `${Number(n.toPrecision(3))} ${units[i]}`
}

const activePath = computed(() => store.tabs.find(t => t.fileId === store.activeFileId)?.path ?? null)
const dragItem = computed(() => store.dragItem)
const dragOverPath = computed(() => store.dragOverPath)

const isTouchDevice = ref(false)
onMounted(() => { isTouchDevice.value = window.matchMedia('(pointer: coarse)').matches })
const dragEnabled = computed(() => !isTouchDevice.value)

// The drop-target highlight for a folder hovered during a drag: full (border + tint)
// for an actionable destination, tint-only when it's the dragged item's own current
// parent (dropping there is a no-op, so the border would be misleading).
function dropClassFor(path) {
  if (dragOverPath.value !== path) return {}
  const item = dragItem.value
  if (item && path === folderOfPath(item.path)) return { 'drop-target-origin': true }
  return { 'drop-target': true }
}

function onFolderDragStart(e, folder) {
  store.dragItem = { type: 'folder', path: folder.path, name: folder.name }
  e.dataTransfer.effectAllowed = 'move'
}

function onFileDragStart(e, file) {
  store.dragItem = { type: 'file', path: file.path, name: file.name }
  e.dataTransfer.effectAllowed = 'move'
}

function cancelRename() { store.renamingPath = null }

async function commitRename(e, file) {
  if (store.renamingPath !== file.path) return
  store.renamingPath = null
  const name = e.target.value.trim()
  if (!name || name === file.name) return
  if (name.includes('/')) { showToast('Name cannot contain "/"', 'error'); return }
  const parent = folderOfPath(file.path)
  try {
    await move(file.path, parent ? parent + '/' + name : name)
  } catch (err) {
    showToast(err.message, 'error')
  }
}

function onItemDragEnd() {
  store.dragItem = null
  store.dragOverPath = null
}
</script>

<style scoped>
.tree-list {
  list-style: none;
  padding-left: 18px;
}
.tree-row {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 6px 6px 6px 2px;
  cursor: pointer;
  border-radius: 2px;
  color: #ccc;
  user-select: none;
  position: relative;
}
.tree-row:hover, .tree-row.menu-open { background: #2a2d2e; }
.tree-row.active { background: #094771; color: #fff; }
.tree-row.dragging { opacity: 0.4; }
.tree-self.drop-target, li.drop-target { box-shadow: inset 0 0 0 1px #0078d4; background: rgba(0, 120, 212, 0.08); }
.tree-self.drop-target-origin, li.drop-target-origin { background: rgba(255, 255, 255, 0.04); }
.file-row { padding-left: 8px; margin-left: 10px; }
.tree-icon { color: #aaa; width: 14px; flex-shrink: 0; display: flex; align-items: center; }
.tree-label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rename-input { flex: 1; min-width: 0; background: #3c3c3c; color: #fff; border: 1px solid #0078d4; outline: none; font: inherit; padding: 0 2px; }
.file-size { flex-shrink: 0; margin-left: 8px; font-size: 11px; color: #777; }
.tree-row.active .file-size { color: #9bb8cf; }
.link-indicator { flex-shrink: 0; font-size: 13px; opacity: 0.8; margin-left: 3px; }
</style>

<template>
  <Teleport to="body">
    <div v-if="store.moveModalOpen" class="move-overlay" @mousedown="onOverlayMouseDown" @click="onOverlayClick">
      <div class="move-modal">
        <div class="move-header">
          <div class="move-title-block">
            <div class="move-kicker">{{ isFolder ? 'MOVE FOLDER' : 'MOVE FILE' }}</div>
            <div class="move-title" :title="originalPath">{{ originalPath }}</div>
          </div>
          <button class="close-btn" @click="close">✕</button>
        </div>
        <div class="move-input-row">
          <div class="move-input-wrap">
            <span class="move-input-prefix">/</span>
            <input
              ref="inputEl"
              v-model="inputPath"
              class="move-input"
              spellcheck="false"
              @keydown.enter="tryMove"
            />
          </div>
        </div>
        <div v-if="conflictError" class="move-error">{{ conflictError }}</div>
        <div class="move-breadcrumbs">
          <span class="crumb" @click="navigateToCrumb('')">root</span>
          <template v-for="(seg, i) in dirSegments" :key="i">
            <span class="crumb-sep">/</span>
            <span class="crumb" @click="navigateToCrumb(dirSegments.slice(0, i + 1).join('/'))">{{ seg }}</span>
          </template>
          <span class="crumb-sep">/</span>
          <span class="crumb-name">{{ namePart }}</span>
        </div>
        <div class="move-folder-list">
          <div v-if="subfolders.length === 0" class="no-subfolders">no subfolders</div>
          <div
            v-for="folder in subfolders"
            :key="folder.path"
            class="folder-item"
            @click="enterFolder(folder)"
          >
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="6,4 10,8 6,12"/>
            </svg>
            <span>{{ folder.name }}</span>
          </div>
        </div>
        <div class="move-footer">
          <button class="new-folder-btn" @click="newFolder">+ New Folder</button>
          <button class="move-btn" :disabled="!canMove" @click="tryMove">Move</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { store, closeMoveModal, getFolderNode, setActiveFile, setFileVersion, showToast } from '../store.js'
import { moveFile, moveFolder } from '../api.js'

const router = useRouter()
const inputEl = ref(null)
const inputPath = ref('')

const isFolder = computed(() => store.moveModalItem?.type === 'folder')
const originalPath = computed(() => store.moveModalItem?.path ?? '')

const dirPart = computed(() => {
  const last = inputPath.value.lastIndexOf('/')
  return last === -1 ? '' : inputPath.value.slice(0, last)
})

const namePart = computed(() => {
  const last = inputPath.value.lastIndexOf('/')
  return last === -1 ? inputPath.value : inputPath.value.slice(last + 1)
})

const dirSegments = computed(() => dirPart.value ? dirPart.value.split('/') : [])

const subfolders = computed(() => getFolderNode(dirPart.value)?.folders ?? [])

const conflictExists = computed(() => {
  const node = getFolderNode(dirPart.value)
  if (!node) return false
  const name = namePart.value
  return node.files.some(f => f.name === name) || node.folders.some(f => f.name === name)
})

// Only show the error when the path has actually changed — the item itself always
// exists at its original path, so conflictExists is always true for the no-op case.
const conflictError = computed(() => {
  if (inputPath.value === originalPath.value) return null
  return conflictExists.value ? 'A file or folder already exists at this path' : null
})

const canMove = computed(() => inputPath.value !== originalPath.value && !conflictExists.value && namePart.value !== '')

watch(() => store.moveModalOpen, (open) => {
  if (!open) return
  inputPath.value = store.moveModalItem?.path ?? ''
})

function close() { closeMoveModal() }

let mouseDownOnOverlay = false
function onOverlayMouseDown(e) { mouseDownOnOverlay = e.target === e.currentTarget }
function onOverlayClick(e) { if (mouseDownOnOverlay && e.target === e.currentTarget) close() }

function navigateToCrumb(path) {
  inputPath.value = path ? path + '/' + namePart.value : namePart.value
}

function enterFolder(folder) {
  inputPath.value = folder.path + '/' + namePart.value
}

function newFolder() {
  const name = window.prompt('New folder name:')
  if (!name) return
  const newDir = dirPart.value ? dirPart.value + '/' + name : name
  inputPath.value = newDir + '/' + namePart.value
}

async function tryMove() {
  if (!canMove.value) return
  const item = store.moveModalItem
  const newPath = inputPath.value
  const destDir = dirPart.value

  if (item.type === 'file') {
    try {
      const data = await moveFile(item.fileId, newPath)
      close()
      if (destDir === store.currentFolderPath) {
        store.fileContents[item.fileId] = data.content
        setFileVersion(item.fileId, data.versionId)
        setActiveFile(item.fileId)
      } else {
        store.pendingFileId = item.fileId
        router.push(destDir ? '/' + destDir : '/')
      }
    } catch (err) {
      showToast('Failed to move file', 'error')
    }
  } else {
    try {
      await moveFolder(item.path, newPath)
      close()
      if (store.currentFolderPath === item.path || store.currentFolderPath.startsWith(item.path + '/')) {
        const newCurrentPath = newPath + store.currentFolderPath.slice(item.path.length)
        router.push(newCurrentPath ? '/' + newCurrentPath : '/')
      }
    } catch (err) {
      showToast('Failed to move folder', 'error')
    }
  }
}
</script>

<style scoped>
.move-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
}
.move-modal {
  background: #1e1e1e;
  border: 1px solid #3a3a3a;
  border-radius: 6px;
  width: 90%;
  max-width: 640px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.move-header {
  display: flex;
  align-items: stretch;
  border-bottom: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.move-title-block {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 1px;
  padding: 8px 40px 8px 14px;
}
.move-kicker {
  font-size: 10px;
  font-weight: 600;
  color: #777;
  letter-spacing: 0.08em;
}
.move-title {
  color: #d4d4d4;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.close-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  background: transparent;
  border: none;
  color: #aaa;
  cursor: pointer;
  font-size: 16px;
  padding: 0 14px;
}
.close-btn:hover { color: #fff; background: #3a3a3a; }
.move-input-row {
  display: flex;
  align-items: center;
  padding: 10px 12px;
  border-bottom: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.move-input-wrap {
  flex: 1;
  min-width: 0;
  position: relative;
  display: flex;
  align-items: center;
}
.move-input-prefix {
  position: absolute;
  left: 5px;
  color: #777;
  font-size: 13px;
  pointer-events: none;
  user-select: none;
}
.move-input {
  flex: 1;
  min-width: 0;
  background: #2d2d2d;
  border: 1px solid #3a3a3a;
  border-radius: 3px;
  color: #d4d4d4;
  font-size: 13px;
  font-family: inherit;
  padding: 5px 4px 5px 10px;
  outline: none;
  width: 100%;
}
.move-input:focus { border-color: #0078d4; }
.move-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  border-top: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.move-btn {
  background: #0078d4;
  border: none;
  border-radius: 3px;
  color: #fff;
  cursor: pointer;
  font-size: 13px;
  padding: 5px 14px;
}
.move-btn:hover:not(:disabled) { background: #1184db; }
.move-btn:disabled { background: #2d2d2d; color: #666; cursor: default; }
.move-error {
  padding: 4px 12px 6px;
  font-size: 12px;
  color: #f48771;
  flex-shrink: 0;
}
.move-breadcrumbs {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0;
  padding: 6px 12px;
  border-bottom: 1px solid #2d2d2d;
  font-size: 12px;
  color: #999;
  flex-shrink: 0;
  min-height: 30px;
}
.crumb {
  cursor: pointer;
  color: #aaa;
  padding: 1px 3px;
  border-radius: 3px;
}
.crumb:hover { color: #fff; background: #2a2d2e; }
.crumb-sep { color: #555; padding: 0 1px; }
.crumb-name { color: #666; padding: 1px 3px; }
.new-folder-btn {
  background: transparent;
  border: none;
  color: #777;
  cursor: pointer;
  font-size: 12px;
  padding: 4px 6px;
  border-radius: 3px;
  white-space: nowrap;
}
.new-folder-btn:hover { color: #fff; background: #2a2d2e; }
.move-folder-list {
  overflow-y: auto;
  max-height: 260px;
  scrollbar-width: thin;
  scrollbar-color: #555 transparent;
  padding: 4px 0;
}
.folder-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 14px;
  cursor: pointer;
  color: #ccc;
  font-size: 13px;
  user-select: none;
}
.folder-item:hover { background: #2a2d2e; color: #fff; }
.folder-item svg { flex-shrink: 0; color: #aaa; }
.folder-item:hover svg { color: #fff; }
.no-subfolders {
  color: #555;
  font-size: 12px;
  font-style: italic;
  padding: 14px 14px;
  text-align: center;
}

@media (max-width: 767px) {
  .move-modal {
    width: 94%;
  }
}
</style>

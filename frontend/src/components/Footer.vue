<template>
  <div class="footer" ref="footerEl">
    <div class="left" ref="leftEl">
      <button class="footer-btn" @click="undo" title="Undo"><span class="icon">↩</span> Undo</button>
      <button class="footer-btn" @click="redo" title="Redo"><span class="icon">↪</span> Redo</button>
      <button class="footer-btn" :class="{ active: store.wordWrap }" @click="toggleWordWrap" title="Toggle word wrap">
        <span class="wrap-check" :class="{ hidden: !store.wordWrap }">✓</span> Wrap
      </button>
      <button class="footer-btn" @click="openHistory" title="View file history"><span class="icon">🕓</span> History</button>
      <button class="footer-btn" :disabled="!activeFile" @click="openMove" title="Move file">Move</button>
    </div>
    <div v-if="activeFile" class="right" ref="rightEl" :class="{ 'hidden-for-space': !infoFits }">
      <span>length: {{ length }}&nbsp;&nbsp;lines: {{ lines }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { store, getFilesInFolder, editorActions, openHistoryModal, openMoveModal, showToast } from '../store.js'
import { updateWrap } from '../api.js'

const activeContent = computed(() => {
  if (!store.activeFileId) return null
  return store.fileContents[store.activeFileId] ?? ''
})

const activeFile = computed(() => {
  if (!store.activeFileId) return null
  return getFilesInFolder(store.currentFolderPath).find(f => f.fileId === store.activeFileId)
})

const length = computed(() => {
  if (activeContent.value === null) return 0
  return new TextEncoder().encode(activeContent.value).length
})

const lines = computed(() => {
  if (activeContent.value === null) return 0
  return activeContent.value.split('\n').length
})

function undo() { editorActions.undo() }
function redo() { editorActions.redo() }

async function toggleWordWrap() {
  const next = !store.wordWrap
  store.wordWrap = next
  try {
    await updateWrap(next)
  } catch (e) {
    store.wordWrap = !next
    showToast('Failed to save word wrap', 'error')
  }
}

function openHistory() {
  if (!activeFile.value) return
  openHistoryModal(activeFile.value)
}

function openMove() {
  if (!activeFile.value) return
  openMoveModal({ type: 'file', fileId: activeFile.value.fileId, path: activeFile.value.path, name: activeFile.value.name })
}

const footerEl = ref(null)
const leftEl = ref(null)
const rightEl = ref(null)
const infoFits = ref(true)
let resizeObserver = null

function checkFit() {
  if (!footerEl.value || !leftEl.value || !rightEl.value) return
  const available = footerEl.value.clientWidth
  const needed = leftEl.value.scrollWidth + rightEl.value.scrollWidth + 12
  infoFits.value = needed <= available
}

onMounted(() => {
  resizeObserver = new ResizeObserver(checkFit)
  resizeObserver.observe(footerEl.value)
  checkFit()
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
})

watch([length, lines, activeFile], () => {
  nextTick(checkFit)
})
</script>

<style scoped>
.footer {
  display: flex;
  flex-wrap: nowrap;
  justify-content: space-between;
  align-items: center;
  height: 22px;
  background: #007acc;
  padding: 0 8px;
  flex-shrink: 0;
  position: relative;
  overflow: hidden;
}
.left { display: flex; gap: 4px; flex-shrink: 0; white-space: nowrap; }
.footer-btn {
  background: transparent;
  border: none;
  color: #fff;
  cursor: pointer;
  padding: 0 6px;
  font-size: 14px;
  border-radius: 2px;
  opacity: 0.75;
  white-space: nowrap;
}
.footer-btn:hover:not(:disabled) { opacity: 1; background: rgba(255,255,255,0.15); }
.footer-btn.active { opacity: 1; }
.footer-btn:disabled { opacity: 0.35; cursor: default; }
.wrap-check.hidden { color: transparent; }
.right { color: #ffffffcc; font-size: 14px; flex-shrink: 0; white-space: nowrap; }
.right.hidden-for-space { position: absolute; visibility: hidden; pointer-events: none; }

@media (max-width: 767px) {
  .icon { display: none; }
}
</style>

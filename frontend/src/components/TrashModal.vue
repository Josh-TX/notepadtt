<template>
  <Teleport to="body">
    <div v-if="store.trashModalOpen" class="trash-overlay" @mousedown="onOverlayMouseDown" @click="onOverlayClick">
      <div class="trash-modal">
        <div class="trash-header">
          <div class="trash-title">Trash</div>
          <button class="close-btn" @click="close">✕</button>
        </div>
        <div class="trash-body">
          <div class="trash-sidebar">
            <input
              v-model="filterText"
              class="trash-filter"
              type="text"
              placeholder="Filter…"
            />
            <div class="trash-list">
              <div v-if="listLoaded && filteredList.length === 0" class="trash-placeholder">Nothing here</div>
              <div
                v-for="item in filteredList"
                :key="item.fileId"
                class="trash-item"
                :class="{ selected: selectedFileId === item.fileId }"
                @click="selectItem(item)"
              >
                <div class="trash-item-path">{{ item.path }}</div>
                <div class="trash-item-stats">
                  <span>{{ formatDate(item.dateDeleted) }}</span>
                  <span class="trash-size-badge">{{ formatSize(item.length) }}</span>
                </div>
              </div>
            </div>
            <div class="trash-sidebar-footer">
              <button class="text-btn danger" :disabled="list.length === 0" @click="doEmptyTrash">Empty Trash</button>
            </div>
          </div>
          <div class="trash-content-wrap">
            <div class="trash-content" ref="contentEl">
              <div v-if="!selectedFileId" class="trash-placeholder">Select a file to preview</div>
              <div v-for="(line, i) in selectedLines" :key="i" class="trash-content-row" :data-line="i + 1">
                <span class="trash-content-linenum">{{ i + 1 }}</span>
                <span class="trash-content-text" v-html="highlightLine(line)"></span>
              </div>
            </div>
            <div v-if="selectedFileId" class="trash-content-footer">
              <button class="text-btn danger" @click="doDeleteForever">Delete File</button>
              <button class="text-btn" @click="doRestore">Restore File</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, computed, watch, nextTick } from 'vue'
import { store, closeTrashModal } from '../store.js'
import { getTrashList, getTrashContent, deleteTrashItem, emptyTrash } from '../api.js'
import { restoreAndOpen } from '../restore.js'
import { findMatchRanges } from '../textMatch.js'

const list = ref([])
const listLoaded = ref(false)
const filterText = ref('')
const selectedFileId = ref(null)
const selectedContent = ref('')
const contentEl = ref(null)
const highlightTerms = ref(null)

// Opening always issues a fresh fetch — never cached across opens, no live refresh
// while open, same convention as HistoryModal.
watch(() => store.trashModalOpen, async (open) => {
  if (!open) return
  list.value = []
  listLoaded.value = false
  filterText.value = ''
  selectedFileId.value = null
  selectedContent.value = ''
  highlightTerms.value = null

  let data = []
  try {
    data = await getTrashList()
  } catch (e) {
    data = []
  }
  list.value = data
  listLoaded.value = true

  // Deep-link from a Trash search result: auto-select that file and scroll/highlight
  // its matched line once the content loads.
  const deepLinkFileId = store.trashModalFileId
  if (!deepLinkFileId) return
  const item = data.find(i => i.fileId === deepLinkFileId)
  if (!item) return
  await selectItem(item)
  highlightTerms.value = store.trashModalHighlightTerms
  const line = store.trashModalScrollLine
  await nextTick()
  if (line !== null) {
    contentEl.value?.querySelector(`[data-line="${line}"]`)?.scrollIntoView({ block: 'center' })
  }
})

const filteredList = computed(() => {
  const needle = filterText.value.trim().toLowerCase()
  if (!needle) return list.value
  return list.value.filter(item => item.path.toLowerCase().includes(needle))
})

const selectedLines = computed(() => selectedContent.value.split('\n'))

async function selectItem(item) {
  selectedFileId.value = item.fileId
  selectedContent.value = ''
  highlightTerms.value = null
  try {
    const data = await getTrashContent(item.fileId)
    selectedContent.value = data.content
  } catch (e) {
    selectedContent.value = ''
  }
}

function highlightLine(line) {
  if (!highlightTerms.value || !highlightTerms.value.length) return escapeHtml(line)
  const merged = findMatchRanges(line, highlightTerms.value)
  let html = ''
  let pos = 0
  for (const [s, e] of merged) {
    html += escapeHtml(line.slice(pos, s))
    html += `<mark>${escapeHtml(line.slice(s, e))}</mark>`
    pos = e
  }
  html += escapeHtml(line.slice(pos))
  return html
}

function escapeHtml(str) {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

async function doRestore() {
  if (!selectedFileId.value) return
  const ok = await restoreAndOpen(selectedFileId.value)
  if (ok) close()
}

async function doDeleteForever() {
  if (!selectedFileId.value) return
  await deleteTrashItem(selectedFileId.value)
  list.value = list.value.filter(item => item.fileId !== selectedFileId.value)
  selectedFileId.value = null
  selectedContent.value = ''
}

async function doEmptyTrash() {
  if (!confirm('Empty trash? This cannot be undone.')) return
  await emptyTrash()
  list.value = []
  selectedFileId.value = null
  selectedContent.value = ''
}

function close() {
  closeTrashModal()
}

let mouseDownOnOverlay = false
function onOverlayMouseDown(e) { mouseDownOnOverlay = e.target === e.currentTarget }
function onOverlayClick(e) { if (mouseDownOnOverlay && e.target === e.currentTarget) close() }

function formatDate(ms) { return new Date(ms).toLocaleString() }
function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
</script>

<style scoped>
.trash-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 40px;
}
.trash-modal {
  position: relative;
  background: #1e1e1e;
  border: 1px solid #3a3a3a;
  border-radius: 6px;
  width: 90%;
  max-width: 1400px;
  height: calc(100vh - 80px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.trash-header {
  position: relative;
  display: flex;
  align-items: center;
  border-bottom: 1px solid #2d2d2d;
  flex-shrink: 0;
  padding: 10px 40px 10px 14px;
}
.trash-title {
  color: #d4d4d4;
  font-size: 13px;
  font-weight: 600;
}
.close-btn {
  position: absolute;
  top: 0;
  bottom: 0;
  right: 0;
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

.trash-body {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
}

.trash-sidebar {
  width: 220px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid #2d2d2d;
  background: #181818;
}
.trash-filter {
  margin: 8px;
  padding: 5px 8px;
  background: #2a2a2a;
  border: 1px solid #3a3a3a;
  border-radius: 4px;
  color: #d4d4d4;
  font-size: 12px;
  outline: none;
}
.trash-filter:focus { border-color: #0078D4; }
.trash-list {
  flex: 1;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: #555 transparent;
}
.trash-item {
  padding: 8px 12px;
  cursor: pointer;
  border-bottom: 1px solid #2a2a2a;
}
.trash-item:hover { background: #2a2d2e; }
.trash-item.selected { background: #094771; }
.trash-item.selected .trash-item-path,
.trash-item.selected .trash-item-stats {
  color: #fff;
}
.trash-item-path {
  font-size: 12px;
  color: #d4d4d4;
  direction: rtl;
  text-align: left;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.trash-item-stats {
  font-size: 11px;
  color: #999;
  margin-top: 4px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.trash-size-badge {
  background: #3a3a3a;
  color: #ccc;
  border-radius: 10px;
  padding: 1px 7px;
  font-size: 10px;
  font-weight: 600;
}
.trash-item.selected .trash-size-badge {
  background: rgba(255, 255, 255, 0.2);
  color: #fff;
}
.trash-placeholder {
  color: #666;
  font-size: 12px;
  padding: 14px 12px;
  text-align: center;
}
.trash-sidebar-footer {
  flex-shrink: 0;
  border-top: 1px solid #2d2d2d;
  padding: 8px;
  display: flex;
  justify-content: center;
}

.trash-content-wrap {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.trash-content {
  flex: 1;
  min-height: 0;
  overflow: auto;
  background: #1f1f1f;
  font-family: 'Cascadia Code', 'Consolas', 'Courier New', monospace;
  font-size: 13px;
}
.trash-content-row {
  display: flex;
  white-space: pre;
}
.trash-content-linenum {
  position: sticky;
  left: 0;
  color: #858585;
  text-align: right;
  padding: 1px 8px;
  min-width: 40px;
  user-select: none;
  border-right: 1px solid #2d2d2d;
  background: #1f1f1f;
  flex-shrink: 0;
}
.trash-content-text {
  color: #d4d4d4;
  padding: 1px 8px;
}
:deep(mark) {
  background: rgba(154, 103, 0, 0.35);
  color: #ffc357;
  border-radius: 2px;
}
.trash-content-footer {
  flex-shrink: 0;
  border-top: 1px solid #2d2d2d;
  padding: 8px 14px;
  display: flex;
  justify-content: space-between;
}

.text-btn {
  background: transparent;
  border: none;
  color: #4fc1ff;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  padding: 0;
}
.text-btn:hover { color: #fff; text-decoration: underline; }
.text-btn:disabled { color: #666; cursor: default; text-decoration: none; }
.text-btn.danger { color: #b55; }
.text-btn.danger:hover { color: #d77; }

@media (max-width: 767px) {
  .trash-overlay {
    padding-top: 0;
    align-items: stretch;
  }
  .trash-modal {
    width: 100%;
    max-width: 100%;
    height: 100%;
    border-radius: 0;
    border: none;
  }
  .trash-sidebar {
    width: 45%;
  }
}
</style>

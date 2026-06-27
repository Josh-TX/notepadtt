<template>
  <Teleport to="body">
    <div v-if="store.historyModalOpen" class="history-overlay" @mousedown="onOverlayMouseDown" @click="onOverlayClick">
      <div class="history-modal">
        <div class="history-header">
          <button class="sidebar-toggle-btn" @click="toggleSidebar" title="Toggle version list">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
              <rect x="1" y="3" width="14" height="1.5" rx="0.5"/>
              <rect x="1" y="7" width="14" height="1.5" rx="0.5"/>
              <rect x="1" y="11" width="14" height="1.5" rx="0.5"/>
            </svg>
          </button>
          <div class="history-title-block">
            <div class="history-kicker">HISTORY</div>
            <div class="history-title">{{ store.historyModalFile?.path }}</div>
          </div>
          <button class="close-btn" @click="close">✕</button>
        </div>
        <div class="history-body">
          <div class="history-sidebar-wrap" :class="{ collapsed: sidebarCollapsed }">
            <div class="history-sidebar">
              <div
                v-if="hasCurrentEntry"
                class="history-entry"
                :class="{ selected: selectedKey === 'current' }"
                @click="selectCurrent"
              >
                <div class="entry-label">{{ isTrashed ? 'Trashed Content' : 'Current Snapshot' }}</div>
                <div class="entry-stats">length: {{ currentLength }}&nbsp;&nbsp;lines: {{ currentLines }}</div>
              </div>
              <div v-if="versionsLoaded && versions.length === 0" class="history-placeholder">No history yet</div>
              <div
                v-for="v in versions"
                :key="v.versionId"
                class="history-entry"
                :class="{ selected: selectedKey === v.versionId, pending: v.pending }"
                @click="selectVersion(v)"
              >
                <div class="entry-date">{{ formatDate(v.date) }}</div>
                <div class="entry-stats">length: {{ byteLength(v.content) }}&nbsp;&nbsp;lines: {{ lineCount(v.content) }}</div>
              </div>
            </div>
          </div>
          <div class="history-content" ref="contentEl">
            <div v-for="(line, i) in selectedLines" :key="i" class="content-row" :data-line="i + 1">
              <span class="content-linenum">{{ i + 1 }}</span>
              <span class="content-text" v-html="highlightLine(line)"></span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, computed, watch, nextTick } from 'vue'
import { store, closeHistoryModal, editorActions } from '../store.js'
import { getFileVersions } from '../api.js'
import { findMatchRanges } from '../textMatch.js'

const versions = ref([])
const versionsLoaded = ref(false)
const currentContent = ref('')
const hasCurrentEntry = ref(false)
const isTrashed = ref(false)
const selectedKey = ref('current')
const sidebarCollapsed = ref(false)
const contentEl = ref(null)
const highlightTerms = ref(null)

// Opening always issues a fresh fetch — version history changes over time, so there's
// no caching across opens like SearchModal has. One request covers both the past
// versions list and (for the not-active-file case) the current/trashed content, which
// the server includes as a synthetic current:true entry — omitted entirely when the
// file has neither a live row nor a FileTrash row left (see handleGetFileVersions).
watch(() => store.historyModalOpen, async (open) => {
  if (!open) return
  sidebarCollapsed.value = false
  versions.value = []
  versionsLoaded.value = false
  currentContent.value = ''
  hasCurrentEntry.value = false
  isTrashed.value = false
  highlightTerms.value = null

  const file = store.historyModalFile
  if (!file) return

  let data = []
  try {
    data = await getFileVersions(file.fileId)
  } catch (e) {
    data = []
  }
  versionsLoaded.value = true
  versions.value = data.filter(v => !v.current)

  const currentEntry = data.find(v => v.current)
  hasCurrentEntry.value = !!currentEntry
  isTrashed.value = !!currentEntry?.trashed

  // A frozen snapshot taken now — does not stay in sync with further WS updates
  // while the modal is open. Trashed content has no live tab to freeze, so it always
  // comes from the fetched entry.
  if (currentEntry && !isTrashed.value && file.fileId === store.activeFileId) {
    currentContent.value = editorActions.getValue()
  } else {
    currentContent.value = currentEntry?.content ?? ''
  }

  // Deep-link from a History search result: select the matching version (or the
  // current/trashed entry if that's what matched) and scroll/highlight once rendered.
  const deepLinkVersionId = store.historyModalVersionId
  if (deepLinkVersionId && versions.value.some(v => v.versionId === deepLinkVersionId)) {
    selectedKey.value = deepLinkVersionId
  } else if (hasCurrentEntry.value) {
    selectedKey.value = 'current'
  } else {
    selectedKey.value = versions.value[0]?.versionId ?? 'current'
  }

  if (store.historyModalScrollLine !== null || store.historyModalHighlightTerms !== null) {
    highlightTerms.value = store.historyModalHighlightTerms
    const line = store.historyModalScrollLine
    await nextTick()
    if (line !== null) {
      contentEl.value?.querySelector(`[data-line="${line}"]`)?.scrollIntoView({ block: 'center' })
    }
  }
})

function close() {
  closeHistoryModal()
}

let mouseDownOnOverlay = false
function onOverlayMouseDown(e) { mouseDownOnOverlay = e.target === e.currentTarget }
function onOverlayClick(e) { if (mouseDownOnOverlay && e.target === e.currentTarget) close() }

function toggleSidebar() {
  sidebarCollapsed.value = !sidebarCollapsed.value
}

function selectCurrent() {
  selectedKey.value = 'current'
  highlightTerms.value = null
}

function selectVersion(v) {
  selectedKey.value = v.versionId
  highlightTerms.value = null
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

const selectedContent = computed(() => {
  if (selectedKey.value === 'current') return currentContent.value
  return versions.value.find(v => v.versionId === selectedKey.value)?.content ?? ''
})

const selectedLines = computed(() => selectedContent.value.split('\n'))

const currentLength = computed(() => byteLength(currentContent.value))
const currentLines = computed(() => lineCount(currentContent.value))

function byteLength(content) { return new TextEncoder().encode(content).length }
function lineCount(content) { return content.split('\n').length }
function formatDate(ms) { return new Date(ms).toLocaleString() }
</script>

<style scoped>
.history-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 40px;
}
.history-modal {
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
.history-header {
  position: relative;
  display: flex;
  border-bottom: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.history-title-block {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 1px;
  padding: 6px 40px 6px 14px;
}
.history-kicker {
  font-size: 10px;
  font-weight: 600;
  color: #777;
  letter-spacing: 0.08em;
}
.history-title {
  color: #d4d4d4;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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

.history-body {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  overflow: hidden;
}

.sidebar-toggle-btn {
  display: none;
  position: absolute;
  top: 0;
  bottom: 0;
  left: 0;
  align-items: center;
  background: transparent;
  border: none;
  color: #aaa;
  cursor: pointer;
  padding: 0 12px;
}
.sidebar-toggle-btn:hover { color: #fff; background: #2a2d2e; }

.history-sidebar-wrap {
  width: 180px;
  flex-shrink: 0;
  overflow: hidden;
}
.history-sidebar {
  width: 180px;
  height: 100%;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: #555 transparent;
  border-right: 1px solid #2d2d2d;
  background: #181818;
}
.history-entry {
  padding: 8px 12px;
  cursor: pointer;
  border-bottom: 1px solid #2a2a2a;
}
.history-entry:hover { background: #2a2d2e; }
.history-entry.selected { background: #094771; }
.history-entry.pending { opacity: 0.8; }
.history-entry.selected .entry-label,
.history-entry.selected .entry-date {
  color: #fff;
}
.history-entry.selected .entry-stats {
  color: #cfe4f5;
}
.entry-label, .entry-date {
  font-size: 12px;
  color: #d4d4d4;
}
.entry-stats {
  font-size: 12px;
  color: #999;
  margin-top: 2px;
}
.history-placeholder {
  color: #666;
  font-size: 12px;
  padding: 14px 12px;
  text-align: center;
}

.history-content {
  flex: 1;
  min-width: 0;
  overflow: auto;
  background: #1f1f1f;
  font-family: 'Cascadia Code', 'Consolas', 'Courier New', monospace;
  font-size: 13px;
}
.content-row {
  display: flex;
  white-space: pre;
}
.content-linenum {
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
.content-text {
  color: #d4d4d4;
  padding: 1px 8px;
}
:deep(mark) {
  background: rgba(154, 103, 0, 0.35);
  color: #ffc357;
  border-radius: 2px;
}

@media (max-width: 767px) {
  .history-overlay {
    padding-top: 0;
    align-items: stretch;
  }
  .history-modal {
    width: 100%;
    max-width: 100%;
    height: 100%;
    border-radius: 0;
    border: none;
  }
  .sidebar-toggle-btn {
    display: flex;
  }
  .history-title-block {
    padding-left: 46px;
  }
  .history-sidebar-wrap {
    width: 35%;
    transition: width 150ms ease;
  }
  .history-sidebar-wrap.collapsed {
    width: 0;
  }
  .history-sidebar {
    width: 35vw;
  }
}
</style>

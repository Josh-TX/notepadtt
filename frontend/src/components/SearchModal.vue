<template>
  <Teleport to="body">
    <div v-if="store.searchModalOpen" class="search-overlay" @mousedown="onOverlayMouseDown" @click="onOverlayClick">
      <div class="search-modal">
        <div class="search-header">
          <div class="search-input-wrap">
            <input
              ref="inputRef"
              v-model="query"
              class="search-input"
              placeholder="Search files..."
              @input="onInput"
              @keydown.escape="close"
            />
            <button
              type="button"
              class="regex-toggle"
              :class="{ active: store.searchIncludeRegex }"
              title="Regex search"
              @click="store.searchIncludeRegex = !store.searchIncludeRegex; onFilterChange()"
            >.*</button>
          </div>
          <button class="close-btn" @click="close">✕</button>
        </div>
        <div class="search-filters">
          <label class="filter-checkbox">
            <input type="checkbox" v-model="store.searchIncludeHistory" @change="onFilterChange" /> History
          </label>
          <label class="filter-checkbox">
            <input type="checkbox" v-model="store.searchIncludeTrash" @change="onFilterChange" /> Trash
          </label>
        </div>
        <div v-if="regexError" class="search-error">{{ regexError }}</div>
        <div class="search-body">
          <div v-if="!query" class="search-placeholder">Type to search…</div>
          <div v-else-if="loading" class="search-loading">
            <div class="loading-bar" :class="{ searching }"></div>
          </div>
          <div v-else-if="results.length === 0" class="search-placeholder">No results</div>
          <div v-else class="search-results">
            <div v-for="result in results" :key="result.fileId" class="search-result-group">
              <div class="result-path">
                <svg v-if="result.source === 'history'" class="source-icon" width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                  <circle cx="8" cy="8" r="6.5"/>
                  <path d="M8 4.5V8l3 2"/>
                </svg>
                <svg v-else-if="result.source === 'trash'" class="source-icon" width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                  <polyline points="2,4 14,4"/>
                  <path d="M3,4 L4,14 L12,14 L13,4"/>
                  <path d="M6,4 V2 H10 V4"/>
                </svg>
                <span>{{ result.path }}</span>
              </div>
              <div
                v-for="(section, i) in result.sections"
                :key="i"
                class="result-snippet"
                @click="openResult(result, section)"
              >
                <div
                  v-for="(line, j) in snippetLines(section.snippet)"
                  :key="j"
                  class="snippet-line"
                >
                  <span class="line-num">{{ section.startLineNumber + j }}</span>
                  <span class="line-content" v-html="highlightLine(line)"></span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { store, closeSearchModal, setSearchState, setActiveFile, setFileVersion, editorActions, openHistoryModal, openTrashModal } from '../store.js'
import { searchFiles, getFile } from '../api.js'
import { findMatchRanges, findRegexMatchRanges } from '../textMatch.js'

const router = useRouter()
const inputRef = ref(null)
const query = ref('')
const results = ref([])
const loading = ref(false)
const searching = ref(false)
const regexError = ref('')
let debounceTimer = null

// Restore last query/results when modal opens; autofocus input
watch(() => store.searchModalOpen, async (open) => {
  if (!open) return
  query.value = store.searchQuery
  results.value = store.searchResults
  await nextTick()
  inputRef.value?.focus()
  inputRef.value?.select()
})

function close() {
  closeSearchModal()
}

let mouseDownOnOverlay = false
function onOverlayMouseDown(e) {
  mouseDownOnOverlay = e.target === e.currentTarget
}
function onOverlayClick(e) {
  if (mouseDownOnOverlay && e.target === e.currentTarget) close()
}

function onInput() {
  results.value = []
  loading.value = false
  regexError.value = ''
  clearTimeout(debounceTimer)
  setSearchState(query.value, [])
  if (!query.value.trim()) return
  loading.value = true
  debounceTimer = setTimeout(runSearch, 400)
}

// Toggling History/Trash/Regex re-runs the current query immediately rather than
// waiting for the input debounce — there's no text being typed to debounce against.
function onFilterChange() {
  clearTimeout(debounceTimer)
  if (!query.value.trim()) return
  loading.value = true
  runSearch()
}

async function runSearch() {
  const q = query.value
  if (!q.trim()) { loading.value = false; searching.value = false; return }
  searching.value = true
  try {
    const data = await searchFiles(q, { history: store.searchIncludeHistory, trash: store.searchIncludeTrash, regex: store.searchIncludeRegex })
    results.value = data
    regexError.value = ''
    setSearchState(q, data)
  } catch (e) {
    results.value = []
    // The only validation error the search endpoint can return is a bad regex
    // pattern (400), so in regex mode surface it inline instead of just going blank.
    regexError.value = store.searchIncludeRegex ? e.message : ''
  } finally {
    loading.value = false
    searching.value = false
  }
}

function snippetLines(snippet) {
  return snippet.split('\n')
}

// terms/pattern used for both SearchModal's own <mark> highlighting and the
// highlightTerms deep-link passed to the Editor/HistoryModal/TrashModal once a result
// is opened. In regex mode the raw pattern is kept whole rather than whitespace-split,
// since splitting would break multi-word patterns.
function searchTerms() {
  return store.searchIncludeRegex ? [query.value.trim()] : query.value.trim().split(/\s+/).filter(Boolean)
}

// Returns escaped HTML with matching terms (or regex matches, in regex mode) wrapped
// in <mark>
function highlightLine(line) {
  let merged
  if (store.searchIncludeRegex) {
    merged = findRegexMatchRanges(line, query.value.trim())
  } else {
    const terms = query.value.trim().split(/\s+/).filter(Boolean)
    if (!terms.length) return escapeHtml(line)
    merged = findMatchRanges(line, terms)
  }

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

// Shared by the "file" branch below and by History results whose file still exists
// live — navigates CurrentFolder/breadcrumbs and opens the file's tab, same mechanics
// either way.
async function navigateToFileLocation(fileId, path) {
  const folderPath = path.includes('/') ? path.split('/').slice(0, -1).join('/') : ''
  if (folderPath === store.currentFolderPath) {
    const data = await getFile(fileId)
    store.fileContents[fileId] = data.content
    setFileVersion(fileId, data.versionId)
    setActiveFile(fileId)
  } else {
    store.pendingFileId = fileId
    router.push(folderPath ? '/' + folderPath : '/')
  }
}

async function openResult(result, section) {
  close()
  const terms = searchTerms()

  if (result.source === 'trash') {
    openTrashModal({ fileId: result.fileId, scrollLine: section.startLineNumber, highlightTerms: terms })
    return
  }

  if (result.source === 'history') {
    if (result.exists) await navigateToFileLocation(result.fileId, result.path)
    openHistoryModal(
      { fileId: result.fileId, name: result.path.split('/').pop(), path: result.path },
      { versionId: result.versionId, scrollLine: section.startLineNumber, highlightTerms: terms },
    )
    return
  }

  if (store.activeFileId === result.fileId) {
    editorActions.scrollToLine(section.startLineNumber)
    editorActions.highlightTerms(terms)
    return
  }
  store.pendingScrollLine = section.startLineNumber
  store.pendingHighlightTerms = terms
  await navigateToFileLocation(result.fileId, result.path)
}
</script>

<style scoped>
.search-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 40px;
}
.search-modal {
  background: #1e1e1e;
  border: 1px solid #3a3a3a;
  border-radius: 6px;
  width: 90%;
  max-width: 960px;
  height: calc(100vh - 80px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.search-header {
  display: flex;
  align-items: center;
  padding: 8px 10px;
  border-bottom: 1px solid #2d2d2d;
  gap: 8px;
  flex-shrink: 0;
}
.search-input-wrap {
  position: relative;
  flex: 1;
}
.search-input {
  width: 100%;
  box-sizing: border-box;
  background: #2d2d2d;
  border: 1px solid #444;
  border-radius: 4px;
  color: #d4d4d4;
  font-size: 14px;
  padding: 6px 34px 6px 10px;
  outline: none;
}
.search-input:focus { border-color: #0078d4; }
.regex-toggle {
  position: absolute;
  right: 4px;
  top: 50%;
  transform: translateY(-50%);
  background: transparent;
  border: 1px solid transparent;
  border-radius: 3px;
  color: #888;
  font-size: 12px;
  font-family: monospace;
  font-weight: bold;
  width: 24px;
  height: 22px;
  cursor: pointer;
  line-height: 1;
}
.regex-toggle:hover { color: #ccc; background: #3a3a3a; }
.regex-toggle.active { color: #fff; background: #0078d4; border-color: #0078d4; }
.close-btn {
  background: transparent;
  border: none;
  color: #aaa;
  cursor: pointer;
  font-size: 16px;
  padding: 4px 6px;
  border-radius: 4px;
  line-height: 1;
}
.close-btn:hover { color: #fff; background: #3a3a3a; }
.search-filters {
  display: flex;
  gap: 16px;
  padding: 6px 12px;
  border-bottom: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.search-error {
  padding: 6px 12px;
  border-bottom: 1px solid #2d2d2d;
  color: #f48771;
  font-size: 12px;
  flex-shrink: 0;
}
.filter-checkbox {
  display: flex;
  align-items: center;
  gap: 5px;
  color: #ccc;
  font-size: 12px;
  cursor: pointer;
  user-select: none;
}
.search-body {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  scrollbar-width: thin;
  scrollbar-color: #555 transparent;
}
.search-placeholder {
  color: #666;
  font-size: 13px;
  padding: 20px;
  text-align: center;
}
.search-loading {
  padding: 8px 12px;
}
.loading-bar {
  height: 2px;
  background: #0078d4;
  opacity: 0.35;
  border-radius: 2px;
}
.loading-bar.searching {
  background: linear-gradient(90deg, transparent, #0078d4, transparent);
  background-size: 200% 100%;
  animation: loading 1.2s linear infinite;
  opacity: 1;
}
@keyframes loading {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
.search-results {
  padding: 6px 0;
}
.search-result-group {
  padding: 8px 12px;
  border-bottom: 1px solid #2a2a2a;
}
.search-result-group:last-child { border-bottom: none; }
.result-path {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: #9cdcfe;
  margin-bottom: 4px;
  word-break: break-all;
}
.source-icon { flex-shrink: 0; color: #999; }
.result-snippet {
  background: #1f1f1f;
  border: 1px solid #2d2d2d;
  border-radius: 3px;
  overflow: hidden;
  margin-bottom: 6px;
  cursor: pointer;
  font-family: 'Cascadia Code', 'Consolas', 'Courier New', monospace;
  font-size: 12px;
}
.result-snippet:last-child { margin-bottom: 0; }
.result-snippet:hover { border-color: #0078d4; }
.snippet-line {
  display: flex;
  align-items: flex-start;
  gap: 0;
}
.line-num {
  color: #858585;
  text-align: right;
  padding: 1px 8px;
  min-width: 36px;
  user-select: none;
  border-right: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.line-content {
  color: #d4d4d4;
  padding: 1px 8px;
  flex: 1;
  white-space: pre-wrap;
  word-break: break-word;
  min-width: 0;
}
:deep(mark) {
  background: rgba(154, 103, 0, 0.35);
  color: #ffc357;
  border-radius: 2px;
}

@media (max-width: 767px) {
  .search-overlay {
    padding-top: 0;
    align-items: stretch;
  }
  .search-modal {
    width: 100%;
    max-width: 100%;
    height: 100%;
    border-radius: 0;
    border: none;
  }
}
</style>

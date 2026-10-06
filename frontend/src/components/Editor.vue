<template>
  <div class="editor-area">
    <div class="editor-wrap" ref="editorEl"></div>
    <div v-if="overlay" class="editor-overlay">
      <div class="overlay-msg">{{ overlay.message }}</div>
      <button v-if="overlay.kind === 'tooLarge'" class="overlay-btn" @click="activateTab(store.activeFileId, true)">Display anyway</button>
      <a v-if="overlay.kind === 'binary'" class="overlay-btn" :href="downloadUrl(activeTab.path)" :download="activeTab.name">Download</a>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, computed, onMounted, onUnmounted } from 'vue'
import CodeMirror from 'codemirror'
import 'codemirror/lib/codemirror.css'
import { store, onContentUpdate, editorActions, sendEdit, activateTab } from '../store.js'
import { downloadUrl } from '../api.js'
import { findMatchRanges } from '../textMatch.js'
import { computeMinimalEdit } from '../cmEdit.js'
import { getModeInfo } from '../langMode.js'

const editorEl = ref(null)
let cm = null

const activeTab = computed(() => store.tabs.find(t => t.fileId === store.activeFileId) ?? null)
const activeFilename = computed(() => activeTab.value?.name ?? null)
const activeKind = computed(() => store.fileStatus[store.activeFileId]?.kind ?? null)

function formatSize(bytes) {
  return bytes >= 1024 * 1024 ? (bytes / 1024 / 1024).toFixed(1) + ' MB' : Math.round(bytes / 1024) + ' KB'
}

// Message shown instead of the editor content: no tabs, or a file we won't render.
const overlay = computed(() => {
  if (!activeTab.value) return { kind: 'empty', message: 'no open files' }
  const st = store.fileStatus[store.activeFileId]
  if (st?.kind === 'tooLarge') return { kind: 'tooLarge', message: `${activeTab.value.name} is large (${formatSize(st.size)}) and isn't shown by default.` }
  if (st?.kind === 'binary') return { kind: 'binary', message: `${activeTab.value.name} is a binary file (${formatSize(st.size)}).` }
  return null
})
let ignoreNextChange = false
let searchMarks = []
let resizeObserver = null
let refreshFrame = null

function clearSearchHighlights() {
  if (!searchMarks.length) return
  searchMarks.forEach(m => m.clear())
  searchMarks = []
}

onMounted(() => {
  cm = CodeMirror(editorEl.value, {
    value: '',
    lineNumbers: true,
    lineWrapping: store.wordWrap,
    viewportMargin: Infinity,
    theme: 'ntt',
    extraKeys: {},
  })

  applyTheme()

  editorActions.undo = () => cm.undo()
  editorActions.redo = () => cm.redo()
  editorActions.getValue = () => cm ? cm.getValue() : ''
  editorActions.scrollToLine = (line) => {
    if (!cm) return
    cm.scrollIntoView({ line: line - 1, ch: 0 }, 150)
  }
  editorActions.highlightTerms = (terms) => {
    clearSearchHighlights()
    if (!cm || !terms || !terms.length) return
    for (let i = 0; i < cm.lineCount(); i++) {
      const ranges = findMatchRanges(cm.getLine(i), terms)
      for (const [s, e] of ranges) {
        searchMarks.push(cm.markText({ line: i, ch: s }, { line: i, ch: e }, { className: 'search-term-highlight' }))
      }
    }
  }

  // clear search highlights on next interaction with the editor
  cm.on('mousedown', clearSearchHighlights)
  cm.on('focus', clearSearchHighlights)

  cm.on('change', (_, changeObj) => {
    if (ignoreNextChange) {
      ignoreNextChange = false
      return
    }
    const fileId = store.activeFileId
    if (!fileId) return
    const content = cm.getValue()
    store.fileContents[fileId] = content
    sendEdit(fileId, changeObj.from, changeObj.to, changeObj.text, changeObj.removed)
  })

  // CodeMirror only re-measures on window 'resize'; the sidebar toggle resizes
  // this container via a CSS transition without firing that event, so watch
  // the container itself.
  resizeObserver = new ResizeObserver(() => {
    if (refreshFrame) return
    refreshFrame = requestAnimationFrame(() => {
      refreshFrame = null
      cm?.refresh()
    })
  })
  resizeObserver.observe(editorEl.value)
})

onUnmounted(() => {
  resizeObserver?.disconnect()
  if (refreshFrame) cancelAnimationFrame(refreshFrame)
  cm?.toTextArea()
})

// apply dark theme via dynamic styles
function applyTheme() {
  let style = document.getElementById('cm-ntt-theme')
  if (!style) {
    style = document.createElement('style')
    style.id = 'cm-ntt-theme'
    document.head.appendChild(style)
  }
  const fontSize = 14
  style.textContent = `
    .CodeMirror.cm-s-ntt {
      background: #1f1f1f;
      color: #d4d4d4;
      height: 100%;
      font-family: 'Cascadia Code', 'Consolas', 'Courier New', monospace;
      font-size: ${fontSize}px;
    }
    .CodeMirror-gutters { background: #1f1f1f; border-right: 1px solid #2d2d2d; }
    .CodeMirror-linenumber { color: #858585; }
    .CodeMirror-cursor { border-left: 1px solid #d4d4d4; }
    .CodeMirror-selected { background: #264f78; }
    .CodeMirror-focused .CodeMirror-selected { background: #264f78; }
    .CodeMirror-scroll { background: #1f1f1f; }
    .search-term-highlight { background: rgba(154, 103, 0, 0.35); color: #ffc357; border-radius: 2px; }
    .cm-keyword    { color: #c586c0; }
    .cm-atom       { color: #569cd6; }
    .cm-number     { color: #b5cea8; }
    .cm-def        { color: #dcdcaa; }
    .cm-variable   { color: #d4d4d4; }
    .cm-variable-2 { color: #9cdcfe; }
    .cm-variable-3, .cm-type { color: #4ec9b0; }
    .cm-property   { color: #9cdcfe; }
    .cm-operator   { color: #d4d4d4; }
    .cm-string     { color: #ce9178; }
    .cm-string-2   { color: #ce9178; }
    .cm-comment    { color: #6a9955; font-style: italic; }
    .cm-builtin    { color: #4ec9b0; }
    .cm-qualifier  { color: #d7ba7d; }
    .cm-tag        { color: #569cd6; }
    .cm-attribute  { color: #9cdcfe; }
    .cm-bracket    { color: #d4d4d4; }
    .cm-meta       { color: #d4d4d4; }
    .cm-error      { color: #f44747; }
    .cm-header     { color: #569cd6; font-weight: bold; }
    .cm-quote      { color: #6a9955; }
    .cm-strong     { font-weight: bold; }
    .cm-em         { font-style: italic; }
    .cm-link       { color: #569cd6; }
    .cm-url        { color: #569cd6; }
    .cm-hr         { color: #858585; }
    .cm-formatting { color: #858585; }
    ${parseColorOverrides('keyword=#569cd6, header=#4babfd')}
  `
}

function parseColorOverrides(raw) {
  return raw.split(',')
    .map(s => s.trim())
    .filter(s => s.includes('='))
    .map(s => {
      const eq = s.indexOf('=')
      const key = s.slice(0, eq).trim()
      const val = s.slice(eq + 1).trim()
      return key && val ? `.cm-${key} { color: ${val}; }` : ''
    })
    .filter(Boolean)
    .join('\n    ')
}

// load content when the active file changes, or becomes displayable (e.g. "display anyway")
watch([() => store.activeFileId, activeKind], ([fileId, kind]) => {
  if (!cm) return
  const showable = fileId && kind === 'ok'
  const content = showable ? (store.fileContents[fileId] ?? '') : ''
  ignoreNextChange = true
  cm.setValue(content)
  cm.clearHistory()
  cm.setOption('readOnly', !showable)
  const modeInfo = getModeInfo(activeFilename.value, 0)
  cm.setOption('mode', modeInfo?.mime ?? null)
  if (store.pendingScrollLine !== null) {
    const line = store.pendingScrollLine
    store.pendingScrollLine = null
    cm.scrollIntoView({ line: line - 1, ch: 0 }, 150)
  }
  if (store.pendingHighlightTerms !== null) {
    const terms = store.pendingHighlightTerms
    store.pendingHighlightTerms = null
    editorActions.highlightTerms(terms)
  }
})

// apply word wrap toggle
watch(() => store.wordWrap, (wrap) => {
  cm?.setOption('lineWrapping', wrap)
})

// a rename can change the language mode
watch(activeFilename, (name) => {
  if (!cm) return
  cm.setOption('mode', getModeInfo(name, 0)?.mime ?? null)
})

// receive live content updates from other clients
let unsubscribe = null
watch(() => store.activeFileId, (fileId) => {
  if (unsubscribe) { unsubscribe(); unsubscribe = null }
  if (!fileId) return
  unsubscribe = onContentUpdate(fileId, (content) => {
    if (!cm || activeKind.value !== 'ok') return
    const edit = computeMinimalEdit(cm.getValue(), content)
    if (!edit) return
    try {
      ignoreNextChange = true
      cm.replaceRange(edit.text, edit.from, edit.to)
    } catch {
      const cursor = cm.getCursor()
      ignoreNextChange = true
      cm.setValue(content)
      cm.setCursor(cursor)
    }
  })
}, { immediate: true })
</script>

<style scoped>
.editor-area {
  flex: 1;
  min-height: 0;
  position: relative;
  background: #1f1f1f;
}
.editor-wrap {
  position: absolute;
  inset: 0;
  overflow: hidden;
}
.editor-overlay {
  position: absolute;
  inset: 0;
  background: #1f1f1f;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  z-index: 5;
}
.overlay-msg { color: #777; font-style: italic; text-align: center; padding: 0 20px; }
.overlay-btn {
  background: #0e639c;
  color: #fff;
  border: none;
  border-radius: 3px;
  padding: 6px 16px;
  font-size: 14px;
  cursor: pointer;
  text-decoration: none;
}
.overlay-btn:hover { background: #1177bb; }
.editor-wrap :deep(.CodeMirror) {
  height: 100%;
}
</style>

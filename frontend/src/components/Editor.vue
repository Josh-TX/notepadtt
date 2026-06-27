<template>
  <div class="editor-wrap" ref="editorEl"></div>
</template>

<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue'
import CodeMirror from 'codemirror'
import 'codemirror/lib/codemirror.css'
import { store, onContentUpdate, editorActions, sendEdit } from '../store.js'
import { findMatchRanges } from '../textMatch.js'
import { computeMinimalEdit } from '../cmEdit.js'

const editorEl = ref(null)
let cm = null
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
  const fontSize = store.settings?.editorFontSize ?? 14
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
  `
}

// load content when active file changes
watch(() => store.activeFileId, (fileId) => {
  if (!cm) return
  const content = fileId ? (store.fileContents[fileId] ?? '') : ''
  ignoreNextChange = true
  cm.setValue(content)
  cm.clearHistory()
  cm.setOption('readOnly', !fileId)
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

// apply font size changes from the Settings modal — CodeMirror needs an explicit
// refresh() to re-measure line heights/widths after the font size changes.
watch(() => store.settings?.editorFontSize, () => {
  if (!cm) return
  applyTheme()
  cm.refresh()
})

// receive live content updates from other clients
let unsubscribe = null
watch(() => store.activeFileId, (fileId) => {
  if (unsubscribe) { unsubscribe(); unsubscribe = null }
  if (!fileId) return
  unsubscribe = onContentUpdate(fileId, (content) => {
    if (!cm) return
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
.editor-wrap {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
.editor-wrap :deep(.CodeMirror) {
  height: 100%;
}
</style>

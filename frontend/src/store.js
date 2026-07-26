import { reactive, computed } from 'vue'

const state = reactive({
  cid: null,
  fileTree: null,           // FolderNode from server
  currentFolderPath: '',    // '' = root
  activeFileId: null,
  fileContents: {},         // fileId -> string
  fileVersions: {},         // fileId -> versionId
  wordWrap: true,
  sidebarOpen: false,
  sidebarWidth: 400,       // live value, kept independent of settings.sidebarWidth — see Footer's wordWrap for the same pattern
  sidebarDragging: false,  // true while the Sidebar resize handle is actively being dragged
  dragItem: null,           // { type: 'file'|'folder', fileId?, path, name } currently being dragged within the FileTree, or null
  dragOverPath: null,       // folder path (''=root) currently highlighted as the FileTree drop target, or null if none/invalid
  isPushLayout: false,     // true when the Sidebar pushes the layout (desktop) rather than overlaying it (mobile) — see initLayoutFlags
  pendingFileId: null,      // fileId to open after folder navigation (set by sidebar file click)
  toast: null,
  toastType: null,
  toastAction: null,        // { label, handler } rendered as a button on the toast's right edge
  searchModalOpen: false,
  searchQuery: '',
  searchResults: [],
  searchIncludeHistory: false, // live SearchModal toggle; always starts unchecked
  searchIncludeTrash: false,   // live SearchModal toggle; always starts unchecked
  searchIncludeRegex: false,   // live SearchModal toggle; always starts unchecked
  historyModalOpen: false,
  historyModalFile: null,   // { fileId, name, path } of the file History was opened for
  historyModalVersionId: null,     // deep-link: version to auto-select (set by a History search result click)
  historyModalScrollLine: null,    // deep-link: line to scroll to once that version is selected
  historyModalHighlightTerms: null, // deep-link: terms to highlight once that version is selected
  trashModalOpen: false,
  trashModalFileId: null,          // deep-link: trashed file to auto-select (set by a Trash search result click)
  trashModalScrollLine: null,      // deep-link: line to scroll to once that file is selected
  trashModalHighlightTerms: null,  // deep-link: terms to highlight once that file is selected
  pendingScrollLine: null,  // 1-based line number to scroll to after next active file load
  pendingHighlightTerms: null, // search terms to highlight after next active file load
  settings: null,           // Settings loaded from GET /api/settings on app mount
  settingsModalOpen: false,
  moveModalOpen: false,
  moveModalItem: null,  // { type: 'file'|'folder', fileId?, path, name }
})

let ws = null
let wsReconnectTimer = null
const contentListeners = {}   // fileId -> Set of callbacks
const treeListeners = []

export function onTreeUpdate(cb) { treeListeners.push(cb) }
export function onContentUpdate(fileId, cb) {
  if (!contentListeners[fileId]) contentListeners[fileId] = new Set()
  contentListeners[fileId].add(cb)
  return () => contentListeners[fileId]?.delete(cb)
}

export const store = state

// Sets isPushLayout from the same 768px breakpoint already used by the Sidebar/main-layout
// CSS, kept live via a 'change' listener so it's accurate across viewport/orientation
// changes. Single source of truth for any JS-level "are we in push-layout mode" check.
export function initLayoutFlags() {
  const mq = window.matchMedia('(min-width: 768px)')
  state.isPushLayout = mq.matches
  mq.addEventListener('change', (e) => { state.isPushLayout = e.matches })
}

export function connectWS() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  ws = new WebSocket(`${proto}://${location.host}/ws`)
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data)
    if (msg.type === 'init') {
      state.cid = msg.cid
    } else if (msg.type === 'filesystem') {
      state.fileTree = msg.tree
      treeListeners.forEach(cb => cb(msg.tree))
    } else if (msg.type === 'content') {
      state.fileContents[msg.fileId] = msg.content
      state.fileVersions[msg.fileId] = msg.versionId
      contentListeners[msg.fileId]?.forEach(cb => cb(msg.content))
    } else if (msg.type === 'editConflict') {
      applyContentUpdate(msg.fileId, msg.content, msg.versionId)
      showToast('Edit conflict: your change was overridden by another client', 'error')
    }
  }
  ws.onclose = () => {
    if (wsReconnectTimer) clearTimeout(wsReconnectTimer)
    wsReconnectTimer = setTimeout(() => connectWS(), 2000)
  }
}

function genVersionId() {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
  let id = ''
  for (let i = 0; i < 5; i++) id += chars[Math.floor(Math.random() * 62)]
  return id
}

// Sends one CodeMirror change event as a WS "edit" message. Optimistically advances
// fileVersions before sending so chained edits keep building off the new version
// without waiting for any server acknowledgment.
export function sendEdit(fileId, from, to, text, removed) {
  const currentVersionId = state.fileVersions[fileId] ?? ''
  const newVersionId = genVersionId()
  state.fileVersions[fileId] = newVersionId
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'edit', fileId, currentVersionId, newVersionId, from, to, text, removed }))
  }
}

export function setCurrentFolder(path) {
  state.currentFolderPath = path
}

export function setActiveFile(fileId) {
  state.activeFileId = fileId
}

export function setFileContent(fileId, content) {
  state.fileContents[fileId] = content
}

export function toggleSidebar() {
  state.sidebarOpen = !state.sidebarOpen
}

export function closeSidebar() {
  state.sidebarOpen = false
}

export function getFilesInFolder(folderPath) {
  return findFolderNode(state.fileTree, folderPath)?.files ?? []
}

export function getFolderNode(folderPath) {
  return findFolderNode(state.fileTree, folderPath)
}

export function folderOfPath(path) {
  return path.includes('/') ? path.split('/').slice(0, -1).join('/') : ''
}

function findFolderNode(node, path) {
  if (!node) return null
  if (node.path === path) return node
  for (const sub of (node.folders ?? [])) {
    const found = findFolderNode(sub, path)
    if (found) return found
  }
  return null
}

export function getCid() { return state.cid }

export function getFileVersion(fileId) { return state.fileVersions[fileId] ?? '' }

export function setFileVersion(fileId, versionId) {
  state.fileVersions[fileId] = versionId
}

// Updates content + version in state and fires content listeners (used for 409 conflict recovery).
export function applyContentUpdate(fileId, content, versionId) {
  state.fileContents[fileId] = content
  state.fileVersions[fileId] = versionId
  contentListeners[fileId]?.forEach(cb => cb(content))
}

let toastTimer = null
export function showToast(msg, type = null, action = null) {
  state.toast = msg
  state.toastType = type
  state.toastAction = action
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(dismissToast, 4000)
}

export function dismissToast() {
  state.toast = null
  state.toastType = null
  state.toastAction = null
  if (toastTimer) {
    clearTimeout(toastTimer)
    toastTimer = null
  }
}

export function openSearchModal() { state.searchModalOpen = true }
export function closeSearchModal() { state.searchModalOpen = false }
export function setSearchState(query, results) {
  state.searchQuery = query
  state.searchResults = results
}

// opts (all optional) deep-link to a specific version/line/highlight, set when opening
// from a History search result rather than the TabBar/FileTree/Footer History action.
export function openHistoryModal(file, opts = {}) {
  state.historyModalFile = file
  state.historyModalOpen = true
  state.historyModalVersionId = opts.versionId ?? null
  state.historyModalScrollLine = opts.scrollLine ?? null
  state.historyModalHighlightTerms = opts.highlightTerms ?? null
}
export function closeHistoryModal() {
  state.historyModalOpen = false
  state.historyModalVersionId = null
  state.historyModalScrollLine = null
  state.historyModalHighlightTerms = null
}

// opts (all optional) deep-link to a specific trashed file/line/highlight, set when
// opening from a Trash search result rather than the Sidebar's Trash row.
export function openTrashModal(opts = {}) {
  state.trashModalOpen = true
  state.trashModalFileId = opts.fileId ?? null
  state.trashModalScrollLine = opts.scrollLine ?? null
  state.trashModalHighlightTerms = opts.highlightTerms ?? null
}
export function closeTrashModal() {
  state.trashModalOpen = false
  state.trashModalFileId = null
  state.trashModalScrollLine = null
  state.trashModalHighlightTerms = null
}

export function openSettingsModal() { state.settingsModalOpen = true }
export function closeSettingsModal() { state.settingsModalOpen = false }

export function openMoveModal(item) {
  state.moveModalItem = item
  state.moveModalOpen = true
}
export function closeMoveModal() {
  state.moveModalOpen = false
  state.moveModalItem = null
}

export const editorActions = { undo: () => {}, redo: () => {}, scrollToLine: () => {}, highlightTerms: () => {}, getValue: () => '' }

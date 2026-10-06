import { reactive } from 'vue'
import * as api from './api.js'

const LS_WRAP = 'ntt.wordWrap'
const LS_SIDEBAR_OPEN = 'ntt.sidebarOpen'
const LS_SIDEBAR_WIDTH = 'ntt.sidebarWidth'

function lsGet(key, fallback) {
  try {
    const v = localStorage.getItem(key)
    return v === null ? fallback : JSON.parse(v)
  } catch { return fallback }
}
function lsSet(key, value) {
  try { localStorage.setItem(key, JSON.stringify(value)) } catch { /* ignore */ }
}

const state = reactive({
  cid: null,
  fileTree: null,           // FolderNode from server
  tabs: [],                 // [{ fileId, path, name, absPath }] — shared across clients
  activeFileId: null,       // per client
  fileContents: {},         // fileId -> string
  fileVersions: {},         // fileId -> versionId
  fileStatus: {},           // fileId -> { kind: 'ok'|'tooLarge'|'binary', size }
  wordWrap: lsGet(LS_WRAP, true),
  sidebarOpen: false,
  sidebarWidth: lsGet(LS_SIDEBAR_WIDTH, 400),
  sidebarDragging: false,
  dragItem: null,           // { type: 'file'|'folder', path, name } being dragged in the FileTree
  dragOverPath: null,       // folder path ('' = root) highlighted as drop target
  isPushLayout: false,      // true when the Sidebar pushes the layout (desktop) rather than overlaying it
  toast: null,
  toastType: null,
  toastAction: null,        // { label, handler } rendered as a button on the toast's right edge
  searchModalOpen: false,
  searchQuery: '',
  searchResults: [],
  searchIncludeRegex: false,
  pendingScrollLine: null,  // 1-based line to scroll to after next active file load
  pendingHighlightTerms: null,
  moveModalOpen: false,
  moveModalItem: null,      // { type: 'file'|'folder', path, name }
})

export const store = state

let ws = null
let wsReconnectTimer = null
const contentListeners = {}   // fileId -> Set of callbacks
const forcedLarge = new Set() // fileIds this client chose "display anyway" for

export function onContentUpdate(fileId, cb) {
  if (!contentListeners[fileId]) contentListeners[fileId] = new Set()
  contentListeners[fileId].add(cb)
  return () => contentListeners[fileId]?.delete(cb)
}

export function initLayoutFlags() {
  const mq = window.matchMedia('(min-width: 768px)')
  state.isPushLayout = mq.matches
  state.sidebarOpen = mq.matches && lsGet(LS_SIDEBAR_OPEN, true)
  mq.addEventListener('change', (e) => { state.isPushLayout = e.matches })
}

export function connectWS() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  ws = new WebSocket(`${proto}://${location.host}/ws`)
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data)
    if (msg.type === 'init') {
      state.cid = msg.cid
      syncFromServer()
    } else if (msg.type === 'filesystem') {
      state.fileTree = msg.tree
    } else if (msg.type === 'tabs') {
      applyTabs(msg.tabs)
    } else if (msg.type === 'content') {
      applyContentUpdate(msg.fileId, msg.content, msg.versionId)
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

// Loads the tree and shared tab list, then (re)activates a tab so this connection's
// server-side subscription is established. Runs on every (re)connect.
export async function syncFromServer() {
  try {
    const [tree, tabData] = await Promise.all([api.getTree(), api.getTabs()])
    state.fileTree = tree
    state.tabs = tabData.tabs
    let id = state.activeFileId
    if (!state.tabs.some(t => t.fileId === id)) {
      id = tabData.activeFileId || state.tabs[0]?.fileId || null
    }
    if (id) await activateTab(id)
    else state.activeFileId = null
  } catch (e) {
    showToast(e.message, 'error')
  }
}

// ---- tabs ----

// Applies the server's shared tab list. If this client's active tab vanished, picks
// the right neighbor (else left) from the old list.
function applyTabs(newTabs) {
  const old = state.tabs
  state.tabs = newTabs
  for (const t of old) {
    if (!newTabs.some(n => n.fileId === t.fileId)) {
      delete state.fileContents[t.fileId]
      delete state.fileVersions[t.fileId]
      delete state.fileStatus[t.fileId]
      forcedLarge.delete(t.fileId)
    }
  }
  if (state.activeFileId && !newTabs.some(t => t.fileId === state.activeFileId)) {
    const next = neighborAfterClose(old, state.activeFileId, newTabs)
    if (next) activateTab(next)
    else state.activeFileId = null
  }
}

function neighborAfterClose(oldTabs, closedId, remaining) {
  if (!remaining.length) return null
  const idx = oldTabs.findIndex(t => t.fileId === closedId)
  const survivors = oldTabs.filter(t => remaining.some(r => r.fileId === t.fileId))
  // right neighbor among survivors, else the left one
  const rightOfClosed = oldTabs.slice(idx + 1).find(t => survivors.includes(t))
  if (rightOfClosed) return rightOfClosed.fileId
  const leftOfClosed = [...oldTabs.slice(0, Math.max(idx, 0))].reverse().find(t => survivors.includes(t))
  return (leftOfClosed ?? remaining[remaining.length - 1]).fileId
}

export async function activateTab(fileId, force = false) {
  if (force) forcedLarge.add(fileId)
  try {
    const data = await api.getFile(fileId, forcedLarge.has(fileId))
    if (data.binary) {
      state.fileStatus[fileId] = { kind: 'binary', size: data.size }
    } else if (data.tooLarge) {
      state.fileStatus[fileId] = { kind: 'tooLarge', size: data.size }
    } else {
      state.fileStatus[fileId] = { kind: 'ok', size: data.size }
      if (fileId in state.fileContents) {
        applyContentUpdate(fileId, data.content, data.versionId)
      } else {
        state.fileContents[fileId] = data.content
        state.fileVersions[fileId] = data.versionId
      }
    }
    state.activeFileId = fileId
  } catch (e) {
    showToast(e.message, 'error')
  }
}

function addTabIfMissing(tab) {
  if (!state.tabs.some(t => t.fileId === tab.fileId)) state.tabs = [...state.tabs, tab]
}

// Opens (or activates) the tab for a file path.
export async function openPath(path) {
  try {
    const tab = await api.openTab(path)
    addTabIfMissing(tab)
    await activateTab(tab.fileId)
  } catch (e) {
    showToast(e.message, 'error')
  }
}

// Activates a tab the server just created for us (new file / duplicate).
export async function activateNewTab(tab) {
  addTabIfMissing(tab)
  await activateTab(tab.fileId)
}

export async function closeTabById(fileId) {
  try {
    await api.closeTab(fileId)
  } catch (e) {
    showToast(e.message, 'error')
    return
  }
  // the server's "tabs" broadcast does the same; applying locally avoids the lag
  applyTabs(state.tabs.filter(t => t.fileId !== fileId))
}

export async function reorderTab(fileId, targetIndex) {
  const ids = state.tabs.map(t => t.fileId)
  const from = ids.indexOf(fileId)
  if (from === -1) return
  ids.splice(from, 1)
  ids.splice(targetIndex, 0, fileId)
  const byId = Object.fromEntries(state.tabs.map(t => [t.fileId, t]))
  state.tabs = ids.map(id => byId[id])
  try {
    await api.reorderTabs(ids)
  } catch (e) {
    showToast(e.message, 'error')
    syncFromServer()
  }
}

export function activeTab() {
  return state.tabs.find(t => t.fileId === state.activeFileId) ?? null
}

export function tabForPath(path) {
  return state.tabs.find(t => t.path === path) ?? null
}

// ---- deleting ----

// Deletes a file/folder (server parks it in .ntt-trash for 5s) and shows an UNDO toast.
export async function deleteWithUndo(path, name) {
  try {
    const { trashId } = await api.deleteEntry(path)
    showToast(`Deleted "${name}"`, null, { label: 'UNDO', handler: () => undoDelete(trashId) })
  } catch (e) {
    showToast(e.message, 'error')
  }
}

async function undoDelete(trashId) {
  try {
    const data = await api.restoreTrash(trashId)
    if (data.fileId) await activateTab(data.fileId)
  } catch (e) {
    showToast(e.message, 'error')
  }
}

// ---- editing ----

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

// Updates content + version in state and fires content listeners.
export function applyContentUpdate(fileId, content, versionId) {
  state.fileContents[fileId] = content
  state.fileVersions[fileId] = versionId
  contentListeners[fileId]?.forEach(cb => cb(content))
}

// ---- tree helpers ----

export function toggleSidebar() {
  state.sidebarOpen = !state.sidebarOpen
  if (state.isPushLayout) lsSet(LS_SIDEBAR_OPEN, state.sidebarOpen)
}

export function closeSidebar() {
  state.sidebarOpen = false
  if (state.isPushLayout) lsSet(LS_SIDEBAR_OPEN, false)
}

export function saveSidebarWidth() { lsSet(LS_SIDEBAR_WIDTH, state.sidebarWidth) }

export function setWordWrap(wrap) {
  state.wordWrap = wrap
  lsSet(LS_WRAP, wrap)
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

// ---- toast / modals ----

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

export function openMoveModal(item) {
  state.moveModalItem = item
  state.moveModalOpen = true
}
export function closeMoveModal() {
  state.moveModalOpen = false
  state.moveModalItem = null
}

export const editorActions = { undo: () => {}, redo: () => {}, scrollToLine: () => {}, highlightTerms: () => {}, getValue: () => '' }

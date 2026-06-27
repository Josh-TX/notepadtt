import { getCid } from './store.js'

async function req(method, url, body) {
  const opts = { method, headers: { 'Content-Type': 'application/json' } }
  if (body !== undefined) opts.body = JSON.stringify(body)
  const res = await fetch(url, opts)
  if (!res.ok) throw new Error(await res.text())
  if (res.status === 204) return null
  return res.json()
}

export function getFiles() { return req('GET', '/api/files') }

export function getFile(fileId) {
  const cid = getCid()
  return req('GET', `/api/files/${fileId}?cid=${cid}`)
}

export function getFileVersions(fileId) {
  return req('GET', `/api/files/${fileId}/versions`)
}

export function createFile(parentPath) {
  return req('POST', '/api/files', { parentPath })
}

export function renameFile(fileId, name) {
  return req('PUT', `/api/files/${fileId}`, { name })
}

export function deleteFile(fileId) {
  return req('DELETE', `/api/files/${fileId}`)
}

export function duplicateFile(fileId) {
  return req('POST', `/api/files/${fileId}/duplicate`)
}

export function reorderFile(fileId, targetIndex) {
  return req('PUT', `/api/files/${fileId}/order`, { targetIndex })
}

export function moveFile(fileId, path) {
  return req('PUT', `/api/files/${fileId}/move`, { path })
}

export function createFolder(parentPath, name) {
  return req('POST', '/api/folders', { parentPath, name })
}

export function renameFolder(path, name) {
  return req('PUT', '/api/folders', { path, name })
}

export function deleteFolder(path) {
  return req('DELETE', '/api/folders', { path })
}

export function moveFolder(path, newPath) {
  return req('PUT', '/api/folders/move', { path, newPath })
}

export function searchFiles(query, opts = {}) {
  const params = new URLSearchParams({ q: query })
  if (opts.history) params.set('history', 'true')
  if (opts.trash) params.set('trash', 'true')
  return req('GET', `/api/search?${params.toString()}`)
}

export function getTrashList() {
  return req('GET', '/api/trash')
}

export function getTrashContent(fileId) {
  return req('GET', `/api/trash/${fileId}`)
}

export function restoreFile(fileId) {
  return req('POST', `/api/trash/${fileId}/restore`)
}

export function deleteTrashItem(fileId) {
  return req('DELETE', `/api/trash/${fileId}`)
}

export function emptyTrash() {
  return req('DELETE', '/api/trash')
}

export function getSettings() {
  return req('GET', '/api/settings')
}

export function saveSettings(settings) {
  return req('PUT', '/api/settings', settings)
}

export function updateWrap(wordWrap) {
  return req('PUT', '/api/settings/wordwrap', { wordWrap })
}

export function updateSidebarWidth(sidebarWidth) {
  return req('PUT', '/api/settings/sidebarwidth', { sidebarWidth })
}

export function updateDesktopSidebarOpen(desktopSidebarOpen) {
  return req('PUT', '/api/settings/desktopsidebaropen', { desktopSidebarOpen })
}


import { getCid } from './store.js'

async function req(method, url, body) {
  const opts = { method, headers: { 'Content-Type': 'application/json' } }
  if (body !== undefined) opts.body = JSON.stringify(body)
  const res = await fetch(url, opts)
  if (!res.ok) throw new Error((await res.text()).trim())
  if (res.status === 204) return null
  return res.json()
}

export function getTree() { return req('GET', '/api/tree') }
export function getTabs() { return req('GET', '/api/tabs') }
export function openTab(path) { return req('POST', '/api/tabs', { path }) }
export function closeTab(fileId) { return req('DELETE', `/api/tabs/${fileId}`) }
export function reorderTabs(fileIds) { return req('PUT', '/api/tabs/order', { fileIds }) }

export function getFile(fileId, force = false) {
  const cid = getCid()
  const params = new URLSearchParams()
  if (cid) params.set('cid', cid)
  if (force) params.set('force', '1')
  return req('GET', `/api/files/${fileId}?${params.toString()}`)
}

export function downloadUrl(path) { return `/api/download?path=${encodeURIComponent(path)}` }

export function createFile(parentPath) { return req('POST', '/api/files', { parentPath }) }
export function createFolder(parentPath, name) { return req('POST', '/api/folders', { parentPath, name }) }
export function duplicate(path) { return req('POST', '/api/duplicate', { path }) }
export function rename(path, name) { return req('PUT', '/api/rename', { path, name }) }
export function move(path, newPath) { return req('PUT', '/api/move', { path, newPath }) }
export function deleteEntry(path) { return req('DELETE', '/api/entries', { path }) }
export function restoreTrash(trashId) { return req('POST', `/api/trash/${trashId}/restore`) }

export function searchFiles(query, opts = {}) {
  const params = new URLSearchParams({ q: query })
  if (opts.regex) params.set('regex', 'true')
  return req('GET', `/api/search?${params.toString()}`)
}

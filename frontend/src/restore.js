import router from './router.js'
import { store, setActiveFile, setFileVersion } from './store.js'
import { restoreFile } from './api.js'

// Restores a trashed file via the shared restore API, then opens it in the main
// editor and navigates CurrentFolder/breadcrumbs to its folder — the behavior shared
// by the delete toast's UNDO action and both Trash Modal restore paths. Returns
// whether the restore succeeded; per spec there's no special error UX for failure
// (e.g. already expired/emptied), callers just skip their on-success behavior.
export async function restoreAndOpen(fileId) {
  let data
  try {
    data = await restoreFile(fileId)
  } catch (e) {
    return false
  }

  const folderPath = data.path.includes('/') ? data.path.split('/').slice(0, -1).join('/') : ''
  if (folderPath === store.currentFolderPath) {
    store.fileContents[fileId] = data.content
    setFileVersion(fileId, data.versionId)
    setActiveFile(fileId)
  } else {
    store.pendingFileId = fileId
    router.push(folderPath ? '/' + folderPath : '/')
  }
  return true
}

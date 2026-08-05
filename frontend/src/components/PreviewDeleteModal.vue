<template>
  <Teleport to="body">
    <div v-if="store.previewDeleteModalOpen" class="pd-overlay" @mousedown="onOverlayMouseDown" @click="onOverlayClick">
      <div class="pd-modal">
        <div class="pd-header">
          <div class="pd-title-block">
            <div class="pd-kicker">DELETE FOLDER</div>
            <div class="pd-title" :title="folder?.path">{{ folder?.name }}</div>
          </div>
          <button class="close-btn" @click="close">✕</button>
        </div>
        <div class="pd-body">
          <div v-if="stats?.trackedCount > 0" class="pd-row">
            Move <strong>{{ stats.trackedCount }}</strong> tracked file{{ stats.trackedCount === 1 ? '' : 's' }}
            ({{ formatSize(stats.trackedSize) }}) to trash — <span class="pd-safe">recoverable</span>
          </div>
          <div v-if="stats?.untrackedCount > 0" class="pd-row">
            Permanently delete <strong>{{ stats.untrackedCount }}</strong> untracked file{{ stats.untrackedCount === 1 ? '' : 's' }}
            ({{ formatSize(stats.untrackedSize) }}) — <span class="pd-danger">not recoverable</span>
          </div>
        </div>
        <div class="pd-footer">
          <button class="pd-cancel-btn" @click="close">Cancel</button>
          <button class="pd-delete-btn" @click="doDelete">Delete</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { store, closePreviewDeleteModal, showToast } from '../store.js'
import { deleteFolder } from '../api.js'

const router = useRouter()

const folder = computed(() => store.previewDeleteModalFolder)
const stats = computed(() => store.previewDeleteModalStats)

function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function close() { closePreviewDeleteModal() }

let mouseDownOnOverlay = false
function onOverlayMouseDown(e) { mouseDownOnOverlay = e.target === e.currentTarget }
function onOverlayClick(e) { if (mouseDownOnOverlay && e.target === e.currentTarget) close() }

async function doDelete() {
  const f = folder.value
  try {
    await deleteFolder(f.path)
    close()
    if (store.currentFolderPath === f.path || store.currentFolderPath.startsWith(f.path + '/')) {
      const parent = f.path.includes('/') ? f.path.split('/').slice(0, -1).join('/') : ''
      router.push(parent ? '/' + parent : '/')
    }
  } catch (err) {
    showToast('Failed to delete folder', 'error')
  }
}
</script>

<style scoped>
.pd-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
}
.pd-modal {
  background: #1e1e1e;
  border: 1px solid #3a3a3a;
  border-radius: 6px;
  width: 90%;
  max-width: 480px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.pd-header {
  display: flex;
  align-items: stretch;
  border-bottom: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.pd-title-block {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 1px;
  padding: 8px 40px 8px 14px;
}
.pd-kicker {
  font-size: 10px;
  font-weight: 600;
  color: #777;
  letter-spacing: 0.08em;
}
.pd-title {
  color: #d4d4d4;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.close-btn {
  flex-shrink: 0;
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
.pd-body {
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  font-size: 13px;
  color: #ccc;
  line-height: 1.5;
  min-height: 80px;
}
.pd-safe { color: #6a9955; }
.pd-danger { color: #f48771; }
.pd-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 8px 12px;
  border-top: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.pd-cancel-btn {
  background: transparent;
  border: 1px solid #3a3a3a;
  border-radius: 3px;
  color: #ccc;
  cursor: pointer;
  font-size: 13px;
  padding: 5px 14px;
}
.pd-cancel-btn:hover { background: #2a2d2e; color: #fff; }
.pd-delete-btn {
  background: #a1260d;
  border: none;
  border-radius: 3px;
  color: #fff;
  cursor: pointer;
  font-size: 13px;
  padding: 5px 14px;
}
.pd-delete-btn:hover { background: #c42b0f; }

@media (max-width: 767px) {
  .pd-modal {
    width: 94%;
  }
}
</style>

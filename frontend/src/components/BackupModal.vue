<template>
  <Teleport to="body">
    <div v-if="store.backupModalOpen" class="backup-overlay" @click.self="close" @keydown.escape="close">
      <div class="backup-modal">
        <div class="backup-header">
          <span class="backup-title">Download Zip Backup</span>
          <button class="close-btn" @click="close">✕</button>
        </div>
        <div class="backup-body">
          <div v-if="error" class="backup-error">{{ error }}</div>
          <div v-else-if="!info" class="backup-info">Calculating size…</div>
          <div v-else class="backup-info">
            {{ info.files }} {{ info.files === 1 ? 'file' : 'files' }}, ~{{ formatBytes(info.bytes) }} before compression
          </div>
        </div>
        <div class="backup-footer">
          <a class="download-btn" :class="{ disabled: !info }" :href="info ? backupUrl : undefined" download @click="close">Download</a>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch } from 'vue'
import { store, closeBackupModal } from '../store.js'
import { backupSize, backupUrl } from '../api.js'

const info = ref(null)
const error = ref('')

watch(() => store.backupModalOpen, async (open) => {
  if (!open) return
  info.value = null
  error.value = ''
  try {
    info.value = await backupSize()
  } catch (e) {
    error.value = e.message
  }
})

function close() { closeBackupModal() }

function formatBytes(bytes) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = bytes
  let i = 0
  while (n >= 1000 && i < units.length - 1) { n /= 1024; i++ }
  if (i === 0) return `${n} B`
  return `${Number(n.toPrecision(3))} ${units[i]}`
}
</script>

<style scoped>
.backup-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
}
.backup-modal {
  background: #1e1e1e;
  border: 1px solid #3a3a3a;
  border-radius: 6px;
  width: 90%;
  max-width: 420px;
  display: flex;
  flex-direction: column;
}
.backup-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px 8px 14px;
  border-bottom: 1px solid #2d2d2d;
}
.backup-title { font-weight: 600; color: #ccc; }
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
.backup-body { padding: 20px 14px; }
.backup-info { color: #ccc; }
.backup-error { color: #f48771; }
.backup-footer {
  display: flex;
  justify-content: flex-end;
  padding: 8px 14px 12px;
}
.download-btn {
  background: #0078d4;
  color: #fff;
  border-radius: 4px;
  padding: 6px 16px;
  text-decoration: none;
  cursor: pointer;
}
.download-btn:hover { background: #1688e0; }
.download-btn.disabled { opacity: 0.5; pointer-events: none; }
</style>

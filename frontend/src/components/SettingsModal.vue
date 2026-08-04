<template>
  <Teleport to="body">
    <div v-if="store.settingsModalOpen" class="settings-overlay" @mousedown="onOverlayMouseDown" @click="onOverlayClick">
      <div class="settings-modal">
        <div class="settings-header">
          <span>Settings</span>
          <button class="close-btn" @click="close">✕</button>
        </div>
        <div class="settings-body">
          <section class="field-row">
            <label>File Extension to Track</label>
            <select v-model="form.onlyTextExt">
              <option :value="true">Common Text Files</option>
              <option :value="false">All Files</option>
            </select>
          </section>

          <section class="field-row">
            <label>Html Title</label>
            <input type="text" class="title-input" v-model="form.title" />
          </section>

          <section class="field-row">
            <label>Tab Close Icon</label>
            <select v-model="form.tabCloseIcon">
              <option value="visible">Visible</option>
              <option value="hidden">Hidden</option>
              <option value="new">Visible on new files</option>
            </select>
          </section>

          <section class="field-row">
            <label>Ctrl+F Behavior</label>
            <select v-model="form.ctrlFSearch">
              <option :value="true">Opens Search Modal</option>
              <option :value="false">Native Find</option>
            </select>
          </section>

          <section class="field-row">
            <label>markdown syntax highlighting</label>
            <select v-model="form.markdownMode">
              <option :value="0">all new files</option>
              <option :value="1">all files without an extension</option>
              <option :value="2">only .md files</option>
            </select>
          </section>

          <section class="field-row">
            <label>Editor Font Size</label>
            <input type="number" min="10" max="24" step="1" v-model.number="form.editorFontSize" />
          </section>

          <section class="field-row">
            <label>Override Syntax Colors</label>
            <input type="text" class="color-overrides-input" v-model="form.colorOverrides" />
          </section>

          <section class="field-group">
            <div class="group-title">Search Results</div>
            <div class="field-row">
              <label>Lines per Result</label>
              <input type="number" min="1" step="1" v-model.number="form.linesPerResult" />
            </div>
            <div class="field-row">
              <label>Max Results per File</label>
              <input type="number" min="1" step="1" v-model.number="form.maxResultsPerFile" />
            </div>
            <div class="field-row">
              <label>Max Files</label>
              <input type="number" min="1" step="1" v-model.number="form.maxFiles" />
            </div>
          </section>

          <section class="field-row">
            <label>Trash TTL</label>
            <input type="text" class="duration-input" v-model="form.trashTTL" />
          </section>

          <section class="field-group">
            <div class="group-title">File History</div>
            <div class="field-row">
              <label>Short Term TTL</label>
              <input type="text" class="duration-input" v-model="form.shortTermTTL" />
            </div>
            <div class="field-row">
              <label>Short Term Min Delay</label>
              <input type="text" class="duration-input" v-model="form.shortTermMinDelay" />
            </div>
            <div class="field-row">
              <label>Med Term TTL</label>
              <input type="text" class="duration-input" v-model="form.medTermTTL" />
            </div>
            <div class="field-row">
              <label>Med Term Min Delay</label>
              <input type="text" class="duration-input" v-model="form.medTermMinDelay" />
            </div>
            <div class="field-row">
              <label>Long Term TTL</label>
              <input type="text" class="duration-input" v-model="form.longTermTTL" />
            </div>
            <div class="field-row">
              <label>Long Term Min Delay</label>
              <input type="text" class="duration-input" v-model="form.longTermMinDelay" />
            </div>
          </section>
        </div>
        <div class="settings-footer">
          <button class="save-btn" @click="save">Save</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, watch } from 'vue'
import { store, closeSettingsModal, showToast } from '../store.js'
import { saveSettings } from '../api.js'

const form = ref({})

// Snapshot the current settings into the editable form each time the modal opens, so
// unsaved edits never leak into store.settings if the user closes without saving.
// wordWrap, sidebarWidth, and sidebarOpen (-> desktopSidebarOpen) are overridden with
// their live store values (not the form) since none is editable here, and all three can
// have changed since settings last loaded (the Footer's wrap toggle, the Sidebar's
// resize handle, the Navbar/Sidebar's sidebar toggle).
watch(() => store.settingsModalOpen, (open) => {
  if (open) form.value = { ...store.settings, wordWrap: store.wordWrap, sidebarWidth: store.sidebarWidth, desktopSidebarOpen: store.sidebarOpen }
})

function close() {
  closeSettingsModal()
}

let mouseDownOnOverlay = false
function onOverlayMouseDown(e) {
  mouseDownOnOverlay = e.target === e.currentTarget
}
function onOverlayClick(e) {
  if (mouseDownOnOverlay && e.target === e.currentTarget) close()
}

async function save() {
  if (form.value.onlyTextExt && !store.settings?.onlyTextExt) {
    const msg = 'Switching to "Common Text Files" will permanently delete tracked history (versions, trash) for any files with a non-text extension. The files themselves on disk are unaffected. Continue?'
    if (!confirm(msg)) return
  }
  try {
    const saved = await saveSettings(form.value)
    store.settings = saved
    close()
  } catch (e) {
    showToast('Failed to save settings — check your input', 'error')
  }
}
</script>

<style scoped>
.settings-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 40px;
}
.settings-modal {
  background: #1e1e1e;
  border: 1px solid #3a3a3a;
  border-radius: 6px;
  width: 90%;
  max-width: 480px;
  max-height: calc(100vh - 80px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.settings-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  border-bottom: 1px solid #2d2d2d;
  color: #d4d4d4;
  font-weight: 600;
  flex-shrink: 0;
}
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
.settings-body {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  scrollbar-width: thin;
  scrollbar-color: #555 transparent;
  padding: 12px;
}
section.field-row, .field-group .field-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.field-group {
  margin-bottom: 16px;
  padding: 10px;
  background: #232323;
  border-radius: 4px;
}
.field-group .field-row { margin-bottom: 8px; }
.field-group .field-row:last-child { margin-bottom: 0; }
.group-title {
  color: #9cdcfe;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-bottom: 8px;
}
label {
  color: #ccc;
  font-size: 13px;
}
select, input[type="text"], input[type="number"] {
  background: #2d2d2d;
  border: 1px solid #444;
  border-radius: 4px;
  color: #d4d4d4;
  font-size: 13px;
  padding: 5px 8px;
  outline: none;
}
select:focus, input:focus { border-color: #0078d4; }
input[type="number"] { width: 70px; }
.duration-input { width: 90px; }
.color-overrides-input { width: 220px; }
.title-input { width: 160px; }
.settings-footer {
  display: flex;
  justify-content: flex-end;
  padding: 10px 12px;
  border-top: 1px solid #2d2d2d;
  flex-shrink: 0;
}
.save-btn {
  background: #0078d4;
  border: none;
  border-radius: 4px;
  color: #fff;
  font-size: 13px;
  padding: 6px 16px;
  cursor: pointer;
}
.save-btn:hover { background: #1a8ad4; }

@media (max-width: 767px) {
  .settings-overlay {
    padding-top: 0;
    align-items: stretch;
  }
  .settings-modal {
    width: 100%;
    max-width: 100%;
    height: 100%;
    max-height: 100%;
    border-radius: 0;
    border: none;
  }
}
</style>

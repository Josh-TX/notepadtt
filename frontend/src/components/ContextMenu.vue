<template>
  <Teleport to="body">
    <div class="ctx-backdrop" @mousedown.self="$emit('close')" @contextmenu.prevent>
      <ul class="ctx-menu" ref="menuEl" :style="menuStyle">
        <li
          v-for="item in items"
          :key="item.label"
          @click="run(item)"
        >{{ item.label }}</li>
      </ul>
    </div>
  </Teleport>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'

const props = defineProps(['x', 'y', 'items'])
const emit = defineEmits(['close'])

const menuEl = ref(null)
const menuW = ref(0)
const menuH = ref(0)

onMounted(() => {
  menuW.value = menuEl.value.offsetWidth
  menuH.value = menuEl.value.offsetHeight
})

const menuStyle = computed(() => {
  const vw = window.innerWidth
  const vh = window.innerHeight
  const left = props.x + menuW.value > vw ? vw - menuW.value : props.x
  const top = props.y + menuH.value > vh ? vh - menuH.value : props.y
  return { left: left + 'px', top: top + 'px' }
})

function run(item) {
  emit('close')
  item.action()
}
</script>

<style scoped>
.ctx-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1000;
}
.ctx-menu {
  position: absolute;
  background: #252526;
  border: 1px solid #3c3c3c;
  border-radius: 3px;
  padding: 4px 0;
  list-style: none;
  min-width: 140px;
  box-shadow: 2px 4px 12px #0008;
}
.ctx-menu li {
  padding: 6px 16px;
  cursor: pointer;
  color: #ccc;
}
.ctx-menu li:hover { background: #094771; color: #fff; }
</style>

import { createApp, h, ref } from 'vue'
import '@/styles/main.css'
import EditBookDialog from '@/components/EditBookDialog.vue'
import SortControl from '@/components/SortControl.vue'
import TileMenu from '@/components/TileMenu.vue'
const editing = ref(false), menu = ref(false), key = ref<any>('title'), dir = ref<any>('asc')
createApp({ render: () => h('div', [
  h('button', { id: 'open', onClick: () => (editing.value = true) }, 'open edit'),
  h('button', { id: 'ctx', onClick: () => (menu.value = true) }, 'open ctx'),
  h('input', { id: 'bg' }),
  h(SortControl, { sortKey: key.value, dir: dir.value, 'onUpdate:sortKey': (k: any) => (key.value = k), 'onUpdate:dir': (d: any) => (dir.value = d) }),
  editing.value ? h(EditBookDialog, { book: { id: 1, title: 'Synthetic', author: 'A', cover_url: '' } as any, onClose: () => (editing.value = false), onSave: () => (editing.value = false) }) : null,
  menu.value ? h(TileMenu, { x: 50, y: 50, items: [{ key: 'a', label: 'One' }, { key: 'b', label: 'Two' }], onClose: () => (menu.value = false), onSelect: () => (menu.value = false) }) : null,
]) }).mount('#app')

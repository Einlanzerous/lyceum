import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import './theme' // applies the persisted theme to <html> before first paint
import './styles/main.css'

createApp(App).use(createPinia()).use(router).mount('#app')

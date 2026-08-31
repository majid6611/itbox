import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { useThemeStore } from './stores/theme'
import './theme.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')

useThemeStore().fetch()

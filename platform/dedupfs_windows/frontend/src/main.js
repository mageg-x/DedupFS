import { createApp } from 'vue'
import App from './App.vue'

// 创建Vue应用
const app = createApp(App)

// 挂载应用
app.mount('#app')

// 暴露window.go对象供Vue组件使用
// Wails v2会自动注入window.go对象
window.go = window.go || {}
// 绑定 Wails 运行时
window.wails = window.wails || {}
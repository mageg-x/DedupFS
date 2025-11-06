<template>
  <div id="app" class="app-container">
    <!-- 主要内容区域 -->
    <div class="main-content">
      <!-- 左侧挂载点列表 -->
      <div class="sidebar">
        <div class="sidebar-header">
          <h3>挂载点列表</h3>
          <button @click="addMountPoint" class="add-button">
            <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
              <path d="M8 2v12M2 8h12" stroke="currentColor" stroke-width="2" stroke-linecap="round"></path>
            </svg>
            <span>添加挂载点</span>
          </button>
        </div>
        
        <div class="mount-points-list">
          <div 
            v-for="(item, index) in mountPoints" 
            :key="item.id"
            @click="selectMountPoint(index)"
            class="mount-point-item"
            :class="{ 'selected': selectedIndex === index }">
            <div class="mount-point-header">
              <div class="mount-point-name">{{ item.name || '未命名' }}</div>
              <div class="mount-status" :class="{ 'mounted': item.isMounted }">
                {{ item.isMounted ? '已挂载' : '未挂载' }}
              </div>
            </div>
            <div class="mount-point-path">{{ item.mountPath }}</div>
            <div v-if="item.isMounted" class="mount-point-stats">
              <div class="storage-bar">
                <div class="storage-fill" :style="{ width: getStoragePercent(item) + '%' }"></div>
              </div>
              <div class="storage-text">{{ formatSize(item.usedSpace) }} / {{ formatSize(item.totalSpace) }}</div>
            </div>
          </div>
          
          <!-- 空状态 -->
          <div v-if="mountPoints.length === 0" class="empty-state">
            <div class="empty-icon">
              <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="#666" stroke-width="1" stroke-linecap="round" stroke-linejoin="round">
                <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                <line x1="16" y1="8" x2="8" y2="8"></line>
                <line x1="16" y1="16" x2="8" y2="16"></line>
                <line x1="10" y1="12" x2="14" y2="12"></line>
              </svg>
            </div>
            <div class="empty-text">暂无挂载点</div>
            <div class="empty-subtext">点击上方按钮添加新的挂载点</div>
          </div>
        </div>
      </div>

      <!-- 右侧配置区域 -->
      <div class="content-area">
        <div v-if="selectedMountPoint" class="config-container">
          <!-- 标签页导航 -->
          <div class="tabs">
            <button 
              v-for="tab in tabs" 
              :key="tab.id"
              class="tab" 
              :class="{ 'active': activeTab === tab.id }"
              @click="activeTab = tab.id">
              {{ tab.name }}
            </button>
          </div>
          
          <!-- 基本配置 -->
          <div v-show="activeTab === 'basic'" class="tab-content">
            <div class="config-header">
              <div class="config-title">
                <h2 class="truncate-text">{{ selectedMountPoint.name || '挂载点配置' }}</h2>
                <div class="config-subtitle">管理您的去重文件系统设置</div>
              </div>
              <div class="action-buttons">
                <button :disabled="!canMount" @click="handleMountAction" class="action-button primary">
                  <span v-if="!selectedMountPoint.isMounted">
                  <svg width="14" height="14" viewBox="0 0 16 20" fill="currentColor">
                    <path d="M8 6v12M2 12h12" stroke="currentColor" stroke-width="2" stroke-linecap="round"></path>
                  </svg>
                    挂载
                  </span>
                  <span v-else>
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                      <line x1="4" y1="12" x2="12" y2="12" stroke="currentColor" stroke-width="2" stroke-linecap="round"></line>
                    </svg>
                    卸载
                  </span>
                </button>
                <button @click="saveMountPoint" class="action-button secondary">
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                    <path d="M2 12.5V14a1 1 0 001 1h10a1 1 0 001-1v-1.5L8.5 8.5 2 15z" fill="none" stroke="currentColor" stroke-width="1.5"></path>
                    <path d="M8 11a3 3 0 100-6 3 3 0 000 6z" fill="none" stroke="currentColor" stroke-width="1.5"></path>
                  </svg>
                  保存配置
                </button>
                <button @click="deleteMountPoint" class="action-button danger">
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                    <polyline points="3 5 5 5 11 5 13 5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"></polyline>
                    <line x1="5" y1="5" x2="5" y2="11" stroke="currentColor" stroke-width="1.5"></line>
                    <line x1="11" y1="5" x2="11" y2="11" stroke="currentColor" stroke-width="1.5"></line>
                    <path d="M5 11h6v1a1 1 0 01-1 1H6a1 1 0 01-1-1v-1z" fill="none" stroke="currentColor" stroke-width="1.5"></path>
                  </svg>
                  删除
                </button>
              </div>
            </div>

            <div class="config-section">
              <div class="section-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                  <line x1="9" y1="3" x2="9" y2="21"></line>
                </svg>
                <h3 class="section-title">基本配置</h3>
              </div>
              <div class="form-grid">
                <div class="form-group">
                  <label class="form-label">挂载点名称</label>
                  <input v-model="selectedMountPoint.name" class="form-input" placeholder="输入挂载点名称">
                </div>
                
                <div class="form-group">
                  <label class="form-label">挂载路径</label>
                  <div class="input-with-button">
                    <select v-model="selectedMountPoint.mountPath" class="form-input" @click="browseMountPath">
                      <option value="">请选择挂载盘符</option>
                      <option v-for="drive in availableDrives" :key="drive" :value="drive">{{ drive }}</option>
                    </select>
                  </div>
                </div>
                
                <div class="form-group">
                  <label class="form-label">数据目录</label>
                  <div class="input-with-button">
                    <input v-model="selectedMountPoint.dataDir" class="form-input" placeholder="输入数据目录">
                    <button @click="browseDataDir" class="browse-button">浏览...</button>
                  </div>
                </div>
                

              </div>
            </div>
          </div>
          
          <!-- 切片配置 -->
          <div v-show="activeTab === 'chunk'" class="tab-content">
            <div class="config-section">
              <div class="section-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                  <polyline points="14 2 14 8 20 8"></polyline>
                  <line x1="16" y1="13" x2="8" y2="13"></line>
                  <line x1="16" y1="17" x2="8" y2="17"></line>
                  <polyline points="10 9 9 9 8 9"></polyline>
                </svg>
                <h3 class="section-title">切片配置</h3>
              </div>
              <div class="form-grid">
                <div class="form-group">
                  <label class="form-label checkbox">
                    <input v-model="selectedMountPoint.chunkConfig.fixedSize" type="checkbox" class="checkbox-input">
                    <span class="checkbox-label">固定长度切片</span>
                  </label>
                </div>
                
                <div class="form-group">
                  <label class="form-label">最小切片大小 (KB)</label>
                  <input v-model.number="selectedMountPoint.chunkConfig.minSize" type="number" class="form-input" placeholder="输入最小切片大小">
                </div>
                
                <div class="form-group">
                  <label class="form-label">平均切片大小 (KB)</label>
                  <input v-model.number="selectedMountPoint.chunkConfig.avgSize" type="number" class="form-input" placeholder="输入平均切片大小">
                </div>
                
                <div class="form-group">
                  <label class="form-label">最大切片大小 (KB)</label>
                  <input v-model.number="selectedMountPoint.chunkConfig.maxSize" type="number" class="form-input" placeholder="输入最大切片大小">
                </div>
              </div>
            </div>
          </div>
          
          <!-- 块配置 -->
          <div v-show="activeTab === 'block'" class="tab-content">
            <div class="config-section">
              <div class="section-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <rect x="3" y="3" width="7" height="7"></rect>
                  <rect x="14" y="3" width="7" height="7"></rect>
                  <rect x="14" y="14" width="7" height="7"></rect>
                  <rect x="3" y="14" width="7" height="7"></rect>
                </svg>
                <h3 class="section-title">块配置</h3>
              </div>
              <div class="form-grid">
                <div class="form-group">
                  <label class="form-label">块大小 (KB)</label>
                  <input v-model.number="selectedMountPoint.blockConfig.size" type="number" class="form-input" placeholder="输入块大小">
                </div>
                
                <div class="form-group">
                  <label class="form-label checkbox">
                    <input v-model="selectedMountPoint.blockConfig.compress" type="checkbox" class="checkbox-input">
                    <span class="checkbox-label">启用压缩</span>
                  </label>
                </div>
                
                <div class="form-group">
                  <label class="form-label checkbox">
                    <input v-model="selectedMountPoint.blockConfig.encrypt" type="checkbox" class="checkbox-input">
                    <span class="checkbox-label">启用加密</span>
                  </label>
                </div>
                
                <div class="form-group" v-if="selectedMountPoint.blockConfig.encrypt">
                  <label class="form-label">加密密码</label>
                  <input v-model="selectedMountPoint.blockConfig.password" type="password" class="form-input" placeholder="输入加密密码">
                </div>
              </div>
            </div>
          </div>
          
          <!-- 统计信息 -->
          <div v-show="activeTab === 'stats'" class="tab-content">
            <div class="config-section stats-section">
              <div class="section-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <line x1="18" y1="20" x2="18" y2="10"></line>
                  <line x1="12" y1="20" x2="12" y2="4"></line>
                  <line x1="6" y1="20" x2="6" y2="14"></line>
                </svg>
                <h3 class="section-title">统计信息</h3>
              </div>
              <div class="stats-grid">
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"></path>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">文件系统ID</div>
                    <div class="stat-value">{{ selectedMountPoint.stats.fsId || 'N/A' }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"></path>
                      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"></path>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">基础目录</div>
                    <div class="stat-value">{{ selectedMountPoint.stats.baseDir || 'N/A' }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                      <polyline points="14 2 14 8 20 8"></polyline>
                      <line x1="16" y1="13" x2="8" y2="13"></line>
                      <line x1="16" y1="17" x2="8" y2="17"></line>
                      <polyline points="10 9 9 9 8 9"></polyline>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">文件数量</div>
                    <div class="stat-value">{{ selectedMountPoint.stats.files || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">目录数量</div>
                    <div class="stat-value">{{ selectedMountPoint.stats.directories || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"></path>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">已用空间</div>
                    <div class="stat-value">{{ formatSize(selectedMountPoint.usedSpace) }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                      <circle cx="8.5" cy="8.5" r="1.5"></circle>
                      <polyline points="21 15 16 10 5 21"></polyline>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">实际大小</div>
                    <div class="stat-value">{{ formatSize(selectedMountPoint.stats.realSize || 0) }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <rect x="3" y="3" width="7" height="7"></rect>
                      <rect x="14" y="3" width="7" height="7"></rect>
                      <rect x="14" y="14" width="7" height="7"></rect>
                      <rect x="3" y="14" width="7" height="7"></rect>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">总切片数</div>
                    <div class="stat-value">{{ selectedMountPoint.stats.totalChunks || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                      <line x1="9" y1="3" x2="9" y2="21"></line>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">块数量</div>
                    <div class="stat-value">{{ selectedMountPoint.stats.blocks || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                      <circle cx="12" cy="12" r="3"></circle>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">引用的切片数</div>
                    <div class="stat-value">{{ selectedMountPoint.stats.referencedChunks || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                      <polyline points="4 14 10 14 10 20"></polyline>
                      <polyline points="20 10 14 10 14 4"></polyline>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">压缩比率</div>
                    <div class="stat-value">{{ (selectedMountPoint.stats.compressionRatio || 0).toFixed(2) }}x</div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
        
        <!-- 未选择状态 -->
        <div v-else class="no-selection">
          <div class="no-selection-icon">
            <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="#666" stroke-width="1" stroke-linecap="round" stroke-linejoin="round">
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
              <path d="M16 3v4M8 3v4M3 16h4M3 8h4M16 21v-4M8 21v-4M21 16h-4M21 8h-4"></path>
            </svg>
          </div>
          <div class="no-selection-title">请从左侧选择一个挂载点</div>
          <div class="no-selection-subtitle">选择挂载点后可以查看和编辑其配置</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'

// 响应式数据
const mountPoints = ref([])
const selectedIndex = ref(-1)
const selectedMountPoint = ref(null)
const activeTab = ref('basic')
const tabs = ref([
  { id: 'basic', name: '基本配置' },
  { id: 'chunk', name: '切片配置' },
  { id: 'block', name: '块配置' },
  { id: 'stats', name: '统计信息' }
])
const availableDrives = ref([])

// 计算属性
const canMount = computed(() => {
  return selectedMountPoint.value && 
         selectedMountPoint.value.mountPath && 
         selectedMountPoint.value.dataDir
})

// 方法定义
const loadMountPoints = async () => {
  try {
    // 调用后端GetMountPoints方法
    mountPoints.value = await window.go.main.App.GetMountPoints()
  } catch (error) {
    showNotification('加载挂载点失败: ' + error.message, 'error')
    console.error('加载挂载点失败:', error)
  }
}

const selectMountPoint = (index) => {
  selectedIndex.value = index
  // 创建副本以避免直接修改源数据
  selectedMountPoint.value = JSON.parse(JSON.stringify(mountPoints.value[index]))
  // 切换到基本配置标签
  activeTab.value = 'basic'
}

const addMountPoint = async () => {
  const newMountPoint = {
    id: Date.now().toString(),
    name: '新挂载点',
    mountPath: '',
    dataDir: '',

    isMounted: false,
    usedSpace: 0,
    totalSpace: 0,
    chunkConfig: {
      fixedSize: false,
      minSize: 64,
      avgSize: 128,
      maxSize: 256
    },
    blockConfig: {
      size: 64,
      compress: false,
      encrypt: false,
      password: ''
    },
    stats: {
      fsId: '',
      baseDir: '',
      files: 0,
      directories: 0,
      realSize: 0,
      totalChunks: 0,
      blocks: 0,
      referencedChunks: 0,
      compressionRatio: 0,
      lastUpdated: ''
    }
  }
  
  try {
    // 调用后端AddMountPoint方法
    await window.go.main.App.AddMountPoint(newMountPoint)
    // 重新加载挂载点列表
    await loadMountPoints()
    // 选中新添加的挂载点
    selectMountPoint(mountPoints.value.length - 1)
    showNotification('挂载点添加成功', 'success')
  } catch (error) {
    showNotification('添加挂载点失败: ' + error.message, 'error')
    console.error('添加挂载点失败:', error)
  }
}

const saveMountPoint = async () => {
  if (!selectedMountPoint.value) return
  
  try {
    // 调用后端SaveMountPoint方法
    await window.go.main.App.SaveMountPoint(selectedMountPoint.value)
    // 重新加载挂载点列表
    await loadMountPoints()
    // 重新选中当前挂载点
    selectMountPoint(selectedIndex.value)
    showNotification('配置已保存', 'success')
  } catch (error) {
    showNotification('保存配置失败: ' + error.message, 'error')
    console.error('保存配置失败:', error)
  }
}

const deleteMountPoint = async () => {
  if (selectedIndex.value < 0 || !confirm('确定要删除此挂载点吗？')) return
  
  try {
    // 调用后端DeleteMountPoint方法
    await window.go.main.App.DeleteMountPoint(selectedMountPoint.value.id)
    // 重新加载挂载点列表
    await loadMountPoints()
    // 重置选中状态
    selectedIndex.value = -1
    selectedMountPoint.value = null
    showNotification('挂载点已删除', 'info')
  } catch (error) {
    showNotification('删除挂载点失败: ' + error.message, 'error')
    console.error('删除挂载点失败:', error)
  }
}

const handleMountAction = async () => {
  if (!selectedMountPoint.value) return
  
  try {
    let result
    if (selectedMountPoint.value.isMounted) {
      // 调用后端Unmount方法
      result = await window.go.main.App.Mount(selectedMountPoint.value.id)
    } else {
      // 调用后端Mount方法
      result = await window.go.main.App.Unmount(selectedMountPoint.value.id)
    }
    
    // 重新加载挂载点列表以更新状态
    await loadMountPoints()
    // 重新选中当前挂载点
    selectMountPoint(selectedIndex.value)
    showNotification(result, 'success')
  } catch (error) {
    showNotification('操作失败: ' + error.message, 'error')
    console.error('挂载/卸载操作失败:', error)
  }
}

const formatSize = (bytes) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

const getStoragePercent = (mountPoint) => {
  if (!mountPoint.totalSpace || mountPoint.totalSpace === 0) return 0
  return Math.min((mountPoint.usedSpace / mountPoint.totalSpace) * 100, 100)
}

// 加载可用盘符列表并处理浏览操作
const browseMountPath = async () => {
  try {
    availableDrives.value = await window.go.main.App.BrowseDriver()
  } catch (error) {
    showNotification('获取可用盘符失败: ' + error.message, 'error')
    console.error('获取可用盘符失败:', error)
  }
}

const browseDataDir = async () => {
  if (!selectedMountPoint.value) {
    showNotification('请先选择或创建挂载点', 'warning')
    return
  }
  
  try {
    const dir = await window.go.main.App.BrowseDataDir('选择数据目录', selectedMountPoint.value.dataDir || '')
    if (dir) {
      selectedMountPoint.value.dataDir = dir
    }
  } catch (error) {
    showNotification('选择目录失败: ' + error.message, 'error')
    console.error('选择目录失败:', error)
  }
}

const minimizeWindow = () => {
  // 最小化窗口功能
  console.log('Minimize window')
}

const closeWindow = () => {
  // 关闭窗口功能
  console.log('Close window')
}

const showNotification = (message, type = 'info') => {
  // 这里可以实现更好的通知系统
  // 暂时使用alert模拟
  console.log(`[${type.toUpperCase()}] ${message}`)
}

// 组件挂载后从后端加载挂载点数据
onMounted(async () => {
  loadMountPoints()
  await browseMountPath()
})
</script>

<style scoped>
html, body {
  width: 100%;
  height: 100%;
  overflow: hidden;
  margin: 0;
  padding: 0;
  box-sizing: border-box;
  font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
  background: rgba(15, 23, 42, 0.5);
}

#app {
  width: 100%;
  height: 100%;
  overflow: hidden; /* 确保根应用容器也不滚动 */
  background: rgba(15, 23, 42, 0.5);
}

/* 应用容器 */
.app-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: rgba(15, 23, 42, 0.5);
  color: #e2e8f0;
}

/* 标题栏 */
.title-bar {
  background: rgba(15, 23, 42, 0.9);
  padding: 6px 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid rgba(71, 85, 105, 0.3);
  flex-shrink: 0;
  height: 36px;
}

.title-bar-content {
  display: flex;
  align-items: center;
  gap: 8px;
}

.logo {
  display: flex;
  align-items: center;
  justify-content: center;
  color: #60a5fa;
}

.title {
  font-weight: 600;
  font-size: 14px;
  letter-spacing: 0.5px;
  background: linear-gradient(90deg, #60a5fa 0%, #38bdf8 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.title-bar-controls {
  display: flex;
  gap: 4px;
}

.window-button {
  background: transparent;
  border: none;
  color: #94a3b8;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease;
}

.window-button:hover {
  background-color: rgba(100, 116, 139, 0.2);
}

.window-button.close:hover {
  background-color: #ef4444;
  color: white;
}

/* 主要内容区域 */
.main-content {
  display: flex;
  flex: 1;
  overflow: hidden;
  min-height: 0;
}

/* 侧边栏 */
.sidebar {
  width: 240px;
  background: rgba(15, 23, 42, 0.7);
  border-right: 1px solid rgba(71, 85, 105, 0.2);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

.sidebar-header {
  padding: 12px;
  border-bottom: 1px solid rgba(71, 85, 105, 0.2);
}

.sidebar-header h3 {
  margin: 0 0 10px 0;
  color: #f1f5f9;
  font-size: 14px;
  font-weight: 600;
}

.add-button {
  width: 100%;
  padding: 8px;
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  color: white;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: 12px;
  font-weight: 500;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  transition: all 0.2s ease;
}

.add-button:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 8px rgba(37, 99, 235, 0.3);
}

/* 挂载点列表 */
.mount-points-list {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.mount-point-item {
  padding: 10px;
  margin-bottom: 8px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s ease;
  background: rgba(30, 41, 59, 0.6);
  border: 1px solid rgba(71, 85, 105, 0.2);
  position: relative;
  overflow: hidden;
}

.mount-point-item::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  width: 3px;
  height: 100%;
  background: linear-gradient(to bottom, #3b82f6, #60a5fa);
  opacity: 0;
  transition: opacity 0.2s ease;
}

.mount-point-item:hover {
  background: rgba(30, 41, 59, 0.8);
  border-color: rgba(96, 165, 250, 0.4);
}

.mount-point-item:hover::before {
  opacity: 1;
}

.mount-point-item.selected {
  background: rgba(30, 58, 138, 0.3);
  border-color: rgba(96, 165, 250, 0.6);
}

.mount-point-item.selected::before {
  opacity: 1;
}

.mount-point-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 6px;
}

.mount-point-name {
  font-weight: 600;
  font-size: 13px;
  margin-bottom: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 140px;
}

.mount-status {
  font-size: 10px;
  padding: 3px 8px;
  border-radius: 10px;
  background-color: rgba(100, 116, 139, 0.3);
  font-weight: 500;
  flex-shrink: 0;
}

.mount-status.mounted {
  background: linear-gradient(135deg, #10b981 0%, #059669 100%);
  color: white;
}

.mount-point-path {
  font-size: 11px;
  color: #94a3b8;
  margin-bottom: 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 存储条 */
.mount-point-stats {
  margin-top: 8px;
}

.storage-bar {
  height: 4px;
  background-color: rgba(71, 85, 105, 0.3);
  border-radius: 2px;
  overflow: hidden;
  margin-bottom: 4px;
}

.storage-fill {
  height: 100%;
  background: linear-gradient(90deg, #3b82f6 0%, #60a5fa 100%);
  border-radius: 2px;
  transition: width 0.3s ease;
}

.storage-text {
  font-size: 10px;
  color: #94a3b8;
}

/* 空状态 */
.empty-state {
  text-align: center;
  padding: 40px 12px;
  color: #64748b;
}

.empty-icon {
  margin-bottom: 12px;
  opacity: 0.5;
}

.empty-text {
  font-size: 14px;
  margin-bottom: 6px;
  color: #cbd5e1;
}

.empty-subtext {
  font-size: 12px;
  color: #64748b;
}

/* 内容区域 */
.content-area {
  height: 100%;
  flex: 1;
  background: rgba(15, 23, 42, 0.5);
  overflow-y: auto;
  padding: 16px;
}

/* 配置容器 */
.config-container {
  max-width: 100%;
}

/* 配置头部 */
  .config-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 20px;
    padding-bottom: 16px;
    border-bottom: 1px solid rgba(71, 85, 105, 0.2);
  }
  
  .config-title {
    flex: 1;
    min-width: 0; /* 重要：允许flex子元素缩小 */
    margin-right: 16px;
  }
  
  .truncate-text {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
  }
  
  .action-buttons {
    flex-shrink: 0; /* 防止按钮区域被压缩 */
  }

.config-title h2 {
  color: #f8fafc;
  font-size: 18px;
  font-weight: 700;
  margin: 0 0 2px 0;
  background: linear-gradient(90deg, #f8fafc 0%, #cbd5e1 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.config-subtitle {
  color: #94a3b8;
  font-size: 12px;
}

.action-buttons {
  display: flex;
  gap: 8px;
}

.action-button {
  padding: 8px 12px;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: 12px;
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 6px;
  transition: all 0.2s ease;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
  flex-shrink: 0;
  min-width: 92px;
}

.action-button.primary {
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  color: white;
}

.action-button.primary:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow: 0 4px 8px rgba(37, 99, 235, 0.3);
}

.action-button.secondary {
  background: rgba(30, 41, 59, 0.7);
  color: #e2e8f0;
  border: 1px solid rgba(71, 85, 105, 0.3);
}

.action-button.secondary:hover {
  background: rgba(30, 41, 59, 0.9);
  border-color: rgba(96, 165, 250, 0.4);
}

.action-button.danger {
  background: linear-gradient(135deg, #ef4444 0%, #dc2626 100%);
  color: white;
}

.action-button.danger:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 8px rgba(220, 38, 38, 0.3);
}

.action-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
  transform: none;
  box-shadow: none;
}

/* 配置区域 */
.config-section {
  margin-bottom: 20px;
  background: rgba(15, 23, 42, 0.5);
  border-radius: 8px;
  padding: 16px;
  border: 1px solid rgba(71, 85, 105, 0.2);
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
}

.section-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid rgba(71, 85, 105, 0.2);
}

.section-header svg {
  color: #60a5fa;
  width: 16px;
  height: 16px;
}

.section-title {
  color: #f1f5f9;
  font-size: 14px;
  font-weight: 600;
  margin: 0;
}

/* 表单样式 */
.form-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 12px;
}

.form-group {
  display: flex;
  flex-direction: column;
}

.form-label {
  color: #e2e8f0;
  font-size: 12px;
  margin-bottom: 6px;
  font-weight: 500;
}

.form-input {
  padding: 8px 12px;
  background: rgba(30, 41, 59, 0.7);
  border: 1px solid rgba(71, 85, 105, 0.3);
  border-radius: 6px;
  color: #f1f5f9;
  font-size: 12px;
  transition: all 0.2s ease;
}

.form-input:focus {
  outline: none;
  border-color: #60a5fa;
  background: rgba(30, 41, 59, 0.9);
  box-shadow: 0 0 0 2px rgba(96, 165, 250, 0.2);
}

.form-input::placeholder {
  color: #64748b;
}

/* 带按钮的输入框 */
.input-with-button {
  display: flex;
  gap: 6px;
}

.input-with-button .form-input {
  flex: 1;
}

.browse-button {
  padding: 8px 12px;
  background: rgba(30, 41, 59, 0.7);
  border: 1px solid rgba(71, 85, 105, 0.3);
  border-radius: 6px;
  color: #e2e8f0;
  cursor: pointer;
  font-size: 12px;
  white-space: nowrap;
  transition: all 0.2s ease;
  flex-shrink: 0;
}

.browse-button:hover {
  background: rgba(30, 41, 59, 0.9);
  border-color: #60a5fa;
}

/* 复选框 */
.checkbox {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  user-select: none;
  padding: 6px 0;
}

.checkbox-input {
  width: 16px;
  height: 16px;
  cursor: pointer;
  accent-color: #3b82f6;
}

.checkbox-label {
  margin: 0;
  cursor: pointer;
  font-weight: 500;
  font-size: 12px;
}

/* 统计信息 */
.stats-section {
  background: rgba(15, 23, 42, 0.6);
  border-left: 3px solid #60a5fa;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 10px;
}

.stat-item {
  padding: 12px;
  background: rgba(30, 41, 59, 0.6);
  border-radius: 6px;
  border: 1px solid rgba(71, 85, 105, 0.2);
  transition: all 0.2s ease;
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.stat-item.highlight {
  background: linear-gradient(135deg, rgba(96, 165, 250, 0.1) 0%, rgba(59, 130, 246, 0.1) 100%);
  border-color: rgba(96, 165, 250, 0.4);
  grid-column: 1 / -1;
}

.stat-item.full-width {
  grid-column: 1 / -1;
}

.stat-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  background: rgba(96, 165, 250, 0.1);
  border-radius: 4px;
  color: #60a5fa;
  flex-shrink: 0;
}

.stat-content {
  flex: 1;
}

.stat-label {
  color: #94a3b8;
  font-size: 10px;
  margin-bottom: 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 500;
}

.stat-value {
  color: #f1f5f9;
  font-size: 12px;
  font-weight: 600;
  word-break: break-all;
}

.stat-item.highlight .stat-value {
  color: #60a5fa;
  font-size: 14px;
}

/* 标签页样式 */
.tabs {
  display: flex;
  margin-bottom: 16px;
  border-bottom: 1px solid rgba(71, 85, 105, 0.2);
}

.tab {
  padding: 8px 16px;
  background: transparent;
  border: none;
  color: #94a3b8;
  cursor: pointer;
  font-size: 12px;
  font-weight: 500;
  transition: all 0.2s ease;
  border-bottom: 2px solid transparent;
}

.tab.active {
  color: #60a5fa;
  border-bottom-color: #60a5fa;
}

.tab-content {
  display: block;
}

/* 未选择状态 */
.no-selection {
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  height: 100%;
  color: #64748b;
  text-align: center;
  padding: 20px;
}

.no-selection-icon {
  margin-bottom: 16px;
  opacity: 0.5;
}

.no-selection-title {
  font-size: 16px;
  color: #e2e8f0;
  margin-bottom: 8px;
  font-weight: 600;
}

.no-selection-subtitle {
  font-size: 12px;
  color: #94a3b8;
  max-width: 300px;
  line-height: 1.4;
}

/* 滚动条样式 */
::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

::-webkit-scrollbar-track {
  background: rgba(15, 23, 42, 0.5);
}

::-webkit-scrollbar-thumb {
  background: rgba(71, 85, 105, 0.5);
  border-radius: 3px;
}

::-webkit-scrollbar-thumb:hover {
  background: rgba(100, 116, 139, 0.7);
}

/* 响应式设计 */
@media (max-width: 800px) {
  .sidebar {
    width: 200px;
  }
  
  .action-buttons {
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  
  .action-button {
    flex: 1;
    min-width: 92px;
  }
}
</style>




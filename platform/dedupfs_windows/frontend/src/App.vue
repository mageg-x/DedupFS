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
          <div v-for="(item, index) in mountPoints" :key="item.id" @click="selectMountPoint(index)"
            class="mount-point-item" :class="{ 'selected': selectedIndex === index }">
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
              <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="#666" stroke-width="1"
                stroke-linecap="round" stroke-linejoin="round">
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
            <button v-for="tab in tabs" :key="tab.id" class="tab" :class="{ 'active': activeTab === tab.id }"
              @click="activeTab = tab.id">
              {{ tab.name }}
            </button>
          </div>

          <!-- 基本配置 -->
          <div v-show="activeTab === 'basic'" class="tab-content">
            <div class="config-header">
              <div class="config-title">
                <h2 class="truncate-text">{{ selectedMountPoint?.name || '挂载点配置' }}</h2>
                <div class="config-subtitle">管理您的去重文件系统设置</div>
              </div>
              <div class="action-buttons">
                <button :disabled="!canMount || isLoading" @click="handleMountAction" class="action-button primary">
                  <span v-if="isLoading" class="loading-indicator">
                    <svg class="loading-spinner" width="14" height="14" viewBox="0 0 24 24">
                      <circle class="loading-path" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3"
                        fill="none" stroke-dasharray="56.54866776461628" stroke-linecap="round"></circle>
                    </svg>
                    {{ selectedMountPoint?.isMounted ? '卸载中...' : '挂载中...' }}
                  </span>
                  <span v-else-if="!selectedMountPoint?.isMounted">
                    <svg width="14" height="14" viewBox="0 0 16 20" fill="currentColor">
                      <path d="M8 6v12M2 12h12" stroke="currentColor" stroke-width="2" stroke-linecap="round"></path>
                    </svg>
                    挂载
                  </span>
                  <span v-else>
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                      <line x1="4" y1="12" x2="12" y2="12" stroke="currentColor" stroke-width="2"
                        stroke-linecap="round"></line>
                    </svg>
                    卸载
                  </span>
                </button>
                <button @click="saveMountPoint" class="action-button secondary"
                  :disabled="selectedMountPoint.isMounted">
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                    <path d="M2 12.5V14a1 1 0 001 1h10a1 1 0 001-1v-1.5L8.5 8.5 2 15z" fill="none" stroke="currentColor"
                      stroke-width="1.5"></path>
                    <path d="M8 11a3 3 0 100-6 3 3 0 000 6z" fill="none" stroke="currentColor" stroke-width="1.5">
                    </path>
                  </svg>
                  保存配置
                </button>
                <button @click="deleteMountPoint" class="action-button danger" :disabled="selectedMountPoint.isMounted">
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                    <polyline points="3 5 5 5 11 5 13 5" stroke="currentColor" stroke-width="1.5"
                      stroke-linecap="round"></polyline>
                    <line x1="5" y1="5" x2="5" y2="11" stroke="currentColor" stroke-width="1.5"></line>
                    <line x1="11" y1="5" x2="11" y2="11" stroke="currentColor" stroke-width="1.5"></line>
                    <path d="M5 11h6v1a1 1 0 01-1 1H6a1 1 0 01-1-1v-1z" fill="none" stroke="currentColor"
                      stroke-width="1.5"></path>
                  </svg>
                  删除
                </button>
              </div>
            </div>

            <div class="config-section">
              <div class="section-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                  stroke-linecap="round" stroke-linejoin="round">
                  <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                  <line x1="9" y1="3" x2="9" y2="21"></line>
                </svg>
                <h3 class="section-title">基本配置</h3>
              </div>
              <div class="form-grid">
                <div class="form-group">
                  <label class="form-label">挂载点名称</label>
                  <input v-model="selectedMountPoint.name" class="form-input" placeholder="输入挂载点名称"
                    :readonly="selectedMountPoint.isMounted" :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>

                <div class="form-group">
                  <label class="form-label">挂载路径</label>
                  <div class="input-with-button">
                    <select v-model="selectedMountPoint.mountPath" class="form-input"
                      :disabled="selectedMountPoint.isMounted" :class="{ 'readonly': selectedMountPoint.isMounted }">
                      <option value="">请选择挂载盘符</option>
                      <option v-for="drive in availableDrives" :key="drive" :value="drive">{{ drive }}</option>
                    </select>
                  </div>
                </div>

                <div class="form-group">
                  <label class="form-label">数据目录</label>
                  <div class="input-with-button">
                    <input v-model="selectedMountPoint.dataDir" class="form-input" placeholder="输入数据目录"
                      :readonly="selectedMountPoint.isMounted" :class="{ 'readonly': selectedMountPoint.isMounted }">
                    <button @click="browseDataDir" class="browse-button"
                      :disabled="selectedMountPoint.isMounted">浏览...</button>
                  </div>
                </div>


              </div>
            </div>
          </div>

          <!-- 切片配置 -->
          <div v-show="activeTab === 'chunk'" class="tab-content">
            <div class="config-section">
              <div class="section-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                  stroke-linecap="round" stroke-linejoin="round">
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
                    <input v-model="selectedMountPoint.chunkConfig.fixedSize" type="checkbox" class="checkbox-input"
                      :disabled="selectedMountPoint.isMounted">
                    <span class="checkbox-label">固定长度切片</span>
                  </label>
                </div>

                <div class="form-group">
                  <label class="form-label">最小切片大小 (KB)</label>
                  <input v-model.number="selectedMountPoint.chunkConfig.minSize" type="number" class="form-input"
                    placeholder="输入最小切片大小" :readonly="selectedMountPoint.isMounted"
                    :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>

                <div class="form-group">
                  <label class="form-label">平均切片大小 (KB)</label>
                  <input v-model.number="selectedMountPoint.chunkConfig.avgSize" type="number" class="form-input"
                    placeholder="输入平均切片大小" :readonly="selectedMountPoint.isMounted"
                    :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>

                <div class="form-group">
                  <label class="form-label">最大切片大小 (KB)</label>
                  <input v-model.number="selectedMountPoint.chunkConfig.maxSize" type="number" class="form-input"
                    placeholder="输入最大切片大小" :readonly="selectedMountPoint.isMounted"
                    :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>
              </div>
            </div>
          </div>

          <!-- 块配置 -->
          <div v-show="activeTab === 'block'" class="tab-content">
            <div class="config-section">
              <div class="section-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                  stroke-linecap="round" stroke-linejoin="round">
                  <rect x="3" y="3" width="7" height="7"></rect>
                  <rect x="14" y="3" width="7" height="7"></rect>
                  <rect x="14" y="14" width="7" height="7"></rect>
                  <rect x="3" y="14" width="7" height="7"></rect>
                </svg>
                <h3 class="section-title">块配置</h3>
              </div>
              <div class="form-grid">
                <div class="form-group">
                  <label class="form-label">块大小 (MB)</label>
                  <input v-model.number="selectedMountPoint.blockConfig.size" type="number" class="form-input"
                    placeholder="输入块大小" :readonly="selectedMountPoint.isMounted"
                    :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>

                <div class="form-group">
                  <label class="form-label checkbox">
                    <input v-model="selectedMountPoint.blockConfig.compress" type="checkbox" class="checkbox-input"
                      :disabled="selectedMountPoint.isMounted">
                    <span class="checkbox-label">启用压缩</span>
                  </label>
                </div>

                <div class="form-group">
                  <label class="form-label checkbox">
                    <input v-model="selectedMountPoint.blockConfig.encrypt" type="checkbox" class="checkbox-input"
                      :disabled="selectedMountPoint.isMounted">
                    <span class="checkbox-label">启用加密</span>
                  </label>
                </div>

                <div class="form-group" v-if="selectedMountPoint?.blockConfig?.encrypt">
                  <label class="form-label">加密密码</label>
                  <input v-model="selectedMountPoint.blockConfig.password" type="password" class="form-input"
                    placeholder="输入加密密码" :readonly="selectedMountPoint.isMounted"
                    :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>
              </div>
            </div>
          </div>

          <!-- 统计信息 -->
          <div v-show="activeTab === 'stats'" class="tab-content">
            <div class="config-section stats-section">
              <div class="section-header">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                  stroke-linecap="round" stroke-linejoin="round">
                  <line x1="18" y1="20" x2="18" y2="10"></line>
                  <line x1="12" y1="20" x2="12" y2="4"></line>
                  <line x1="6" y1="20" x2="6" y2="14"></line>
                </svg>
                <h3 class="section-title">统计信息</h3>
              </div>
              <div class="stats-grid">
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <path
                        d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z">
                      </path>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">文件系统ID</div>
                    <div class="stat-value">{{ selectedMountPoint?.stats?.fsId || 'N/A' }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"></path>
                      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"></path>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">基础目录</div>
                    <div class="stat-value">{{ selectedMountPoint?.stats?.baseDir || 'N/A' }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                      <polyline points="14 2 14 8 20 8"></polyline>
                      <line x1="16" y1="13" x2="8" y2="13"></line>
                      <line x1="16" y1="17" x2="8" y2="17"></line>
                      <polyline points="10 9 9 9 8 9"></polyline>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">文件数量</div>
                    <div class="stat-value">{{ selectedMountPoint?.stats?.files || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">目录数量</div>
                    <div class="stat-value">{{ selectedMountPoint?.stats?.directories || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"></path>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">原始大小</div>
                    <div class="stat-value">{{ formatSize(selectedMountPoint?.stats?.spaceUsed) }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                      <circle cx="8.5" cy="8.5" r="1.5"></circle>
                      <polyline points="21 15 16 10 5 21"></polyline>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">实际大小</div>
                    <div class="stat-value">{{ formatSize(selectedMountPoint?.stats?.realSize || 0) }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <rect x="3" y="3" width="7" height="7"></rect>
                      <rect x="14" y="3" width="7" height="7"></rect>
                      <rect x="14" y="14" width="7" height="7"></rect>
                      <rect x="3" y="14" width="7" height="7"></rect>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">总切片数</div>
                    <div class="stat-value">{{ selectedMountPoint?.stats?.totalChunks || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                      <line x1="9" y1="3" x2="9" y2="21"></line>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">块数量</div>
                    <div class="stat-value">{{ selectedMountPoint?.stats?.blocks || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                      <circle cx="12" cy="12" r="3"></circle>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">引用的切片数</div>
                    <div class="stat-value">{{ selectedMountPoint?.stats?.referencedChunks || 0 }}</div>
                  </div>
                </div>
                <div class="stat-item">
                  <div class="stat-icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                      stroke-linecap="round" stroke-linejoin="round">
                      <polyline points="4 14 10 14 10 20"></polyline>
                      <polyline points="20 10 14 10 14 4"></polyline>
                    </svg>
                  </div>
                  <div class="stat-content">
                    <div class="stat-label">压缩比率</div>
                    <div class="stat-value">{{ (selectedMountPoint?.stats?.compressionRatio || 0).toFixed(2) }}x</div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 未选择状态 -->
        <div v-else class="no-selection">
          <div class="no-selection-icon">
            <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="#666" stroke-width="1"
              stroke-linecap="round" stroke-linejoin="round">
              <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
              <path d="M16 3v4M8 3v4M3 16h4M3 8h4M16 21v-4M8 21v-4M21 16h-4M21 8h-4"></path>
            </svg>
          </div>
          <div class="no-selection-title">请从左侧选择一个挂载点</div>
          <div class="no-selection-subtitle">选择挂载点后可以查看和编辑其配置</div>
        </div>
      </div>
    </div>
    <!-- 通知组件 -->
    <div class="notifications-container">
      <div v-for="notification in notifications" :key="notification.id"
        :class="['notification', `notification-${notification.type}`]">
        <span class="notification-content">{{ truncateText(notification.message, 100) }}</span>
        <button class="notification-close" @click="removeNotification(notification.id)" tabindex="0">
          ×
        </button>
      </div>
    </div>

    <!-- 自定义对话框 -->
    <div v-if="dialog.visible" class="dialog-overlay" @click="handleDialogCancel">
      <div class="dialog-content" @click.stop>
        <div class="dialog-header">
          <h3 class="dialog-title">{{ dialog.title }}</h3>
        </div>
        <div class="dialog-body">
          <p class="dialog-message">{{ dialog.message }}</p>
        </div>
        <div class="dialog-footer">
          <button class="dialog-button cancel-button" @click="handleDialogCancel">
            {{ dialog.cancelText }}
          </button>
          <button class="dialog-button confirm-button" @click="handleDialogConfirm">
            {{ dialog.confirmText }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'

// 挂载点相关状态
const mountPoints = ref([])
const selectedIndex = ref(-1)
const selectedMountPoint = ref(null)
const isLoading = ref(false)
const activeTab = ref('basic')
const tabs = ref([
  { id: 'basic', name: '基本配置' },
  { id: 'chunk', name: '切片配置' },
  { id: 'block', name: '块配置' },
  { id: 'stats', name: '统计信息' }
])
const availableDrives = ref(
  Array.from({ length: 23 }, (_, i) => String.fromCharCode(68 + i) + ':')
)

// 计算属性
const canMount = computed(() => {
  return selectedMountPoint.value &&
    selectedMountPoint.value.mountPath &&
    selectedMountPoint.value.dataDir
})

// 方法定义
// 加载挂载点统计信息
const loadMountPointStats = async () => {
  if (!selectedMountPoint.value || !selectedMountPoint.value.isMounted) return
  
  try {
    const stats = await window.go.main.App.Stats(selectedMountPoint.value.id)
    if (stats) {
      // 更新选中挂载点的统计信息
      selectedMountPoint.value.stats = stats
      
      // 同时更新mountPoints数组中的对应项
      const index = mountPoints.value.findIndex(mp => mp.id === selectedMountPoint.value.id)
      if (index !== -1) {
        mountPoints.value[index].stats = stats
      }
    }
  } catch (error) {
      console.error('获取统计信息失败:', error)
      // 静默失败，不显示通知以避免打扰用户
  }
}

// 监听activeTab变化，切换到统计信息标签时加载最新统计数据
watch(activeTab, (newTab, oldTab) => {
  if (newTab === 'stats') {
    loadMountPointStats()
  }
})

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
  try {
    let newMountPoint = await window.go.main.App.CreateDefaultConfig()
    // 看看 mountPoints 中是否已经存在同名的挂载点
    if (mountPoints.value.some(mp => mp.name === newMountPoint.name)) {
      return
    }
    // 添加到 mountPoints 中
    mountPoints.value.push(newMountPoint)
    selectMountPoint(mountPoints.value.length - 1)
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
  if (selectedIndex.value < 0) return

  showConfirmDialog('确认删除', '确定要删除此挂载点吗？',
    async () => {
      // 确认删除后的逻辑
      try {
        // 调用后端DeleteMountPoint方法
        await window.go.main.App.DeleteMountPoint(selectedMountPoint.value.id)
        // 刷新挂载点列表
        await loadMountPoints()
        // 清空选中状态
        selectedMountPoint.value = null
        selectedIndex.value = -1
        // 显示删除成功通知
        showNotification('删除成功', 'success')
      } catch (error) {
        // 显示删除失败通知
        showNotification(`删除失败: ${error.message}`, 'error')
      }
    }
  )
}

const handleMountAction = async () => {
  if (!selectedMountPoint.value) return
  let result = selectedMountPoint.value.isMounted ? '卸载' : '挂载'

  // 记录开始时间
  const startTime = Date.now()

  try {
    // 设置加载状态
    isLoading.value = true

    // 修复逻辑：isMounted为true时应调用Unmount，否则调用Mount
    if (selectedMountPoint.value.isMounted) {
      // 调用后端Unmount方法
      await window.go.main.App.Unmount(selectedMountPoint.value.id)
    } else {
      await window.go.main.App.AddMountPoint(selectedMountPoint.value)
      // 调用后端Mount方法
      await window.go.main.App.Mount(selectedMountPoint.value.id)
    }

    // 计算已用时间
    const elapsedTime = Date.now() - startTime
    // 确保至少等待3秒
    if (elapsedTime < 3000) {
      await new Promise(resolve => setTimeout(resolve, 3000 - elapsedTime))
    }

    // 重新加载挂载点列表以更新状态
    await loadMountPoints()
    // 重新选中当前挂载点
    selectMountPoint(selectedIndex.value)
    showNotification(result + "成功", 'success')
  } catch (error) {
    showNotification(result + '失败', 'error')
    console.error(result + '失败', error)
  } finally {
    // 清除加载状态
    isLoading.value = false
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
  return Math.min((mountPoint.UsedSpace / mountPoint.totalSpace) * 100, 100)
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


// 对话框状态
const dialog = ref({
  visible: false,
  title: '',
  message: '',
  confirmText: '确定',
  cancelText: '取消',
  onConfirm: null,
  onCancel: null
})

// 显示确认对话框
const showConfirmDialog = (title, message, onConfirm, onCancel = null, confirmText = '确定', cancelText = '取消') => {
  dialog.value = {
    visible: true,
    title,
    message,
    confirmText,
    cancelText,
    onConfirm,
    onCancel
  }
}

// 处理对话框确认
const handleDialogConfirm = () => {
  if (dialog.value.onConfirm && typeof dialog.value.onConfirm === 'function') {
    dialog.value.onConfirm()
  }
  dialog.value.visible = false
}

// 处理对话框取消
const handleDialogCancel = () => {
  if (dialog.value.onCancel && typeof dialog.value.onCancel === 'function') {
    dialog.value.onCancel()
  }
  dialog.value.visible = false
}

// 通知列表
const notifications = ref([])

// 移除通知的函数
const removeNotification = (id) => {
  const index = notifications.value.findIndex(n => n.id === id)
  if (index > -1) {
    notifications.value.splice(index, 1)
  }
}

// 截断文本的函数
const truncateText = (text, maxLength) => {
  if (!text) return ''
  return text.length > maxLength ? text.substring(0, maxLength) + '...' : text
}

const showNotification = (message, type = 'info') => {
  // 创建通知对象
  const id = Date.now()
  notifications.value.push({
    id,
    message,
    type
  })

  // 3秒后自动关闭通知
  setTimeout(() => {
    removeNotification(id)
  }, 3000)

  console.log(`[${type.toUpperCase()}] ${message}`)
}

const checkMountPointStatus = async () => {
  // 启动定时检查挂载点状态的定时器
  const statusCheckInterval = setInterval(async () => {
    try {
      // 遍历所有挂载点
      for (let i = 0; i < mountPoints.value.length; i++) {
        const currentMP = mountPoints.value[i]
        // 调用后端GetMountPoint方法获取最新状态
        const updatedMP = await window.go.main.App.GetMountPoint(currentMP.id)

        // 更新挂载点状态
        if (updatedMP && updatedMP.isMounted !== currentMP.isMounted) {
          // 直接更新isMounted属性
          currentMP.isMounted = updatedMP.isMounted

          // 如果当前选中的是这个挂载点，也要更新selectedMountPoint
          if (selectedIndex.value === i) {
            selectedMountPoint.value.isMounted = updatedMP.isMounted
          }
        }
      }
    } catch (error) {
      console.error('定时检查挂载点状态失败:', error)
    }
  }, 1000) // 每秒检查一次
}

// 组件挂载后从后端加载挂载点数据
onMounted(async () => {
  loadMountPoints()
  checkMountPointStatus()
})

// 组件卸载时清理定时器
onUnmounted(() => {
  clearInterval(statusCheckInterval)
})
</script>

<style scoped>
html,
body {
  width: 100%;
  height: 100%;
  margin: 0;
  padding: 0;
  box-sizing: border-box;
  font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
  background: rgba(15, 23, 42, 0.5);
}

#app {
  width: 100%;
  height: 100%;
  overflow: auto;
  /* 确保根应用容器也不滚动 */
  background: rgba(15, 23, 42, 0.5);
}

/* 只读状态样式 */
.form-input.readonly,
select.form-input.readonly {
  background-color: #1e293b;
  color: #94a3b8;
  cursor: not-allowed;
  border-color: #334155;
}

/* 禁用状态增强样式 */
.form-input[readonly],
select.form-input:disabled,
.checkbox-input:disabled+.checkbox-label {
  cursor: not-allowed;
}

.checkbox-input:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

/* 按钮禁用状态增强 */
.action-button:disabled,
.browse-button:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

/* 应用容器 */
.app-container {
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: #0f172a;
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
  color: #60a5fa;
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
  background: #0f172a;
}

/* 通知组件样式 */
.notifications-container {
  position: fixed;
  top: 20px;
  right: 20px;
  z-index: 1000;
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-width: 350px;
}

.notification {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 6px;
  border-radius: 8px;
  backdrop-filter: blur(12px);
  color: #e2e8f0;
  font-size: 12px;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.3);
  opacity: 0;
  transform: translateX(100%);
  animation: slideIn 0.3s ease-out forwards;
}

@keyframes slideIn {
  to {
    opacity: 1;
    transform: translateX(0);
  }
}

.notification-content {
  flex: 1;
  margin: 0px 4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 250px;
}

.notification-close {
  background: transparent;
  border: none;
  color: inherit;
  font-size: 20px;
  cursor: pointer;
  padding: 4px;
  margin: 0;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  transition: background-color 0.2s;
  user-select: none;
  -webkit-user-select: none;
  outline: none;
}

.notification-close:hover,
.notification-close:focus {
  background-color: rgba(255, 255, 255, 0.15);
}

/* 对话框样式 */
.dialog-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(2px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 2000;
}

.dialog-content {
  background: rgba(15, 23, 42, 0.95);
  border: 1px solid rgba(71, 85, 105, 0.3);
  border-radius: 8px;
  padding: 20px;
  width: 400px;
  max-width: 90%;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.4);
  color: #e2e8f0;
}

.dialog-header {
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid rgba(71, 85, 105, 0.2);
}

.dialog-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: #f1f5f9;
}

.dialog-body {
  margin-bottom: 20px;
}

.dialog-message {
  margin: 0;
  color: #cbd5e1;
  font-size: 14px;
  line-height: 1.5;
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

.dialog-button {
  padding: 8px 16px;
  border: none;
  border-radius: 4px;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
  font-weight: 500;
}

.cancel-button {
  background: transparent;
  color: #94a3b8;
  border: 1px solid rgba(148, 163, 184, 0.3);
}

.cancel-button:hover {
  background: rgba(148, 163, 184, 0.1);
  color: #cbd5e1;
}

.confirm-button {
  background: rgba(37, 99, 235, 0.8);
  color: white;
  border: 1px solid rgba(96, 165, 250, 0.3);
}

.confirm-button:hover {
  background: rgba(59, 130, 246, 0.8);
  border-color: rgba(96, 165, 250, 0.5);
}

/* 不同类型通知的样式 */
.notification-info {
  background: rgba(37, 99, 235, 0.9);
  border: 1px solid rgba(96, 165, 250, 0.4);
}

.notification-success {
  background: rgba(16, 185, 129, 0.9);
  border: 1px solid rgba(52, 211, 153, 0.4);
}

.notification-warning {
  background: rgba(245, 158, 11, 0.9);
  border: 1px solid rgba(251, 191, 36, 0.4);
}

.notification-error {
  background: rgba(220, 38, 38, 0.9);
  border: 1px solid rgba(239, 68, 68, 0.4);
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
  background: #3b82f6;
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
  background: #10b981;
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
  background: #3b82f6;
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
  min-width: 0;
  /* 重要：允许flex子元素缩小 */
  margin-right: 16px;
}

.truncate-text {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}

.action-buttons {
  flex-shrink: 0;
  /* 防止按钮区域被压缩 */
}

.config-title h2 {
  color: #f8fafc;
  font-size: 18px;
  font-weight: 700;
  margin: 0 0 2px 0;
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
  justify-content: center;
  gap: 6px;
  transition: all 0.2s ease;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
  flex-shrink: 0;
  min-width: 92px;
}

.action-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
  transform: none;
  box-shadow: none;
}

/* 加载动画样式 */
.loading-indicator {
  display: flex;
  align-items: center;
  gap: 6px;
}

.loading-spinner {
  animation: spin 1s linear infinite;
}

.loading-path {
  stroke-dashoffset: 14;
}

@keyframes spin {
  0% {
    transform: rotate(0deg);
  }

  100% {
    transform: rotate(360deg);
  }
}

.action-button.primary {
  background: #3b82f6;
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
  background: #ef4444;
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

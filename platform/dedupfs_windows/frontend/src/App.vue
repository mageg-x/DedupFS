<template>
  <div id="app" class="app-container">
    <!-- 主要内容区域 -->
    <div class="main-content">
      <!-- 左侧挂载点列表 -->
      <div class="sidebar">
        <div class="sidebar-header">
          <h3>{{ t('mountPointList') }}</h3>
          <button @click="addMountPoint" class="add-button">
            <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
              <path d="M8 2v12M2 8h12" stroke="currentColor" stroke-width="2" stroke-linecap="round"></path>
            </svg>
            <span>{{ t('addMountPoint') }}</span>
          </button>
        </div>

        <div class="mount-points-list">
          <div v-for="(item, index) in mountPoints" :key="item.id" @click="selectMountPoint(index)"
            class="mount-point-item" :class="{ 'selected': selectedIndex === index }">
            <div class="mount-point-header">
              <div class="mount-point-name">{{ item.name || t('unnamed') }}</div>
              <div class="mount-status" :class="{ 'mounted': item.isMounted }">
                {{ item.isMounted ? t('mounted') : t('notMounted') }}
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
            <div class="empty-text">{{ t('noMountPoints') }}</div>
            <div class="empty-subtext">{{ t('clickAddButton') }}</div>
          </div>
        </div>
      </div>

      <!-- 右侧配置区域 -->
      <div class="content-area">
        <div v-if="selectedMountPoint" class="config-container">
          <!-- 标签页导航 -->
          <div class="tabs-container">
            <div class="tabs">
              <button v-for="tab in tabs" :key="tab.id" class="tab" :class="{ 'active': activeTab === tab.id }"
                @click="activeTab = tab.id">
                {{ tab.name }}
              </button>
            </div>

            <!-- 右侧功能图标 -->
            <div class="header-actions">
              <!-- 语言切换下拉菜单 -->
              <div class="dropdown">
                <button @click="showLanguageDropdown = !showLanguageDropdown" class="dropdown-toggle">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                    stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="12" r="10"></circle>
                    <line x1="2" y1="12" x2="22" y2="12"></line>
                    <path
                      d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z">
                    </path>
                  </svg>
                </button>
                <div v-if="showLanguageDropdown" class="dropdown-menu">
                  <div v-for="lang in languages" :key="lang.code" @click="changeLanguage(lang.code)"
                    class="dropdown-item">
                    <span class="flag-icon">{{ lang.flag }}</span>
                    <span>{{ lang.name }}</span>
                  </div>
                </div>
              </div>

              <!-- GitHub 链接 -->
              <a href="#" class="github-link" @click.prevent="openGitHub">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                  stroke-linecap="round" stroke-linejoin="round">
                  <path
                    d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.87a3.37 3.37 0 0 0-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0 0 20 4.77 5.07 5.07 0 0 0 19.91 1S18.73.65 16 2.48a13.38 13.38 0 0 0-7 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 0 0 5 4.77a5.44 5.44 0 0 0-1.5 3.78c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 0 0 9 18.13V22">
                  </path>
                </svg>
              </a>
            </div>
          </div>

          <!-- 基本配置 -->
          <div v-show="activeTab === 'basic'" class="tab-content">
            <div class="config-header">
              <div class="config-title">
                <h2 class="truncate-text">{{ selectedMountPoint?.name || t('mountPointConfig') }}</h2>
                <div class="config-subtitle">{{ t('manageDedupFSSettings') }}</div>
              </div>
              <div class="action-buttons">
                <button :disabled="!canMount || isLoading" @click="handleMountAction" class="action-button primary">
                  <span v-if="isLoading" class="loading-indicator">
                    <svg class="loading-spinner" width="14" height="14" viewBox="0 0 24 24">
                      <circle class="loading-path" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3"
                        fill="none" stroke-dasharray="56.54866776461628" stroke-linecap="round"></circle>
                    </svg>
                    {{ selectedMountPoint?.isMounted ? t('unmounting') : t('mounting') }}
                  </span>
                  <span v-else-if="!selectedMountPoint?.isMounted">
                    <svg width="14" height="14" viewBox="0 0 16 20" fill="currentColor">
                      <path d="M8 6v12M2 12h12" stroke="currentColor" stroke-width="2" stroke-linecap="round"></path>
                    </svg>
                    {{ t('mount') }}
                  </span>
                  <span v-else>
                    <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
                      <line x1="4" y1="12" x2="12" y2="12" stroke="currentColor" stroke-width="2"
                        stroke-linecap="round"></line>
                    </svg>
                    {{ t('unmount') }}
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
                  {{ t('saveConfig') }}
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
                  {{ t('delete') }}
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
                <h3 class="section-title">{{ t('mountPointConfig') }}</h3>
              </div>
              <div class="form-grid">
                <div class="form-group">
                  <label class="form-label">{{ t('mountPointName') }}</label>
                  <input v-model="selectedMountPoint.name" class="form-input"
                    placeholder="{{ t('inputMountPointName') }}" :readonly="selectedMountPoint.isMounted"
                    :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>

                <div class="form-group">
                  <label class="form-label">{{ t('mountPath') }}</label>
                  <div class="input-with-button">
                    <select v-model="selectedMountPoint.mountPath" class="form-input"
                      :disabled="selectedMountPoint.isMounted" :class="{ 'readonly': selectedMountPoint.isMounted }">
                      <option value="">{{ t('selectDriveLetter') }}</option>
                      <option v-for="drive in availableDrives" :key="drive" :value="drive">{{ drive }}</option>
                    </select>
                  </div>
                </div>

                <div class="form-group">
                  <label class="form-label">{{ t('dataDir') }}</label>
                  <div class="input-with-button">
                    <input v-model="selectedMountPoint.dataDir" class="form-input" placeholder="{{ t('inputDataDir') }}"
                      :readonly="selectedMountPoint.isMounted" :class="{ 'readonly': selectedMountPoint.isMounted }">
                    <button @click="browseDataDir" class="browse-button" :disabled="selectedMountPoint.isMounted">{{
                      t('browse') }}</button>
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
                <h3 class="section-title">{{ t('chunkConfig') }}</h3>
              </div>
              <div class="form-grid">
                <div class="form-group">
                  <label class="form-label checkbox">
                    <input v-model="selectedMountPoint.chunkConfig.fixedSize" type="checkbox" class="checkbox-input"
                      :disabled="selectedMountPoint.isMounted">
                    <span class="checkbox-label">{{ t('fixedSizeChunking') }}</span>
                  </label>
                </div>

                <div class="form-group">
                  <label class="form-label">{{ t('minChunkSize') }}</label>
                  <input v-model.number="selectedMountPoint.chunkConfig.minSize" type="number" class="form-input"
                    placeholder="{{ t('inputMinChunkSize') }}" :readonly="selectedMountPoint.isMounted"
                    :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>

                <div class="form-group">
                  <label class="form-label">{{ t('avgChunkSize') }}</label>
                  <input v-model.number="selectedMountPoint.chunkConfig.avgSize" type="number" class="form-input"
                    placeholder="{{ t('inputAvgChunkSize') }}" :readonly="selectedMountPoint.isMounted"
                    :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>

                <div class="form-group">
                  <label class="form-label">{{ t('maxChunkSize') }}</label>
                  <input v-model.number="selectedMountPoint.chunkConfig.maxSize" type="number" class="form-input"
                    placeholder="{{ t('inputMaxChunkSize') }}" :readonly="selectedMountPoint.isMounted"
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
                <h3 class="section-title">{{ t('blockConfig') }}</h3>
              </div>
              <div class="form-grid">
                <div class="form-group">
                  <label class="form-label">{{ t('blockSize') }}</label>
                  <input v-model.number="selectedMountPoint.blockConfig.size" type="number" class="form-input"
                    placeholder="{{ t('inputBlockSize') }}" :readonly="selectedMountPoint.isMounted"
                    :class="{ 'readonly': selectedMountPoint.isMounted }">
                </div>

                <div class="form-group">
                  <label class="form-label checkbox">
                    <input v-model="selectedMountPoint.blockConfig.compress" type="checkbox" class="checkbox-input"
                      :disabled="selectedMountPoint.isMounted">
                    <span class="checkbox-label">{{ t('enableCompression') }}</span>
                  </label>
                </div>

                <div class="form-group">
                  <label class="form-label checkbox">
                    <input v-model="selectedMountPoint.blockConfig.encrypt" type="checkbox" class="checkbox-input"
                      :disabled="selectedMountPoint.isMounted">
                    <span class="checkbox-label">{{ t('enableEncryption') }}</span>
                  </label>
                </div>

                <div class="form-group" v-if="selectedMountPoint?.blockConfig?.encrypt">
                  <label class="form-label">{{ t('encryptionPassword') }}</label>
                  <input v-model="selectedMountPoint.blockConfig.password" type="password" class="form-input"
                    placeholder="{{ t('inputEncryptionPassword') }}" :readonly="selectedMountPoint.isMounted"
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
                <h3 class="section-title">{{ t('statsInfo') }}</h3>
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
                    <div class="stat-label">{{ t('fileSystemId') }}</div>
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
                    <div class="stat-label">{{ t('baseDir') }}</div>
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
                    <div class="stat-label">{{ t('fileCount') }}</div>
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
                    <div class="stat-label">{{ t('directoryCount') }}</div>
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
                    <div class="stat-label">{{ t('originalSize') }}</div>
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
                    <div class="stat-label">{{ t('actualSize') }}</div>
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
                    <div class="stat-label">{{ t('totalChunks') }}</div>
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
                    <div class="stat-label">{{ t('blockCount') }}</div>
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
                    <div class="stat-label">{{ t('referencedChunks') }}</div>
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
                    <div class="stat-label">{{ t('compressionRatio') }}</div>
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

// ==================== 国际化相关 ====================
const currentLang = ref('zh')
const showLanguageDropdown = ref(false)

const languages = [
  { code: 'zh', name: '中文', flag: '🇨🇳' },
  { code: 'en', name: 'English', flag: '🇺🇸' }
]

const messages = {
  zh: {
    // 通用
    confirm: '确定',
    cancel: '取消',
    success: '成功',
    error: '失败',
    warning: '警告',
    info: '信息',

    // 挂载点管理
    mountPointList: '挂载点列表',
    addMountPoint: '添加挂载点',
    unnamed: '未命名',
    mounted: '已挂载',
    notMounted: '未挂载',
    noMountPoints: '暂无挂载点',
    clickAddButton: '点击上方按钮添加新的挂载点',

    // 标签页
    basicConfig: '基本配置',
    chunkConfig: '切片配置',
    blockConfig: '块配置',
    statsInfo: '统计信息',

    // 基本配置
    mountPointConfig: '挂载点配置',
    manageDedupFSSettings: '管理您的去重文件系统设置',
    mounting: '挂载中...',
    unmounting: '卸载中...',
    mount: '挂载',
    unmount: '卸载',
    saveConfig: '保存配置',
    delete: '删除',
    mountPointName: '挂载点名称',
    inputMountPointName: '输入挂载点名称',
    mountPath: '挂载路径',
    selectDriveLetter: '请选择挂载盘符',
    dataDir: '数据目录',
    inputDataDir: '输入数据目录',
    browse: '浏览...',

    // 切片配置
    fixedSizeChunking: '固定长度切片',
    minChunkSize: '最小切片大小 (KB)',
    inputMinChunkSize: '输入最小切片大小',
    avgChunkSize: '平均切片大小 (KB)',
    inputAvgChunkSize: '输入平均切片大小',
    maxChunkSize: '最大切片大小 (KB)',
    inputMaxChunkSize: '输入最大切片大小',

    // 块配置
    blockSize: '块大小 (MB)',
    inputBlockSize: '输入块大小',
    enableCompression: '启用压缩',
    enableEncryption: '启用加密',
    encryptionPassword: '加密密码',
    inputEncryptionPassword: '输入加密密码',

    // 统计信息
    fileSystemId: '文件系统ID',
    baseDir: '基础目录',
    fileCount: '文件数量',
    directoryCount: '目录数量',
    originalSize: '原始大小',
    actualSize: '实际大小',
    totalChunks: '总切片数',
    blockCount: '块数量',
    referencedChunks: '引用的切片数',
    compressionRatio: '压缩比率',

    // 通知和对话框
    configSaved: '配置已保存',
    confirmDelete: '确认删除',
    confirmDeleteMountPoint: '确定要删除此挂载点吗？',
    mountSuccess: '挂载成功',
    unmountSuccess: '卸载成功',
    mountFailed: '挂载失败',
    unmountFailed: '卸载失败',
    loadMountPointsFailed: '加载挂载点失败',
    addMountPointFailed: '添加挂载点失败',
    saveConfigFailed: '保存配置失败',
    deleteFailed: '删除失败',
    pleaseSelectOrCreateMountPoint: '请先选择或创建挂载点',
    selectDataDir: '选择数据目录',
    selectDirFailed: '选择目录失败',

    // 语言切换
    language: '语言',
    chinese: '中文',
    english: '英文'
  },
  en: {
    // Common
    confirm: 'Confirm',
    cancel: 'Cancel',
    success: 'Success',
    error: 'Error',
    warning: 'Warning',
    info: 'Info',

    // Mount Point Management
    mountPointList: 'Mount Points',
    addMountPoint: 'Add Mount Point',
    unnamed: 'Unnamed',
    mounted: 'Mounted',
    notMounted: 'Not Mounted',
    noMountPoints: 'No Mount Points',
    clickAddButton: 'Click the button above to add a new mount point',

    // Tabs
    basicConfig: 'Basic Config',
    chunkConfig: 'Chunk Config',
    blockConfig: 'Block Config',
    statsInfo: 'Statistics',

    // Basic Config
    mountPointConfig: 'Mount Point Config',
    manageDedupFSSettings: 'Manage your deduplicated file system settings',
    mounting: 'Mounting...',
    unmounting: 'Unmounting...',
    mount: 'Mount',
    unmount: 'Unmount',
    saveConfig: 'Save',
    delete: 'Delete',
    mountPointName: 'Mount Point Name',
    inputMountPointName: 'Enter mount point name',
    mountPath: 'Mount Path',
    selectDriveLetter: 'Select drive letter',
    dataDir: 'Data Directory',
    inputDataDir: 'Enter data directory',
    browse: 'Browse...',

    // Chunk Config
    fixedSizeChunking: 'Fixed Size Chunking',
    minChunkSize: 'Minimum Chunk Size (KB)',
    inputMinChunkSize: 'Enter minimum chunk size',
    avgChunkSize: 'Average Chunk Size (KB)',
    inputAvgChunkSize: 'Enter average chunk size',
    maxChunkSize: 'Maximum Chunk Size (KB)',
    inputMaxChunkSize: 'Enter maximum chunk size',

    // Block Config
    blockSize: 'Block Size (MB)',
    inputBlockSize: 'Enter block size',
    enableCompression: 'Enable Compression',
    enableEncryption: 'Enable Encryption',
    encryptionPassword: 'Encryption Password',
    inputEncryptionPassword: 'Enter encryption password',

    // Statistics
    fileSystemId: 'File System ID',
    baseDir: 'Base Directory',
    fileCount: 'File Count',
    directoryCount: 'Directory Count',
    originalSize: 'Original Size',
    actualSize: 'Actual Size',
    totalChunks: 'Total Chunks',
    blockCount: 'Block Count',
    referencedChunks: 'Referenced Chunks',
    compressionRatio: 'Compression Ratio',

    // Notifications and Dialogs
    configSaved: 'Configuration saved',
    confirmDelete: 'Confirm Delete',
    confirmDeleteMountPoint: 'Are you sure you want to delete this mount point?',
    mountSuccess: 'Mount successful',
    unmountSuccess: 'Unmount successful',
    mountFailed: 'Mount failed',
    unmountFailed: 'Unmount failed',
    loadMountPointsFailed: 'Failed to load mount points',
    addMountPointFailed: 'Failed to add mount point',
    saveConfigFailed: 'Failed to save configuration',
    deleteFailed: 'Deletion failed',
    pleaseSelectOrCreateMountPoint: 'Please select or create a mount point first',
    selectDataDir: 'Select Data Directory',
    selectDirFailed: 'Failed to select directory',

    // Language Switch
    language: 'Language',
    chinese: '中文',
    english: 'English'
  }
}

const t = (key) => {
  return messages[currentLang.value][key] || key
}

const changeLanguage = (lang) => {
  currentLang.value = lang
  showLanguageDropdown.value = false
  updateTabNames()
}

// ==================== 挂载点数据相关 ====================
const mountPoints = ref([])
const selectedIndex = ref(-1)
const selectedMountPoint = ref(null)
const availableDrives = ref(
  Array.from({ length: 23 }, (_, i) => String.fromCharCode(68 + i) + ':')
)

const loadMountPoints = async () => {
  try {
    mountPoints.value = await window.go.main.App.GetMountPoints()
  } catch (error) {
    showNotification(t('loadMountPointsFailed') + ': ' + error.message, 'error')
    console.error('加载挂载点失败:', error)
  }
}

const selectMountPoint = (index) => {
  selectedIndex.value = index
  selectedMountPoint.value = JSON.parse(JSON.stringify(mountPoints.value[index]))
  activeTab.value = 'basic'
}

const addMountPoint = async () => {
  try {
    let newMountPoint = await window.go.main.App.CreateDefaultConfig()
    newMountPoint.name = t('unnamed')
    if (mountPoints.value.some(mp => mp.name === newMountPoint.name)) {
      return
    }
    mountPoints.value.push(newMountPoint)
    selectMountPoint(mountPoints.value.length - 1)
  } catch (error) {
    showNotification(t('addMountPointFailed') + ': ' + error.message, 'error')
    console.error('添加挂载点失败:', error)
  }
}

const deleteMountPoint = async () => {
  if (selectedIndex.value < 0) return

  showConfirmDialog(t('confirmDelete'), t('confirmDeleteMountPoint'),
    async () => {
      try {
        await window.go.main.App.DeleteMountPoint(selectedMountPoint.value.id)
        await loadMountPoints()
        selectedMountPoint.value = null
        selectedIndex.value = -1
        showNotification(t('success'), 'success')
      } catch (error) {
        showNotification(t('deleteFailed') + ': ' + error.message, 'error')
      }
    }
  )
}

// ==================== 挂载/卸载相关 ====================
const isLoading = ref(false)
const canMount = computed(() => {
  return selectedMountPoint.value &&
    selectedMountPoint.value.mountPath &&
    selectedMountPoint.value.dataDir
})

const handleMountAction = async () => {
  if (!selectedMountPoint.value) return
  let result = selectedMountPoint.value.isMounted ? t('unmount') : t('mount')

  const startTime = Date.now()
  try {
    isLoading.value = true

    if (selectedMountPoint.value.isMounted) {
      await window.go.main.App.Unmount(selectedMountPoint.value.id)
    } else {
      await window.go.main.App.AddMountPoint(selectedMountPoint.value)
      await window.go.main.App.Mount(selectedMountPoint.value.id)
    }

    const elapsedTime = Date.now() - startTime
    if (elapsedTime < 3000) {
      await new Promise(resolve => setTimeout(resolve, 3000 - elapsedTime))
    }

    await loadMountPoints()
    selectMountPoint(selectedIndex.value)
    showNotification(result + t('success'), 'success')
  } catch (error) {
    showNotification(result + t('error'), 'error')
    console.error(result + t('error'), error)
  } finally {
    isLoading.value = false
  }
}

// ==================== 配置保存相关 ====================
const saveMountPoint = async () => {
  if (!selectedMountPoint.value) return

  try {
    await window.go.main.App.SaveMountPoint(selectedMountPoint.value)
    await loadMountPoints()
    selectMountPoint(selectedIndex.value)
    showNotification(t('configSaved'), 'success')
  } catch (error) {
    showNotification(t('saveConfigFailed') + ': ' + error.message, 'error')
    console.error('保存配置失败:', error)
  }
}

const browseDataDir = async () => {
  if (!selectedMountPoint.value) {
    showNotification(t('pleaseSelectOrCreateMountPoint'), 'warning')
    return
  }

  try {
    const dir = await window.go.main.App.BrowseDataDir(t('selectDataDir'), selectedMountPoint.value.dataDir || '')
    if (dir) {
      selectedMountPoint.value.dataDir = dir
    }
  } catch (error) {
    showNotification(t('selectDirFailed') + ': ' + error.message, 'error')
    console.error('选择目录失败:', error)
  }
}

// ==================== 标签页相关 ====================
const activeTab = ref('basic')
const tabs = ref([
  { id: 'basic', name: t('basicConfig') },
  { id: 'chunk', name: t('chunkConfig') },
  { id: 'block', name: t('blockConfig') },
  { id: 'stats', name: t('statsInfo') }
])

const updateTabNames = () => {
  tabs.value = [
    { id: 'basic', name: t('basicConfig') },
    { id: 'chunk', name: t('chunkConfig') },
    { id: 'block', name: t('blockConfig') },
    { id: 'stats', name: t('statsInfo') }
  ]
}

// ==================== 统计信息相关 ====================
const loadMountPointStats = async () => {
  if (!selectedMountPoint.value || !selectedMountPoint.value.isMounted) return

  try {
    const stats = await window.go.main.App.Stats(selectedMountPoint.value.id)
    if (stats) {
      selectedMountPoint.value.stats = stats
      const index = mountPoints.value.findIndex(mp => mp.id === selectedMountPoint.value.id)
      if (index !== -1) {
        mountPoints.value[index].stats = stats
      }
    }
  } catch (error) {
    console.error('获取统计信息失败:', error)
  }
}

watch(activeTab, (newTab, oldTab) => {
  if (newTab === 'stats') {
    loadMountPointStats()
  }
})

// ==================== 工具函数 ====================
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

// ==================== 通知系统 ====================
const notifications = ref([])

const showNotification = (message, type = 'info') => {
  const id = Date.now()
  notifications.value.push({
    id,
    message,
    type
  })

  setTimeout(() => {
    removeNotification(id)
  }, 3000)

  console.log(`[${type.toUpperCase()}] ${message}`)
}

const removeNotification = (id) => {
  const index = notifications.value.findIndex(n => n.id === id)
  if (index > -1) {
    notifications.value.splice(index, 1)
  }
}

const truncateText = (text, maxLength) => {
  if (!text) return ''
  return text.length > maxLength ? text.substring(0, maxLength) + '...' : text
}

// ==================== 对话框系统 ====================
const dialog = ref({
  visible: false,
  title: '',
  message: '',
  confirmText: t('confirm'),
  cancelText: t('cancel'),
  onConfirm: null,
  onCancel: null
})

const showConfirmDialog = (title, message, onConfirm, onCancel = null, confirmText = t('confirm'), cancelText = t('cancel')) => {
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

const handleDialogConfirm = () => {
  if (dialog.value.onConfirm && typeof dialog.value.onConfirm === 'function') {
    dialog.value.onConfirm()
  }
  dialog.value.visible = false
}

const handleDialogCancel = () => {
  if (dialog.value.onCancel && typeof dialog.value.onCancel === 'function') {
    dialog.value.onCancel()
  }
  dialog.value.visible = false
}

// ==================== 状态检查 ====================
let statusCheckInterval = null

const checkMountPointStatus = async () => {
  statusCheckInterval = setInterval(async () => {
    try {
      if (isLoading.value) return
      for (let i = 0; i < mountPoints.value.length; i++) {
        const currentMP = mountPoints.value[i]
        const updatedMP = await window.go.main.App.GetMountPoint(currentMP.id)

        if (updatedMP && updatedMP.isMounted !== currentMP.isMounted) {
          currentMP.isMounted = updatedMP.isMounted
          if (selectedIndex.value === i) {
            selectedMountPoint.value.isMounted = updatedMP.isMounted
          }
        }
      }
    } catch (error) {
      console.error('定时检查挂载点状态失败:', error)
    }
  }, 1000)
}

// ==================== 外部链接 ====================
const openGitHub = async () => {
  try {
    await window.go.main.App.OpenURL('https://github.com/mageg-x/DedupFS')
  } catch (error) {
    console.error('Failed to open URL:', error)
  }
}

// ==================== 生命周期 ====================
onMounted(async () => {
  loadMountPoints()
  checkMountPointStatus()
})

onUnmounted(() => {
  if (statusCheckInterval) {
    clearInterval(statusCheckInterval)
  }
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

.form-input[readonly],
select.form-input:disabled,
.checkbox-input:disabled+.checkbox-label {
  cursor: not-allowed;
}

.checkbox-input:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

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

/* 标签页容器 */
.tabs-container {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.header-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: -18px;
}

.header-actions .dropdown {
  position: relative;
}

.header-actions .dropdown-menu {
  position: absolute;
  top: 100%;
  right: 0;
  bottom: auto;
  left: auto;
  margin-top: 8px;
  margin-bottom: 0;
}

/* 下拉菜单样式 */
.dropdown {
  position: relative;
}

.dropdown-toggle {
  background: none;
  border: none;
  color: #cbd5e1;
  cursor: pointer;
  padding: 6px;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background-color 0.2s;
}

.dropdown-toggle:hover {
  background-color: rgba(100, 116, 139, 0.2);
}

.dropdown-menu {
  position: absolute;
  bottom: 100%;
  left: 0;
  background: rgba(15, 23, 42, 0.95);
  border: 1px solid rgba(71, 85, 105, 0.3);
  border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.3);
  margin-bottom: 8px;
  min-width: 100px;
  z-index: 20;
}

.dropdown-item {
  padding: 8px 12px;
  cursor: pointer;
  color: #cbd5e1;
  font-size: 12px;
  transition: background-color 0.2s;
  display: flex;
  align-items: center;
  gap: 8px;
}

.dropdown-item:hover {
  background-color: rgba(59, 130, 246, 0.2);
}

.flag-icon {
  display: inline-block;
  font-size: 18px;
  width: 24px;
  text-align: center;
  margin-right: 8px;
  line-height: 1;
}

/* GitHub链接样式 */
.github-link {
  color: #cbd5e1;
  text-decoration: none;
  padding: 6px;
  border-radius: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background-color 0.2s, color 0.2s;
}

.github-link:hover {
  background-color: rgba(100, 116, 139, 0.2);
  color: #e2e8f0;
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

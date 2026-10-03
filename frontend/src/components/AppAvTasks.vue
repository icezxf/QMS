<template>
  <div class="av-tasks">
    <div class="toolbar">
      <div class="toolbar-left">
        <el-select v-model="statusFilter" placeholder="全部状态" style="width: 140px" @change="reload">
          <el-option label="全部" value="" />
          <el-option label="完成" value="done" />
          <el-option label="失败" value="failed" />
          <el-option label="暂停" value="paused" />
          <el-option label="待处理" value="pending" />
          <el-option label="已取消" value="cancelled" />
        </el-select>
        <el-input
          v-model="keyword"
          placeholder="搜索番号"
          style="width: 240px; margin-left: 12px"
          clearable
          @keyup.enter="reload"
        >
          <template #append>
            <el-button :icon="Search" @click="reload" />
          </template>
        </el-input>
      </div>
      <div class="toolbar-right">
        <el-button type="danger" size="small" @click="clearAll">清空记录</el-button>
      </div>
    </div>

    <el-table :data="list" v-loading="loading" stripe style="width: 100%" row-key="id">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="expand-content">
            <div v-if="parseWarnings(row.warnings).length > 0" class="warnings-block">
              <div class="warnings-title">⚠️ 警告（{{ parseWarnings(row.warnings).length }} 条）</div>
              <ul class="warnings-list">
                <li v-for="(w, i) in parseWarnings(row.warnings)" :key="i">{{ w }}</li>
              </ul>
            </div>
            <div v-else class="no-warnings">✓ 无警告</div>
            <div class="detail-block">
              <div><b>消息：</b>{{ row.message || '—' }}</div>
              <div><b>文件路径：</b>{{ row.file_path || '—' }}</div>
              <div><b>数据源：</b>{{ row.provider || '—' }}</div>
              <div><b>时间：</b>{{ formatTime(row.created_at) }}</div>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="id" label="ID" width="70" />
      <el-table-column label="番号" width="140">
        <template #default="{ row }">
          <el-link v-if="row.media_id" type="primary" @click="goMedia(row.media_id)">
            {{ row.code }}
          </el-link>
          <span v-else>{{ row.code || '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusTag(row.status)" size="small">
            {{ statusText(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="警告" width="90">
        <template #default="{ row }">
          <el-tag v-if="parseWarnings(row.warnings).length > 0" type="warning" size="small">
            {{ parseWarnings(row.warnings).length }}
          </el-tag>
          <span v-else>—</span>
        </template>
      </el-table-column>
      <el-table-column prop="provider" label="数据源" width="160" class-name="hide-mobile" show-overflow-tooltip />
      <el-table-column prop="message" label="消息" class-name="hide-mobile" show-overflow-tooltip />
      <el-table-column label="时间" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
    </el-table>

    <div class="pagination" v-if="total > pageSize">
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="prev, pager, next, total"
        @current-change="load"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search } from '@element-plus/icons-vue'

const router = useRouter()
const list = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const statusFilter = ref('')
const keyword = ref('')

const load = async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/avscrape/tasks', {
      params: {
        page: page.value,
        page_size: pageSize.value,
        status: statusFilter.value,
        keyword: keyword.value,
      },
    })
    list.value = res.data.list || []
    total.value = res.data.total || 0
  } catch {
    ElMessage.error('加载任务失败')
  } finally {
    loading.value = false
  }
}

const reload = () => {
  page.value = 1
  load()
}

const goMedia = (id: number) => {
  router.push(`/avscrape/library/${id}`)
}

const statusTag = (s: string) => {
  switch (s) {
    case 'done': return 'success'
    case 'failed': return 'danger'
    case 'paused': return 'warning'
    case 'cancelled': return 'info'
    default: return ''
  }
}

const statusText = (s: string) => {
  switch (s) {
    case 'done': return '完成'
    case 'failed': return '失败'
    case 'paused': return '暂停'
    case 'cancelled': return '取消'
    case 'pending': return '待处理'
    default: return s || '—'
  }
}

const parseWarnings = (json: string) => {
  if (!json) return []
  try {
    const arr = JSON.parse(json)
    return Array.isArray(arr) ? arr : []
  } catch {
    return []
  }
}

const formatTime = (t: string) => {
  if (!t) return ''
  return new Date(t).toLocaleString('zh-CN')
}

const clearAll = async () => {
  try {
    await ElMessageBox.confirm('确定清空所有任务记录吗？', '确认', { type: 'warning' })
    await axios.delete('/api/avscrape/tasks')
    ElMessage.success('已清空')
    load()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('清空失败')
  }
}

onMounted(load)
</script>

<style scoped>
.av-tasks {
  padding: 20px;
}
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.toolbar-left {
  display: flex;
  align-items: center;
}
.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}
.expand-content {
  padding: 12px 20px;
  background: #fafafa;
}
.warnings-block {
  margin-bottom: 12px;
}
.warnings-title {
  font-weight: 600;
  color: #e6a23c;
  margin-bottom: 8px;
}
.warnings-list {
  margin: 0;
  padding-left: 20px;
  color: #606266;
  font-size: 13px;
  line-height: 1.8;
}
.no-warnings {
  color: #67c23a;
  margin-bottom: 12px;
}
.detail-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
  color: #606266;
  padding-top: 8px;
  border-top: 1px dashed #dcdfe6;
}

/* ===== 移动端适配 ===== */
@media (max-width: 768px) {
  .av-tasks {
    padding: 12px;
  }
  .toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }
  .toolbar-left {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
  }
  .toolbar-left .el-select,
  .toolbar-left .el-input {
    width: 100% !important;
    margin-left: 0 !important;
  }
  .toolbar-right {
    align-self: flex-end;
  }
  :deep(.el-table__header-wrapper),
  :deep(.el-table__body-wrapper) {
    font-size: 12px;
  }
  :deep(.el-table .cell) {
    padding: 0 4px;
  }
  :deep(.hide-mobile) {
    display: none !important;
  }
  .expand-content {
    padding: 10px;
  }
  .warnings-list {
    font-size: 12px;
    padding-left: 16px;
  }
  .detail-block {
    font-size: 12px;
  }
}

@media (max-width: 480px) {
  .av-tasks {
    padding: 8px;
  }
  :deep(.el-table__header-wrapper),
  :deep(.el-table__body-wrapper) {
    font-size: 11px;
  }
  .warnings-title {
    font-size: 13px;
  }
}
</style>

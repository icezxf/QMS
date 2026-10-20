<template>
  <div class="av-tasks">
    <!-- ★ 顶部状态条：移动端自动适配宽度 -->
    <el-alert
      v-if="hasActiveTask"
      title="后台正在执行刮削任务..."
      type="warning"
      show-icon
      :closable="false"
      class="status-alert"
    >
      <template #default>
        <span>任务处理中，列表每 30 秒自动刷新。</span>
      </template>
    </el-alert>

    <div class="toolbar">
      <div class="toolbar-left">
        <el-select v-model="statusFilter" placeholder="全部状态" @change="reload">
          <el-option label="全部" value="" />
          <el-option label="完成" value="done" />
          <el-option label="失败" value="failed" />
          <el-option label="暂停" value="paused" />
          <el-option label="重复" value="duplicate" />
          <el-option label="待处理" value="pending" />
          <el-option label="已取消" value="cancelled" />
        </el-select>
        <el-input v-model="keyword" placeholder="搜索番号" clearable @keyup.enter="reload">
          <template #append>
            <el-button :icon="Search" @click="reload" />
          </template>
        </el-input>
      </div>
      <div class="toolbar-right">
        <el-button type="warning" @click="clearAndResetId">清空并重置 ID</el-button>
        <el-button type="danger" @click="clearAll">清空记录</el-button>
      </div>
    </div>

    <el-table :data="list" v-loading="loading" stripe style="width: 100%" row-key="id">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="expand-content">
            <!-- 移动端操作区 -->
            <div v-if="isMobile && row.status === 'paused' && row.media_id" class="mobile-actions">
              <el-button type="success" size="small" @click="release(row)">放行</el-button>
              <el-button type="primary" size="small" @click="restart(row)">重启</el-button>
              <el-button type="danger" size="small" @click="cancel(row)">取消</el-button>
            </div>

            <div v-if="parseDictLogs(row.dict_logs).length > 0" class="dict-block">
              <div class="dict-title">📖 字典转换（{{ parseDictLogs(row.dict_logs).length }} 条）</div>
              <ul class="dict-list">
                <li v-for="(d, i) in parseDictLogs(row.dict_logs)" :key="i">{{ d }}</li>
              </ul>
            </div>
            <div v-else class="no-dict">— 无字典转换记录</div>

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
      
      <el-table-column prop="id" label="ID" width="60" v-if="!isMobile" />
      <el-table-column label="番号" min-width="110">
        <template #default="{ row }">
          <el-link v-if="row.media_id" type="primary" @click="goMedia(row.media_id)">
            {{ row.code }}
          </el-link>
          <span v-else>{{ row.code || '—' }}</span>
        </template>
      </el-table-column>
      
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="statusTag(row.status)" size="small" :effect="row.status === 'running' ? 'dark' : 'light'">
            {{ statusText(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      
      <el-table-column v-if="!isMobile" label="字典" width="60" align="center">
        <template #default="{ row }">
          <el-tag v-if="parseDictLogs(row.dict_logs).length > 0" type="primary" size="small">
            {{ parseDictLogs(row.dict_logs).length }}
          </el-tag>
          <span v-else>—</span>
        </template>
      </el-table-column>
      
      <el-table-column v-if="!isMobile" label="警告" width="60" align="center">
        <template #default="{ row }">
          <el-tag v-if="parseWarnings(row.warnings).length > 0" type="warning" size="small">
            {{ parseWarnings(row.warnings).length }}
          </el-tag>
          <span v-else>—</span>
        </template>
      </el-table-column>

      <el-table-column v-if="!isMobile" prop="provider" label="数据源" width="120" show-overflow-tooltip />
      <el-table-column v-if="!isMobile" prop="message" label="消息" min-width="160" show-overflow-tooltip />
      
      <el-table-column label="时间" width="160" v-if="!isMobile">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>

      <el-table-column v-if="!isMobile" label="操作" width="180" align="center">
        <template #default="{ row }">
          <template v-if="row.status === 'paused' && row.media_id">
            <el-button size="small" type="success" @click="release(row)">放行</el-button>
            <el-button size="small" type="primary" @click="restart(row)">重启</el-button>
            <el-button size="small" type="danger" @click="cancel(row)">取消</el-button>
          </template>
          <span v-else>—</span>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination" v-if="total > pageSize">
      <el-pagination v-model:current-page="page" :page-size="pageSize" :total="total" layout="prev, pager, next" @current-change="load" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { useDeviceType } from '@/composables/useDeviceType'

const router = useRouter()
const { isMobile } = useDeviceType()

const list = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const statusFilter = ref('')
const keyword = ref('')
let refreshTimer: any = null

const hasActiveTask = computed(() => list.value.some(t => t.status === 'pending' || t.status === 'running'))

const load = async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/avscrape/tasks', { params: { page: page.value, page_size: pageSize.value, status: statusFilter.value, keyword: keyword.value } })
    list.value = res.data.list || []
    total.value = res.data.total || 0
  } catch { ElMessage.error('加载任务失败') } finally { loading.value = false }

  if (hasActiveTask.value && !refreshTimer) {
    refreshTimer = setInterval(() => { if (document.visibilityState === 'visible') load() }, 30000)
  } else if (!hasActiveTask.value && refreshTimer) {
    clearInterval(refreshTimer); refreshTimer = null
  }
}

const reload = () => { page.value = 1; load() }
const goMedia = (id: number) => router.push(`/avscrape/library/${id}`)

const statusTag = (s: string) => {
  switch (s) {
    case 'done': return 'success'
    case 'failed': return 'danger'
    case 'paused': return 'warning'
    case 'running': return 'primary'
    case 'duplicate': return 'warning'
    case 'cancelled': return 'info'
    default: return 'info'
  }
}
const statusText = (s: string) => {
  switch (s) {
    case 'done': return '完成'
    case 'failed': return '失败'
    case 'paused': return '暂停'
    case 'running': return '刮削中'
    case 'duplicate': return '重复'
    case 'cancelled': return '取消'
    case 'pending': return '待处理'
    default: return s || '—'
  }
}

const parseWarnings = (json: string) => { if (!json) return []; try { return Array.isArray(JSON.parse(json)) ? JSON.parse(json) : [] } catch { return [] } }
const parseDictLogs = (json: string) => { if (!json) return []; try { return Array.isArray(JSON.parse(json)) ? JSON.parse(json) : [] } catch { return [] } }
const formatTime = (t: string) => t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : ''

const clearAll = async () => {
  try { await ElMessageBox.confirm('确定清空所有任务记录吗？', '确认', { type: 'warning' }); await axios.delete('/api/avscrape/tasks'); ElMessage.success('已清空'); reload() } 
  catch (e: any) { if (e !== 'cancel') ElMessage.error('清空失败') }
}
const clearAndResetId = async () => {
  try { await ElMessageBox.confirm('将清空所有任务记录，并把任务 ID 从 1 开始。确定？', '警告', { type: 'warning' }); const res = await axios.post('/api/avscrape/tasks/clear-and-reset'); ElMessage.success(res.data?.msg || '已清空并重置 ID'); reload() } 
  catch (e: any) { if (e !== 'cancel') ElMessage.error('操作失败') }
}
const release = async (row: any) => {
  try { await ElMessageBox.confirm(`放行番号 ${row.code}？`, '确认放行', { type: 'warning' }); await axios.post(`/api/avscrape/library/${row.media_id}/release`); ElMessage.success('已放行'); load() } 
  catch (e: any) { if (e !== 'cancel') ElMessage.error('放行失败') }
}
const restart = async (row: any) => {
  try { await ElMessageBox.confirm(`重启番号 ${row.code}？`, '确认重启', { type: 'warning' }); await axios.post(`/api/avscrape/library/${row.media_id}/restart`); ElMessage.success('已重启'); load() } 
  catch (e: any) { if (e !== 'cancel') ElMessage.error('重启失败') }
}
const cancel = async (row: any) => {
  try { await ElMessageBox.confirm(`取消番号 ${row.code}？`, '确认取消', { type: 'warning' }); await axios.post(`/api/avscrape/library/${row.media_id}/cancel`); ElMessage.success('已取消'); load() } 
  catch (e: any) { if (e !== 'cancel') ElMessage.error('取消失败') }
}

onMounted(load)
onUnmounted(() => { if (refreshTimer) clearInterval(refreshTimer) })
</script>

<style scoped>
.av-tasks { padding: 20px; overflow-x: hidden; }
.toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; }
.toolbar-left { display: flex; align-items: center; }
.pagination { margin-top: 20px; display: flex; justify-content: center; }
.expand-content { padding: 12px 16px; background: #fafafa; }
.mobile-actions { display: flex; gap: 8px; margin-bottom: 12px; padding-bottom: 12px; border-bottom: 1px dashed #dcdfe6; }
.dict-block { margin-bottom: 12px; }
.dict-title { font-weight: 600; color: #409eff; margin-bottom: 8px; }
.dict-list { margin: 0; padding-left: 20px; color: #606266; font-size: 13px; line-height: 1.8; }
.no-dict { color: #909399; margin-bottom: 12px; font-size: 13px; }
.warnings-block { margin-bottom: 12px; }
.warnings-title { font-weight: 600; color: #e6a23c; margin-bottom: 8px; }
.warnings-list { margin: 0; padding-left: 20px; color: #606266; font-size: 13px; line-height: 1.8; }
.no-warnings { color: #67c23a; margin-bottom: 12px; }
.detail-block { display: flex; flex-direction: column; gap: 4px; font-size: 13px; color: #606266; padding-top: 8px; border-top: 1px dashed #dcdfe6; word-break: break-all; }

/* ★ 移动端适配 */
@media (max-width: 768px) {
  .av-tasks { padding: 12px; }
  .status-alert { margin-bottom: 12px; padding: 8px 12px !important; }
  .toolbar { flex-direction: column; align-items: stretch; gap: 10px; }
  .toolbar-left { flex-direction: column; align-items: stretch; gap: 8px; }
  .toolbar-left .el-select, .toolbar-left .el-input { width: 100% !important; margin-left: 0 !important; }
  .toolbar-right { display: flex; gap: 8px; }
  .toolbar-right .el-button { flex: 1; margin-left: 0; }
  :deep(.el-table__header-wrapper), :deep(.el-table__body-wrapper) { font-size: 12px; }
  :deep(.el-table .cell) { padding: 0 4px; }
  .expand-content { padding: 10px; }
  .dict-list, .warnings-list { font-size: 12px; padding-left: 16px; }
  .detail-block { font-size: 12px; }
  .mobile-actions .el-button { flex: 1; margin-left: 0; }
}
</style>

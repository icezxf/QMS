<template>
  <div class="aven-paths">
    <div class="aven-paths__header">
      <h2>欧美刮削目录</h2>
      <el-button type="primary" @click="openAdd" :style="isMobile ? 'width: 100%' : ''">添加目录</el-button>
    </div>

    <el-table :data="list" border stripe v-loading="loading">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div class="expand-content">
            <div v-if="isMobile" class="mobile-actions">
              <el-button 
                size="small" 
                type="primary" 
                :loading="scanningIds.includes(row.id)" 
                :disabled="scanningIds.includes(row.id)"
                @click="scan(row.id)"
              >
                {{ scanningIds.includes(row.id) ? '扫描中' : '扫描' }}
              </el-button>
              <el-button size="small" @click="openEdit(row)">编辑</el-button>
              <el-button size="small" type="danger" @click="del(row.id)">删除</el-button>
            </div>
            <div v-if="isMobile" class="detail-block">
              <div><b>来源类型：</b>{{ sourceTypeText(row.source_type) }}</div>
              <div><b>目标路径：</b>{{ row.target_path || '—' }}</div>
              <div><b>操作方式：</b>{{ modeText(row.mode) }}</div>
              <div><b>整理方式：</b>{{ moveMethodText(row.move_method) }}</div>
              <div><b>命名模板：</b>{{ row.name_template || DEFAULT_NAME_TEMPLATE }}</div>
            </div>
          </div>
        </template>
      </el-table-column>

      <el-table-column prop="id" label="ID" width="60" v-if="!isMobile" />
      <el-table-column prop="name" label="名称" min-width="120" />
      <el-table-column prop="source_path" label="源路径" min-width="200" show-overflow-tooltip />
      
      <el-table-column label="来源类型" width="100" v-if="!isMobile">
        <template #default="{ row }">{{ sourceTypeText(row.source_type) }}</template>
      </el-table-column>
      <el-table-column prop="target_path" label="目标路径" min-width="200" show-overflow-tooltip v-if="!isMobile" />
      <el-table-column label="操作方式" width="130" v-if="!isMobile">
        <template #default="{ row }">{{ modeText(row.mode) }}</template>
      </el-table-column>
      <el-table-column label="整理方式" width="100" v-if="!isMobile">
        <template #default="{ row }">{{ moveMethodText(row.move_method) }}</template>
      </el-table-column>
      <el-table-column label="启用" width="80">
        <template #default="{ row }">
          <el-tag :type="row.enable ? 'success' : 'info'" size="small">{{ row.enable ? '启用' : '禁用' }}</el-tag>
        </template>
      </el-table-column>

      <el-table-column label="操作" width="200" v-if="!isMobile">
        <template #default="{ row }">
          <el-button 
            size="small" 
            type="primary" 
            :loading="scanningIds.includes(row.id)" 
            :disabled="scanningIds.includes(row.id)"
            @click="scan(row.id)"
          >
            {{ scanningIds.includes(row.id) ? '扫描中' : '扫描' }}
          </el-button>
          <el-button size="small" @click="openEdit(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="del(row.id)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 编辑表单 -->
    <el-dialog
      v-model="dialogVisible"
      :title="editId ? '编辑欧美刮削目录' : '添加欧美刮削目录'"
      :width="isMobile ? '100%' : '700px'"
      :fullscreen="isMobile"
    >
      <el-form 
        :model="form" 
        :label-position="isMobile ? 'top' : 'right'" 
        :label-width="isMobile ? 'auto' : '120px'"
      >
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="给这个刮削目录起个名字" />
        </el-form-item>

        <el-form-item label="来源类型">
          <el-radio-group v-model="form.source_type" @change="onSourceTypeChange">
            <el-radio-button label="115">115 网盘</el-radio-button>
            <el-radio-button label="openlist">OpenList</el-radio-button>
            <el-radio-button label="local">本地目录</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item v-if="form.source_type !== 'local'" label="网盘账号">
          <el-select v-model="form.account_id" placeholder="选择账号" style="width: 100%" @change="onAccountChange">
            <el-option v-for="a in accountList" :key="a.id" :label="a.name || a.username" :value="a.id" />
          </el-select>
        </el-form-item>

        <el-form-item label="源路径">
          <el-input v-model="form.source_path" placeholder="点击右侧按钮选择目录">
            <template #append>
              <el-button @click="openPicker('source')">选择</el-button>
            </template>
          </el-input>
        </el-form-item>

        <el-form-item label="目标路径">
          <el-input v-model="form.target_path" placeholder="点击右侧按钮选择目录">
            <template #append>
              <el-button @click="openPicker('target')">选择</el-button>
            </template>
          </el-input>
        </el-form-item>

        <el-form-item label="操作方式">
          <el-radio-group v-model="form.mode">
            <el-radio-button label="scrape_only">仅刮削</el-radio-button>
            <el-radio-button label="scrape_and_rename">刮削和整理</el-radio-button>
            <el-radio-button label="rename_only">仅整理</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="整理方式">
          <el-radio-group v-model="form.move_method">
            <el-radio-button label="move">移动</el-radio-button>
            <el-radio-button label="copy">复制</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="命名模板">
          <el-input v-model="form.name_template" placeholder="{resolution}/{actors}/{title}" />
          <div class="template-hint">
            可用变量：<code>{resolution}</code> 分辨率、
            <code>{actor}</code> 首个演员、
            <code>{actors}</code> 全部演员、
            <code>{title}</code> 标题、
            <code>{year}</code> 年份、
            <code>{studio}</code> 片商、
            <code>{series}</code> 系列、
            <code>{code}</code> StashDB ID
          </div>
        </el-form-item>

        <el-form-item label="启用">
          <el-switch v-model="form.enable" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false" :style="isMobile ? 'width: 100%; margin-bottom: 10px; margin-left: 0;' : ''">取消</el-button>
        <el-button type="primary" @click="save" :style="isMobile ? 'width: 100%; margin-left: 0;' : ''">保存</el-button>
      </template>
    </el-dialog>

    <!-- 目录选择器 -->
    <el-dialog
      v-model="pickerVisible"
      title="选择目录"
      :width="isMobile ? '100%' : '600px'"
      :fullscreen="isMobile"
    >
      <div class="picker-path">
        <span>当前路径：{{ pickerParentPath || '根目录' }}</span>
        <el-button size="small" @click="pickerGoRoot">返回根目录</el-button>
      </div>
      <el-table
        :data="pickerList"
        border
        :height="isMobile ? '60vh' : '350px'"
        v-loading="pickerLoading"
        @row-click="pickerEnter"
        style="cursor: pointer"
      >
        <el-table-column label="名称" min-width="150">
          <template #default="{ row }">📁 {{ row.name }}</template>
        </el-table-column>
        <el-table-column label="路径" min-width="150" show-overflow-tooltip v-if="!isMobile" />
        <el-table-column label="选择" width="100" align="center">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click.stop="pickerConfirm(row)">选此目录</el-button>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="pickerVisible = false" :style="isMobile ? 'width: 100%; margin-left: 0;' : ''">取消</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox, ElNotification } from 'element-plus'
import { useDeviceType } from '@/composables/useDeviceType'

const { isMobile } = useDeviceType()

const list = ref<any[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const editId = ref<number | null>(null)
const accountList = ref<any[]>([])
const scanningIds = ref<number[]>([])
let globalTimer: any = null

const DEFAULT_NAME_TEMPLATE = '{resolution}/{actors}/{title}'

const form = ref({
  name: '',
  source_type: '115',
  account_id: 0,
  source_path: '',
  target_path: '',
  mode: 'scrape_and_rename',
  move_method: 'move',
  name_template: DEFAULT_NAME_TEMPLATE,
  enable: true,
})

const pickerVisible = ref(false)
const pickerTarget = ref<'source' | 'target'>('source')
const pickerList = ref<any[]>([])
const pickerLoading = ref(false)
const pickerParentId = ref('')
const pickerParentPath = ref('')

const sourceTypeText = (t: string) =>
  ({ '115': '115 网盘', openlist: 'OpenList', local: '本地目录' } as any)[t] || t
const modeText = (m: string) =>
  ({ scrape_only: '仅刮削', scrape_and_rename: '刮削和整理', rename_only: '仅整理' } as any)[m] || m
const moveMethodText = (m: string) =>
  ({ move: '移动', copy: '复制' } as any)[m] || m

const loadList = async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/aven/paths')
    list.value = res.data.list || []
  } catch {
    ElMessage.error('加载目录列表失败')
  } finally {
    loading.value = false
  }
}

const loadAccounts = async () => {
  try {
    const res = await axios.get('/api/account/list')
    accountList.value = res.data.data || []
  } catch {}
}

// ★ 轮询逻辑
const startPolling = () => {
  if (globalTimer) clearInterval(globalTimer)
  
  const timeSnapshots: Record<number, string> = {}
  list.value.forEach((item: any) => {
    timeSnapshots[item.id] = item.last_scan_at || ''
  })

  globalTimer = setInterval(async () => {
    if (document.visibilityState === 'hidden') return
    try {
      const listRes = await axios.get('/api/aven/paths')
      const newList = listRes.data.list || []
      let stillScanning = false

      for (const id of scanningIds.value) {
        const currentItem = newList.find((item: any) => item.id === id)
        if (!currentItem) continue

        if (currentItem.last_scan_at !== timeSnapshots[id]) {
          scanningIds.value = scanningIds.value.filter(v => v !== id)
          const finishTime = new Date(currentItem.last_scan_at).toLocaleString('zh-CN', { hour12: false })
          
          ElNotification({
            title: '扫描完成',
            dangerouslyUseHTMLString: true,
            message: `<div style="margin-top: 6px;"><span>目录【${currentItem.name}】扫描已完成。</span><br/><span style="color: #909399; font-size: 12px;">完成时间：${finishTime}</span></div>`,
            type: 'success',
            duration: 0,
            position: 'bottom-right'
          })
        } else {
          stillScanning = true
        }
      }

      list.value = newList

      if (!stillScanning && scanningIds.value.length === 0) {
        clearInterval(globalTimer)
        globalTimer = null
      }
    } catch {}
  }, 30000)
}

const scan = async (id: number) => {
  if (scanningIds.value.includes(id)) return
  scanningIds.value.push(id)
  if (!globalTimer) startPolling()
  
  try {
    const res = await axios.post(`/api/aven/paths/${id}/scan`)
    ElMessage.success(res.data.msg || '任务已加入后台队列')
  } catch (e: any) {
    scanningIds.value = scanningIds.value.filter(v => v !== id)
    ElMessage.error(e.response?.data?.error || '添加任务失败')
  }
}

const onSourceTypeChange = () => {
  form.value.account_id = 0
  form.value.source_path = ''
  form.value.target_path = ''
}

const onAccountChange = () => {
  form.value.source_path = ''
  form.value.target_path = ''
}

const openAdd = () => {
  editId.value = null
  form.value = {
    name: '',
    source_type: '115',
    account_id: 0,
    source_path: '',
    target_path: '',
    mode: 'scrape_and_rename',
    move_method: 'move',
    name_template: DEFAULT_NAME_TEMPLATE,
    enable: true,
  }
  dialogVisible.value = true
}

const openEdit = (row: any) => {
  editId.value = row.id
  form.value = {
    name: row.name,
    source_type: row.source_type,
    account_id: row.account_id,
    source_path: row.source_path,
    target_path: row.target_path,
    mode: row.mode,
    move_method: row.move_method,
    name_template: row.name_template || DEFAULT_NAME_TEMPLATE,
    enable: row.enable,
  }
  dialogVisible.value = true
}

const save = async () => {
  try {
    if (editId.value) {
      await axios.put(`/api/aven/paths/${editId.value}`, form.value)
    } else {
      await axios.post('/api/aven/paths', form.value)
    }
    ElMessage.success('保存成功')
    dialogVisible.value = false
    await loadList()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  }
}

const del = async (id: number) => {
  try {
    await ElMessageBox.confirm('确定删除这个刮削目录？', '提示', { type: 'warning' })
    await axios.delete(`/api/aven/paths/${id}`)
    ElMessage.success('删除成功')
    await loadList()
  } catch {}
}

const openPicker = (target: 'source' | 'target') => {
  if (form.value.source_type !== 'local' && !form.value.account_id) {
    ElMessage.warning('请先选择网盘账号')
    return
  }
  pickerTarget.value = target
  pickerParentId.value = ''
  pickerParentPath.value = ''
  pickerVisible.value = true
  loadPickerList()
}

const loadPickerList = async () => {
  pickerLoading.value = true
  try {
    const res = await axios.get('/api/path/list', {
      params: {
        source_type: form.value.source_type,
        account_id: form.value.account_id,
        parent_id: pickerParentId.value,
        parent_path: pickerParentPath.value,
      },
    })
    pickerList.value = res.data.data || []
  } catch {
    ElMessage.error('加载目录失败')
    pickerList.value = []
  } finally {
    pickerLoading.value = false
  }
}

const pickerEnter = (row: any) => {
  pickerParentId.value = row.id
  pickerParentPath.value = row.path
  loadPickerList()
}

const pickerConfirm = (row: any) => {
  if (pickerTarget.value === 'source') {
    form.value.source_path = row.path
  } else {
    form.value.target_path = row.path
  }
  pickerVisible.value = false
}

const pickerGoRoot = () => {
  pickerParentId.value = ''
  pickerParentPath.value = ''
  loadPickerList()
}

onMounted(async () => {
  await loadList()
  loadAccounts()
  
  try {
    const res = await axios.get('/api/aven/tasks', { params: { status: 'running', page_size: 1 } })
    if (res.data.list && res.data.list.length > 0) {
      scanningIds.value = list.value.map((item: any) => item.id)
      startPolling()
    }
  } catch {}
})

onUnmounted(() => {
  if (globalTimer) clearInterval(globalTimer)
})
</script>

<style scoped>
.aven-paths {
  padding: 20px;
  overflow-x: hidden;
}
.aven-paths__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.picker-path {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  font-size: 14px;
  color: #666;
}
.template-hint {
  font-size: 12px;
  color: #999;
  margin-top: 4px;
  line-height: 1.6;
}
.template-hint code {
  background: #f5f7fa;
  padding: 1px 4px;
  border-radius: 3px;
  color: #e6a23c;
}
.expand-content { padding: 12px; background: #fafafa; }
.mobile-actions { display: flex; gap: 8px; margin-bottom: 12px; padding-bottom: 12px; border-bottom: 1px dashed #dcdfe6; }
.mobile-actions .el-button { flex: 1; margin-left: 0; }
.detail-block { display: flex; flex-direction: column; gap: 6px; font-size: 13px; color: #606266; }

@media (max-width: 768px) {
  .aven-paths { padding: 12px; }
  .aven-paths__header { flex-direction: column; align-items: stretch; gap: 10px; margin-bottom: 12px; }
  .aven-paths__header h2 { font-size: 18px; margin: 0; }
  :deep(.el-table__header-wrapper), :deep(.el-table__body-wrapper) { font-size: 12px; }
  :deep(.el-table .cell) { padding: 0 4px; }
  .picker-path { flex-direction: column; align-items: stretch; gap: 8px; font-size: 12px; }
  .picker-path .el-button { width: 100%; }
  .template-hint { font-size: 11px; }
}
@media (max-width: 480px) {
  .aven-paths { padding: 8px; }
  :deep(.el-table__header-wrapper), :deep(.el-table__body-wrapper) { font-size: 11px; }
}
</style>

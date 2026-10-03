<template>
  <div class="av-paths">
    <div class="av-paths__header">
      <h2>AV 刮削目录</h2>
      <el-button type="primary" @click="openAdd">添加目录</el-button>
    </div>

    <el-table :data="list" border stripe v-loading="loading">
      <el-table-column prop="id" label="ID" width="60" class-name="hide-mobile" />
      <el-table-column prop="name" label="名称" min-width="120" />
      <el-table-column label="来源类型" width="100" class-name="hide-mobile">
        <template #default="{ row }">{{ sourceTypeText(row.source_type) }}</template>
      </el-table-column>
      <el-table-column prop="source_path" label="源路径" min-width="200" show-overflow-tooltip />
      <el-table-column prop="target_path" label="目标路径" min-width="200" show-overflow-tooltip class-name="hide-mobile" />
      <el-table-column label="操作方式" width="130" class-name="hide-mobile">
        <template #default="{ row }">{{ modeText(row.mode) }}</template>
      </el-table-column>
      <el-table-column label="整理方式" width="100" class-name="hide-mobile">
        <template #default="{ row }">{{ moveMethodText(row.move_method) }}</template>
      </el-table-column>
      <el-table-column label="启用" width="80" class-name="hide-mobile">
        <template #default="{ row }">
          <el-tag :type="row.enable ? 'success' : 'info'">{{ row.enable ? '启用' : '禁用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="240" fixed="right">
        <template #default="{ row }">
          <el-button size="small" type="primary" @click="scan(row.id)">扫描</el-button>
          <el-button size="small" @click="openEdit(row)">编辑</el-button>
          <el-button size="small" type="danger" @click="del(row.id)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 编辑表单 -->
    <el-dialog
      v-model="dialogVisible"
      :title="editId ? '编辑 AV 刮削目录' : '添加 AV 刮削目录'"
      width="700px"
      class="av-dialog-responsive"
    >
      <el-form :model="form" label-width="120px">
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
          <el-input v-model="form.name_template" placeholder="{actor}/{number}" />
          <div class="template-hint">
            可用变量：<code>{actor}</code> 首个演员、
            <code>{actors}</code> 全部演员、
            <code>{number}</code> 番号、
            <code>{title}</code> 标题、
            <code>{year}</code> 年份、
            <code>{studio}</code> 片商、
            <code>{label}</code> 厂牌、
            <code>{series}</code> 系列
          </div>
        </el-form-item>

        <el-form-item label="启用">
          <el-switch v-model="form.enable" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>

    <!-- 目录选择器 -->
    <el-dialog
      v-model="pickerVisible"
      title="选择目录"
      width="600px"
      class="av-dialog-responsive"
    >
      <div class="picker-path">
        <span>当前路径：{{ pickerParentPath || '根目录' }}</span>
        <el-button size="small" @click="pickerGoRoot">返回根目录</el-button>
      </div>
      <el-table
        :data="pickerList"
        border
        height="350"
        v-loading="pickerLoading"
        @row-click="pickerEnter"
        style="cursor: pointer"
      >
        <el-table-column label="名称" min-width="200">
          <template #default="{ row }">📁 {{ row.name }}</template>
        </el-table-column>
        <el-table-column label="路径" min-width="200" show-overflow-tooltip class-name="hide-mobile" />
        <el-table-column label="选择" width="100">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click.stop="pickerConfirm(row)">选此目录</el-button>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="pickerVisible = false">取消</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'

const list = ref<any[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const editId = ref<number | null>(null)
const accountList = ref<any[]>([])

const form = ref({
  name: '',
  source_type: '115',
  account_id: 0,
  source_path: '',
  target_path: '',
  mode: 'scrape_and_rename',
  move_method: 'move',
  name_template: '{actor}/{number}',
  enable: true,
})

// 目录选择器状态
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
    const res = await axios.get('/api/avscrape/paths')
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
  } catch {
    // ignore
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
    name_template: '{actor}/{number}',
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
    name_template: row.name_template || '{actor}/{number}',
    enable: row.enable,
  }
  dialogVisible.value = true
}

const save = async () => {
  try {
    if (editId.value) {
      await axios.put(`/api/avscrape/paths/${editId.value}`, form.value)
    } else {
      await axios.post('/api/avscrape/paths', form.value)
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
    await axios.delete(`/api/avscrape/paths/${id}`)
    ElMessage.success('删除成功')
    await loadList()
  } catch {
    // cancelled
  }
}

const scan = async (id: number) => {
  try {
    const res = await axios.post(`/api/avscrape/paths/${id}/scan`)
    ElMessage.success(res.data.msg || '任务已加入队列')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '添加任务失败')
  }
}

// ========== 目录选择器 ==========

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

onMounted(() => {
  loadList()
  loadAccounts()
})
</script>

<style scoped>
.av-paths {
  padding: 20px;
}
.av-paths__header {
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

/* ===== 移动端适配 ===== */
@media (max-width: 768px) {
  .av-paths {
    padding: 12px;
  }
  .av-paths__header {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
    margin-bottom: 12px;
  }
  .av-paths__header h2 {
    font-size: 18px;
    margin: 0;
  }
  :deep(.hide-mobile) {
    display: none !important;
  }
  :deep(.el-table__header-wrapper),
  :deep(.el-table__body-wrapper) {
    font-size: 12px;
  }
  :deep(.el-table .cell) {
    padding: 0 4px;
  }
  .picker-path {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
    font-size: 12px;
  }
  .picker-path .el-button {
    width: 100%;
  }
  .template-hint {
    font-size: 11px;
  }
}

@media (max-width: 480px) {
  .av-paths {
    padding: 8px;
  }
  :deep(.el-table__header-wrapper),
  :deep(.el-table__body-wrapper) {
    font-size: 11px;
  }
}
</style>

<!-- 非 scoped：作用于 teleport 到 body 的 dialog -->
<style>
@media (max-width: 768px) {
  .av-dialog-responsive {
    width: 95% !important;
    max-width: 95vw !important;
    margin: 5vh auto !important;
  }
  .av-dialog-responsive .el-dialog__header {
    padding: 16px 16px 8px;
  }
  .av-dialog-responsive .el-dialog__body {
    padding: 12px 16px;
  }
  .av-dialog-responsive .el-dialog__footer {
    padding: 8px 16px 16px;
  }
  .av-dialog-responsive .el-form-item {
    margin-bottom: 14px;
  }
  .av-dialog-responsive .el-form-item__label {
    width: 100px !important;
    font-size: 13px;
    padding-right: 8px;
  }
  .av-dialog-responsive .el-form-item__content {
    font-size: 13px;
  }
  .av-dialog-responsive .el-input,
  .av-dialog-responsive .el-select {
    width: 100% !important;
  }
  .av-dialog-responsive .el-radio-group {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .av-dialog-responsive .el-radio-button__inner {
    font-size: 12px;
    padding: 8px 12px;
  }
  .av-dialog-responsive .el-dialog__title {
    font-size: 16px;
  }
}

@media (max-width: 480px) {
  .av-dialog-responsive {
    width: 100% !important;
    max-width: 100vw !important;
    margin: 0 !important;
    border-radius: 0;
  }
  .av-dialog-responsive .el-form-item__label {
    width: 80px !important;
    font-size: 12px;
  }
  .av-dialog-responsive .el-form-item__content {
    font-size: 12px;
  }
  .av-dialog-responsive .el-radio-button__inner {
    font-size: 11px;
    padding: 6px 10px;
  }
}
</style>

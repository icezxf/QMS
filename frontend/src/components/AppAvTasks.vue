<template>
  <div class="av-tasks">
    <div class="av-tasks__header">
      <h2>AV 刮削记录</h2>
      <div>
        <el-button @click="loadList">刷新</el-button>
        <el-button type="danger" @click="clearAll">清空记录</el-button>
      </div>
    </div>

    <el-table :data="list" border stripe v-loading="loading">
      <el-table-column prop="id" label="ID" width="60" />
      <el-table-column prop="code" label="番号" width="120" />
      <el-table-column prop="file_path" label="文件路径" min-width="280" show-overflow-tooltip />
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)">{{ statusText(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="provider" label="刮削源" width="160" show-overflow-tooltip />
      <el-table-column prop="message" label="信息" min-width="200" show-overflow-tooltip />
      <el-table-column prop="created_at" label="时间" width="180">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-model:current-page="page"
      :page-size="pageSize"
      :total="total"
      layout="prev, pager, next, total"
      style="margin-top: 16px; text-align: right"
      @current-change="loadList"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'

const list = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)

const statusText = (s: string) => {
  return { done: '成功', failed: '失败', pending: '等待中', running: '进行中' }[s] || s
}
const statusType = (s: string) => {
  return { done: 'success', failed: 'danger', pending: 'info', running: 'warning' }[s] as any
}
const formatTime = (t: string) => {
  if (!t) return ''
  return new Date(t).toLocaleString('zh-CN', { hour12: false })
}

const loadList = async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/avscrape/tasks', {
      params: { page: page.value, page_size: pageSize.value },
    })
    list.value = res.data.list || []
    total.value = res.data.total || 0
  } catch {
    ElMessage.error('加载记录失败')
  } finally {
    loading.value = false
  }
}

const clearAll = async () => {
  try {
    await ElMessageBox.confirm('确定清空所有刮削记录？', '提示', { type: 'warning' })
    await axios.delete('/api/avscrape/tasks')
    ElMessage.success('已清空')
    await loadList()
  } catch {
    // cancelled
  }
}

onMounted(loadList)
</script>

<style scoped>
.av-tasks {
  padding: 20px;
}
.av-tasks__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
</style>
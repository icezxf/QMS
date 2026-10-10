<template>
  <div class="actor-link">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>演员库</span>
          <div class="toolbar">
            <el-input
              v-model="search"
              placeholder="搜索演员名字/别名"
              clearable
              class="search-input"
              @keyup.enter="loadList"
              @clear="loadList"
            />
            <el-button type="primary" @click="loadList">搜索</el-button>
            <el-button @click="showConfig = true">设置</el-button>
            <el-button
              type="info"
              :loading="aggregating"
              @click="doAggregate"
            >
              聚合演员
            </el-button>
            <el-button
              type="warning"
              :loading="syncing"
              @click="doSyncStashDB"
            >
              从 StashDB 拉元数据
            </el-button>
            <el-button
              type="success"
              :loading="pushing"
              @click="doPushEmby"
            >
              推送到 Emby
            </el-button>
          </div>
        </div>
      </template>

      <el-table :data="list" v-loading="loading" stripe style="width: 100%">
        <el-table-column label="头像" width="80">
          <template #default="{ row }">
            <el-avatar :size="48" :src="avatarSrc(row)" />
          </template>
        </el-table-column>
        <el-table-column prop="name" label="姓名" min-width="140" />
        <el-table-column label="别名" min-width="180">
          <template #default="{ row }">
            {{ formatAliases(row.aliases) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="bio"
          label="简介"
          min-width="240"
          show-overflow-tooltip
        />
        <el-table-column label="StashDB" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.stashdb_id" type="success" size="small">
              已关联
            </el-tag>
            <el-tag v-else type="info" size="small">未关联</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Emby" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.emby_person_id" type="success" size="small">
              已推送
            </el-tag>
            <el-tag v-else type="info" size="small">未推送</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button link type="danger" @click="removeActor(row)">
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        :total="total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        class="pagination"
        @current-change="loadList"
        @size-change="loadList"
      />
    </el-card>

    <el-dialog v-model="showConfig" title="Emby / StashDB 设置" width="480px">
      <el-form :model="config" label-width="120px">
        <el-form-item label="Emby 地址">
          <el-input
            v-model="config.emby_url"
            placeholder="http://192.168.1.10:8096"
          />
        </el-form-item>
        <el-form-item label="Emby API Key">
          <el-input v-model="config.emby_api_key" placeholder="Emby API Key" />
        </el-form-item>
        <el-form-item label="StashDB API Key">
          <el-input v-model="config.stashdb_key" placeholder="StashDB API Key" />
        </el-form-item>
        <el-form-item label="定时同步 Cron">
          <el-input v-model="config.sync_cron" placeholder="0 3 * * *" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showConfig = false">取消</el-button>
        <el-button type="primary" @click="saveConfig">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useHttpClient } from '@/http/client'
import {
  aggregateActors,
  deleteActor,
  fetchActorEmbyConfig,
  fetchActorList,
  pushEmby,
  saveActorEmbyConfig,
  syncStashDB,
  type ActorEmbyConfig,
  type ActorProfile,
} from '@/api/actor'

const http = useHttpClient()

const list = ref<ActorProfile[]>([])
const total = ref(0)
const page = ref(1)
const size = ref(20)
const search = ref('')
const loading = ref(false)

const aggregating = ref(false)
const syncing = ref(false)
const pushing = ref(false)

const showConfig = ref(false)
const config = ref<ActorEmbyConfig>({
  emby_url: '',
  emby_api_key: '',
  stashdb_key: '',
  sync_cron: '0 3 * * *',
})

async function loadList() {
  loading.value = true
  try {
    const data = await fetchActorList(http, {
      page: page.value,
      size: size.value,
      search: search.value,
    })
    list.value = data.list
    total.value = data.total
  } catch {
    ElMessage.error('加载演员列表失败')
  } finally {
    loading.value = false
  }
}

async function doAggregate() {
  aggregating.value = true
  try {
    await aggregateActors(http)
    ElMessage.success('聚合任务已提交，请稍后刷新')
    setTimeout(loadList, 1500)
  } catch {
    ElMessage.error('聚合失败')
  } finally {
    aggregating.value = false
  }
}

async function doSyncStashDB() {
  syncing.value = true
  try {
    const data = await syncStashDB(http)
    if (data.ok) ElMessage.success('StashDB 同步已启动')
    else ElMessage.warning(data.message || '功能待实现')
  } catch {
    ElMessage.error('StashDB 同步失败')
  } finally {
    syncing.value = false
  }
}

async function doPushEmby() {
  pushing.value = true
  try {
    const data = await pushEmby(http)
    if (data.ok) ElMessage.success('Emby 推送已启动')
    else ElMessage.warning(data.message || '功能待实现')
  } catch {
    ElMessage.error('Emby 推送失败')
  } finally {
    pushing.value = false
  }
}

async function removeActor(row: ActorProfile) {
  await ElMessageBox.confirm(`确认删除演员「${row.name}」？`, '提示', {
    type: 'warning',
  })
  await deleteActor(http, row.id)
  ElMessage.success('已删除')
  loadList()
}

async function loadConfig() {
  try {
    const data = await fetchActorEmbyConfig(http)
    config.value = { ...config.value, ...data }
  } catch {
    // 配置尚未初始化时忽略
  }
}

async function saveConfig() {
  await saveActorEmbyConfig(http, config.value)
  ElMessage.success('已保存')
  showConfig.value = false
}

function avatarSrc(row: ActorProfile): string {
  if (!row.avatar_url) return ''
  // 如果后端返回的是外链，直接使用；如果是本地路径，走 /actor/:id/avatar
  if (row.avatar_url.startsWith('http')) return row.avatar_url
  return `${import.meta.env.VITE_API_BASE || ''}/actor/${row.id}/avatar`
}

function formatAliases(s: string): string {
  if (!s) return ''
  try {
    const arr = JSON.parse(s)
    return Array.isArray(arr) ? arr.join(', ') : ''
  } catch {
    return s
  }
}

onMounted(() => {
  loadList()
  loadConfig()
})
</script>

<style scoped>
.actor-link {
  padding: 16px;
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.search-input {
  width: 220px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>

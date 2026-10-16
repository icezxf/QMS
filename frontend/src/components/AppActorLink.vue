<template>
  <div class="actor-link">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>Emby 演员库</span>
          <div class="toolbar">
            <el-select
              v-model="selectedLibrary"
              placeholder="选择媒体库"
              style="width: 220px"
              :loading="loadingLibraries"
              @change="loadPersons"
            >
              <el-option
                v-for="lib in libraries"
                :key="lib.Id"
                :label="lib.Name"
                :value="lib.Id"
              />
            </el-select>
            <el-input
              v-model="search"
              placeholder="搜索演员名字"
              clearable
              style="width: 200px"
            />
            <el-button @click="showConfig = true">设置</el-button>
            <el-button :loading="loading" @click="loadAll">刷新</el-button>
          </div>
        </div>
      </template>

      <el-table
        :data="filteredPersons"
        v-loading="loading"
        stripe
        style="width: 100%"
        :default-sort="{ prop: 'Name', order: 'ascending' }"
      >
        <el-table-column label="头像" width="80">
          <template #default="{ row }">
            <el-avatar :size="48" :src="embyPersonImageUrl(row.Id)">
              {{ row.Name?.charAt(0) || '?' }}
            </el-avatar>
          </template>
        </el-table-column>
        <el-table-column prop="Name" label="姓名" min-width="160" sortable />
        <el-table-column
          prop="Id"
          label="Emby ID"
          min-width="240"
          show-overflow-tooltip
        />
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">
              详情
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="footer-note">共 {{ filteredPersons.length }} 位演员</div>
    </el-card>

    <!-- 演员详情抽屉 -->
    <el-drawer
      v-model="showDetail"
      size="520px"
      :title="detail?.Name || '演员详情'"
    >
      <div v-if="detail" class="detail" v-loading="loadingDetail">
        <div class="avatar-wrap">
          <el-avatar :size="120" :src="embyPersonImageUrl(detail.Id)">
            {{ detail.Name?.charAt(0) || '?' }}
          </el-avatar>
        </div>
        <div class="meta">
          <div class="row">
            <span class="label">姓名</span>
            <span>{{ detail.Name || '—' }}</span>
          </div>
          <div class="row">
            <span class="label">生日</span>
            <span>{{ detail.Birthday || '—' }}</span>
          </div>
          <div class="row">
            <span class="label">出生地</span>
            <span>{{ detail.PlaceOfBirth || '—' }}</span>
          </div>
          <div class="row">
            <span class="label">职业</span>
            <span>{{ detail.KnownForDepartment || '—' }}</span>
          </div>
          <div class="row">
            <span class="label">标签</span>
            <span>{{ (detail.Tags || []).join(', ') || '—' }}</span>
          </div>
        </div>
        <div class="bio">
          <div class="label">简介</div>
          <p>{{ detail.Biography || '暂无简介' }}</p>
        </div>
      </div>
      <el-empty v-else description="暂无详情" />
    </el-drawer>

    <!-- 设置弹窗 -->
    <el-dialog v-model="showConfig" title="Emby 设置" width="520px">
      <el-form :model="config" label-width="120px">
        <el-form-item label="Emby 地址">
          <el-input
            v-model="config.emby_url"
            placeholder="http://192.168.1.10:8096"
          />
        </el-form-item>
        <el-form-item label="Emby API Key">
          <el-input
            v-model="config.emby_api_key"
            placeholder="Emby API Key"
          />
        </el-form-item>
        <el-form-item label="Emby 用户名">
          <el-input
            v-model="config.emby_user"
            placeholder="例如 admin（或直接填 UUID）"
          />
        </el-form-item>
        <el-form-item>
          <el-button :loading="testing" @click="doTest">测试连接</el-button>
          <el-button type="primary" @click="saveConfig">保存</el-button>
        </el-form-item>
      </el-form>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useHttpClient } from '@/http/client'
import {
  embyPersonImageUrl,
  fetchActorEmbyConfig,
  fetchEmbyLibraries,
  fetchEmbyPersonDetail,
  fetchEmbyPersons,
  saveActorEmbyConfig,
  testEmbyConnection,
  type ActorEmbyConfig,
  type EmbyLibrary,
  type EmbyPerson,
  type EmbyPersonDetail,
} from '@/api/actor'

const http = useHttpClient()

const libraries = ref<EmbyLibrary[]>([])
const selectedLibrary = ref('')
const persons = ref<EmbyPerson[]>([])
const search = ref('')
const loading = ref(false)
const loadingLibraries = ref(false)

const showDetail = ref(false)
const loadingDetail = ref(false)
const detail = ref<EmbyPersonDetail | null>(null)

const showConfig = ref(false)
const testing = ref(false)
const config = ref<ActorEmbyConfig>({
  emby_url: '',
  emby_api_key: '',
  emby_user: '',
  stashdb_key: '',
  sync_cron: '0 3 * * *',
})

const filteredPersons = computed(() => {
  const kw = search.value.trim().toLowerCase()
  if (!kw) return persons.value
  return persons.value.filter((p) =>
    (p.Name || '').toLowerCase().includes(kw),
  )
})

async function loadConfig() {
  try {
    const data = await fetchActorEmbyConfig(http)
    config.value = { ...config.value, ...data }
  } catch {
    // 配置尚未初始化时忽略
  }
}

async function loadLibraries() {
  loadingLibraries.value = true
  try {
    libraries.value = await fetchEmbyLibraries(http)
    if (libraries.value.length > 0 && !selectedLibrary.value) {
      selectedLibrary.value = libraries.value[0].Id
    }
  } catch (e: any) {
    ElMessage.warning(e?.message || '拉取媒体库失败，请检查设置')
  } finally {
    loadingLibraries.value = false
  }
}

async function loadPersons() {
  if (!selectedLibrary.value) return
  loading.value = true
  try {
    persons.value = await fetchEmbyPersons(http, selectedLibrary.value)
  } catch (e: any) {
    ElMessage.error(e?.message || '拉取演员失败')
  } finally {
    loading.value = false
  }
}

async function loadAll() {
  await loadLibraries()
  await loadPersons()
}

async function openDetail(row: EmbyPerson) {
  showDetail.value = true
  loadingDetail.value = true
  detail.value = null
  try {
    detail.value = await fetchEmbyPersonDetail(http, row.Id)
  } catch (e: any) {
    ElMessage.error(e?.message || '拉取详情失败')
  } finally {
    loadingDetail.value = false
  }
}

async function saveConfig() {
  await saveActorEmbyConfig(http, config.value)
  ElMessage.success('已保存')
  showConfig.value = false
  await loadAll()
}

async function doTest() {
  testing.value = true
  try {
    const r = await testEmbyConnection(http, {
      emby_url: config.value.emby_url,
      emby_api_key: config.value.emby_api_key,
      emby_user: config.value.emby_user,
    })
    if (r.ok) ElMessage.success(r.message)
    else ElMessage.error(r.message)
  } finally {
    testing.value = false
  }
}

onMounted(async () => {
  await loadConfig()
  await loadAll()
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
.footer-note {
  margin-top: 12px;
  text-align: right;
  color: #909399;
  font-size: 13px;
}
.detail {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.detail .avatar-wrap {
  display: flex;
  justify-content: center;
}
.detail .meta {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.detail .row {
  display: flex;
  gap: 12px;
}
.detail .label {
  width: 80px;
  color: #909399;
  flex-shrink: 0;
}
.detail .bio p {
  margin: 6px 0 0;
  line-height: 1.6;
  color: #303133;
  white-space: pre-wrap;
}
</style>
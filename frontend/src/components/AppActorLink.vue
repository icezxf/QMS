<template>
  <div class="actor-link">
    <div class="actor-link__header">
      <h2>Emby 演员管理</h2>
      <div class="toolbar">
        <el-select
          v-model="selectedLibrary"
          placeholder="选择媒体库"
          style="width: 200px"
          :loading="loadingLibraries"
          @change="loadPersons"
        >
          <el-option label="全部媒体库" value="" />
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
          style="width: 180px"
        />
        <el-button @click="showConfig = true">设置</el-button>
        <el-button :loading="loading" @click="loadAll">刷新</el-button>
        <el-button
          type="warning"
          :disabled="selectedRows.length === 0 || scraping"
          :loading="scraping"
          @click="batchScrape"
        >
          批量刮削（{{ selectedRows.length }}）
        </el-button>
      </div>
    </div>

    <el-table
      ref="tableRef"
      :data="filteredPersons"
      border
      stripe
      v-loading="loading"
      style="width: 100%"
      @selection-change="onSelectionChange"
    >
      <el-table-column type="selection" width="48" reserve-selection />
      <el-table-column label="头像" :width="isMobile ? 56 : 70">
        <template #default="{ row }">
          <el-avatar :size="isMobile ? 32 : 40" :src="embyImageUrl(row.Id)">
            {{ row.Name?.charAt(0) || '?' }}
          </el-avatar>
        </template>
      </el-table-column>
      <el-table-column
        prop="Name"
        label="姓名"
        :min-width="isMobile ? 100 : 160"
        sortable
        show-overflow-tooltip
      />
      <el-table-column
        v-if="!isMobile"
        prop="Id"
        label="Emby ID"
        min-width="220"
        show-overflow-tooltip
        class-name="hide-mobile"
      />
      <el-table-column
        label="刮削状态"
        :width="isMobile ? 60 : 110"
        align="center"
      >
        <template #default="{ row }">
          <template v-if="isMobile">
            <span :class="['status-dot', scrapeStatus[row.Id] || 'idle']" />
          </template>
          <template v-else>
            <el-tag v-if="scrapeStatus[row.Id] === 'pending'" type="info" size="small">待刮削</el-tag>
            <el-tag v-else-if="scrapeStatus[row.Id] === 'running'" type="warning" size="small">刮削中</el-tag>
            <el-tag v-else-if="scrapeStatus[row.Id] === 'success'" type="success" size="small">成功</el-tag>
            <el-tag v-else-if="scrapeStatus[row.Id] === 'failed'" type="danger" size="small">失败</el-tag>
            <span v-else>—</span>
          </template>
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        :width="isMobile ? 96 : 180"
        fixed="right"
        align="center"
      >
        <template #default="{ row }">
          <template v-if="isMobile">
            <el-tooltip content="编辑" placement="top">
              <el-button link type="primary" :icon="Edit" @click="openEdit(row)" />
            </el-tooltip>
            <el-tooltip content="详情" placement="top">
              <el-button link type="primary" :icon="View" @click="openDetail(row)" />
            </el-tooltip>
            <el-tooltip content="刮削" placement="top">
              <el-button
                link
                type="warning"
                :icon="MagicStick"
                :loading="scrapeStatus[row.Id] === 'running'"
                @click="scrapeOne(row)"
              />
            </el-tooltip>
          </template>
          <template v-else>
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="primary" @click="openDetail(row)">详情</el-button>
            <el-button
              link
              type="warning"
              :loading="scrapeStatus[row.Id] === 'running'"
              @click="scrapeOne(row)"
            >
              刮削
            </el-button>
          </template>
        </template>
      </el-table-column>
    </el-table>

    <div class="actor-link__footer">共 {{ filteredPersons.length }} 位演员</div>

    <!-- 批量刮削进度弹窗 -->
    <el-dialog
      v-model="showScrapeProgress"
      title="刮削进度"
      width="640px"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      :show-close="!scraping"
    >
      <div class="progress-header">
        <el-progress
          :percentage="scrapeProgress.percent"
          :status="scrapeProgress.failed > 0 ? 'warning' : undefined"
        />
        <div class="progress-text">
          已处理 {{ scrapeProgress.done }} / {{ scrapeProgress.total }}
          <span v-if="scrapeProgress.success > 0" class="success">成功 {{ scrapeProgress.success }}</span>
          <span v-if="scrapeProgress.failed > 0" class="fail">失败 {{ scrapeProgress.failed }}</span>
        </div>
      </div>

      <el-table
        :data="scrapeResults"
        max-height="360"
        border
        size="small"
        :row-class-name="resultRowClass"
      >
        <el-table-column prop="name" label="演员" min-width="140" show-overflow-tooltip />
        <el-table-column label="结果" width="80" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'success'" type="success" size="small">成功</el-tag>
            <el-tag v-else-if="row.status === 'failed'" type="danger" size="small">失败</el-tag>
            <el-tag v-else-if="row.status === 'running'" type="warning" size="small">处理中</el-tag>
            <el-tag v-else type="info" size="small">等待</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="message" label="详情" min-width="240" show-overflow-tooltip />
      </el-table>

      <template #footer>
        <el-button v-if="!scraping" @click="showScrapeProgress = false">关闭</el-button>
        <el-button v-else disabled>处理中…</el-button>
      </template>
    </el-dialog>

    <!-- 详情抽屉 -->
    <el-drawer
      v-model="showDetail"
      size="520px"
      :title="detail?.Name || '演员详情'"
    >
      <div v-if="detail" class="detail">
        <div class="avatar-wrap">
          <el-avatar :size="120" :src="embyImageUrl(detail.Id)">
            {{ detail.Name?.charAt(0) || '?' }}
          </el-avatar>
        </div>
        <div class="meta">
          <div class="row"><span class="label">姓名</span><span>{{ detail.Name || '—' }}</span></div>
          <div class="row"><span class="label">生日</span><span>{{ detail.Birthday || '—' }}</span></div>
          <div class="row"><span class="label">出生地</span><span>{{ detail.PlaceOfBirth || '—' }}</span></div>
          <div class="row"><span class="label">职业</span><span>{{ detail.KnownForDepartment || '—' }}</span></div>
          <div class="row"><span class="label">标签</span><span>{{ (detail.Tags || []).join(', ') || '—' }}</span></div>
        </div>
        <div class="bio">
          <div class="label">简介</div>
          <p>{{ detail.Biography || '暂无简介' }}</p>
        </div>
      </div>
      <el-empty v-else description="暂无详情" />
    </el-drawer>

    <!-- 编辑抽屉 -->
    <el-drawer
      v-model="showEdit"
      size="560px"
      :title="`编辑：${editForm.name || ''}`"
    >
      <el-form :model="editForm" label-width="100px">
        <el-form-item label="姓名">
          <el-input v-model="editForm.name" />
        </el-form-item>
        <el-form-item label="生日">
          <el-input v-model="editForm.birthday" placeholder="1993-08-16" />
        </el-form-item>
        <el-form-item label="出生地">
          <el-input v-model="editForm.placeOfBirth" />
        </el-form-item>
        <el-form-item label="职业">
          <el-input v-model="editForm.knownForDepartment" placeholder="Acting" />
        </el-form-item>
        <el-form-item label="标签">
          <el-select
            v-model="editForm.tags"
            multiple
            filterable
            allow-create
            default-first-option
            style="width: 100%"
            placeholder="输入后回车添加"
          />
        </el-form-item>
        <el-form-item label="标语">
          <el-select
            v-model="editForm.taglines"
            multiple
            filterable
            allow-create
            default-first-option
            style="width: 100%"
            placeholder="输入后回车添加"
          />
        </el-form-item>
        <el-form-item label="简介">
          <el-input
            v-model="editForm.biography"
            type="textarea"
            :rows="8"
            placeholder="演员简介"
          />
        </el-form-item>

        <el-form-item label="头像">
          <div class="avatar-edit">
            <el-avatar :size="80" :src="previewAvatarUrl">
              {{ editForm.name?.charAt(0) || '?' }}
            </el-avatar>
            <el-input
              v-model="avatarUrl"
              placeholder="粘贴图片 URL"
              style="flex: 1"
            />
            <el-upload
              :show-file-list="false"
              :before-upload="onAvatarFileSelected"
            >
              <el-button>本地图片</el-button>
            </el-upload>
          </div>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="saving" @click="saveEdit">
            保存到 Emby
          </el-button>
          <el-button @click="showEdit = false">取消</el-button>
        </el-form-item>
      </el-form>
    </el-drawer>

    <!-- 设置弹窗 -->
    <el-dialog v-model="showConfig" title="Emby 设置" width="520px">
      <el-form :model="config" label-width="120px">
        <el-form-item label="Emby 地址">
          <el-input v-model="config.emby_url" placeholder="http://192.168.1.10:8096" />
        </el-form-item>
        <el-form-item label="Emby API Key">
          <el-input v-model="config.emby_api_key" placeholder="Emby API Key" />
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
import { Edit, View, MagicStick } from '@element-plus/icons-vue'
import { useHttpClient } from '@/http/client'
import { useDeviceType } from '@/composables/useDeviceType'
import {
  embyImageUrl,
  fetchEmbyLibraries,
  fetchEmbyPersonDetail,
  fetchEmbyPersons,
  fetchEmbyProxyConfig,
  fetchEmbyUsers,
  saveEmbyProxyConfig,
  testEmbyProxy,
  updateEmbyPerson,
  uploadEmbyAvatarFile,
  uploadEmbyAvatarFromUrl,
  scrapeEmbyPerson,
  type EmbyLibrary,
  type EmbyPerson,
  type EmbyPersonDetail,
  type EmbyProxyConfig,
} from '@/api/actor'

const http = useHttpClient()
const { isMobile } = useDeviceType()

const currentUserId = ref('')
const libraries = ref<EmbyLibrary[]>([])
const selectedLibrary = ref('')
const persons = ref<EmbyPerson[]>([])
const search = ref('')
const loading = ref(false)
const loadingLibraries = ref(false)

const tableRef = ref<any>()
const selectedRows = ref<EmbyPerson[]>([])

const showDetail = ref(false)
const detail = ref<EmbyPersonDetail | null>(null)

const showEdit = ref(false)
const saving = ref(false)
const editingPersonId = ref('')
const editForm = ref({
  name: '',
  biography: '',
  birthday: '',
  placeOfBirth: '',
  knownForDepartment: '',
  tags: [] as string[],
  taglines: [] as string[],
})
const avatarUrl = ref('')
const localAvatarPreview = ref('')

const previewAvatarUrl = computed(() => {
  if (localAvatarPreview.value) return localAvatarPreview.value
  return avatarUrl.value || embyImageUrl(editingPersonId.value)
})

const showConfig = ref(false)
const testing = ref(false)
const config = ref<EmbyProxyConfig>({
  emby_url: '',
  emby_api_key: '',
})

// ===== 刮削状态 =====
// 'idle' | 'pending' | 'running' | 'success' | 'failed'
const scrapeStatus = ref<Record<string, string>>({})
const scraping = ref(false)
const showScrapeProgress = ref(false)
const scrapeProgress = ref({
  total: 0,
  done: 0,
  success: 0,
  failed: 0,
  percent: 0,
})
const scrapeResults = ref<
  Array<{ id: string; name: string; status: string; message: string }>
>([])

const filteredPersons = computed(() => {
  const kw = search.value.trim().toLowerCase()
  if (!kw) return persons.value
  return persons.value.filter((p) => (p.Name || '').toLowerCase().includes(kw))
})

function onSelectionChange(rows: EmbyPerson[]) {
  selectedRows.value = rows
}

function resultRowClass({ row }: { row: { status: string } }) {
  if (row.status === 'failed') return 'row-failed'
  if (row.status === 'success') return 'row-success'
  return ''
}

async function loadConfig() {
  try {
    const data = await fetchEmbyProxyConfig(http)
    config.value = { ...config.value, ...data }
  } catch {
    // ignore
  }
}

async function loadLibraries() {
  loadingLibraries.value = true
  try {
    const users = await fetchEmbyUsers(http)
    if (users.length === 0) {
      ElMessage.warning('Emby 里没有用户')
      return
    }
    currentUserId.value = users[0].Id
    libraries.value = await fetchEmbyLibraries(http, currentUserId.value)
  } catch (e: any) {
    ElMessage.warning(e?.message || '拉取媒体库失败，请检查设置')
  } finally {
    loadingLibraries.value = false
  }
}

async function loadPersons() {
  if (!currentUserId.value) return
  loading.value = true
  try {
    persons.value = await fetchEmbyPersons(
      http,
      currentUserId.value,
      selectedLibrary.value,
    )
    scrapeStatus.value = {}
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
  detail.value = null
  try {
    detail.value = await fetchEmbyPersonDetail(http, row.Id)
  } catch (e: any) {
    ElMessage.error(e?.message || '拉取详情失败')
  }
}

async function openEdit(row: EmbyPerson) {
  editingPersonId.value = row.Id
  avatarUrl.value = ''
  localAvatarPreview.value = ''
  try {
    const d = await fetchEmbyPersonDetail(http, row.Id)
    editForm.value = {
      name: d.Name || '',
      biography: d.Biography || '',
      birthday: d.Birthday || '',
      placeOfBirth: d.PlaceOfBirth || '',
      knownForDepartment: d.KnownForDepartment || '',
      tags: d.Tags || [],
      taglines: d.Taglines || [],
    }
  } catch {
    editForm.value = {
      name: row.Name,
      biography: '',
      birthday: '',
      placeOfBirth: '',
      knownForDepartment: '',
      tags: [],
      taglines: [],
    }
  }
  showEdit.value = true
}

async function saveEdit() {
  if (!editForm.value.name) {
    ElMessage.warning('姓名必填')
    return
  }
  saving.value = true
  try {
    await updateEmbyPerson(http, editingPersonId.value, editForm.value)
    if (avatarUrl.value) {
      await uploadEmbyAvatarFromUrl(http, editingPersonId.value, avatarUrl.value)
      ElMessage.success('元数据和头像已保存')
    } else {
      ElMessage.success('元数据已保存')
    }
    showEdit.value = false
    await loadPersons()
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function onAvatarFileSelected(file: File) {
  if (!editingPersonId.value) return
  try {
    await uploadEmbyAvatarFile(http, editingPersonId.value, file)
    ElMessage.success('头像已上传')
    localAvatarPreview.value = URL.createObjectURL(file)
  } catch (e: any) {
    ElMessage.error(e?.message || '上传头像失败')
  }
  return false
}

// ============ 刮削逻辑 ============

async function scrapeOne(row: EmbyPerson) {
  if (scraping.value) {
    ElMessage.warning('已有刮削任务进行中')
    return
  }
  scraping.value = true
  scrapeStatus.value[row.Id] = 'running'
  showScrapeProgress.value = true
  scrapeProgress.value = {
    total: 1,
    done: 0,
    success: 0,
    failed: 0,
    percent: 0,
  }
  scrapeResults.value = [
    { id: row.Id, name: row.Name, status: 'running', message: '' },
  ]

  try {
    const result = await scrapeEmbyPerson(http, row.Id, row.Name)
    if (result.ok) {
      scrapeStatus.value[row.Id] = 'success'
      scrapeResults.value[0].status = 'success'
      scrapeResults.value[0].message = result.message || '刮削成功'
      scrapeProgress.value.success = 1
    } else {
      scrapeStatus.value[row.Id] = 'failed'
      scrapeResults.value[0].status = 'failed'
      scrapeResults.value[0].message = result.message || '刮削失败'
      scrapeProgress.value.failed = 1
    }
  } catch (e: any) {
    scrapeStatus.value[row.Id] = 'failed'
    scrapeResults.value[0].status = 'failed'
    scrapeResults.value[0].message = e?.message || '请求失败'
    scrapeProgress.value.failed = 1
  } finally {
    scrapeProgress.value.done = 1
    scrapeProgress.value.percent = 100
    scraping.value = false
  }
}

async function batchScrape() {
  const targets = selectedRows.value
  if (targets.length === 0) {
    ElMessage.warning('请先选择要刮削的演员')
    return
  }
  if (scraping.value) {
    ElMessage.warning('已有刮削任务进行中')
    return
  }

  scraping.value = true
  showScrapeProgress.value = true
  scrapeProgress.value = {
    total: targets.length,
    done: 0,
    success: 0,
    failed: 0,
    percent: 0,
  }
  scrapeResults.value = targets.map((t) => ({
    id: t.Id,
    name: t.Name,
    status: 'pending',
    message: '',
  }))

  for (let i = 0; i < targets.length; i++) {
    const t = targets[i]
    scrapeStatus.value[t.Id] = 'running'
    scrapeResults.value[i].status = 'running'

    try {
      const result = await scrapeEmbyPerson(http, t.Id, t.Name)
      if (result.ok) {
        scrapeStatus.value[t.Id] = 'success'
        scrapeResults.value[i].status = 'success'
        scrapeResults.value[i].message = result.message || '刮削成功'
        scrapeProgress.value.success++
      } else {
        scrapeStatus.value[t.Id] = 'failed'
        scrapeResults.value[i].status = 'failed'
        scrapeResults.value[i].message = result.message || '刮削失败'
        scrapeProgress.value.failed++
      }
    } catch (e: any) {
      scrapeStatus.value[t.Id] = 'failed'
      scrapeResults.value[i].status = 'failed'
      scrapeResults.value[i].message = e?.message || '请求失败'
      scrapeProgress.value.failed++
    }

    scrapeProgress.value.done++
    scrapeProgress.value.percent = Math.round(
      (scrapeProgress.value.done / scrapeProgress.value.total) * 100,
    )
  }

  scraping.value = false
  ElMessage.success(
    `刮削完成：成功 ${scrapeProgress.value.success}，失败 ${scrapeProgress.value.failed}`,
  )
}

async function saveConfig() {
  await saveEmbyProxyConfig(http, config.value)
  ElMessage.success('已保存')
  showConfig.value = false
  await loadAll()
}

async function doTest() {
  testing.value = true
  try {
    const r = await testEmbyProxy(http, config.value)
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
  padding: 20px;
}
.actor-link__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  gap: 12px;
  flex-wrap: wrap;
}
.actor-link__header h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.actor-link__footer {
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
.avatar-edit {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
}
.progress-header {
  margin-bottom: 12px;
}
.progress-text {
  margin-top: 8px;
  font-size: 13px;
  color: #606266;
  display: flex;
  gap: 12px;
}
.progress-text .success {
  color: #67c23a;
}
.progress-text .fail {
  color: #f56c6c;
}

.status-dot {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #dcdfe6;
}
.status-dot.running {
  background: #e6a23c;
}
.status-dot.success {
  background: #67c23a;
}
.status-dot.failed {
  background: #f56c6c;
}

:deep(.row-failed) {
  background: #fef0f0;
}
:deep(.row-success) {
  background: #f0f9eb;
}

@media (max-width: 768px) {
  .actor-link {
    padding: 12px;
  }
  .actor-link__header {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
    margin-bottom: 12px;
  }
  .actor-link__header h2 {
    font-size: 18px;
  }
  .toolbar {
    gap: 6px;
  }
  .toolbar .el-select,
  .toolbar .el-input {
    width: 100% !important;
  }
  :deep(.el-table__header-wrapper),
  :deep(.el-table__body-wrapper) {
    font-size: 12px;
  }
  :deep(.el-table .cell) {
    padding: 0 4px;
  }
  :deep(.el-table .el-button + .el-button) {
    margin-left: 4px;
  }
  :deep(.el-table .el-button.is-link) {
    padding: 4px;
  }
  :deep(.el-table .el-button.is-link .el-icon) {
    font-size: 16px;
  }
}

@media (max-width: 480px) {
  .actor-link {
    padding: 8px;
  }
  :deep(.el-table__header-wrapper),
  :deep(.el-table__body-wrapper) {
    font-size: 11px;
  }
}
</style>

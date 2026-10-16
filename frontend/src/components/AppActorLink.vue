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
        <el-button type="primary" :loading="loading" @click="loadAll">刷新</el-button>
      </div>
    </div>

    <el-table
      :data="filteredPersons"
      border
      stripe
      v-loading="loading"
      style="width: 100%"
    >
      <el-table-column label="头像" width="70">
        <template #default="{ row }">
          <el-avatar :size="40" :src="embyImageUrl(row.Id)">
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
        class-name="hide-mobile"
      />
      <el-table-column label="操作" width="120" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button link type="primary" @click="openDetail(row)">详情</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="actor-link__footer">共 {{ filteredPersons.length }} 位演员</div>

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
import { useHttpClient } from '@/http/client'
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
  type EmbyLibrary,
  type EmbyPerson,
  type EmbyPersonDetail,
  type EmbyProxyConfig,
} from '@/api/actor'

const http = useHttpClient()

const currentUserId = ref('')
const libraries = ref<EmbyLibrary[]>([])
const selectedLibrary = ref('')
const persons = ref<EmbyPerson[]>([])
const search = ref('')
const loading = ref(false)
const loadingLibraries = ref(false)

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

const filteredPersons = computed(() => {
  const kw = search.value.trim().toLowerCase()
  if (!kw) return persons.value
  return persons.value.filter((p) => (p.Name || '').toLowerCase().includes(kw))
})

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
  if (!selectedLibrary.value || !currentUserId.value) return
  loading.value = true
  try {
    persons.value = await fetchEmbyPersons(
      http,
      currentUserId.value,
      selectedLibrary.value,
    )
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
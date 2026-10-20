<template>
  <div class="actor-link">
    <div class="actor-link__header">
      <h2>Emby 演员管理</h2>
      <div class="toolbar">
        <el-select v-model="selectedLibrary" placeholder="选择媒体库" :style="isMobile ? 'width: 100%' : 'width: 200px'" :loading="loadingLibraries" @change="loadPersons">
          <el-option label="全部媒体库" value="" />
          <el-option v-for="lib in libraries" :key="lib.Id" :label="lib.Name" :value="lib.Id" />
        </el-select>
        <el-input v-model="search" placeholder="搜索演员名字" clearable :style="isMobile ? 'width: 100%' : 'width: 180px'" />
        
        <el-checkbox v-model="onlyNoAvatar">仅显示无头像</el-checkbox>
        
        <el-button @click="showConfig = true">设置</el-button>
        <el-button @click="openScript">脚本</el-button>
        <el-button :loading="loading" @click="loadAll">刷新</el-button>
        <el-button type="warning" :disabled="selectedRows.length === 0 || scraping" :loading="scraping" @click="batchScrape">
          批量刮削（{{ selectedRows.length }}）
        </el-button>
        
        <el-dropdown @command="handleBatchAction" v-if="selectedRows.length > 0">
          <el-button>批量操作 <el-icon class="el-icon--right"><ArrowDown /></el-icon></el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="clear_tags">清空选中标签</el-dropdown-item>
              <el-dropdown-item command="clear_taglines">清空选中档案</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </div>

    <el-table :data="filteredPersons" border stripe v-loading="loading" style="width: 100%" @selection-change="onSelectionChange">
      <el-table-column type="selection" width="48" reserve-selection />
      <el-table-column label="头像" :width="isMobile ? 56 : 70">
        <template #default="{ row }">
          <el-avatar :size="isMobile ? 32 : 40" :src="embyImageUrl(row.Id, avatarVersions[row.Id])">
            {{ row.Name?.charAt(0) || '?' }}
          </el-avatar>
        </template>
      </el-table-column>
      <el-table-column prop="Name" label="姓名" min-width="100" sortable show-overflow-tooltip />
      
      <el-table-column label="档案速览" min-width="160" show-overflow-tooltip>
        <template #default="{ row }">
          <span style="color: #67c23a; font-size: 12px;">
            {{ extractTagline(row.Taglines) }}
          </span>
        </template>
      </el-table-column>
      
      <el-table-column v-if="!isMobile" prop="Id" label="Emby ID" min-width="180" show-overflow-tooltip />
      <el-table-column label="刮削状态" :width="isMobile ? 60 : 110" align="center">
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
      <el-table-column label="操作" :width="isMobile ? 96 : 180" fixed="right" align="center">
        <template #default="{ row }">
          <template v-if="isMobile">
            <el-tooltip content="编辑" placement="top"><el-button link type="primary" :icon="Edit" @click="openEdit(row)" /></el-tooltip>
            <el-tooltip content="详情" placement="top"><el-button link type="primary" :icon="View" @click="openDetail(row)" /></el-tooltip>
            <el-tooltip content="刮削" placement="top"><el-button link type="warning" :icon="MagicStick" :loading="scrapeStatus[row.Id] === 'running'" @click="scrapeOne(row)" /></el-tooltip>
          </template>
          <template v-else>
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="primary" @click="openDetail(row)">详情</el-button>
            <el-button link type="warning" :loading="scrapeStatus[row.Id] === 'running'" @click="scrapeOne(row)">刮削</el-button>
          </template>
        </template>
      </el-table-column>
    </el-table>

    <div class="actor-link__footer">共 {{ filteredPersons.length }} 位演员</div>

    <!-- 批量刮削进度弹窗 -->
    <el-dialog v-model="showScrapeProgress" title="刮削进度" :width="isMobile ? '90%' : '640px'" :close-on-click-modal="false" :close-on-press-escape="false" :show-close="!scraping">
      <div class="progress-header">
        <el-progress :percentage="scrapeProgress.percent" :status="scrapeProgress.failed > 0 ? 'warning' : undefined" />
        <div class="progress-text">
          已处理 {{ scrapeProgress.done }} / {{ scrapeProgress.total }}
          <span v-if="scrapeProgress.success > 0" class="success">成功 {{ scrapeProgress.success }}</span>
          <span v-if="scrapeProgress.failed > 0" class="fail">失败 {{ scrapeProgress.failed }}</span>
        </div>
      </div>
      <el-table :data="scrapeResults" max-height="360" border size="small" :row-class-name="resultRowClass">
        <el-table-column prop="name" label="演员" min-width="100" show-overflow-tooltip />
        <el-table-column label="结果" width="80" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.status === 'success'" type="success" size="small">成功</el-tag>
            <el-tag v-else-if="row.status === 'failed'" type="danger" size="small">失败</el-tag>
            <el-tag v-else-if="row.status === 'running'" type="warning" size="small">处理中</el-tag>
            <el-tag v-else type="info" size="small">等待</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="message" label="详情" min-width="160" show-overflow-tooltip />
      </el-table>
      <template #footer>
        <el-button v-if="!scraping" @click="showScrapeProgress = false">关闭</el-button>
        <el-button v-else disabled>处理中…</el-button>
      </template>
    </el-dialog>

    <!-- 详情抽屉 -->
    <el-drawer v-model="showDetail" :size="isMobile ? '100%' : '520px'" :title="detail?.name || '演员详情'">
      <div v-if="detail" class="detail">
        <div class="avatar-wrap">
          <el-avatar :size="isMobile ? 80 : 120" :src="embyImageUrl(detail.id, avatarVersions[detail.id])">
            {{ detail.name?.charAt(0) || '?' }}
          </el-avatar>
        </div>
        <div class="meta">
          <div class="row"><span class="label">姓名</span><span>{{ detail.name || '—' }}</span></div>
          <div class="row"><span class="label">类型</span><span>{{ detail.character || '—' }}</span></div>
          <div class="row"><span class="label">档案</span><span>{{ (detail.taglines || []).join(' | ') || '—' }}</span></div>
          <div class="row"><span class="label">生日</span><span>{{ detail.birthday || '—' }}</span></div>
          <div class="row"><span class="label">逝世</span><span>{{ detail.deathday || '—' }}</span></div>
          <div class="row"><span class="label">出道</span><span>{{ detail.premiereDate || '—' }}</span></div>
          <div class="row"><span class="label">年份</span><span>{{ detail.productionYear || '—' }}</span></div>
          <div class="row"><span class="label">出生地</span><span>{{ detail.placeOfBirth || '—' }}</span></div>
          <div class="row"><span class="label">地区</span><span>{{ (detail.productionLocations || []).join(', ') || '—' }}</span></div>
          <div class="row"><span class="label">职业</span><span>{{ detail.knownForDepartment || '—' }}</span></div>
          <div class="row"><span class="label">标签</span><span>{{ (detail.tags || []).join(', ') || '—' }}</span></div>
          <div class="row">
            <span class="label">外部 ID</span>
            <span class="provider-links">
              <template v-if="detail.providerIds && Object.keys(detail.providerIds).length">
                <a v-for="(val, key) in detail.providerIds" :key="key" :href="getProviderUrl(key as string, val as string)" target="_blank" class="provider-link">
                  {{ key }}
                </a>
              </template>
              <span v-else>—</span>
            </span>
          </div>
        </div>
        <div class="bio">
          <div class="label">简介</div>
          <p>{{ detail.biography || '暂无简介' }}</p>
        </div>
      </div>
      <el-empty v-else description="暂无详情" />
    </el-drawer>

    <!-- 编辑抽屉 -->
    <el-drawer v-model="showEdit" :size="isMobile ? '100%' : '560px'" :title="`编辑：${editForm.name || ''}`">
      <el-form :model="editForm" :label-position="isMobile ? 'top' : 'right'" :label-width="isMobile ? 'auto' : '100px'">
        
        <el-form-item label="外部ID匹配">
          <el-input v-model="forceMatchId" placeholder="粘贴 TPDB 或 JavStash 的 UUID" style="flex: 1">
            <template #append>
              <el-button :loading="matching" @click="doForceMatch">拉取</el-button>
            </template>
          </el-input>
        </el-form-item>

        <el-form-item label="类型"><el-input v-model="editForm.character" placeholder="Actor" /></el-form-item>
        <el-form-item label="姓名"><el-input v-model="editForm.name" /></el-form-item>
        <el-form-item label="生日"><el-input v-model="editForm.birthday" placeholder="YYYY-MM-DD" /></el-form-item>
        <el-form-item label="逝世"><el-input v-model="editForm.deathday" placeholder="YYYY-MM-DD" /></el-form-item>
        <el-form-item label="出道年份"><el-input v-model="editForm.premiereDate" placeholder="YYYY-MM-DD" /></el-form-item>
        <el-form-item label="年份"><el-input-number v-model="editForm.productionYear" :controls="false" placeholder="YYYY" style="width: 100%" /></el-form-item>
        <el-form-item label="出生地"><el-input v-model="editForm.placeOfBirth" /></el-form-item>
        <el-form-item label="地区"><el-select v-model="editForm.productionLocations" multiple filterable allow-create default-first-option style="width: 100%" placeholder="输入后回车添加" /></el-form-item>
        <el-form-item label="职业"><el-input v-model="editForm.knownForDepartment" placeholder="Acting" /></el-form-item>
        
        <el-form-item label="标签">
          <el-select v-model="editForm.tags" multiple filterable allow-create default-first-option style="width: 100%" placeholder="输入后回车添加" />
        </el-form-item>
        <el-form-item label="标语(档案)">
          <el-select v-model="editForm.taglines" multiple filterable allow-create default-first-option style="width: 100%" placeholder="输入后回车添加" />
        </el-form-item>
        
        <el-form-item label="简介">
          <el-input v-model="editForm.biography" type="textarea" :rows="isMobile ? 5 : 8" placeholder="演员简介" />
        </el-form-item>
        
        <el-form-item label="头像">
          <div class="avatar-edit" :style="isMobile ? 'flex-direction: column; align-items: flex-start;' : ''">
            <el-avatar :size="80" :src="previewAvatarUrl">{{ editForm.name?.charAt(0) || '?' }}</el-avatar>
            <el-input v-model="avatarUrl" placeholder="粘贴图片 URL" :style="isMobile ? 'width: 100%;' : 'flex: 1;'" />
            <el-upload :show-file-list="false" :before-upload="onAvatarFileSelected"><el-button :style="isMobile ? 'width: 100%;' : ''">本地图片</el-button></el-upload>
          </div>
        </el-form-item>
        
        <el-form-item label="网络选图" v-if="fetchedImages.length > 0">
          <div class="image-list">
            <img v-for="(img, idx) in fetchedImages" :key="idx" :src="img" @click="avatarUrl = img" :class="{ active: avatarUrl === img }" />
          </div>
        </el-form-item>
        
        <el-form-item>
          <el-button type="primary" :loading="saving" @click="saveEdit" style="width: 100%">保存到 Emby</el-button>
        </el-form-item>
        <el-form-item>
          <el-button @click="showEdit = false" style="width: 100%">取消</el-button>
        </el-form-item>
      </el-form>
    </el-drawer>

    <!-- 设置弹窗 -->
    <el-dialog v-model="showConfig" title="Emby / 各类数据源设置" :width="isMobile ? '90%' : '580px'">
      <el-form :model="config" :label-position="isMobile ? 'top' : 'right'" :label-width="isMobile ? 'auto' : '150px'">
        <el-form-item label="Emby 地址"><el-input v-model="config.emby_url" placeholder="http://192.168.1.10:8096" /></el-form-item>
        <el-form-item label="Emby API Key"><el-input v-model="config.emby_api_key" placeholder="Emby API Key" /></el-form-item>
        <el-form-item label="StashDB API Key"><el-input v-model="config.stashdb_api_key" placeholder="stashdb.org 获取" /></el-form-item>
        <el-form-item label="JavStash API Key"><el-input v-model="config.javstash_api_key" placeholder="javstash.org 获取" /></el-form-item>
        <el-form-item label="TPDB API Key"><el-input v-model="config.tpdb_api_key" placeholder="theporndb.net 获取" /></el-form-item>
        
        <!-- ★ 新增的 DeepL Key 输入框 -->
        <el-form-item label="DeepL API Key">
          <el-input v-model="config.deepl_api_key" placeholder="用于翻译英文演员简介，留空则不翻译" />
        </el-form-item>

        <el-form-item label="维基百科联系邮箱"><el-input v-model="config.wiki_email" placeholder="icezxf555@gmail.com" /></el-form-item>
        <el-form-item>
          <el-button :loading="testing" @click="doTest" style="width: 100%; margin-bottom: 10px">测试 Emby 连接</el-button>
          <el-button type="primary" @click="saveConfig" style="width: 100%; margin-left: 0;">保存</el-button>
        </el-form-item>
      </el-form>
    </el-dialog>

    <!-- 脚本调试抽屉 -->
    <el-drawer v-model="showScript" :size="isMobile ? '100%' : '760px'" title="刮削脚本">
      <div class="script-panel">
        <div class="script-toolbar">
          <el-tag v-if="scriptIsDefault" type="info">默认脚本</el-tag>
          <el-tag v-else type="success">自定义脚本</el-tag>
          <div class="spacer" />
          <el-button size="small" @click="reloadScript">重新加载</el-button>
          <el-button size="small" @click="doResetScript">恢复默认</el-button>
          <el-button size="small" type="primary" @click="doSaveScript">保存</el-button>
        </div>
        <el-input v-model="scriptText" type="textarea" :rows="isMobile ? 15 : 22" placeholder="YAML 脚本" class="script-editor" />
        <div class="script-test">
          <h4>试运行</h4>
          <el-form :inline="!isMobile">
            <el-form-item label="Emby 演员 ID" :style="isMobile ? 'width: 100%' : 'width: 260px'"><el-input v-model="testPersonId" placeholder="local-xxx" /></el-form-item>
            <el-form-item label="姓名" :style="isMobile ? 'width: 100%' : 'width: 160px'"><el-input v-model="testPersonName" /></el-form-item>
            <el-form-item><el-button type="warning" :loading="scriptRunning" @click="doRunScript" :style="isMobile ? 'width: 100%' : ''">执行</el-button></el-form-item>
          </el-form>
          
          <div v-if="scriptResults.length > 0" class="script-results">
            <div v-for="r in scriptResults" :key="r.index" :class="['result-row', r.ok ? 'ok' : 'fail']" @click="showLogDetail(r)" style="cursor: pointer;">
              <span class="idx">#{{ r.index + 1 }}</span>
              <span class="action">{{ r.action }}</span>
              <span class="dur">{{ r.duration }}</span>
              <span class="msg">{{ r.message }}</span>
            </div>
          </div>
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Edit, View, MagicStick, ArrowDown } from '@element-plus/icons-vue'
import { useHttpClient } from '@/http/client'
import { useDeviceType } from '@/composables/useDeviceType'
import {
  embyImageUrl, fetchEmbyLibraries, fetchEmbyPersonDetail, fetchEmbyPersons, fetchEmbyProxyConfig,
  fetchEmbyUsers, fetchScrapeScript, resetScrapeScript, saveEmbyProxyConfig, saveScrapeScript,
  scrapeEmbyPerson, testEmbyProxy, testScrapeScript, updateEmbyPerson, uploadEmbyAvatarFile,
  uploadEmbyAvatarFromUrl, previewActor, 
  type EmbyLibrary, type EmbyPerson, type EmbyPersonDetail, type EmbyProxyConfig,
} from '@/api/actor'

const http = useHttpClient()
const { isMobile } = useDeviceType()

const currentUserId = ref('')
const libraries = ref<EmbyLibrary[]>([])
const selectedLibrary = ref('')
const persons = ref<EmbyPerson[]>([])
const search = ref('')
const onlyNoAvatar = ref(false) // ★ 仅显示无头像
const loading = ref(false)
const loadingLibraries = ref(false)
const selectedRows = ref<EmbyPerson[]>([])
const showDetail = ref(false)
const detail = ref<EmbyPersonDetail | null>(null)
const showEdit = ref(false)
const saving = ref(false)
const editingPersonId = ref('')
const editForm = ref({ 
  name: '', 
  character: '',
  biography: '', 
  birthday: '', 
  deathday: '', 
  premiereDate: '', 
  productionYear: 0, 
  placeOfBirth: '', 
  knownForDepartment: '', 
  productionLocations: [] as string[],
  tags: [] as string[], 
  taglines: [] as string[] 
})
const avatarUrl = ref('')
const localAvatarPreview = ref('')
const fetchedImages = ref<string[]>([])
const avatarVersions = ref<Record<string, number>>({})
const forceMatchId = ref('')
const matching = ref(false)

const previewAvatarUrl = computed(() => {
  if (localAvatarPreview.value) return localAvatarPreview.value
  return avatarUrl.value || embyImageUrl(editingPersonId.value)
})

const showConfig = ref(false)
const testing = ref(false)
// ★ 给 config 初始化加上 deepl_api_key 空值
const config = ref<EmbyProxyConfig>({
  emby_url: '', 
  emby_api_key: '', 
  stashdb_endpoint: '', 
  stashdb_api_key: '', 
  javstash_endpoint: '', 
  javstash_api_key: '', 
  tpdb_api_key: '', 
  wiki_email: '',
  deepl_api_key: '', 
})

const scrapeStatus = ref<Record<string, string>>({})
const scraping = ref(false)
const showScrapeProgress = ref(false)
const scrapeProgress = ref({ total: 0, done: 0, success: 0, failed: 0, percent: 0 })
const scrapeResults = ref<Array<{ id: string; name: string; status: string; message: string }>>([])

const showScript = ref(false)
const scriptText = ref('')
const scriptIsDefault = ref(true)
const scriptRunning = ref(false)
const testPersonId = ref('')
const testPersonName = ref('')
const scriptResults = ref<Array<{ index: number; action: string; ok: boolean; message: string; duration: string }>>([])

const filteredPersons = computed(() => {
  let result = persons.value
  
  // 1. 名字过滤
  const kw = search.value.trim().toLowerCase()
  if (kw) {
    result = result.filter((p) => (p.Name || '').toLowerCase().includes(kw))
  }
  
  // 2. 无头像过滤
  if (onlyNoAvatar.value) {
    result = result.filter((p) => {
      // 兼容判断：如果 PrimaryImageTag 没有，且 ImageTags 里也没有 Primary
      return !p.PrimaryImageTag && !(p as any).ImageTags?.Primary
    })
  }
  
  return result
})

function onSelectionChange(rows: EmbyPerson[]) { selectedRows.value = rows }
function resultRowClass({ row }: { row: { status: string } }) {
  if (row.status === 'failed') return 'row-failed'
  if (row.status === 'success') return 'row-success'
  return ''
}

function extractTagline(taglines: string[] | undefined) {
  if (!taglines || taglines.length === 0) return '—'
  const doc = taglines.find(t => t.includes('身高'))
  if (doc) return doc.replace('【档案】', '')
  return taglines[0]
}

async function handleBatchAction(command: string) {
  if (command === 'clear_tags') {
    await ElMessageBox.confirm(`确定清空选中的 ${selectedRows.value.length} 个演员的标签吗？`, '提示', { type: 'warning' })
    for (const row of selectedRows.value) {
      await updateEmbyPerson(http, row.Id, { name: row.Name, tags: [] })
    }
    ElMessage.success('标签已清空')
    await loadPersons()
  } else if (command === 'clear_taglines') {
    await ElMessageBox.confirm(`确定清空选中的 ${selectedRows.value.length} 个演员的档案(标语)吗？`, '提示', { type: 'warning' })
    for (const row of selectedRows.value) {
      await updateEmbyPerson(http, row.Id, { name: row.Name, taglines: [] })
    }
    ElMessage.success('档案已清空')
    await loadPersons()
  }
}

function showLogDetail(r: any) {
  if (!r.message) return
  ElMessageBox.alert(
    `<pre style="white-space: pre-wrap; word-wrap: break-word; max-height: 400px; overflow-y: auto; font-size: 12px;">${r.message}</pre>`,
    `步骤 #${r.index + 1} 详情`,
    { dangerouslyUseHTMLString: true }
  )
}

function getProviderUrl(key: string, id: string): string {
  switch (key) {
    case 'Tmdb': return `https://www.themoviedb.org/person/${id}`
    case 'Imdb': return `https://www.imdb.com/name/${id}/`
    case 'Wikidata': return `https://www.wikidata.org/wiki/${id}`
    case 'StashDB': return `https://stashdb.org/performers/${id}`
    case 'JavStash': return `https://javstash.org/performers/${id}`
    case 'Wikipedia': return `https://${id}`
    case 'Porndb': return `https://theporndb.net/performers/${id}`
    default: return '#'
  }
}

async function loadConfig() {
  try {
    const data = await fetchEmbyProxyConfig(http)
    config.value = { ...config.value, ...data }
  } catch {}
}

async function loadLibraries() {
  loadingLibraries.value = true
  try {
    const users = await fetchEmbyUsers(http)
    if (users.length === 0) { ElMessage.warning('Emby 里没有用户'); return }
    currentUserId.value = users[0].Id
    libraries.value = await fetchEmbyLibraries(http, currentUserId.value)
  } catch (e: any) { ElMessage.warning(e?.message || '拉取媒体库失败') } finally { loadingLibraries.value = false }
}

async function loadPersons() {
  if (!currentUserId.value) return
  loading.value = true
  try {
    persons.value = await fetchEmbyPersons(http, currentUserId.value, selectedLibrary.value)
    scrapeStatus.value = {}
  } catch (e: any) { ElMessage.error(e?.message || '拉取演员失败') } finally { loading.value = false }
}

async function loadAll() { await loadLibraries(); await loadPersons() }

async function openDetail(row: EmbyPerson) {
  showDetail.value = true
  detail.value = null
  try { detail.value = await fetchEmbyPersonDetail(http, row.Id) } catch (e: any) { ElMessage.error(e?.message || '拉取详情失败') }
}

async function openEdit(row: EmbyPerson) {
  editingPersonId.value = row.Id
  avatarUrl.value = ''
  localAvatarPreview.value = ''
  fetchedImages.value = []
  forceMatchId.value = ''
  
  try {
    const d = await fetchEmbyPersonDetail(http, row.Id)
    editForm.value = {
      name: d.name || '', 
      character: d.character || '',
      biography: d.biography || '', 
      birthday: d.birthday || '',
      deathday: d.deathday || '',
      premiereDate: d.premiereDate || '', 
      productionYear: d.productionYear || 0,
      placeOfBirth: d.placeOfBirth || '', 
      knownForDepartment: d.knownForDepartment || '',
      productionLocations: d.productionLocations || [],
      tags: d.tags || [], 
      taglines: d.taglines || [],
    }
    
    try {
      const preview = await previewActor(http, editForm.value.name || row.Name)
      if (preview.Images && preview.Images.length > 0) {
        fetchedImages.value = preview.Images
        if (!avatarUrl.value) avatarUrl.value = preview.Images[0]
      }
    } catch (e) {
      console.error('拉取网络图片失败', e)
    }
  } catch {
    editForm.value = { 
      name: row.Name, character: '', biography: '', birthday: '', deathday: '', 
      premiereDate: '', productionYear: 0, placeOfBirth: '', knownForDepartment: '', 
      productionLocations: [], tags: [], taglines: [] 
    }
  }
  showEdit.value = true
}

async function doForceMatch() {
  if (!forceMatchId.value) return
  matching.value = true
  try {
    const preview = await previewActor(http, forceMatchId.value)
    
    if (preview.Name) editForm.value.name = preview.Name
    if (preview.BirthDate) editForm.value.birthday = preview.BirthDate
    if (preview.PlaceOfBirth) editForm.value.placeOfBirth = preview.PlaceOfBirth
    if (preview.Bio) editForm.value.biography = preview.Bio
    
    // 拼装档案（Taglines）
    if (preview.Height || preview.CupSize) {
      const doc = `【档案】身高 ${preview.Height || 0}cm | 体重 ${preview.Weight || 0}kg | 三围 ${preview.Waist || 0}-${preview.Hip || 0} | ${preview.CupSize || ''}罩杯`
      const oldAlias = editForm.value.taglines.find(t => t.includes('别名'))
      if (oldAlias) {
        editForm.value.taglines = [doc, oldAlias]
      } else {
        editForm.value.taglines = [doc]
      }
    }
    
    if (preview.Images && preview.Images.length > 0) {
      fetchedImages.value = preview.Images
      avatarUrl.value = preview.Images[0]
    }
    
    ElMessage.success('拉取成功，档案和简介已更新')
  } catch (e: any) {
    ElMessage.error(e?.message || '拉取失败，请检查 ID 是否正确')
  } finally {
    matching.value = false
  }
}

async function saveEdit() {
  if (!editForm.value.name) { ElMessage.warning('姓名必填'); return }
  saving.value = true
  try {
    await updateEmbyPerson(http, editingPersonId.value, editForm.value)
    
    const hasNewAvatar = !!avatarUrl.value
    if (hasNewAvatar) {
      try {
        await uploadEmbyAvatarFromUrl(http, editingPersonId.value, avatarUrl.value)
      } catch (e) {
        console.error('图片下载超时', e)
        ElMessage.warning('元数据已保存，但图片下载超时了')
      }
    }

    const target = persons.value.find(p => p.Id === editingPersonId.value)
    if (target) target.Name = editForm.value.name
    
    if (detail.value && detail.value.id === editingPersonId.value) {
      detail.value.name = editForm.value.name
      detail.value.character = editForm.value.character
      detail.value.biography = editForm.value.biography
      detail.value.birthday = editForm.value.birthday
      detail.value.deathday = editForm.value.deathday
      detail.value.premiereDate = editForm.value.premiereDate
      detail.value.productionYear = editForm.value.productionYear
      detail.value.placeOfBirth = editForm.value.placeOfBirth
      detail.value.knownForDepartment = editForm.value.knownForDepartment
      detail.value.productionLocations = [...editForm.value.productionLocations]
      detail.value.tags = [...editForm.value.tags]
      detail.value.taglines = [...editForm.value.taglines]
    }

    if (hasNewAvatar) {
      avatarVersions.value[editingPersonId.value] = Date.now()
    }

    ElMessage.success(hasNewAvatar ? '元数据和头像已保存并更新' : '元数据已保存并更新')
    showEdit.value = false
    loadPersons().catch(() => {})
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
    avatarVersions.value[editingPersonId.value] = Date.now()
  } catch (e: any) { ElMessage.error(e?.message || '上传头像失败') }
  return false
}

async function scrapeOne(row: EmbyPerson) {
  if (scraping.value) { ElMessage.warning('已有刮削任务进行中'); return }
  scraping.value = true; scrapeStatus.value[row.Id] = 'running'; showScrapeProgress.value = true
  scrapeProgress.value = { total: 1, done: 0, success: 0, failed: 0, percent: 0 }
  scrapeResults.value = [{ id: row.Id, name: row.Name, status: 'running', message: '' }]
  try {
    const result = await scrapeEmbyPerson(http, row.Id, row.Name)
    if (result.ok) {
      scrapeStatus.value[row.Id] = 'success'; scrapeResults.value[0].status = 'success'; scrapeResults.value[0].message = result.message || '刮削成功'; scrapeProgress.value.success = 1
      avatarVersions.value[row.Id] = Date.now()
      
      // 实时更新档案速览
      fetchEmbyPersonDetail(http, row.Id).then(d => {
        const target = persons.value.find(p => p.Id === row.Id)
        if (target) target.Taglines = d.taglines
      })
    } else {
      scrapeStatus.value[row.Id] = 'failed'; scrapeResults.value[0].status = 'failed'; scrapeResults.value[0].message = result.message || '刮削失败'; scrapeProgress.value.failed = 1
    }
  } catch (e: any) {
    scrapeStatus.value[row.Id] = 'failed'; scrapeResults.value[0].status = 'failed'; scrapeResults.value[0].message = e?.message || '请求失败'; scrapeProgress.value.failed = 1
  } finally { scrapeProgress.value.done = 1; scrapeProgress.value.percent = 100; scraping.value = false }
}

async function batchScrape() {
  const targets = selectedRows.value
  if (targets.length === 0) { ElMessage.warning('请先选择要刮削的演员'); return }
  if (scraping.value) { ElMessage.warning('已有刮削任务进行中'); return }
  scraping.value = true; showScrapeProgress.value = true
  scrapeProgress.value = { total: targets.length, done: 0, success: 0, failed: 0, percent: 0 }
  scrapeResults.value = targets.map((t) => ({ id: t.Id, name: t.Name, status: 'pending', message: '' }))

  for (let i = 0; i < targets.length; i++) {
    const t = targets[i]
    scrapeStatus.value[t.Id] = 'running'; scrapeResults.value[i].status = 'running'
    try {
      const result = await scrapeEmbyPerson(http, t.Id, t.Name)
      if (result.ok) {
        scrapeStatus.value[t.Id] = 'success'; scrapeResults.value[i].status = 'success'; scrapeResults.value[i].message = result.message || '刮削成功'; scrapeProgress.value.success++
        avatarVersions.value[t.Id] = Date.now()
        
        // 实时更新档案速览
        fetchEmbyPersonDetail(http, t.Id).then(d => {
          const target = persons.value.find(p => p.Id === t.Id)
          if (target) target.Taglines = d.taglines
        })
      } else {
        scrapeStatus.value[t.Id] = 'failed'; scrapeResults.value[i].status = 'failed'; scrapeResults.value[i].message = result.message || '刮削失败'; scrapeProgress.value.failed++
      }
    } catch (e: any) {
      scrapeStatus.value[t.Id] = 'failed'; scrapeResults.value[i].status = 'failed'; scrapeResults.value[i].message = e?.message || '请求失败'; scrapeProgress.value.failed++
    }
    scrapeProgress.value.done++; scrapeProgress.value.percent = Math.round((scrapeProgress.value.done / scrapeProgress.value.total) * 100)
  }
  scraping.value = false
  ElMessage.success(`刮削完成：成功 ${scrapeProgress.value.success}，失败 ${scrapeProgress.value.failed}`)
}

async function openScript() { showScript.value = true; await reloadScript() }
async function reloadScript() {
  try { const r = await fetchScrapeScript(http); scriptText.value = r.script; scriptIsDefault.value = r.is_default } catch (e: any) { ElMessage.error(e?.message || '加载脚本失败') }
}
async function doSaveScript() {
  try { await saveScrapeScript(http, scriptText.value); ElMessage.success('已保存'); scriptIsDefault.value = false } catch (e: any) { ElMessage.error(e?.message || '保存失败') }
}
async function doResetScript() {
  try { const s = await resetScrapeScript(http); scriptText.value = s; scriptIsDefault.value = true; ElMessage.success('已恢复默认') } catch (e: any) { ElMessage.error(e?.message || '恢复失败') }
}
async function doRunScript() {
  if (!testPersonId.value) { ElMessage.warning('请填 Emby 演员 ID'); return }
  scriptRunning.value = true; scriptResults.value = []
  try {
    const r = await testScrapeScript(http, { script: scriptText.value, person_id: testPersonId.value, person_name: testPersonName.value })
    scriptResults.value = r.results || []; if (r.ok) ElMessage.success('执行成功'); else ElMessage.error(r.error || '执行失败')
  } catch (e: any) { ElMessage.error(e?.message || '执行失败') } finally { scriptRunning.value = false }
}
async function saveConfig() {
  await saveEmbyProxyConfig(http, config.value); ElMessage.success('已保存'); showConfig.value = false; await loadAll()
}
async function doTest() {
  testing.value = true
  try {
    const r = await testEmbyProxy(http, { emby_url: config.value.emby_url, emby_api_key: config.value.emby_api_key })
    if (r.ok) ElMessage.success(r.message); else ElMessage.error(r.message)
  } catch (e: any) { ElMessage.error(e?.message || '测试失败') } finally { testing.value = false }
}

onMounted(async () => { await loadConfig(); await loadAll() })
</script>

<style scoped>
.actor-link { padding: 20px; }
.actor-link__header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; gap: 12px; flex-wrap: wrap; }
.actor-link__header h2 { margin: 0; font-size: 18px; font-weight: 600; }
.toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; width: 100%; }
.actor-link__footer { margin-top: 12px; text-align: right; color: #909399; font-size: 13px; }
.detail { display: flex; flex-direction: column; gap: 16px; }
.detail .avatar-wrap { display: flex; justify-content: center; }
.detail .meta { display: flex; flex-direction: column; gap: 8px; }
.detail .row { display: flex; gap: 12px; }
.detail .label { width: 80px; color: #909399; flex-shrink: 0; }
.detail .bio p { margin: 6px 0 0; line-height: 1.6; color: #303133; white-space: pre-wrap; }
.avatar-edit { display: flex; align-items: center; gap: 12px; width: 100%; }
.progress-header { margin-bottom: 12px; }
.progress-text { margin-top: 8px; font-size: 13px; color: #606266; display: flex; gap: 12px; }
.progress-text .success { color: #67c23a; }
.progress-text .fail { color: #f56c6c; }
.status-dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; background: #dcdfe6; }
.status-dot.running { background: #e6a23c; }
.status-dot.success { background: #67c23a; }
.status-dot.failed { background: #f56c6c; }
:deep(.row-failed) { background: #fef0f0; }
:deep(.row-success) { background: #f0f9eb; }
.provider-links { display: flex; gap: 8px; flex-wrap: wrap; }
.provider-link { display: inline-block; padding: 2px 8px; background: #f0f9eb; color: #67c23a; border-radius: 4px; text-decoration: none; font-size: 12px; border: 1px solid #e1f3d8; transition: all 0.2s; }
.provider-link:hover { background: #67c23a; color: #fff; }

.image-list {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  max-height: 200px;
  overflow-y: auto;
  padding: 4px;
  border: 1px dashed #dcdfe6;
  border-radius: 4px;
}
.image-list img {
  width: 60px;
  height: 80px;
  object-fit: cover;
  border-radius: 4px;
  cursor: pointer;
  border: 2px solid transparent;
  transition: all 0.2s;
}
.image-list img:hover { border-color: #a0cfff; }
.image-list img.active { border-color: #409eff; box-shadow: 0 0 8px rgba(64, 158, 255, 0.5); }

.script-panel { display: flex; flex-direction: column; gap: 16px; }
.script-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.script-toolbar .spacer { flex: 1; }
.script-editor :deep(textarea) { font-family: 'Consolas', 'Monaco', monospace; font-size: 13px; line-height: 1.5; }
.script-test h4 { margin: 0 0 8px; }
.script-results { margin-top: 12px; border: 1px solid #eee; border-radius: 4px; overflow: hidden; }
.result-row { display: flex; align-items: center; gap: 8px; padding: 6px 10px; font-size: 13px; border-bottom: 1px solid #f5f5f5; }
.result-row:last-child { border-bottom: none; }
.result-row.ok { background: #f0f9eb; }
.result-row.fail { background: #fef0f0; }
.result-row .idx { width: 30px; color: #999; }
.result-row .action { width: 180px; color: #409eff; font-family: monospace; }
.result-row .dur { width: 60px; color: #999; }
.result-row .msg { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* ★ 移动端全面适配 */
@media (max-width: 768px) {
  .actor-link { padding: 12px; }
  .actor-link__header { flex-direction: column; align-items: stretch; gap: 10px; margin-bottom: 12px; }
  .toolbar { flex-direction: column; align-items: stretch; }
  .toolbar .el-select, .toolbar .el-input, .toolbar .el-button { width: 100% !important; margin-left: 0; margin-bottom: 6px; }
  .toolbar .el-dropdown { width: 100%; }
  .toolbar .el-dropdown .el-button { width: 100%; }
  
  :deep(.el-table__header-wrapper), :deep(.el-table__body-wrapper) { font-size: 12px; }
  :deep(.el-table .cell) { padding: 0 4px; }
  :deep(.el-table .el-button + .el-button) { margin-left: 4px; }
  :deep(.el-table .el-button.is-link) { padding: 4px; }
  :deep(.el-table .el-button.is-link .el-icon) { font-size: 16px; }
  
  /* 详情页调整 */
  .detail .row { flex-direction: column; gap: 4px; }
  .detail .label { width: 100%; font-weight: 600; font-size: 13px; }
  
  /* 编辑页头像调整 */
  .avatar-edit { flex-direction: column; align-items: flex-start; }
  .avatar-edit .el-input { width: 100%; }
  .avatar-edit .el-button { width: 100%; }

  /* ★ 核心：强制修复移动端表单 label 导致的横向溢出 */
  :deep(.el-form-item__label) {
    float: none !important;
    display: block !important;
    text-align: left !important;
    padding: 0 0 8px 0 !important;
    width: 100% !important;
  }
  :deep(.el-form-item__content) {
    margin-left: 0 !important;
    width: 100% !important;
  }
  /* 抽屉本身的内边距缩小 */
  :deep(.el-drawer__body) {
    padding: 16px 12px !important;
  }
}
</style>
<template>
  <div class="av-library">
    <div class="toolbar">
      <div class="toolbar-left">
        <el-select v-model="statusFilter" placeholder="全部状态" style="width: 140px" @change="reload">
          <el-option label="全部" value="" />
          <el-option label="已完成" value="completed" />
          <el-option label="已暂停" value="paused" />
          <el-option label="已放行" value="released" />
          <el-option label="刮削中" value="scraping" />
        </el-select>
        <el-input
          v-model="keyword"
          placeholder="搜索番号/标题/演员"
          style="width: 260px; margin-left: 12px"
          clearable
          @keyup.enter="reload"
        >
          <template #append>
            <el-button :icon="Search" @click="reload" />
          </template>
        </el-input>
      </div>
      <div class="toolbar-right">
        <span class="total-hint">共 {{ total }} 部</span>
      </div>
    </div>

    <div v-loading="loading" class="card-grid">
      <div
        v-for="m in list"
        :key="m.id"
        class="av-card"
        :class="{ 'is-paused': m.status === 'paused' }"
        @click="goDetail(m.id)"
      >
        <div class="av-card__poster">
          <img :src="resolveImage(m.poster)" :alt="m.code" loading="lazy" />
          <div class="av-card__badge" v-if="m.status === 'paused'">暂停</div>
          <div class="av-card__badge av-card__badge--ok" v-else-if="m.status === 'completed'">✓</div>
          <div class="av-card__rating" v-if="m.rating > 0">★ {{ m.rating.toFixed(1) }}</div>
        </div>
        <div class="av-card__body">
          <div class="av-card__code">{{ m.code }}</div>
          <div class="av-card__title" :title="m.title">{{ m.title || '（无标题）' }}</div>
          <div class="av-card__actors">{{ actorSummary(m.actors) }}</div>
        </div>
        <div class="av-card__actions" v-if="m.status === 'paused'" @click.stop>
          <el-button size="small" type="success" @click="release(m)">放行</el-button>
          <el-button size="small" type="primary" @click="restart(m)">重启</el-button>
          <el-button size="small" type="danger" @click="cancel(m)">取消</el-button>
        </div>
        <div class="av-card__pause-reason" v-if="m.status === 'paused' && m.pause_reason" :title="m.pause_reason">
          {{ m.pause_reason }}
        </div>
      </div>

      <div v-if="!loading && list.length === 0" class="empty">
        <el-empty description="暂无刮削结果" />
      </div>
    </div>

    <div class="pagination" v-if="total > pageSize">
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="prev, pager, next"
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
const pageSize = ref(24)
const loading = ref(false)
const statusFilter = ref('')
const keyword = ref('')

const load = async () => {
  loading.value = true
  try {
    const res = await axios.get('/api/avscrape/library', {
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
    ElMessage.error('加载媒体库失败')
  } finally {
    loading.value = false
  }
}

const reload = () => {
  page.value = 1
  load()
}

const goDetail = (id: number) => {
  router.push(`/avscrape/library/${id}`)
}

const resolveImage = (url: string) => {
  if (!url) return 'data:image/svg+xml;utf8,<svg xmlns="http://www.w3.org/2000/svg" width="200" height="300"><rect width="200" height="300" fill="%23eee"/><text x="50%" y="50%" text-anchor="middle" fill="%23999" font-size="14">无封面</text></svg>'
  if (url.startsWith('http')) return url
  return url
}

const actorSummary = (actorsStr: string) => {
  try {
    const actors = JSON.parse(actorsStr || '[]')
    if (!actors.length) return '—'
    const names = actors.slice(0, 3).map((a: any) => a.name).filter(Boolean)
    return actors.length > 3 ? names.join('、') + ` 等 ${actors.length} 人` : names.join('、')
  } catch {
    return '—'
  }
}

const release = async (m: any) => {
  try {
    await ElMessageBox.confirm(
      `放行后，番号 ${m.code} 将跳过失败检查强制走完流程，确定吗？`,
      '确认放行',
      { type: 'warning' },
    )
    await axios.post(`/api/avscrape/library/${m.id}/release`)
    ElMessage.success('已放行')
    load()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('放行失败')
  }
}

const restart = async (m: any) => {
  try {
    await ElMessageBox.confirm(
      `重启会删除临时文件并清空记录，番号 ${m.code} 将从零开始重新刮削，确定吗？`,
      '确认重启',
      { type: 'warning' },
    )
    await axios.post(`/api/avscrape/library/${m.id}/restart`)
    ElMessage.success('已重启')
    load()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('重启失败')
  }
}

const cancel = async (m: any) => {
  try {
    await ElMessageBox.confirm(
      `取消会删除临时文件和记录，番号 ${m.code} 的所有刮削数据将丢失，确定吗？`,
      '确认取消',
      { type: 'warning' },
    )
    await axios.post(`/api/avscrape/library/${m.id}/cancel`)
    ElMessage.success('已取消')
    load()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('取消失败')
  }
}

onMounted(load)
</script>

<style scoped>
.av-library {
  padding: 20px;
}
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}
.toolbar-left {
  display: flex;
  align-items: center;
}
.total-hint {
  color: #909399;
  font-size: 13px;
}
.card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 16px;
  min-height: 300px;
}
.av-card {
  background: #fff;
  border-radius: 8px;
  overflow: hidden;
  cursor: pointer;
  transition: transform 0.15s, box-shadow 0.15s;
  border: 1px solid #ebeef5;
  display: flex;
  flex-direction: column;
}
.av-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
}
.av-card.is-paused {
  border-color: #f56c6c;
  box-shadow: 0 0 0 2px rgba(245, 108, 108, 0.15);
}
.av-card__poster {
  position: relative;
  width: 100%;
  aspect-ratio: 2 / 3;
  background: #f5f5f5;
  overflow: hidden;
}
.av-card__poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.av-card__badge {
  position: absolute;
  top: 6px;
  left: 6px;
  background: #f56c6c;
  color: #fff;
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}
.av-card__badge--ok {
  background: #67c23a;
}
.av-card__rating {
  position: absolute;
  bottom: 6px;
  right: 6px;
  background: rgba(0, 0, 0, 0.65);
  color: #ffd04b;
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}
.av-card__body {
  padding: 10px;
  flex: 1;
}
.av-card__code {
  font-size: 13px;
  font-weight: 600;
  color: #409eff;
  margin-bottom: 4px;
}
.av-card__title {
  font-size: 13px;
  color: #303133;
  line-height: 1.4;
  height: 2.8em;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  margin-bottom: 6px;
}
.av-card__actors {
  font-size: 12px;
  color: #909399;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.av-card__actions {
  display: flex;
  gap: 6px;
  padding: 8px 10px;
  border-top: 1px solid #f0f0f0;
}
.av-card__actions .el-button {
  flex: 1;
  padding: 5px 0;
}
.av-card__pause-reason {
  font-size: 11px;
  color: #f56c6c;
  padding: 0 10px 8px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.empty {
  grid-column: 1 / -1;
}
.pagination {
  margin-top: 24px;
  display: flex;
  justify-content: center;
}

/* ===== 移动端适配 ===== */
@media (max-width: 768px) {
  .av-library {
    padding: 12px;
  }
  .toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
    margin-bottom: 14px;
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
  .total-hint {
    text-align: right;
  }
  .card-grid {
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 10px;
  }
  .av-card__title {
    font-size: 12px;
  }
  .av-card__actors {
    font-size: 11px;
  }
  .av-card__actions {
    flex-direction: column;
    gap: 4px;
    padding: 6px;
  }
  .av-card__actions .el-button {
    padding: 4px 0;
    font-size: 12px;
  }
}

@media (max-width: 480px) {
  .av-library {
    padding: 8px;
  }
  .card-grid {
    grid-template-columns: repeat(2, 1fr);
    gap: 8px;
  }
  .av-card__body {
    padding: 8px;
  }
  .av-card__code {
    font-size: 12px;
  }
  .av-card__title {
    font-size: 11px;
    height: 2.6em;
  }
  .av-card__actors {
    font-size: 10px;
  }
  .av-card__rating {
    font-size: 10px;
    padding: 1px 6px;
  }
  .av-card__badge {
    font-size: 11px;
    padding: 1px 6px;
  }
}
</style>

<template>
  <div class="aven-detail">
    <el-page-header @back="goBack" :content="media?.title || '加载中'" />

    <div v-if="loadError" class="aven-detail__error">
      <el-alert :title="loadError" type="error" show-icon :closable="false" />
      <el-button style="margin-top: 16px" @click="loadDetail">重新加载</el-button>
    </div>

    <div v-else-if="!media" class="aven-detail__loading" v-loading="loading" />

    <template v-else>
      <el-alert
        v-if="media.status === 'paused'"
        :title="'刮削暂停：' + (media.pause_reason || '未知原因')"
        type="error"
        show-icon
        :closable="false"
        style="margin-top: 16px"
      >
        <template #default>
          <div style="margin-top: 12px">
            <el-button type="success" size="small" @click="release">放行</el-button>
            <el-button type="primary" size="small" @click="restart">重启</el-button>
            <el-button type="danger" size="small" @click="cancel">取消</el-button>
          </div>
        </template>
      </el-alert>

      <el-alert
        v-else-if="media.status === 'released'"
        title="已放行，下次扫描将强制走完"
        type="warning"
        show-icon
        :closable="false"
        style="margin-top: 16px"
      />

      <div
        class="aven-detail__hero"
        :style="{ backgroundImage: media.fanart ? `url(${resolveUrl(media.fanart)})` : 'none' }"
      >
        <div class="aven-detail__hero-overlay">
          <img
            v-if="media.poster"
            :src="resolveUrl(media.poster)"
            class="aven-detail__poster"
            @error="onImgError"
          />
          <div class="aven-detail__meta">
            <h1>{{ media.title || '（无标题）' }}</h1>
            <p v-if="media.original_title">{{ media.original_title }}</p>
            <p>
              Studio：{{ media.studio || '—' }} ｜ 发行：{{ formatDate(media.release_date) }} ｜
              时长：{{ media.runtime || 0 }} 分钟
            </p>
            <p v-if="media.resolution">分辨率：{{ media.resolution }}<span v-if="media.is_hdr"> ｜ HDR</span></p>
            <p v-if="media.rating > 0">评分：★ {{ media.rating.toFixed(2) }}</p>
          </div>
        </div>
      </div>

      <el-tabs style="margin-top: 20px">
        <el-tab-pane label="剧情简介">
          <p class="aven-detail__plot">{{ media.plot || '暂无简介' }}</p>
        </el-tab-pane>

        <el-tab-pane label="演员">
          <el-row :gutter="16">
            <el-col
              v-for="(actor, idx) in parseJSON(media.actors)"
              :key="idx"
              :xs="8"
              :sm="6"
              :md="4"
              :lg="3"
              style="margin-bottom: 16px"
            >
              <div class="aven-actor">
                <img
                  v-if="actor.image"
                  :src="resolveUrl(actor.image)"
                  class="aven-actor__img"
                  @error="onImgError"
                />
                <div v-else class="aven-actor__img aven-actor__img--empty">无图</div>
                <div class="aven-actor__name" :title="actor.name">{{ actor.name }}</div>
              </div>
            </el-col>
          </el-row>
          <div v-if="parseJSON(media.actors).length === 0" class="aven-detail__empty-tip">暂无演员信息</div>
        </el-tab-pane>

        <el-tab-pane label="剧照">
          <div class="aven-detail__previews">
            <el-image
              v-for="(img, idx) in parseJSON(media.preview_images)"
              :key="idx"
              :src="resolveUrl(img)"
              :preview-src-list="parseJSON(media.preview_images).map(resolveUrl)"
              class="aven-preview-img"
              fit="cover"
              :initial-index="idx"
            />
          </div>
          <div v-if="parseJSON(media.preview_images).length === 0" class="aven-detail__empty-tip">暂无剧照</div>
        </el-tab-pane>

        <el-tab-pane label="标签">
          <el-tag v-for="(g, idx) in parseJSON(media.genres)" :key="idx" style="margin: 4px">{{ g }}</el-tag>
          <div v-if="parseJSON(media.genres).length === 0" class="aven-detail__empty-tip">暂无标签</div>
        </el-tab-pane>
      </el-tabs>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'

const route = useRoute()
const router = useRouter()
const media = ref<any>(null)
const loading = ref(false)
const loadError = ref('')

const parseJSON = (str: string) => {
  if (!str) return []
  try {
    const v = JSON.parse(str)
    return Array.isArray(v) ? v : []
  } catch {
    return []
  }
}

const formatDate = (d: string) => {
  if (!d) return '—'
  if (d.length >= 10) return d.slice(0, 10)
  return d
}

const resolveUrl = (url: string) => {
  if (!url) return ''
  if (url.startsWith('http://')) return url.replace('http://', 'https://')
  return url
}

const onImgError = (e: Event) => {
  const img = e.target as HTMLImageElement
  img.style.display = 'none'
}

const goBack = () => {
  router.push('/aven/library')
}

const loadDetail = async () => {
  const id = route.params.id
  if (!id) {
    loadError.value = '缺少媒体 ID'
    return
  }

  loading.value = true
  loadError.value = ''
  try {
    const res = await axios.get(`/api/aven/library/${id}`)
    const raw = res.data
    let payload: any = null
    if (raw && typeof raw === 'object') {
      if (raw.data && typeof raw.data === 'object' && (raw.data.id || raw.data.title)) {
        payload = raw.data
      } else if (raw.id || raw.title) {
        payload = raw
      }
    }
    if (!payload) {
      loadError.value = 'API 返回数据格式异常'
      return
    }
    media.value = payload
  } catch (e: any) {
    const msg = e?.response?.data?.error || e?.response?.data?.message || e?.message || '加载失败'
    loadError.value = `加载详情失败：${msg}`
  } finally {
    loading.value = false
  }
}

const release = async () => {
  try {
    await ElMessageBox.confirm('放行后将跳过失败检查强制走完流程，确定吗？', '确认放行', { type: 'warning' })
    await axios.post(`/api/aven/library/${route.params.id}/release`)
    ElMessage.success('已放行')
    loadDetail()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('放行失败')
  }
}

const restart = async () => {
  try {
    await ElMessageBox.confirm('重启会删除临时文件并清空记录，确定吗？', '确认重启', { type: 'warning' })
    await axios.post(`/api/aven/library/${route.params.id}/restart`)
    ElMessage.success('已重启')
    router.push('/aven/library')
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('重启失败')
  }
}

const cancel = async () => {
  try {
    await ElMessageBox.confirm('取消会删除临时文件和记录，确定吗？', '确认取消', { type: 'warning' })
    await axios.post(`/api/aven/library/${route.params.id}/cancel`)
    ElMessage.success('已取消')
    router.push('/aven/library')
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('取消失败')
  }
}

onMounted(loadDetail)
</script>

<style scoped>
.aven-detail {
  padding: 20px;
}
.aven-detail__error {
  margin-top: 20px;
}
.aven-detail__loading {
  min-height: 200px;
  margin-top: 20px;
}
.aven-detail__hero {
  position: relative;
  margin-top: 16px;
  aspect-ratio: 16 / 9;
  max-height: 500px;
  min-height: 260px;
  background-size: contain;
  background-repeat: no-repeat;
  background-position: center;
  background-color: #1a1a1a;
  border-radius: 8px;
  overflow: hidden;
}
.aven-detail__hero-overlay {
  display: flex;
  align-items: flex-end;
  padding: 24px;
  background: linear-gradient(transparent, rgba(0, 0, 0, 0.85));
  color: #fff;
  min-height: 100%;
  box-sizing: border-box;
}
.aven-detail__poster {
  width: 180px;
  aspect-ratio: 2 / 3;
  height: auto;
  object-fit: cover;
  object-position: center top;
  border-radius: 4px;
  margin-right: 24px;
  flex-shrink: 0;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.5);
  background: #333;
}
.aven-detail__meta {
  flex: 1;
  min-width: 0;
}
.aven-detail__meta h1 {
  margin: 0 0 8px;
  font-size: 24px;
  line-height: 1.3;
}
.aven-detail__meta p {
  margin: 4px 0;
  font-size: 14px;
  line-height: 1.5;
  word-break: break-word;
}
.aven-detail__plot {
  line-height: 1.8;
  white-space: pre-wrap;
  word-break: break-word;
}
.aven-detail__empty-tip {
  color: #909399;
  font-size: 14px;
  padding: 20px 0;
  text-align: center;
}
.aven-actor {
  text-align: center;
}
.aven-actor__img {
  width: 100%;
  aspect-ratio: 1 / 1;
  height: auto;
  object-fit: cover;
  object-position: center top;
  border-radius: 50%;
  background: #f5f7fa;
  border: 2px solid #ebeef5;
  display: block;
}
.aven-actor__img--empty {
  display: flex;
  align-items: center;
  justify-content: center;
  color: #c0c4cc;
  font-size: 12px;
}
.aven-actor__name {
  margin-top: 8px;
  font-size: 13px;
  color: #303133;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.aven-detail__previews {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.aven-preview-img {
  width: 180px;
  height: 120px;
  border-radius: 4px;
  overflow: hidden;
}

@media (max-width: 768px) {
  .aven-detail {
    padding: 12px;
  }
  .aven-detail__hero {
    aspect-ratio: 4 / 3;
    min-height: 200px;
    max-height: 320px;
  }
  .aven-detail__hero-overlay {
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: 16px;
  }
  .aven-detail__poster {
    width: 120px;
    margin: 0 0 12px 0;
  }
  .aven-detail__meta h1 {
    font-size: 18px;
    margin-bottom: 6px;
  }
  .aven-detail__meta p {
    font-size: 12px;
  }
  .aven-actor__name {
    font-size: 12px;
  }
  .aven-preview-img {
    width: calc(50% - 4px);
    height: 90px;
  }
}

@media (max-width: 480px) {
  .aven-detail {
    padding: 8px;
  }
  .aven-detail__poster {
    width: 100px;
  }
  .aven-detail__meta h1 {
    font-size: 16px;
  }
  .aven-detail__meta p {
    font-size: 11px;
  }
  .aven-preview-img {
    width: 100%;
    height: 120px;
  }
}
</style>
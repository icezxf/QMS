<template>
  <div class="av-detail">
    <el-page-header @back="goBack" :content="media?.code || '加载中'" />

    <div v-if="loadError" class="av-detail__error">
      <el-alert :title="loadError" type="error" show-icon :closable="false" />
      <el-button style="margin-top: 16px" @click="loadDetail">重新加载</el-button>
    </div>

    <div v-else-if="!media" class="av-detail__loading" v-loading="loading" />

    <template v-else>
      <!-- 暂停提示 -->
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
            <el-button type="success" size="small" @click="release">放行（跳过失败强制走完）</el-button>
            <el-button type="primary" size="small" @click="restart">重启（清空重刮）</el-button>
            <el-button type="danger" size="small" @click="cancel">取消（删除记录）</el-button>
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

      <!-- Hero 区 -->
      <div
        class="av-detail__hero"
        :style="{ backgroundImage: media.fanart ? `url(${media.fanart})` : 'none' }"
      >
        <div class="av-detail__hero-overlay">
          <img
            v-if="media.poster"
            :src="media.poster"
            class="av-detail__poster"
            @error="onImgError"
          />
          <div class="av-detail__meta">
            <h1>{{ media.title || '（无标题）' }}</h1>
            <p v-if="media.original_title">{{ media.original_title }}</p>
            <p>
              番号：{{ media.code || '—' }} ｜ 发行：{{ formatDate(media.release_date) }} ｜
              时长：{{ media.runtime || 0 }} 分钟
            </p>
            <p>片商：{{ media.studio || '—' }} ｜ 厂牌：{{ media.label || '—' }} ｜ 导演：{{ media.director || '—' }}</p>
            <p v-if="media.rating > 0">评分：★ {{ media.rating.toFixed(2) }}</p>
          </div>
        </div>
      </div>

      <!-- Tabs -->
      <el-tabs style="margin-top: 20px">
        <el-tab-pane label="剧情简介">
          <p class="av-detail__plot">{{ media.plot || '暂无简介' }}</p>
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
              <div class="av-actor">
                <img
                  v-if="actor.image"
                  :src="actor.image"
                  class="av-actor__img"
                  @error="onImgError"
                />
                <div v-else class="av-actor__img av-actor__img--empty">无图</div>
                <div class="av-actor__name" :title="actor.name">{{ actor.name }}</div>
              </div>
            </el-col>
          </el-row>
          <div v-if="parseJSON(media.actors).length === 0" class="av-detail__empty-tip">暂无演员信息</div>
        </el-tab-pane>

        <el-tab-pane label="剧照">
          <div class="av-detail__previews">
            <el-image
              v-for="(img, idx) in parseJSON(media.preview_images)"
              :key="idx"
              :src="img"
              :preview-src-list="parseJSON(media.preview_images)"
              class="av-preview-img"
              fit="cover"
              :initial-index="idx"
            />
          </div>
          <div v-if="parseJSON(media.preview_images).length === 0" class="av-detail__empty-tip">暂无剧照</div>
        </el-tab-pane>

        <el-tab-pane label="标签">
          <el-tag v-for="(g, idx) in parseJSON(media.genres)" :key="idx" style="margin: 4px">{{ g }}</el-tag>
          <div v-if="parseJSON(media.genres).length === 0" class="av-detail__empty-tip">暂无标签</div>
        </el-tab-pane>

        <el-tab-pane label="预告片" v-if="media.trailer">
          <video :src="media.trailer" controls class="av-trailer" />
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

const onImgError = (e: Event) => {
  const img = e.target as HTMLImageElement
  img.style.display = 'none'
}

const goBack = () => {
  router.push('/avscrape/library')
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
    const res = await axios.get(`/api/avscrape/library/${id}`)
    const raw = res.data

    // ===== 兼容两种 API 格式 =====
    // 格式 A：直接返回媒体对象 { id, code, title, ... }
    // 格式 B：包装 { code: 200, data: { id, code, ... } }
    let payload: any = null
    if (raw && typeof raw === 'object') {
      if (raw.data && typeof raw.data === 'object' && (raw.data.id || raw.data.code)) {
        // 格式 B
        payload = raw.data
      } else if (raw.id || raw.code) {
        // 格式 A
        payload = raw
      }
    }

    if (!payload) {
      loadError.value = 'API 返回数据格式异常'
      console.error('[AppAvDetail] 未识别的 API 返回:', raw)
      return
    }

    media.value = payload
  } catch (e: any) {
    const msg = e?.response?.data?.error || e?.response?.data?.message || e?.message || '加载失败'
    loadError.value = `加载详情失败：${msg}`
    console.error('[AppAvDetail] 加载详情失败:', e)
  } finally {
    loading.value = false
  }
}

const release = async () => {
  try {
    await ElMessageBox.confirm('放行后将跳过失败检查强制走完流程，确定吗？', '确认放行', { type: 'warning' })
    await axios.post(`/api/avscrape/library/${route.params.id}/release`)
    ElMessage.success('已放行，正在重新扫描')
    loadDetail()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('放行失败')
  }
}

const restart = async () => {
  try {
    await ElMessageBox.confirm(
      '重启会删除临时文件并清空记录，从零开始重新刮削，确定吗？',
      '确认重启',
      { type: 'warning' },
    )
    await axios.post(`/api/avscrape/library/${route.params.id}/restart`)
    ElMessage.success('已重启，正在重新扫描')
    router.push('/avscrape/library')
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('重启失败')
  }
}

const cancel = async () => {
  try {
    await ElMessageBox.confirm('取消会删除临时文件和记录，确定吗？', '确认取消', { type: 'warning' })
    await axios.post(`/api/avscrape/library/${route.params.id}/cancel`)
    ElMessage.success('已取消')
    router.push('/avscrape/library')
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('取消失败')
  }
}

onMounted(loadDetail)
</script>

<style scoped>
.av-detail {
  padding: 20px;
}
.av-detail__error {
  margin-top: 20px;
}
.av-detail__loading {
  min-height: 200px;
  margin-top: 20px;
}
.av-detail__hero {
  position: relative;
  margin-top: 16px;
  aspect-ratio: 16 / 9;
  max-height: 500px;
  min-height: 260px;
  background-size: contain;
  background-repeat: no-repeat;
  background-position: center;
  background-color: #000;
  border-radius: 8px;
  overflow: hidden;
}
.av-detail__hero-overlay {
  display: flex;
  align-items: flex-end;
  padding: 24px;
  background: linear-gradient(transparent, rgba(0, 0, 0, 0.85));
  color: #fff;
  min-height: 100%;
  box-sizing: border-box;
}
.av-detail__poster {
  width: 180px;
  aspect-ratio: 2 / 3;
  height: auto;
  object-fit: cover;
  object-position: right top;
  border-radius: 4px;
  margin-right: 24px;
  flex-shrink: 0;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.5);
  background: #333;
}
.av-detail__meta {
  flex: 1;
  min-width: 0;
}
.av-detail__meta h1 {
  margin: 0 0 8px;
  font-size: 24px;
  line-height: 1.3;
}
.av-detail__meta p {
  margin: 4px 0;
  font-size: 14px;
  line-height: 1.5;
  word-break: break-word;
}
.av-detail__plot {
  line-height: 1.8;
  white-space: pre-wrap;
  word-break: break-word;
}
.av-detail__empty-tip {
  color: #909399;
  font-size: 14px;
  padding: 20px 0;
  text-align: center;
}
.av-actor {
  text-align: center;
}
.av-actor__img {
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
.av-actor__img--empty {
  display: flex;
  align-items: center;
  justify-content: center;
  color: #c0c4cc;
  font-size: 12px;
}
.av-actor__name {
  margin-top: 8px;
  font-size: 13px;
  color: #303133;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.av-detail__previews {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.av-preview-img {
  width: 180px;
  height: 120px;
  border-radius: 4px;
  overflow: hidden;
}
.av-trailer {
  width: 100%;
  max-width: 800px;
}

/* ===== 移动端适配 ===== */
@media (max-width: 768px) {
  .av-detail {
    padding: 12px;
  }
  .av-detail__hero {
    aspect-ratio: 4 / 3;
    min-height: 200px;
    max-height: 320px;
  }
  .av-detail__hero-overlay {
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: 16px;
  }
  .av-detail__poster {
    width: 120px;
    margin: 0 0 12px 0;
  }
  .av-detail__meta h1 {
    font-size: 18px;
    margin-bottom: 6px;
  }
  .av-detail__meta p {
    font-size: 12px;
  }
  .av-actor__name {
    font-size: 12px;
  }
  .av-preview-img {
    width: calc(50% - 4px);
    height: 90px;
  }
}

@media (max-width: 480px) {
  .av-detail {
    padding: 8px;
  }
  .av-detail__poster {
    width: 100px;
  }
  .av-detail__meta h1 {
    font-size: 16px;
  }
  .av-detail__meta p {
    font-size: 11px;
  }
  .av-preview-img {
    width: 100%;
    height: 120px;
  }
}
</style>

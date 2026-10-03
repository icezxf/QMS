<template>
  <div class="av-detail" v-if="media">
    <el-page-header @back="$router.back()" :content="media.code" />

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

    <div class="av-detail__hero" :style="{ backgroundImage: `url(${media.fanart || media.poster})` }">
      <div class="av-detail__hero-overlay">
        <img :src="media.poster" class="av-detail__poster" />
        <div class="av-detail__meta">
          <h1>{{ media.title }}</h1>
          <p>{{ media.original_title }}</p>
          <p>番号：{{ media.code }} ｜ 发行：{{ media.release_date }} ｜ 时长：{{ media.runtime }} 分钟</p>
          <p>片商：{{ media.studio }} ｜ 厂牌：{{ media.label }} ｜ 导演：{{ media.director }}</p>
          <p v-if="media.rating > 0">评分：★ {{ media.rating.toFixed(2) }}</p>
        </div>
      </div>
    </div>

    <el-tabs style="margin-top: 20px">
      <el-tab-pane label="剧情简介">
        <p>{{ media.plot || '暂无简介' }}</p>
      </el-tab-pane>
      <el-tab-pane label="演员">
        <el-row :gutter="16">
          <el-col
            v-for="(actor, idx) in parseJSON(media.actors)"
            :key="idx"
            :xs="12"
            :sm="8"
            :md="6"
            :lg="4"
            style="margin-bottom: 16px"
          >
            <el-card>
              <img :src="actor.image" class="av-actor__img" />
              <div class="av-actor__name">{{ actor.name }}</div>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>
      <el-tab-pane label="剧照">
        <el-image
          v-for="(img, idx) in parseJSON(media.preview_images)"
          :key="idx"
          :src="img"
          :preview-src-list="parseJSON(media.preview_images)"
          class="av-preview-img"
          fit="cover"
        />
      </el-tab-pane>
      <el-tab-pane label="标签">
        <el-tag v-for="(g, idx) in parseJSON(media.genres)" :key="idx" style="margin: 4px">{{ g }}</el-tag>
      </el-tab-pane>
      <el-tab-pane label="预告片" v-if="media.trailer">
        <video :src="media.trailer" controls class="av-trailer" />
      </el-tab-pane>
    </el-tabs>
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

const parseJSON = (str: string) => {
  try {
    return JSON.parse(str || '[]')
  } catch {
    return []
  }
}

const loadDetail = async () => {
  try {
    const res = await axios.get(`/api/avscrape/library/${route.params.id}`)
    media.value = res.data
  } catch {
    ElMessage.error('加载详情失败')
  }
}

const release = async () => {
  try {
    await ElMessageBox.confirm('放行后将跳过失败检查强制走完流程，确定吗？', '确认放行', { type: 'warning' })
    await axios.post(`/api/avscrape/library/${route.params.id}/release`)
    ElMessage.success('已放行')
    loadDetail()
  } catch (e: any) {
    if (e !== 'cancel') ElMessage.error('放行失败')
  }
}

const restart = async () => {
  try {
    await ElMessageBox.confirm('重启会删除临时文件并清空记录，从零开始重新刮削，确定吗？', '确认重启', { type: 'warning' })
    await axios.post(`/api/avscrape/library/${route.params.id}/restart`)
    ElMessage.success('已重启')
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
.av-detail__hero {
  position: relative;
  margin-top: 16px;
  min-height: 300px;
  background-size: cover;
  background-position: center;
  border-radius: 8px;
  overflow: hidden;
}
.av-detail__hero-overlay {
  display: flex;
  align-items: flex-end;
  padding: 24px;
  background: linear-gradient(transparent, rgba(0, 0, 0, 0.8));
  color: #fff;
}
.av-detail__poster {
  width: 180px;
  height: 260px;
  object-fit: cover;
  border-radius: 4px;
  margin-right: 24px;
  flex-shrink: 0;
}
.av-detail__meta h1 {
  margin: 0 0 8px;
  font-size: 24px;
}
.av-detail__meta p {
  margin: 4px 0;
  font-size: 14px;
}
.av-actor__img {
  width: 100%;
  height: 120px;
  object-fit: cover;
  border-radius: 4px;
}
.av-actor__name {
  text-align: center;
  margin-top: 8px;
  font-size: 14px;
}
.av-preview-img {
  width: 180px;
  height: 120px;
  margin: 4px;
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
    min-height: 200px;
  }
  .av-detail__hero-overlay {
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: 16px;
  }
  .av-detail__poster {
    width: 130px;
    height: 195px;
    margin: 0 0 12px 0;
  }
  .av-detail__meta h1 {
    font-size: 18px;
    margin-bottom: 6px;
  }
  .av-detail__meta p {
    font-size: 12px;
  }
  .av-actor__img {
    height: 90px;
  }
  .av-actor__name {
    font-size: 12px;
  }
  .av-preview-img {
    width: 45%;
    height: 90px;
    margin: 2px;
  }
}

@media (max-width: 480px) {
  .av-detail {
    padding: 8px;
  }
  .av-detail__poster {
    width: 100px;
    height: 150px;
  }
  .av-detail__meta h1 {
    font-size: 16px;
  }
  .av-detail__meta p {
    font-size: 11px;
  }
  .av-actor__img {
    height: 80px;
  }
  .av-preview-img {
    width: 100%;
    height: 120px;
    margin: 2px 0;
  }
}
</style>

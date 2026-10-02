<template>
  <div class="av-library">
    <div class="av-library__header">
      <h2>AV 媒体库</h2>
      <el-input v-model="searchCode" placeholder="输入番号刮削" style="width: 300px" @keyup.enter="handleScrape">
        <template #append>
          <el-button @click="handleScrape">刮削</el-button>
        </template>
      </el-input>
    </div>
    <el-row :gutter="16">
      <el-col v-for="item in list" :key="item.id" :span="6" style="margin-bottom: 16px">
        <el-card :body-style="{ padding: '0px' }" shadow="hover" @click="goDetail(item.id)">
          <img :src="item.poster || defaultCover" class="av-card__cover" @error="onImgError" />
          <div class="av-card__info">
            <div class="av-card__title">{{ item.title }}</div>
            <div class="av-card__code">{{ item.code }}</div>
          </div>
        </el-card>
      </el-col>
    </el-row>
    <el-pagination
      v-model:current-page="page"
      :page-size="pageSize"
      :total="total"
      layout="prev, pager, next"
      @current-change="loadList"
      style="margin-top: 16px; text-align: right"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { ElMessage } from 'element-plus'

const router = useRouter()
const list = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const searchCode = ref('')

// 默认封面占位图，可以用一个纯色 data URI 或项目已有的占位图
const defaultCover = 'data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIzMDAiIGhlaWdodD0iMjYwIj48cmVjdCB3aWR0aD0iMTAwJSIgaGVpZ2h0PSIxMDAlIiBmaWxsPSIjZWVlIi8+PHRleHQgeD0iNTAlIiB5PSI1MCUiIGZvbnQtc2l6ZT0iMTQiIGZpbGw9IiM5OTkiIHRleHQtYW5jaG9yPSJtaWRkbGUiIGR5PSIuM2VtIj5ObyBDb3ZlcitLPC90ZXh0Pjwvc3ZnPg=='

const onImgError = (e: Event) => {
  const img = e.target as HTMLImageElement
  img.src = defaultCover
}

const loadList = async () => {
  try {
    const res = await axios.get('/api/avscrape/library', {
      params: { page: page.value, page_size: pageSize.value },
    })
    list.value = res.data.list
    total.value = res.data.total
  } catch {
    ElMessage.error('加载媒体库失败')
  }
}

const handleScrape = async () => {
  if (!searchCode.value) return
  try {
    await axios.post('/api/avscrape/scrape', { code: searchCode.value })
    ElMessage.success('刮削任务已提交')
    searchCode.value = ''
    await loadList()
  } catch {
    ElMessage.error('刮削失败')
  }
}

const goDetail = (id: number) => {
  router.push(`/avscrape-detail/${id}`)
}

onMounted(loadList)
</script>

<style scoped>
.av-library {
  padding: 20px;
}
.av-library__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.av-card__cover {
  width: 100%;
  height: 260px;
  object-fit: cover;
}
.av-card__info {
  padding: 8px;
}
.av-card__title {
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.av-card__code {
  font-size: 12px;
  color: #999;
}
</style>
<template>
  <div class="aven-config">
    <h2>欧美刮削设置</h2>
    <el-form :model="config" label-width="200px" style="max-width: 760px">
      <el-divider content-position="left">数据源</el-divider>

      <el-form-item label="启用 StashDB">
        <el-switch v-model="config.enable_stashdb" />
        <span class="hint">（主数据源：标题 / 剧情 / 演员 / 标签）</span>
      </el-form-item>
      <el-form-item label="StashDB 地址" v-if="config.enable_stashdb">
        <el-input v-model="config.stashdb_endpoint" placeholder="https://stashdb.org/graphql" />
      </el-form-item>
      <el-form-item label="StashDB API Key" v-if="config.enable_stashdb">
        <el-input
          v-model="config.stashdb_api_key"
          type="password"
          show-password
          placeholder="在 stashdb.org → Settings → API Key 生成"
        />
        <span class="hint">（必需，否则无法查询）</span>
      </el-form-item>

      <el-divider content-position="left">TPDB（补充源）</el-divider>

      <el-form-item label="启用 TPDB">
        <el-switch v-model="config.enable_tpdb" />
        <span class="hint">（补充 poster / rating）</span>
      </el-form-item>
      <el-form-item label="TPDB 地址" v-if="config.enable_tpdb">
        <el-input v-model="config.tpdb_endpoint" placeholder="https://theporndb.net/graphql" />
      </el-form-item>
      <el-form-item label="TPDB API Key" v-if="config.enable_tpdb">
        <el-input
          v-model="config.tpdb_api_key"
          type="password"
          show-password
          placeholder="在 theporndb.net → 用户设置 → API Tokens 生成"
        />
        <span class="hint">（必需，否则无法补充 poster/rating）</span>
      </el-form-item>

      <el-divider content-position="left">翻译</el-divider>

      <el-form-item label="开启翻译">
        <el-switch v-model="config.enable_translate" />
      </el-form-item>
      <el-form-item label="翻译标题" v-if="config.enable_translate">
        <el-switch v-model="config.translate_title" />
      </el-form-item>
      <el-form-item label="翻译简介" v-if="config.enable_translate">
        <el-switch v-model="config.translate_plot" />
      </el-form-item>
      <el-form-item label="翻译标签" v-if="config.enable_translate">
        <el-switch v-model="config.translate_tags" />
      </el-form-item>
      <el-form-item label="翻译引擎" v-if="config.enable_translate">
        <el-select v-model="config.translate_engine" style="width: 100%">
          <el-option label="Gemini AI（推荐）" value="gemini" />
          <el-option label="DeepL（免费版）" value="deepl" />
          <el-option label="必应翻译（Azure）" value="bing" />
          <el-option label="Google 免费" value="google_free" />
          <el-option label="MyMemory" value="mymemory" />
        </el-select>
      </el-form-item>

      <template v-if="config.enable_translate && config.translate_engine === 'gemini'">
        <el-form-item label="Gemini API Key">
          <el-input v-model="config.translate_gemini_key" type="password" show-password />
        </el-form-item>
        <el-form-item label="Gemini 模型">
          <el-select v-model="config.translate_gemini_model" style="width: 100%">
            <el-option label="gemini-3.8-flash" value="gemini-3.8-flash" />
            <el-option label="gemini-3.7-flash" value="gemini-3.7-flash" />
            <el-option label="gemini-2.5-flash-lite" value="gemini-2.5-flash-lite" />
          </el-select>
        </el-form-item>
      </template>

      <el-form-item
        label="DeepL API Key"
        v-if="config.enable_translate && config.translate_engine === 'deepl'"
      >
        <el-input v-model="config.translate_deepl_key" placeholder="以 :fx 结尾" show-password />
      </el-form-item>

      <template v-if="config.enable_translate && config.translate_engine === 'bing'">
        <el-form-item label="必应 API Key">
          <el-input v-model="config.translate_bing_key" />
        </el-form-item>
        <el-form-item label="必应 Region">
          <el-input v-model="config.translate_bing_region" placeholder="例如 eastasia" />
        </el-form-item>
      </template>

      <el-form-item label="翻译目标语言" v-if="config.enable_translate">
        <el-input v-model="config.translate_target" placeholder="zh" />
      </el-form-item>

      <template v-if="config.enable_translate">
        <el-divider content-position="left">Google Cloud Translation（可选）</el-divider>
        <el-form-item label="Google API Key">
          <el-input
            v-model="config.google_translate_api_key"
            type="password"
            show-password
            placeholder="在 GCP 启用 Cloud Translation API 后创建"
          />
        </el-form-item>
        <el-form-item label="标签走 Google">
          <el-switch
            v-model="config.google_translate_for_tags"
            :disabled="!config.google_translate_api_key || config.google_translate_for_all"
          />
        </el-form-item>
        <el-form-item label="全部走 Google">
          <el-switch
            v-model="config.google_translate_for_all"
            :disabled="!config.google_translate_api_key"
          />
        </el-form-item>
      </template>

      <el-divider content-position="left">附加标签</el-divider>

      <el-form-item label="分辨率标签">
        <el-switch v-model="config.extra_tag_resolution" />
      </el-form-item>
      <el-form-item label="有码/无码标签">
        <el-switch v-model="config.extra_tag_uncensored" />
      </el-form-item>
      <el-form-item label="中文字幕标签">
        <el-switch v-model="config.extra_tag_chinese_sub" />
      </el-form-item>

      <el-divider content-position="left">水印</el-divider>

      <el-form-item label="4K 水印">
        <el-switch v-model="config.watermark_4k" />
      </el-form-item>
      <el-form-item label="5K 水印">
        <el-switch v-model="config.watermark_5k" />
      </el-form-item>
      <el-form-item label="6K 水印">
        <el-switch v-model="config.watermark_6k" />
      </el-form-item>
      <el-form-item label="7K 水印">
        <el-switch v-model="config.watermark_7k" />
      </el-form-item>
      <el-form-item label="8K 水印">
        <el-switch v-model="config.watermark_8k" />
      </el-form-item>
      <el-form-item label="字幕 水印">
        <el-switch v-model="config.watermark_subtitle" />
      </el-form-item>
      <el-form-item label="破解 水印">
        <el-switch v-model="config.watermark_crack" />
      </el-form-item>
      <el-form-item label="流出 水印">
        <el-switch v-model="config.watermark_leak" />
      </el-form-item>
      <el-form-item label="无码 水印">
        <el-switch v-model="config.watermark_uncensored" />
      </el-form-item>

      <el-form-item>
        <el-button type="primary" @click="saveConfig">保存</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { ElMessage } from 'element-plus'

interface AvenConfig {
  enable_stashdb: boolean
  stashdb_endpoint: string
  stashdb_api_key: string

  enable_tpdb: boolean
  tpdb_endpoint: string
  tpdb_api_key: string

  enable_translate: boolean
  translate_title: boolean
  translate_plot: boolean
  translate_tags: boolean
  translate_engine: string
  translate_deepl_key: string
  translate_bing_key: string
  translate_bing_region: string
  translate_gemini_key: string
  translate_gemini_model: string
  translate_target: string

  google_translate_api_key: string
  google_translate_for_tags: boolean
  google_translate_for_all: boolean

  extra_tag_resolution: boolean
  extra_tag_uncensored: boolean
  extra_tag_chinese_sub: boolean

  watermark_4k: boolean
  watermark_5k: boolean
  watermark_6k: boolean
  watermark_7k: boolean
  watermark_8k: boolean
  watermark_subtitle: boolean
  watermark_crack: boolean
  watermark_leak: boolean
  watermark_uncensored: boolean
}

const config = ref<AvenConfig>({
  enable_stashdb: true,
  stashdb_endpoint: 'https://stashdb.org/graphql',
  stashdb_api_key: '',

  enable_tpdb: true,
  tpdb_endpoint: 'https://theporndb.net/graphql',
  tpdb_api_key: '',

  enable_translate: false,
  translate_title: true,
  translate_plot: true,
  translate_tags: true,
  translate_engine: 'gemini',
  translate_deepl_key: '',
  translate_bing_key: '',
  translate_bing_region: '',
  translate_gemini_key: '',
  translate_gemini_model: 'gemini-3.8-flash',
  translate_target: 'zh',

  google_translate_api_key: '',
  google_translate_for_tags: false,
  google_translate_for_all: false,

  extra_tag_resolution: true,
  extra_tag_uncensored: true,
  extra_tag_chinese_sub: true,

  watermark_4k: true,
  watermark_5k: true,
  watermark_6k: true,
  watermark_7k: true,
  watermark_8k: true,
  watermark_subtitle: true,
  watermark_crack: true,
  watermark_leak: true,
  watermark_uncensored: true,
})

const loadConfig = async () => {
  try {
    const res = await axios.get('/api/aven/config')
    config.value = { ...config.value, ...res.data }
  } catch (e) {
    ElMessage.error('加载配置失败')
  }
}

const saveConfig = async () => {
  try {
    await axios.post('/api/aven/config', config.value)
    ElMessage.success('配置已保存')
  } catch (e) {
    ElMessage.error('保存失败')
  }
}

onMounted(loadConfig)
</script>

<style scoped>
.aven-config {
  padding: 20px;
}
.hint {
  margin-left: 8px;
  color: #999;
  font-size: 12px;
}

@media (max-width: 768px) {
  .aven-config {
    padding: 12px;
  }
  .aven-config h2 {
    font-size: 18px;
    margin-bottom: 12px;
  }
  :deep(.el-form) {
    max-width: 100% !important;
  }
  :deep(.el-form-item) {
    margin-bottom: 16px;
  }
  :deep(.el-form-item__label) {
    width: 120px !important;
    font-size: 13px;
    padding-right: 8px;
  }
  :deep(.el-form-item__content) {
    font-size: 13px;
  }
  :deep(.el-input),
  :deep(.el-select),
  :deep(.el-textarea) {
    width: 100% !important;
  }
  .hint {
    display: block;
    margin-left: 0;
    margin-top: 4px;
    font-size: 11px;
  }
}
</style>
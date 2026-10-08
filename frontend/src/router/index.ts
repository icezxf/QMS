import { createRouter, type RouteRecordRaw } from 'vue-router'
import { createQMediaSyncHashHistory } from './history'
import { createAsyncRouteComponent } from './asyncRoute'
import { useAuthStore } from '@/stores/auth'
import { http } from '@/http/client'
import type { PageMeta } from './pageMeta'

const AppHome = createAsyncRouteComponent('AppHome', () => import('@/components/AppHome.vue'))
const AppLogin = createAsyncRouteComponent('AppLogin', () => import('@/components/AppLogin.vue'))
const AppUserSettings = createAsyncRouteComponent(
  'AppUserSettings',
  () => import('@/components/AppUserSettings.vue'),
)
const AppStrmSettings = createAsyncRouteComponent(
  'AppStrmSettings',
  () => import('@/components/AppStrmSettings.vue'),
)
const AppEmbySettings = createAsyncRouteComponent(
  'AppEmbySettings',
  () => import('@/components/AppEmbySettings.vue'),
)
const AppSyncRecords = createAsyncRouteComponent(
  'AppSyncRecords',
  () => import('@/components/AppSyncRecords.vue'),
)
const AppSyncTaskDetail = createAsyncRouteComponent(
  'AppSyncTaskDetail',
  () => import('@/components/AppSyncTaskDetail.vue'),
)
const AppSyncDirectories = createAsyncRouteComponent(
  'AppSyncDirectories',
  () => import('@/components/AppSyncDirectories.vue'),
)
const AppSyncDirectoryForm = createAsyncRouteComponent(
  'AppSyncDirectoryForm',
  () => import('@/components/AppSyncDirectoryForm.vue'),
)
const AppCloudAccounts = createAsyncRouteComponent(
  'AppCloudAccounts',
  () => import('@/components/AppCloudAccounts.vue'),
)
const AppThreadSettings = createAsyncRouteComponent(
  'AppThreadSettings',
  () => import('@/components/AppThreadSettings.vue'),
)
const AppLogSettings = createAsyncRouteComponent(
  'AppLogSettings',
  () => import('@/components/AppLogSettings.vue'),
)
const AppTmdbSettings = createAsyncRouteComponent(
  'AppTmdbSettings',
  () => import('@/components/AppTmdbSettings.vue'),
)
const AppAiSettings = createAsyncRouteComponent(
  'AppAiSettings',
  () => import('@/components/AppAiSettings.vue'),
)
const AppCategoryStrategy = createAsyncRouteComponent(
  'AppCategoryStrategy',
  () => import('@/components/AppCategoryStrategy.vue'),
)
const AppScrapePathes = createAsyncRouteComponent(
  'AppScrapePathes',
  () => import('@/components/AppScrapePathes.vue'),
)
const AppScrapePathForm = createAsyncRouteComponent(
  'AppScrapePathForm',
  () => import('@/components/AppScrapePathForm.vue'),
)
const AppScrapeRecords = createAsyncRouteComponent(
  'AppScrapeRecords',
  () => import('@/components/AppScrapeRecords.vue'),
)
const AppUploadQueue = createAsyncRouteComponent(
  'AppUploadQueue',
  () => import('@/components/AppUploadQueue.vue'),
)
const AppDownloadQueue = createAsyncRouteComponent(
  'AppDownloadQueue',
  () => import('@/components/AppDownloadQueue.vue'),
)
const AppNotificationChannels = createAsyncRouteComponent(
  'AppNotificationChannels',
  () => import('@/components/AppNotificationChannels.vue'),
)
const AppApiKeys = createAsyncRouteComponent(
  'AppApiKeys',
  () => import('@/components/AppApiKeys.vue'),
)
const AppLoginSessions = createAsyncRouteComponent(
  'AppLoginSessions',
  () => import('@/components/user-settings/LoginSessions.vue'),
)
const AppFileManager = createAsyncRouteComponent(
  'AppFileManager',
  () => import('@/components/AppFileManager.vue'),
)
const AppUpdate = createAsyncRouteComponent('AppUpdate', () => import('@/components/AppUpdate.vue'))
const AppBackupSettings = createAsyncRouteComponent(
  'AppBackupSettings',
  () => import('@/components/AppBackupSettings.vue'),
)
const AppBackupRecords = createAsyncRouteComponent(
  'AppBackupRecords',
  () => import('@/components/AppBackupRecords.vue'),
)
const AppBackupRestore = createAsyncRouteComponent(
  'AppBackupRestore',
  () => import('@/components/AppBackupRestore.vue'),
)
const AppProxySettings = createAsyncRouteComponent(
  'AppProxySettings',
  () => import('@/components/AppProxySettings.vue'),
)
const AppDatabaseRepair = createAsyncRouteComponent(
  'AppDatabaseRepair',
  () => import('@/components/AppDatabaseRepair.vue'),
)

// ===== AV 刮削组件 =====
const AppAvLibrary = createAsyncRouteComponent(
  'AppAvLibrary',
  () => import('@/components/AppAvLibrary.vue'),
)
const AppAvDetail = createAsyncRouteComponent(
  'AppAvDetail',
  () => import('@/components/AppAvDetail.vue'),
)
const AppAvPaths = createAsyncRouteComponent(
  'AppAvPaths',
  () => import('@/components/AppAvPaths.vue'),
)
const AppAvTasks = createAsyncRouteComponent(
  'AppAvTasks',
  () => import('@/components/AppAvTasks.vue'),
)
const AppAvConfig = createAsyncRouteComponent(
  'AppAvConfig',
  () => import('@/components/AppAvConfig.vue'),
)
// =================================

// ===== 改动 1：欧美刮削组件 =====
const AppAvenLibrary = createAsyncRouteComponent(
  'AppAvenLibrary',
  () => import('@/components/AppAvenLibrary.vue'),
)
const AppAvenDetail = createAsyncRouteComponent(
  'AppAvenDetail',
  () => import('@/components/AppAvenDetail.vue'),
)
const AppAvenPaths = createAsyncRouteComponent(
  'AppAvenPaths',
  () => import('@/components/AppAvenPaths.vue'),
)
const AppAvenTasks = createAsyncRouteComponent(
  'AppAvenTasks',
  () => import('@/components/AppAvenTasks.vue'),
)
const AppAvenConfig = createAsyncRouteComponent(
  'AppAvenConfig',
  () => import('@/components/AppAvenConfig.vue'),
)
// ================================

// 定义路由元信息类型
declare module 'vue-router' {
  interface RouteMeta {
    title: string
    requiresAuth: boolean
    parent?: string
    icon?: string
    showInMenu?: boolean
    page?: PageMeta
  }
}

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: AppLogin,
    meta: {
      title: '登录',
      requiresAuth: false,
      showInMenu: false,
    },
  },
  {
    path: '/',
    name: 'home',
    component: AppHome,
    meta: {
      title: '首页',
      page: {
        title: '控制台',
        description: '系统运行状态监控与管理',
        icon: 'House',
        variant: 'management',
      },
      requiresAuth: true,
      icon: 'House',
      showInMenu: true,
    },
  },
  {
    path: '/accounts',
    name: 'accounts',
    component: AppCloudAccounts,
    meta: {
      title: '网盘账号',
      page: {
        title: '网盘账号管理',
        description: '管理网盘账号授权与绑定',
        icon: 'Cloudy',
        variant: 'management',
      },
      requiresAuth: true,
      icon: 'Cloudy',
      showInMenu: true,
    },
  },
  {
    path: '/sync',
    name: 'sync',
    redirect: '/sync-directories',
    meta: {
      title: 'STRM 同步',
      requiresAuth: true,
      icon: 'DocumentCopy',
      showInMenu: true,
    },
  },
  {
    path: '/sync-directories',
    name: 'sync-directories',
    component: AppSyncDirectories,
    meta: {
      title: 'STRM 同步目录',
      page: {
        title: '同步目录管理',
        description: '管理云盘与本地目录的同步配置',
        icon: 'FolderOpened',
        variant: 'management',
      },
      requiresAuth: true,
      parent: 'sync',
      icon: 'FolderOpened',
      showInMenu: true,
    },
  },
  {
    path: '/sync-directory/add',
    name: 'sync-directory-add',
    component: AppSyncDirectoryForm,
    meta: {
      title: '添加同步目录',
      page: {
        description: '配置云盘或本地目录的同步规则',
        icon: 'Folder',
        variant: 'detail',
      },
      requiresAuth: true,
      parent: 'sync',
      showInMenu: false,
    },
  },
  {
    path: '/sync-directory/edit/:id',
    name: 'sync-directory-edit',
    component: AppSyncDirectoryForm,
    meta: {
      title: '编辑同步目录',
      page: {
        description: '配置云盘或本地目录的同步规则',
        icon: 'Folder',
        variant: 'detail',
      },
      requiresAuth: true,
      parent: 'sync',
      showInMenu: false,
    },
  },
  {
    path: '/sync-records',
    name: 'sync-records',
    component: AppSyncRecords,
    meta: {
      title: 'STRM 同步记录',
      page: {
        description: '只会保留 7 天的记录，每天 0 点会删除 7 天前的所有记录',
        icon: 'List',
        variant: 'compact',
      },
      requiresAuth: true,
      parent: 'sync',
      icon: 'List',
      showInMenu: true,
    },
  },
  {
    path: '/settings/strm',
    name: 'settings-strm',
    component: AppStrmSettings,
    meta: {
      title: 'STRM 设置',
      page: {
        description: '配置 STRM 文件生成、同步和 Webhook 行为',
        icon: 'Setting',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'sync',
      icon: 'Setting',
      showInMenu: true,
    },
  },
  {
    path: '/sync-records/:id',
    name: 'sync-task-detail',
    component: AppSyncTaskDetail,
    meta: {
      title: '同步任务详情',
      page: {
        description: '查看同步任务的执行情况和详细信息',
        icon: 'List',
        variant: 'detail',
      },
      requiresAuth: true,
      parent: 'sync',
      showInMenu: false,
    },
  },
  {
    path: '/scrape',
    name: 'scrape',
    redirect: '/scrape-pathes',
    meta: {
      title: '刮削 & 整理',
      requiresAuth: true,
      icon: 'Film',
      showInMenu: true,
    },
  },
  {
    path: '/scrape-pathes',
    name: 'scrape-pathes',
    component: AppScrapePathes,
    meta: {
      title: '刮削目录',
      page: {
        title: '刮削目录管理',
        description: '管理媒体文件的刮削和整理规则',
        icon: 'FolderOpened',
        variant: 'management',
      },
      requiresAuth: true,
      parent: 'scrape',
      icon: 'FolderOpened',
      showInMenu: true,
    },
  },
  {
    path: '/scrape-path/add',
    name: 'scrape-path-add',
    component: AppScrapePathForm,
    meta: {
      title: '添加刮削目录',
      page: {
        description: '配置媒体文件的刮削和整理规则',
        icon: 'Folder',
        variant: 'detail',
      },
      requiresAuth: true,
      parent: 'scrape',
      showInMenu: false,
    },
  },
  {
    path: '/scrape-path/edit/:id',
    name: 'scrape-path-edit',
    component: AppScrapePathForm,
    meta: {
      title: '编辑刮削目录',
      page: {
        description: '配置媒体文件的刮削和整理规则',
        icon: 'Folder',
        variant: 'detail',
      },
      requiresAuth: true,
      parent: 'scrape',
      showInMenu: false,
    },
  },
  {
    path: '/scrape-records',
    name: 'scrape-records',
    component: AppScrapeRecords,
    meta: {
      title: '刮削记录',
      page: {
        description: '查看媒体文件的刮削和整理记录',
        icon: 'List',
        variant: 'compact',
      },
      requiresAuth: true,
      parent: 'scrape',
      icon: 'List',
      showInMenu: true,
    },
  },
  {
    path: '/settings/tmdb',
    name: 'settings-tmdb',
    component: AppTmdbSettings,
    meta: {
      title: '刮削设置',
      page: {
        description: '配置 TMDB 和 fanart.tv 的媒体数据服务',
        icon: 'Film',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'scrape',
      icon: 'Film',
      showInMenu: true,
    },
  },
  {
    path: '/settings/ai',
    name: 'settings-ai',
    component: AppAiSettings,
    meta: {
      title: 'AI 识别设置',
      page: {
        description: '配置 AI 识别服务和模型参数',
        icon: 'View',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'scrape',
      icon: 'View',
      showInMenu: true,
    },
  },
  {
    path: '/settings/category-strategy',
    name: 'settings-category-strategy',
    component: AppCategoryStrategy,
    meta: {
      title: '二级分类设置',
      page: {
        description: '配置电影和电视剧的二级分类规则',
        icon: 'Operation',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'scrape',
      icon: 'Operation',
      showInMenu: true,
    },
  },

  // ===== AV 刮削路由 =====
  {
    path: '/avscrape',
    name: 'avscrape',
    redirect: '/avscrape/library',
    meta: {
      title: 'AV 刮削',
      requiresAuth: true,
      icon: 'VideoCamera',
      showInMenu: true,
    },
  },
  {
    path: '/avscrape/library',
    name: 'av-library',
    component: AppAvLibrary,
    meta: {
      title: 'AV 媒体库',
      page: {
        description: '浏览 AV 刮削结果，查看番号、演员和元数据',
        icon: 'Film',
        variant: 'management',
      },
      requiresAuth: true,
      parent: 'avscrape',
      icon: 'Film',
      showInMenu: true,
    },
  },
  {
    path: '/avscrape/library/:id',
    name: 'av-detail',
    component: AppAvDetail,
    meta: {
      title: 'AV 详情',
      page: {
        description: '查看作品详情和元数据',
        icon: 'Film',
        variant: 'detail',
      },
      requiresAuth: true,
      parent: 'avscrape',
      showInMenu: false,
    },
  },
  {
    path: '/avscrape/paths',
    name: 'av-paths',
    component: AppAvPaths,
    meta: {
      title: 'AV 刮削目录',
      page: {
        description: '管理 AV 刮削目录和源路径',
        icon: 'FolderOpened',
        variant: 'management',
      },
      requiresAuth: true,
      parent: 'avscrape',
      icon: 'FolderOpened',
      showInMenu: true,
    },
  },
  {
    path: '/avscrape/tasks',
    name: 'av-tasks',
    component: AppAvTasks,
    meta: {
      title: 'AV 任务记录',
      page: {
        description: '查看 AV 刮削任务执行记录',
        icon: 'List',
        variant: 'compact',
      },
      requiresAuth: true,
      parent: 'avscrape',
      icon: 'List',
      showInMenu: true,
    },
  },
  {
    path: '/avscrape/config',
    name: 'av-config',
    component: AppAvConfig,
    meta: {
      title: 'AV 配置',
      page: {
        description: '配置 AV 刮削源、翻译和评分服务',
        icon: 'Setting',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'avscrape',
      icon: 'Setting',
      showInMenu: true,
    },
  },
  // =================================

  // ===== 改动 2：欧美刮削路由 =====
  {
    path: '/aven',
    name: 'aven',
    redirect: '/aven/library',
    meta: {
      title: '欧美刮削',
      requiresAuth: true,
      icon: 'VideoCamera',
      showInMenu: true,
    },
  },
  {
    path: '/aven/library',
    name: 'aven-library',
    component: AppAvenLibrary,
    meta: {
      title: '欧美媒体库',
      page: {
        description: '浏览欧美刮削结果',
        icon: 'Film',
        variant: 'management',
      },
      requiresAuth: true,
      parent: 'aven',
      icon: 'Film',
      showInMenu: true,
    },
  },
  {
    path: '/aven/library/:id',
    name: 'aven-detail',
    component: AppAvenDetail,
    meta: {
      title: '欧美详情',
      page: {
        description: '查看作品详情',
        icon: 'Film',
        variant: 'detail',
      },
      requiresAuth: true,
      parent: 'aven',
      showInMenu: false,
    },
  },
  {
    path: '/aven/paths',
    name: 'aven-paths',
    component: AppAvenPaths,
    meta: {
      title: '欧美刮削目录',
      page: {
        description: '管理欧美刮削目录',
        icon: 'FolderOpened',
        variant: 'management',
      },
      requiresAuth: true,
      parent: 'aven',
      icon: 'FolderOpened',
      showInMenu: true,
    },
  },
  {
    path: '/aven/tasks',
    name: 'aven-tasks',
    component: AppAvenTasks,
    meta: {
      title: '欧美任务记录',
      page: {
        description: '查看欧美刮削任务执行记录',
        icon: 'List',
        variant: 'compact',
      },
      requiresAuth: true,
      parent: 'aven',
      icon: 'List',
      showInMenu: true,
    },
  },
  {
    path: '/aven/config',
    name: 'aven-config',
    component: AppAvenConfig,
    meta: {
      title: '欧美配置',
      page: {
        description: '配置 StashDB、翻译和刮削行为',
        icon: 'Setting',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'aven',
      icon: 'Setting',
      showInMenu: true,
    },
  },
  // =================================

  {
    path: '/transfer',
    name: 'transfer',
    redirect: '/upload-queue',
    meta: {
      title: '上传下载',
      requiresAuth: true,
      icon: 'Download',
      showInMenu: true,
    },
  },
  {
    path: '/upload-queue',
    name: 'upload-queue',
    component: AppUploadQueue,
    meta: {
      title: '上传队列',
      page: {
        description:
          '查看 STRM 同步和刮削流程产生的元数据上传任务，每天 1 点会删除 7 天前的所有任务（含排队中的任务）',
        icon: 'Upload',
        variant: 'compact',
      },
      requiresAuth: true,
      parent: 'transfer',
      icon: 'Upload',
      showInMenu: true,
    },
  },
  {
    path: '/download-queue',
    name: 'download-queue',
    component: AppDownloadQueue,
    meta: {
      title: '下载队列',
      page: {
        description:
          '查看 STRM 同步产生的下载任务和处理进度，每天 1 点会删除 7 天前的所有任务（含排队中的任务）',
        icon: 'Download',
        variant: 'compact',
      },
      requiresAuth: true,
      parent: 'transfer',
      icon: 'Download',
      showInMenu: true,
    },
  },
  {
    path: '/file-manager',
    name: 'file-manager',
    component: AppFileManager,
    meta: {
      title: '网盘文件管理',
      page: {
        description: '浏览和管理媒体文件，支持 STRM 生成、移动、复制、重命名和删除操作',
        icon: 'Folder',
        variant: 'management',
      },
      requiresAuth: true,
      icon: 'Folder',
      showInMenu: true,
    },
  },
  {
    path: '/database',
    name: 'database',
    redirect: '/database/backup/settings',
    meta: {
      title: '备份管理',
      requiresAuth: true,
      icon: 'DataAnalysis',
      showInMenu: true,
    },
  },
  {
    path: '/database/backup/settings',
    name: 'database-backup-settings',
    component: AppBackupSettings,
    meta: {
      title: '备份设置',
      page: {
        description: '配置数据库自动备份策略',
        icon: 'Tools',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'database',
      icon: 'Tools',
      showInMenu: true,
    },
  },
  {
    path: '/database/backup/records',
    name: 'database-backup-records',
    component: AppBackupRecords,
    meta: {
      title: '备份记录',
      page: {
        description: '查看数据库备份任务的执行记录',
        icon: 'List',
        variant: 'compact',
      },
      requiresAuth: true,
      parent: 'database',
      icon: 'List',
      showInMenu: true,
    },
  },
  {
    path: '/database/backup/restore',
    name: 'database-backup-restore',
    component: AppBackupRestore,
    meta: {
      title: '备份恢复',
      page: {
        description: '从备份文件恢复数据库',
        icon: 'RefreshLeft',
        variant: 'detail',
      },
      requiresAuth: true,
      parent: 'database',
      icon: 'RefreshLeft',
      showInMenu: true,
    },
  },
  {
    path: '/database/repair',
    name: 'database-repair',
    component: AppDatabaseRepair,
    meta: {
      title: '数据库修复',
      page: {
        description: '补齐缺失表结构并检查主键序列',
        icon: 'DataLine',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'database',
      icon: 'DataLine',
      showInMenu: true,
    },
  },
  {
    path: '/settings',
    name: 'settings',
    redirect: '/settings/user',
    meta: {
      title: '系统设置',
      requiresAuth: true,
      icon: 'Setting',
      showInMenu: true,
    },
  },
  {
    path: '/settings/user',
    name: 'settings-user',
    component: AppUserSettings,
    meta: {
      title: '用户管理',
      page: {
        description: '管理用户资料和安全设置',
        icon: 'UserFilled',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'settings',
      icon: 'UserFilled',
      showInMenu: true,
    },
  },
  {
    path: '/settings/api-keys',
    name: 'settings-api-keys',
    component: AppApiKeys,
    meta: {
      title: 'API Key',
      page: {
        description: '管理用于外部调用的 API Key',
        icon: 'Key',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'settings',
      icon: 'Key',
      showInMenu: true,
    },
  },
  {
    path: '/settings/sessions',
    name: 'settings-sessions',
    component: AppLoginSessions,
    meta: {
      title: '登录设备',
      page: {
        description: '查看和撤销已登录的设备',
        icon: 'Monitor',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'settings',
      icon: 'Monitor',
      showInMenu: true,
    },
  },
  {
    path: '/settings/notification',
    name: 'settings-notification',
    component: AppNotificationChannels,
    meta: {
      title: '通知管理',
      page: {
        description: '管理系统通知渠道和推送规则',
        icon: 'Promotion',
        variant: 'management',
      },
      requiresAuth: true,
      parent: 'settings',
      icon: 'Promotion',
      showInMenu: true,
    },
  },
  {
    path: '/settings/emby',
    name: 'settings-emby',
    component: AppEmbySettings,
    meta: {
      title: 'Emby',
      page: {
        description: '配置 Emby 服务器、通知链接和同步行为',
        icon: 'VideoPlay',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'settings',
      icon: 'VideoPlay',
      showInMenu: true,
    },
  },
  {
    path: '/settings/threads',
    name: 'settings-threads',
    component: AppThreadSettings,
    meta: {
      title: '接口速率',
      page: {
        description: '调整下载、网盘接口和 OpenList 的请求速率',
        icon: 'Operation',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'settings',
      icon: 'Operation',
      showInMenu: true,
    },
  },
  {
    path: '/settings/log',
    name: 'settings-log',
    component: AppLogSettings,
    meta: {
      title: '日志设置',
      page: {
        description: '配置日志等级和日志文件保留策略',
        icon: 'List',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'settings',
      icon: 'List',
      showInMenu: true,
    },
  },
  {
    path: '/settings/proxy',
    name: 'settings-proxy',
    component: AppProxySettings,
    meta: {
      title: '网络代理',
      page: {
        description: '配置访问外部服务时使用的网络代理',
        icon: 'Link',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'settings',
      icon: 'Link',
      showInMenu: true,
    },
  },
  {
    path: '/settings/update',
    name: 'settings-update',
    component: AppUpdate,
    meta: {
      title: '版本更新',
      page: {
        description: '检查版本、查看更新说明并执行升级',
        icon: 'Upload',
        variant: 'settings',
      },
      requiresAuth: true,
      parent: 'settings',
      icon: 'Upload',
      showInMenu: true,
    },
  },
  {
    path: '/proxy',
    redirect: '/settings/proxy',
  },
  {
    path: '/settings/database-repair',
    redirect: '/database/repair',
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/',
  },
]

const router = createRouter({
  history: createQMediaSyncHashHistory(),
  routes,
})

router.beforeEach(async (to) => {
  const authStore = useAuthStore()

  if (!authStore.hasInitialized && authStore.authStatus === 'checking') {
    await authStore.bootstrapAuth(http)
  }

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    return {
      name: 'login',
      query: { redirect: to.fullPath },
      replace: true,
    }
  }

  if (to.name === 'login' && authStore.isAuthenticated) {
    return { name: 'home', replace: true }
  }

  return true
})

router.afterEach((to, _from, failure) => {
  if (!failure && to.meta.title) {
    document.title = `${to.meta.title} - QMediaSync`
  }
})

export default router
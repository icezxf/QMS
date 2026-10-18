import type { AxiosInstance } from 'axios'
import { SERVER_URL } from '@/const'
import { unwrapResponse } from './response'
import type { APIResponse } from './types'

// ============ 配置 ============

export interface EmbyProxyConfig {
  emby_url: string
  emby_api_key: string
  stashdb_endpoint: string
  stashdb_api_key: string
  javstash_endpoint: string
  javstash_api_key: string
  tpdb_api_key: string
  wiki_email: string
}

export async function fetchEmbyProxyConfig(
  http: AxiosInstance,
): Promise<EmbyProxyConfig> {
  const response = await http.get<APIResponse<EmbyProxyConfig>>(
    `${SERVER_URL}/emby-proxy-config`,
  )
  return unwrapResponse(response) as EmbyProxyConfig
}

export async function saveEmbyProxyConfig(
  http: AxiosInstance,
  payload: EmbyProxyConfig,
): Promise<void> {
  const response = await http.put<APIResponse<null>>(
    `${SERVER_URL}/emby-proxy-config`,
    payload,
  )
  unwrapResponse(response)
}

export async function testEmbyProxy(
  http: AxiosInstance,
  payload: { emby_url: string; emby_api_key: string },
): Promise<{ ok: boolean; message: string }> {
  const response = await http.post<APIResponse<{ ok: boolean; message: string }>>(
    `${SERVER_URL}/emby-proxy-config/test`,
    payload,
  )
  return unwrapResponse(response) ?? { ok: false, message: '未知错误' }
}

// ============ 脚本管理 ============

export interface ScriptResponse {
  script: string
  is_default: boolean
}

export interface ScriptStepResult {
  index: number
  action: string
  ok: boolean
  message: string
  duration: string
}

export interface ScriptTestResponse {
  ok: boolean
  error?: string
  results?: ScriptStepResult[]
  vars?: Record<string, any>
}

export async function fetchScrapeScript(
  http: AxiosInstance,
): Promise<ScriptResponse> {
  const r = await http.get<APIResponse<ScriptResponse>>(
    `${SERVER_URL}/emby-proxy-config/script`,
  )
  return unwrapResponse(r) as ScriptResponse
}

export async function saveScrapeScript(
  http: AxiosInstance,
  script: string,
): Promise<void> {
  const r = await http.put<APIResponse<null>>(
    `${SERVER_URL}/emby-proxy-config/script`,
    { script },
  )
  unwrapResponse(r)
}

export async function resetScrapeScript(
  http: AxiosInstance,
): Promise<string> {
  const r = await http.delete<APIResponse<{ script: string }>>(
    `${SERVER_URL}/emby-proxy-config/script`,
  )
  return unwrapResponse(r)?.script ?? ''
}

export async function testScrapeScript(
  http: AxiosInstance,
  payload: { script: string; person_id: string; person_name: string },
): Promise<ScriptTestResponse> {
  const r = await http.post<APIResponse<ScriptTestResponse>>(
    `${SERVER_URL}/emby-proxy-config/script/test`,
    payload,
  )
  return unwrapResponse(r) ?? { ok: false, error: '未知错误' }
}

// ============ 代理 URL 与请求 ============

export function embyProxyUrl(path: string): string {
  const p = path.startsWith('/') ? path : '/' + path
  return `${SERVER_URL}/emby-proxy${p}`
}

// ★ 增加 version 参数用于刷新缓存
export function embyImageUrl(personId: string, version?: number | string): string {
  let url = embyProxyUrl(`/api/v1/people/${personId}/image`)
  if (version) {
    url += `?v=${version}`
  }
  return url
}

export async function embyGet<T = any>(
  http: AxiosInstance,
  path: string,
  params?: Record<string, any>,
): Promise<T> {
  const response = await http.get<T>(embyProxyUrl(path), { params })
  return response.data
}

export async function embyPatch<T = any>(
  http: AxiosInstance,
  path: string,
  body: any,
): Promise<T> {
  const response = await http.patch<T>(embyProxyUrl(path), body)
  return response.data
}

// ============ 类型定义 ============

export interface EmbyUser {
  Id: string
  Name: string
  Policy?: any
}

export interface EmbyLibrary {
  Id: string
  Name: string
  CollectionType?: string
}

export interface EmbyPerson {
  Id: string
  Name: string
  Type: string
  Role?: string
}

// Lux 详情接口返回小写
export interface EmbyPersonDetail {
  id: string
  name: string
  biography: string
  birthday: string
  placeOfBirth: string
  knownForDepartment: string
  tags: string[]
  taglines: string[]
  providerIds?: Record<string, string>
  imageUrl: string
}

// ★ 预览接口返回结构
export interface PreviewResponse {
  Name: string
  BirthDate: string
  PlaceOfBirth: string
  Bio: string
  Height: number
  Weight: number
  CupSize: string
  Waist: number
  Hip: number
  ImageURL: string
  Images: string[]
  AliasesString: string
  ProviderIds: Record<string, string>
}

// ============ Emby 接口封装 ============

export async function fetchEmbyUsers(http: AxiosInstance): Promise<EmbyUser[]> {
  const data = await embyGet<EmbyUser[]>(http, '/emby/Users')
  return Array.isArray(data) ? data : []
}

export async function fetchEmbyLibraries(
  http: AxiosInstance,
  userId: string,
): Promise<EmbyLibrary[]> {
  const data = await embyGet<{ Items: EmbyLibrary[] }>(
    http,
    `/emby/Users/${userId}/Views`,
  )
  return data?.Items ?? []
}

export async function fetchEmbyPersons(
  http: AxiosInstance,
  userId: string,
  libraryId: string,
): Promise<EmbyPerson[]> {
  const params: Record<string, any> = {
    Recursive: true,
    Fields: 'People',
    Limit: 500,
    StartIndex: 0,
  }
  if (libraryId) {
    params.ParentId = libraryId
  }

  const data = await embyGet<{
    Items: Array<{ People?: EmbyPerson[] }>
    TotalRecordCount: number
  }>(http, `/emby/Users/${userId}/Items`, params)

  const seen = new Map<string, EmbyPerson>()
  for (const item of data?.Items ?? []) {
    for (const p of item.People ?? []) {
      if (p.Type !== 'Person') continue
      if (!seen.has(p.Id)) seen.set(p.Id, p)
    }
  }
  return Array.from(seen.values())
}

export async function fetchEmbyPersonDetail(
  http: AxiosInstance,
  personId: string,
): Promise<EmbyPersonDetail> {
  return embyGet<EmbyPersonDetail>(http, `/api/v1/people/${personId}`)
}

export async function updateEmbyPerson(
  http: AxiosInstance,
  personId: string,
  payload: {
    name: string
    biography?: string
    birthday?: string
    placeOfBirth?: string
    knownForDepartment?: string
    tags?: string[]
    taglines?: string[]
  },
): Promise<void> {
  await embyPatch(http, `/api/v1/people/${personId}`, payload)
}

export async function uploadEmbyAvatarFromUrl(
  http: AxiosInstance,
  personId: string,
  imageUrl: string,
): Promise<void> {
  const resp = await fetch(imageUrl)
  const blob = await resp.blob()
  await http.post(
    embyProxyUrl(`/emby/Items/${personId}/Images/Primary`),
    blob,
    { headers: { 'Content-Type': blob.type || 'image/jpeg' } },
  )
}

export async function uploadEmbyAvatarFile(
  http: AxiosInstance,
  personId: string,
  file: File,
): Promise<void> {
  await http.post(
    embyProxyUrl(`/emby/Items/${personId}/Images/Primary`),
    file,
    { headers: { 'Content-Type': file.type || 'image/jpeg' } },
  )
}

// ============ 刮削 ============

export interface ScrapeResult {
  ok: boolean
  message?: string
  results?: ScriptStepResult[]
}

export async function scrapeEmbyPerson(
  http: AxiosInstance,
  personId: string,
  personName: string,
): Promise<ScrapeResult> {
  const response = await http.post<APIResponse<ScrapeResult>>(
    `${SERVER_URL}/emby-proxy-scrape/person/${personId}`,
    { name: personName },
  )
  return unwrapResponse(response) ?? { ok: false, message: '未知错误' }
}

// 预览（调用后端的 actionSmartFetch）
export async function previewActor(
  http: AxiosInstance,
  name: string,
): Promise<PreviewResponse> {
  const response = await http.post<APIResponse<PreviewResponse>>(
    `${SERVER_URL}/emby-proxy-scrape/preview`,
    { name },
  )
  return unwrapResponse(response) as PreviewResponse
}

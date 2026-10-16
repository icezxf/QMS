import type { AxiosInstance } from 'axios'
import { SERVER_URL } from '@/const'
import { unwrapResponse } from './response'
import type { APIResponse } from './types'

// ============ 配置 ============

export interface EmbyProxyConfig {
  emby_url: string
  emby_api_key: string
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
  payload: EmbyProxyConfig,
): Promise<{ ok: boolean; message: string }> {
  const response = await http.post<APIResponse<{ ok: boolean; message: string }>>(
    `${SERVER_URL}/emby-proxy-config/test`,
    payload,
  )
  return unwrapResponse(response) ?? { ok: false, message: '未知错误' }
}

// ============ 通用代理 URL 工具 ============

export function embyProxyUrl(path: string): string {
  const p = path.startsWith('/') ? path : '/' + path
  return `${SERVER_URL}/emby-proxy${p}`
}

export function embyImageUrl(personId: string): string {
  return embyProxyUrl(`/api/v1/people/${personId}/image`)
}

// 通用请求封装
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

export interface EmbyPersonDetail {
  Id: string
  Name: string
  Biography: string
  Birthday: string
  PlaceOfBirth: string
  KnownForDepartment: string
  Tags: string[]
  Taglines: string[]
  imageUrl: string
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
  // 只有指定库时才传 ParentId
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
  // 前端先拉图片 → 转 blob → POST 到代理
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

// ============ 刮削（占位，后端接口待实现） ============

export interface ScrapeResult {
  ok: boolean
  message?: string
}

export async function scrapeEmbyPerson(
  http: AxiosInstance,
  personId: string,
  personName: string,
): Promise<ScrapeResult> {
  const response = await http.post<APIResponse<ScrapeResult>>(
    embyProxyUrl(`/person/${personId}/scrape`),
    { name: personName },
  )
  return unwrapResponse(response) ?? { ok: false, message: '未知错误' }
}

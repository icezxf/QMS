import type { AxiosInstance } from 'axios'
import { SERVER_URL } from '@/const'
import { unwrapResponse } from './response'
import type { APIResponse } from './types'

// ============ 本地演员库 ============

export interface ActorProfile {
  id: number
  name: string
  aliases: string
  avatar_url: string
  bio: string
  stashdb_id: string
  emby_person_id: string
  sync_status: string
  last_sync_at: number
  created_at: string
  updated_at: string
}

export interface ActorListResult {
  list: ActorProfile[]
  total: number
}

export interface ActorEmbyConfig {
  emby_url: string
  emby_api_key: string
  emby_user: string
  stashdb_key: string
  sync_cron: string
}

// ============ 本地接口 ============

export async function fetchActorList(
  http: AxiosInstance,
  params: { page: number; size: number; search?: string },
): Promise<ActorListResult> {
  const response = await http.get<APIResponse<ActorListResult>>(
    `${SERVER_URL}/actor/list`,
    { params },
  )
  return unwrapResponse(response) ?? { list: [], total: 0 }
}

export async function fetchActorDetail(
  http: AxiosInstance,
  id: number,
): Promise<ActorProfile> {
  const response = await http.get<APIResponse<ActorProfile>>(
    `${SERVER_URL}/actor/${id}`,
  )
  return unwrapResponse(response) as ActorProfile
}

export async function deleteActor(
  http: AxiosInstance,
  id: number,
): Promise<void> {
  const response = await http.delete<APIResponse<null>>(
    `${SERVER_URL}/actor/${id}`,
  )
  unwrapResponse(response)
}

export async function aggregateActors(http: AxiosInstance): Promise<void> {
  const response = await http.post<APIResponse<null>>(
    `${SERVER_URL}/actor/aggregate`,
  )
  unwrapResponse(response)
}

export async function syncStashDB(
  http: AxiosInstance,
): Promise<{ ok: boolean; message?: string }> {
  const response = await http.post<APIResponse<{ ok: boolean; message?: string }>>(
    `${SERVER_URL}/actor/sync-stashdb`,
  )
  return unwrapResponse(response) ?? { ok: false }
}

export async function pushEmby(
  http: AxiosInstance,
): Promise<{ ok: boolean; message?: string }> {
  const response = await http.post<APIResponse<{ ok: boolean; message?: string }>>(
    `${SERVER_URL}/actor/push-emby`,
  )
  return unwrapResponse(response) ?? { ok: false }
}

export async function fetchActorEmbyConfig(
  http: AxiosInstance,
): Promise<ActorEmbyConfig> {
  const response = await http.get<APIResponse<ActorEmbyConfig>>(
    `${SERVER_URL}/actor/config`,
  )
  return unwrapResponse(response) as ActorEmbyConfig
}

export async function saveActorEmbyConfig(
  http: AxiosInstance,
  payload: ActorEmbyConfig,
): Promise<void> {
  const response = await http.put<APIResponse<null>>(
    `${SERVER_URL}/actor/config`,
    payload,
  )
  unwrapResponse(response)
}

// ============ Emby 演员浏览 ============

export interface EmbyLibrary {
  Id: string
  Name: string
  CollectionType: string
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

export async function fetchEmbyLibraries(
  http: AxiosInstance,
): Promise<EmbyLibrary[]> {
  const response = await http.get<APIResponse<{ list: EmbyLibrary[] }>>(
    `${SERVER_URL}/actor/emby/libraries`,
  )
  return unwrapResponse(response)?.list ?? []
}

export async function fetchEmbyPersons(
  http: AxiosInstance,
  libraryId: string,
): Promise<EmbyPerson[]> {
  const response = await http.get<APIResponse<{ list: EmbyPerson[] }>>(
    `${SERVER_URL}/actor/emby/persons`,
    { params: { library_id: libraryId } },
  )
  return unwrapResponse(response)?.list ?? []
}

export async function fetchEmbyPersonDetail(
  http: AxiosInstance,
  personId: string,
): Promise<EmbyPersonDetail> {
  const response = await http.get<APIResponse<EmbyPersonDetail>>(
    `${SERVER_URL}/actor/emby/person/${personId}`,
  )
  return unwrapResponse(response) as EmbyPersonDetail
}

export async function testEmbyConnection(
  http: AxiosInstance,
  payload: { emby_url: string; emby_api_key: string; emby_user: string },
): Promise<{ ok: boolean; message: string }> {
  const response = await http.post<APIResponse<{ ok: boolean; message: string }>>(
    `${SERVER_URL}/actor/emby/test`,
    payload,
  )
  return unwrapResponse(response) ?? { ok: false, message: '未知错误' }
}

// 头像 URL（供 <img src> 使用）
export function embyPersonImageUrl(personId: string): string {
  return `${SERVER_URL}/actor/emby/person/${personId}/image`
}

// 本地演员头像 URL
export function localActorAvatarUrl(actorId: number): string {
  return `${SERVER_URL}/actor/${actorId}/avatar`
}
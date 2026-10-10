import type { AxiosInstance } from 'axios'
import { SERVER_URL } from '@/const'
import { unwrapResponse } from './response'
import type { APIResponse } from './types'

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
  stashdb_key: string
  sync_cron: string
}

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

export async function syncStashDB(http: AxiosInstance): Promise<{ ok: boolean; message?: string }> {
  const response = await http.post<APIResponse<{ ok: boolean; message?: string }>>(
    `${SERVER_URL}/actor/sync-stashdb`,
  )
  return unwrapResponse(response) ?? { ok: false }
}

export async function pushEmby(http: AxiosInstance): Promise<{ ok: boolean; message?: string }> {
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

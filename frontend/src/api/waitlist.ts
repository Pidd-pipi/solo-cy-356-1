import { del, get, post } from '@/utils/request'
import type { Plot } from './plot'
import type { UserInfo } from './auth'

export type WaitlistStatusValue = 'waiting' | 'invited' | 'adopted' | 'cancelled'

export interface WaitlistEntry {
  id: number
  plot_id: number
  plot?: Plot | null
  user_id: number
  user?: UserInfo | null
  status: WaitlistStatusValue
  note: string
  rank: number
  invited_at?: string | null
  canceled_at?: string | null
  created_at: string
}

export interface MyWaitlistStatus {
  plot_id: number
  waitlist_id: number
  status: WaitlistStatusValue | ''
  rank: number
  waiting_count: number
  active_count: number
  reserved: boolean
  invited_to_me: boolean
}

export interface WaitlistSummary extends MyWaitlistStatus {
  mine: WaitlistEntry | null
}

// 申请候补
export function applyWaitlist(plotId: number, note = ''): Promise<WaitlistEntry> {
  return post(`/plots/${plotId}/waitlist`, { note })
}

// 取消自己的候补申请
export function cancelWaitlist(id: number): Promise<WaitlistEntry> {
  return del(`/waitlist/${id}`)
}

// 管理员/认养人查看候补名单（按申请时间排序）
export function listPlotWaitlist(plotId: number): Promise<{ list: WaitlistEntry[]; total: number }> {
  return get(`/plots/${plotId}/waitlist`)
}

// 候补人数 + 当前用户申请状态
export function getWaitlistSummary(plotId: number): Promise<WaitlistSummary> {
  return get(`/plots/${plotId}/waitlist/summary`)
}

// 地块列表批量查询
export function batchWaitlistStatus(plotIds: number[]): Promise<{ list: MyWaitlistStatus[] }> {
  return get('/waitlist/status', { params: { plot_ids: plotIds.join(',') } })
}

// 我的候补申请
export function listMyWaitlist(params?: Record<string, any>): Promise<{ list: WaitlistEntry[]; total: number; page: number; page_size: number }> {
  return get('/waitlist/mine', { params })
}

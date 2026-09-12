import { defineStore } from 'pinia'
import {
  applyWaitlist,
  batchWaitlistStatus,
  cancelWaitlist,
  listMyWaitlist,
  listPlotWaitlist,
  type MyWaitlistStatus,
  type WaitlistEntry
} from '@/api/waitlist'

interface WaitlistState {
  // 地块列表页：plot_id -> 候补状态（人数 + 我的申请）
  statusByPlot: Record<number, MyWaitlistStatus>
  // 候补名单弹窗：plot_id -> 名单
  listByPlot: Record<number, WaitlistEntry[]>
  // 我的候补
  mine: WaitlistEntry[]
  mineTotal: number
  loading: boolean
}

export const useWaitlistStore = defineStore('waitlist', {
  state: (): WaitlistState => ({
    statusByPlot: {},
    listByPlot: {},
    mine: [],
    mineTotal: 0,
    loading: false
  }),
  getters: {
    // 某地块候补人数（排队中）
    waitingCount: (state) => (plotId: number) => state.statusByPlot[plotId]?.waiting_count ?? 0,
    // 当前用户在某地块的有效申请（无则 null）
    myEntry: (state) => (plotId: number): WaitlistEntry | null => {
      const s = state.statusByPlot[plotId]
      if (!s || !s.waitlist_id) return null
      return {
        id: s.waitlist_id,
        plot_id: s.plot_id,
        user_id: 0,
        status: (s.status || 'waiting') as WaitlistEntry['status'],
        note: '',
        rank: s.rank,
        created_at: ''
      }
    }
  },
  actions: {
    async fetchBatchStatus(plotIds: number[]) {
      if (plotIds.length === 0) return
      const data = await batchWaitlistStatus(plotIds)
      const map: Record<number, MyWaitlistStatus> = {}
      for (const item of data.list) map[item.plot_id] = item
      this.statusByPlot = { ...this.statusByPlot, ...map }
    },
    async fetchPlotList(plotId: number) {
      this.loading = true
      try {
        const data = await listPlotWaitlist(plotId)
        this.listByPlot = { ...this.listByPlot, [plotId]: data.list }
        return data.list
      } finally {
        this.loading = false
      }
    },
    async apply(plotId: number, note = '') {
      await applyWaitlist(plotId, note)
      await this.fetchBatchStatus([plotId])
    },
    async cancel(plotId: number, entryId: number) {
      await cancelWaitlist(entryId)
      await this.fetchBatchStatus([plotId])
    },
    async fetchMine(params?: Record<string, any>) {
      this.loading = true
      try {
        const data = await listMyWaitlist(params)
        this.mine = data.list
        this.mineTotal = data.total
      } finally {
        this.loading = false
      }
    }
  }
})

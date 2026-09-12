<template>
  <span class="waitlist-actions">
    <!-- 候补人数（管理员/认养人可点击查看名单） -->
    <el-button
      v-if="canViewList && status && status.waiting_count >= 0"
      link type="primary" size="small" @click="$emit('view-list', plotId)"
    >
      候补 {{ status.waiting_count }} 人
      <template v-if="status.active_count > status.waiting_count">（{{ status.active_count }} 人有效）</template>
    </el-button>
    <span v-else-if="status" class="wait-count">候补 {{ status.waiting_count }} 人</span>

    <!-- 当前用户申请状态与操作 -->
    <template v-if="my">
      <StatusBadge :value="my.status" :meta-map="WaitlistStatusMeta" />
      <span v-if="my.status === 'waiting'" class="rank-text">（排第 {{ my.rank }} 位）</span>
      <el-button v-if="my.status === 'invited'" type="success" size="small" @click="$emit('adopt', plotId)">
        立即认养
      </el-button>
      <el-button
        v-if="my.status === 'waiting' || my.status === 'invited'"
        type="danger" link size="small" @click="$emit('cancel', plotId, my.id)"
      >
        取消申请
      </el-button>
    </template>
    <!-- 地块已被认养/待释放且本人无有效申请时可申请候补 -->
    <el-button
      v-else-if="canApply && loggedIn"
      type="warning" plain size="small" @click="$emit('apply', plotId)"
    >
      申请候补
    </el-button>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { WaitlistStatusMeta } from '@/constants'
import type { MyWaitlistStatus } from '@/api/waitlist'
import type { Plot } from '@/api/plot'

const props = defineProps<{
  plot: Plot
  status?: MyWaitlistStatus
  canViewList: boolean
  loggedIn?: boolean
}>()

defineEmits<{
  (e: 'apply', plotId: number): void
  (e: 'cancel', plotId: number, entryId: number): void
  (e: 'adopt', plotId: number): void
  (e: 'view-list', plotId: number): void
}>()

const plotId = computed(() => props.plot.id)
const my = computed(() => {
  const s = props.status
  if (!s || !s.waitlist_id || !s.status) return null
  return { id: s.waitlist_id, status: s.status, rank: s.rank }
})
// 已认养 / 待释放地块才允许候补；空闲且无人受邀时直接认养即可
const canApply = computed(() => props.plot.status === 'adopted' || props.plot.status === 'harvested')
</script>

<style scoped>
.waitlist-actions { display: inline-flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.wait-count { color: #909399; font-size: 12px; }
.rank-text { color: #e6a23c; font-size: 12px; }
</style>

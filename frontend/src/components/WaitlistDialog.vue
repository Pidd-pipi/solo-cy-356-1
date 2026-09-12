<template>
  <el-dialog
    :model-value="modelValue"
    :title="`候补名单（${plotName || '地块'}）`"
    width="640px"
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <el-table :data="entries" v-loading="loading" size="small" stripe>
      <el-table-column label="位次" width="70">
        <template #default="{ row }">
          <el-tag v-if="row.status === 'invited'" type="danger" size="small">受邀</el-tag>
          <span v-else-if="row.status === 'waiting'">第 {{ row.rank }} 位</span>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="申请人" min-width="120">
        <template #default="{ row }">{{ row.user?.nickname || row.user?.username || `用户${row.user_id}` }}</template>
      </el-table-column>
      <el-table-column label="状态" width="150">
        <template #default="{ row }"><StatusBadge :value="row.status" :meta-map="WaitlistStatusMeta" /></template>
      </el-table-column>
      <el-table-column prop="created_at" label="申请时间" width="170" />
      <el-table-column label="备注" min-width="120">
        <template #default="{ row }">{{ row.note || '-' }}</template>
      </el-table-column>
    </el-table>
    <EmptyState v-if="!loading && entries.length === 0" description="暂无候补申请" />
    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import StatusBadge from '@/components/StatusBadge.vue'
import EmptyState from '@/components/EmptyState.vue'
import { WaitlistStatusMeta } from '@/constants'
import type { WaitlistEntry } from '@/api/waitlist'

defineProps<{
  modelValue: boolean
  entries: WaitlistEntry[]
  loading: boolean
  plotName?: string
}>()

defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()
</script>

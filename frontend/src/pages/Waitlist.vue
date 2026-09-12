<template>
  <div class="page-card">
    <div style="display: flex; justify-content: space-between; align-items: center">
      <h3 class="page-title">我的候补认养</h3>
      <el-radio-group v-model="statusFilter" size="small" @change="onFilter">
        <el-radio-button label="">全部</el-radio-button>
        <el-radio-button label="waiting">候补中</el-radio-button>
        <el-radio-button label="invited">已获资格</el-radio-button>
        <el-radio-button label="cancelled">已取消</el-radio-button>
      </el-radio-group>
    </div>

    <DataTable
      :data="store.mine"
      :loading="store.loading"
      :total="store.mineTotal"
      :page-size="pagination.size.value"
      :current-page="pagination.page.value"
      @update:current-page="onPage"
    >
      <el-table-column label="地块编号" width="110">
        <template #default="{ row }">{{ row.plot?.code || `#${row.plot_id}` }}</template>
      </el-table-column>
      <el-table-column label="地块名称" min-width="140">
        <template #default="{ row }">{{ row.plot?.name || '-' }}</template>
      </el-table-column>
      <el-table-column label="状态" width="170">
        <template #default="{ row }">
          <StatusBadge :value="row.status" :meta-map="WaitlistStatusMeta" />
          <span v-if="row.status === 'waiting'" class="rank">排第 {{ row.rank }} 位</span>
        </template>
      </el-table-column>
      <el-table-column label="申请时间" width="180">
        <template #default="{ row }">{{ row.created_at }}</template>
      </el-table-column>
      <el-table-column label="受邀时间" width="180">
        <template #default="{ row }">{{ row.invited_at ? formatDateTime(row.invited_at) : '-' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="200">
        <template #default="{ row }">
          <el-button v-if="row.status === 'invited'" type="success" size="small" @click="goAdopt">立即认养</el-button>
          <el-button v-if="row.status === 'waiting' || row.status === 'invited'" type="danger" link size="small" @click="cancel(row)">
            取消申请
          </el-button>
          <span v-if="row.status === 'cancelled' || row.status === 'adopted'" class="muted">-</span>
        </template>
      </el-table-column>
    </DataTable>

    <EmptyState v-if="!store.loading && store.mine.length === 0" description="暂无候补申请，可到“地块认养”页对已认养地块申请候补" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useWaitlistStore } from '@/stores/waitlist'
import { usePagination } from '@/hooks/usePagination'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import EmptyState from '@/components/EmptyState.vue'
import { WaitlistStatusMeta } from '@/constants'
import { formatDateTime } from '@/utils/format'
import type { WaitlistEntry } from '@/api/waitlist'

const router = useRouter()
const store = useWaitlistStore()
const pagination = usePagination()
const statusFilter = ref('')

async function fetch() {
  await store.fetchMine({
    page: pagination.page.value,
    page_size: pagination.size.value,
    ...(statusFilter.value ? { status: statusFilter.value } : {})
  })
}

function onPage(page: number) {
  pagination.page.value = page
  fetch()
}

function onFilter() {
  pagination.page.value = 1
  fetch()
}

function goAdopt() {
  ElMessage.success('你已获得优先认养资格，前往地块列表完成认养')
  router.push('/plots')
}

async function cancel(row: WaitlistEntry) {
  try {
    await ElMessageBox.confirm('确认取消该候补申请吗？取消后需要重新排队。', '取消候补', { type: 'warning' })
  } catch {
    return
  }
  await store.cancel(row.plot_id, row.id)
  ElMessage.success('候补申请已取消')
  await fetch()
}

onMounted(fetch)
</script>

<style scoped>
.rank { margin-left: 8px; color: #e6a23c; font-size: 12px; }
.muted { color: #909399; }
</style>

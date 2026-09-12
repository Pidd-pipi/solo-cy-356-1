<template>
  <div class="page-card">
    <div style="display: flex; justify-content: space-between; align-items: center">
      <h3 class="page-title">菜园地块认养（GIS 分布）</h3>
      <div>
        <el-button v-if="isAdmin" type="primary" @click="openCreate">+ 新增地块</el-button>
      </div>
    </div>

    <el-card shadow="never" style="margin-bottom: 16px">
      <template #header>地块分布图（按经纬度示意）</template>
      <svg :viewBox="viewBoxStr" class="plot-map" xmlns="http://www.w3.org/2000/svg">
        <rect x="0" y="0" :width="mapW" :height="mapH" fill="#e8f5e9" stroke="#a5d6a7" />
        <line v-for="i in 4" :key="'h' + i" :x1="0" :y1="(mapH / 5) * i" :x2="mapW" :y2="(mapH / 5) * i" stroke="#c8e6c9" stroke-dasharray="4 4" />
        <line v-for="i in 4" :key="'v' + i" :x1="(mapW / 5) * i" :y1="0" :x2="(mapW / 5) * i" :y2="mapH" stroke="#c8e6c9" stroke-dasharray="4 4" />
        <g v-for="p in store.plots" :key="p.id">
          <circle :cx="mapX(p)" :cy="mapY(p)" r="10" :fill="colorOf(p.status)" stroke="#fff" stroke-width="2" />
          <text :x="mapX(p)" :y="mapY(p) - 14" text-anchor="middle" font-size="10" fill="#333">{{ p.code }}</text>
          <title>{{ p.name }}｜{{ PlotStatusMeta[p.status]?.label }}</title>
        </g>
      </svg>
    </el-card>

    <DataTable :data="store.plots" :loading="store.loading" :total="store.total" :page-size="pagination.size.value" :current-page="pagination.page.value" @update:current-page="onPage">
      <el-table-column prop="code" label="编号" width="90" />
      <el-table-column prop="name" label="地块名称" min-width="140" />
      <el-table-column label="面积" width="90">
        <template #default="{ row }">{{ formatArea(row.area) }}</template>
      </el-table-column>
      <el-table-column label="土壤" width="90">
        <template #default="{ row }">{{ SoilTypeText[row.soil_type] }}</template>
      </el-table-column>
      <el-table-column label="日照" width="90">
        <template #default="{ row }">{{ SunlightText[row.sunlight] }}</template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }"><StatusBadge :value="row.status" :meta-map="PlotStatusMeta" /></template>
      </el-table-column>
      <el-table-column label="认养人" width="120">
        <template #default="{ row }">{{ row.adopter?.nickname || row.adopter?.username || '-' }}</template>
      </el-table-column>
      <el-table-column label="候补认养" min-width="220">
        <template #default="{ row }">
          <WaitlistActions
            :plot="row"
            :status="waitStore.statusByPlot[row.id]"
            :can-view-list="role === 'admin' || row.adopter_id === user?.id"
            :logged-in="isLoggedIn"
            :is-owner="row.adopter_id === user?.id"
            @apply="onApply"
            @cancel="onCancel"
            @adopt="onInvitedAdopt"
            @view-list="onViewList"
          />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="220">
        <template #default="{ row }">
          <el-button v-if="canAdopt(row)" type="success" size="small" @click="adopt(row)">认养</el-button>
          <el-button v-if="canRelease(row)" type="warning" size="small" @click="release(row)">释放</el-button>
        </template>
      </el-table-column>
    </DataTable>

    <WaitlistDialog
      v-model="listVisible"
      :entries="waitStore.listByPlot[listPlotId] || []"
      :loading="waitStore.loading"
      :plot-name="listPlotName"
    />

    <el-dialog v-model="createVisible" title="新增地块（管理员）" width="520px">
      <el-form :model="createForm" label-width="90px">
        <el-form-item label="地块名称"><el-input v-model="createForm.name" /></el-form-item>
        <el-form-item label="地块编号"><el-input v-model="createForm.code" placeholder="如 P-007" /></el-form-item>
        <el-form-item label="面积(m²)"><el-input-number v-model="createForm.area" :min="1" /></el-form-item>
        <el-form-item label="土壤类型">
          <el-select v-model="createForm.soil_type">
            <el-option v-for="(t, k) in SoilTypeText" :key="k" :label="t" :value="k" />
          </el-select>
        </el-form-item>
        <el-form-item label="日照条件">
          <el-select v-model="createForm.sunlight">
            <el-option v-for="(t, k) in SunlightText" :key="k" :label="t" :value="k" />
          </el-select>
        </el-form-item>
        <el-form-item label="纬度"><el-input-number v-model="createForm.latitude" :precision="4" :step="0.001" /></el-form-item>
        <el-form-item label="经度"><el-input-number v-model="createForm.longitude" :precision="4" :step="0.001" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="createForm.description" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="submitCreate">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { usePlotStore } from '@/stores/plot'
import { useWaitlistStore } from '@/stores/waitlist'
import { createPlot, type Plot } from '@/api/plot'
import { useAuth } from '@/hooks/useAuth'
import { usePagination } from '@/hooks/usePagination'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import WaitlistActions from '@/components/WaitlistActions.vue'
import WaitlistDialog from '@/components/WaitlistDialog.vue'
import { PlotStatusMeta, SoilTypeText, SunlightText } from '@/constants'
import { formatArea, clamp } from '@/utils/format'

const store = usePlotStore()
const waitStore = useWaitlistStore()
const pagination = usePagination()
const { user, role, isAdmin, isLoggedIn } = useAuth()

const createVisible = ref(false)
const creating = ref(false)
const createForm = reactive({ name: '', code: '', area: 10, soil_type: 'loam', sunlight: 'full', latitude: 31.2304, longitude: 121.4737, description: '' })

// 候补名单弹窗
const listVisible = ref(false)
const listPlotId = ref(0)
const listPlotName = ref('')

const mapW = 600
const mapH = 360
const viewBoxStr = `0 0 ${mapW} ${mapH}`

function mapX(p: Plot) {
  return clamp(((p.longitude - 121.47) / 0.01) * 1000 + mapW / 2, 20, mapW - 20)
}
function mapY(p: Plot) {
  return clamp(((31.24 - p.latitude) / 0.02) * 1000 + mapH / 2, 20, mapH - 20)
}
function colorOf(status: string) {
  if (status === 'available') return '#67c23a'
  if (status === 'adopted') return '#e6a23c'
  return '#909399'
}

function onPage(page: number) {
  pagination.page.value = page
  fetch()
}

async function fetch() {
  await store.fetchPlots({ page: pagination.page.value, page_size: pagination.size.value })
  if (isLoggedIn.value) {
    await waitStore.fetchBatchStatus(store.plots.map((p) => p.id))
  }
}

function canRelease(row: Plot) {
  return row.status === 'harvested' && (role.value === 'admin' || row.adopter_id === user.value?.id)
}

// 空闲地块：未被受邀资格预占，或受邀人就是本人时，本人可直接发起认养
function canAdopt(row: Plot) {
  if (row.status !== 'available') return false
  const wl = waitStore.statusByPlot[row.id]
  if (!wl) return true
  return !wl.reserved || wl.invited_to_me
}

async function adopt(row: Plot) {
  try {
    await ElMessageBox.confirm(`确认认养地块 ${row.name}（${row.code}）吗？`, '认养确认', { type: 'success' })
  } catch {
    return
  }
  await store.adopt(row.id)
  ElMessage.success('认养成功，开始你的都市农夫之旅')
  await fetch()
}

async function onInvitedAdopt(plotId: number) {
  const row = store.plots.find((p) => p.id === plotId)
  await adopt(row as Plot)
}

async function release(row: Plot) {
  try {
    await ElMessageBox.confirm(`确认释放地块 ${row.name} 吗？释放后最早候补者将获得优先认养资格。`, '释放确认', { type: 'warning' })
  } catch {
    return
  }
  const { releasePlot } = await import('@/api/plot')
  await releasePlot(row.id)
  ElMessage.success('地块已释放，已通知最早候补者')
  await fetch()
}

async function onApply(plotId: number) {
  try {
    const { value } = await ElMessageBox.prompt(`申请候补认养该地块，释放后将按申请时间优先通知（可留备注）`, '申请候补', {
      confirmButtonText: '提交申请',
      cancelButtonText: '取消',
      inputType: 'textarea',
      inputValue: ''
    })
    await waitStore.apply(plotId, value || '')
    ElMessage.success('候补申请已提交')
  } catch (e: any) {
    if (e === 'cancel') return
  }
}

async function onCancel(plotId: number, entryId: number) {
  try {
    await ElMessageBox.confirm('确认取消该候补申请吗？取消后需要重新排队。', '取消候补', { type: 'warning' })
  } catch {
    return
  }
  await waitStore.cancel(plotId, entryId)
  ElMessage.success('候补申请已取消')
}

async function onViewList(plotId: number) {
  const row = store.plots.find((p) => p.id === plotId)
  listPlotId.value = plotId
  listPlotName.value = row ? `${row.name}（${row.code}）` : ''
  listVisible.value = true
  await waitStore.fetchPlotList(plotId)
}

function openCreate() {
  createVisible.value = true
}

async function submitCreate() {
  creating.value = true
  try {
    await createPlot({ ...createForm })
    ElMessage.success('地块创建成功')
    createVisible.value = false
    await fetch()
  } finally {
    creating.value = false
  }
}

onMounted(fetch)
</script>

<style scoped>
.plot-map { width: 100%; height: 360px; border-radius: 8px; }
</style>

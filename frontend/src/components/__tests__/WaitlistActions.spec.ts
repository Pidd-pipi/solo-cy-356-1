import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import WaitlistActions from '@/components/WaitlistActions.vue'
import type { MyWaitlistStatus } from '@/api/waitlist'
import type { Plot } from '@/api/plot'

// 纯组件测试：只通过 props 驱动、断言渲染结果与 emitted 事件，不发起任何网络请求。

function adoptedPlot(id = 7): Plot {
  return {
    id,
    name: '测试地块',
    code: 'P-T1',
    area: 10,
    soil_type: 'loam',
    sunlight: 'full',
    latitude: 31,
    longitude: 121,
    status: 'adopted',
    adopter_id: 100,
    adopter: null,
    description: '',
    created_at: ''
  }
}

function mountComponent(props: {
  plot?: Plot
  status?: MyWaitlistStatus
  canViewList?: boolean
  loggedIn?: boolean
  isOwner?: boolean
}) {
  return mount(WaitlistActions, {
    props: { plot: adoptedPlot(), canViewList: false, ...props },
    global: { plugins: [ElementPlus] }
  })
}

function findButtonByText(wrapper: ReturnType<typeof mount>, text: string) {
  return wrapper.findAll('button').find((b) => (b.element.textContent || '').replace(/\s+/g, '').includes(text))
}

const noEntry: MyWaitlistStatus = {
  plot_id: 7,
  waitlist_id: 0,
  status: '',
  rank: 0,
  waiting_count: 0,
  active_count: 0,
  reserved: false,
  invited_to_me: false
}

describe('WaitlistActions.vue', () => {
  it('认养人查看自己的地块：不显示申请候补入口，只显示候补人数/名单入口', () => {
    const wrapper = mountComponent({
      status: { ...noEntry, waiting_count: 2, active_count: 2 },
      canViewList: true,
      loggedIn: true,
      isOwner: true
    })

    // 不显示申请候补 / 取消 / 立即认养
    expect(findButtonByText(wrapper, '申请候补')).toBeUndefined()
    expect(findButtonByText(wrapper, '取消申请')).toBeUndefined()
    expect(findButtonByText(wrapper, '立即认养')).toBeUndefined()

    // 候补人数入口可见
    const entry = findButtonByText(wrapper, '候补2人')
    expect(entry).toBeTruthy()

    // 点击名单入口触发 view-list 事件
    entry!.trigger('click')
    expect(wrapper.emitted('view-list')?.[0]).toEqual([7])
    expect(wrapper.emitted('apply')).toBeUndefined()
  })

  it('普通市民对已认养地块：可以申请候补，点击触发 apply', async () => {
    const wrapper = mountComponent({
      status: noEntry,
      canViewList: false,
      loggedIn: true,
      isOwner: false
    })

    const applyBtn = findButtonByText(wrapper, '申请候补')
    expect(applyBtn).toBeTruthy()
    expect(findButtonByText(wrapper, '取消申请')).toBeUndefined()
    expect(findButtonByText(wrapper, '立即认养')).toBeUndefined()

    await applyBtn!.trigger('click')
    expect(wrapper.emitted('apply')?.[0]).toEqual([7])
  })

  it('候补中：显示排队状态与取消申请，点击触发 cancel(plotId, entryId)', async () => {
    const wrapper = mountComponent({
      status: { ...noEntry, waitlist_id: 55, status: 'waiting', rank: 2, waiting_count: 3, active_count: 3 } as MyWaitlistStatus,
      canViewList: false,
      loggedIn: true,
      isOwner: false
    })

    // 申请入口消失，显示排队位次与取消按钮
    expect(findButtonByText(wrapper, '申请候补')).toBeUndefined()
    expect(wrapper.text()).toContain('候补中')
    expect(wrapper.text()).toContain('排第 2 位')
    const cancelBtn = findButtonByText(wrapper, '取消申请')
    expect(cancelBtn).toBeTruthy()

    await cancelBtn!.trigger('click')
    expect(wrapper.emitted('cancel')?.[0]).toEqual([7, 55])
    expect(wrapper.emitted('adopt')).toBeUndefined()
  })

  it('受邀后：显示立即认养，点击触发 adopt；仍可取消', async () => {
    const wrapper = mountComponent({
      status: { ...noEntry, waitlist_id: 55, status: 'invited', active_count: 1, reserved: true, invited_to_me: true } as MyWaitlistStatus,
      canViewList: false,
      loggedIn: true,
      isOwner: false
    })

    expect(findButtonByText(wrapper, '申请候补')).toBeUndefined()
    expect(wrapper.text()).toContain('优先认养资格')

    const adoptBtn = findButtonByText(wrapper, '立即认养')
    expect(adoptBtn).toBeTruthy()
    const cancelBtn = findButtonByText(wrapper, '取消申请')
    expect(cancelBtn).toBeTruthy()

    await adoptBtn!.trigger('click')
    expect(wrapper.emitted('adopt')?.[0]).toEqual([7])

    await cancelBtn!.trigger('click')
    expect(wrapper.emitted('cancel')?.[0]).toEqual([7, 55])
  })
})

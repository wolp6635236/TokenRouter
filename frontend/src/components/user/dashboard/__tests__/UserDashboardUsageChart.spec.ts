import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

import { defineComponent, h, onMounted } from 'vue'
import UserDashboardUsageChart from '../UserDashboardUsageChart.vue'
import UserDashboardUsageToolbar from '../UserDashboardUsageToolbar.vue'
import { provideUsageChartState } from '../usageChartState'
import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import { Line } from 'vue-chartjs'
import { usageAPI } from '@/api/usage'
import { formatDayKey } from '../usageChartData'

// 图表替身接收数据和选项，供组件测试检查配置。
vi.mock('vue-chartjs', () => ({
  Line: {
    name: 'Line',
    props: ['data', 'options', 'plugins'],
    render: () => null,
  },
}))

enableAutoUnmount(afterEach)

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      locale: { value: 'en' },
    }),
  }
})

vi.mock('@/composables/useBalanceDisplay', () => ({
  useBalanceDisplay: () => ({
    formatBalanceAmount: (value: number) => String(value),
  }),
}))

vi.mock('@/api/usage', () => ({
  usageAPI: {
    getDashboardTrend: vi.fn(),
    getDashboardModels: vi.fn(),
  },
}))

vi.mock('@/api/keys', () => ({
  keysAPI: {
    list: vi.fn().mockResolvedValue({ items: [{ id: 7, name: 'personal-key' }] }),
  },
}))

vi.mock('@/api/groups', () => ({
  userGroupsAPI: {
    getAvailable: vi.fn().mockResolvedValue([{ id: 3, name: 'Claude' }]),
  },
}))

vi.mock('@/api/team', () => ({
  teamAPI: {
    current: vi.fn().mockRejectedValue({ reason: 'TEAM_NOT_FOUND' }),
    keys: vi.fn(),
  },
}))

const trendOf = (points: Array<{ date: string; requests: number; actual_cost?: number }>) => ({
  trend: points.map((item) => ({
    input_tokens: 0,
    output_tokens: 0,
    cache_creation_tokens: 0,
    cache_read_tokens: 0,
    total_tokens: 0,
    cost: 0,
    actual_cost: 0,
    ...item,
  })),
  start_date: '',
  end_date: '',
  granularity: 'day',
})

// 模拟仪表盘页面：提供共享状态，同时渲染标题行工具栏和正文图表。
const DashboardHarness = defineComponent({
  props: { refreshing: { type: Boolean, default: false } },
  emits: ['refresh'],
  setup(props, { emit }) {
    const state = provideUsageChartState()
    onMounted(() => {
      void state.load()
      void state.loadFilterOptions()
    })
    return () => h('div', [
      h(UserDashboardUsageToolbar, { refreshing: props.refreshing, onRefresh: () => emit('refresh') }),
      h(UserDashboardUsageChart),
    ])
  },
})

const mountChart = async () => {
  const wrapper = mount(DashboardHarness, {
    global: {
      stubs: {
        Line: true,
        DateRangePicker: true,
        Select: true,
        // 直接渲染筛选面板内容，便于断言各个筛选框
        FilterDropdown: { template: '<div><slot /></div>' },
        RouterLink: { template: '<a><slot /></a>' },
        ModelIcon: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('UserDashboardUsageChart', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.mocked(usageAPI.getDashboardTrend).mockReset()
    vi.mocked(usageAPI.getDashboardModels).mockReset()
    vi.mocked(usageAPI.getDashboardModels).mockResolvedValue({
      models: [{ model: 'claude-opus', requests: 1 } as any],
      start_date: '',
      end_date: '',
    })
  })

  afterEach(() => {
    localStorage.clear()
  })

  it('默认拉取近 7 天和上一周期，并在页签上显示合计与环比', async () => {
    const today = formatDayKey(new Date())
    vi.mocked(usageAPI.getDashboardTrend)
      .mockResolvedValueOnce(trendOf([{ date: today, requests: 30 }]) as any)
      .mockResolvedValueOnce(trendOf([{ date: '2000-01-01', requests: 20 }]) as any)

    const wrapper = await mountChart()

    expect(usageAPI.getDashboardTrend).toHaveBeenCalledTimes(2)
    expect(vi.mocked(usageAPI.getDashboardTrend).mock.calls[0][0]).toMatchObject({ granularity: 'day' })
    expect(wrapper.get('[data-testid="usage-metric-requests"]').text()).toContain('30')
    expect(wrapper.get('[data-testid="usage-delta-requests"]').text()).toContain('+50%')
    expect(wrapper.get('[data-testid="usage-delta-cost"]').text()).toContain('dashboard.usageChart.noPrevious')
    // 范围只由日期选择器表达，默认 7 天覆盖今天在内的 7 个日历日
    const picker = wrapper.findComponent(DateRangePicker)
    const start = new Date()
    start.setDate(start.getDate() - 6)
    expect(picker.props('startDate')).toBe(formatDayKey(start))
    expect(picker.props('endDate')).toBe(today)
  })

  it('所选范围没有用量时显示空状态', async () => {
    vi.mocked(usageAPI.getDashboardTrend).mockResolvedValue(trendOf([]) as any)
    const wrapper = await mountChart()
    expect(wrapper.find('[data-testid="usage-chart-empty"]').exists()).toBe(true)
  })

  it('日期选择器选近 24 小时后按小时重新取数，并记住指标与范围', async () => {
    vi.mocked(usageAPI.getDashboardTrend).mockResolvedValue(trendOf([]) as any)
    const wrapper = await mountChart()

    await wrapper.get('[data-testid="usage-metric-cost"]').trigger('click')
    wrapper.findComponent(DateRangePicker).vm.$emit('change', { startDate: '2000-01-01', endDate: '2000-01-02', preset: 'last24Hours' })
    await flushPromises()

    const lastCall = vi.mocked(usageAPI.getDashboardTrend).mock.calls.at(-2)?.[0]
    expect(lastCall).toMatchObject({ granularity: 'hour' })
    expect(wrapper.get('[data-testid="usage-metric-cost"]').attributes('aria-selected')).toBe('true')
    expect(JSON.parse(localStorage.getItem('dashboard-usage-chart') || '{}')).toEqual({ metric: 'cost', range: '24h' })
  })

  it('Token 指标可以开关分项', async () => {
    vi.mocked(usageAPI.getDashboardTrend).mockResolvedValue(trendOf([]) as any)
    const wrapper = await mountChart()
    const chip = wrapper.get('[data-testid="usage-series-cache_read_tokens"]')
    expect(chip.attributes('aria-pressed')).toBe('true')
    await chip.trigger('click')
    expect(chip.attributes('aria-pressed')).toBe('false')
  })

  it('筛选条件与使用记录页一致，选择后按筛选重新取数', async () => {
    vi.mocked(usageAPI.getDashboardTrend).mockResolvedValue(trendOf([]) as any)
    const wrapper = await mountChart()

    const selects = wrapper.findAllComponents(Select)
    expect(selects).toHaveLength(7)
    const [keySelect, modelSelect, groupSelect, typeSelect] = selects
    expect(keySelect.props('options')).toEqual([
      { value: null, label: 'usage.allApiKeys' },
      { value: 7, label: 'personal-key' },
    ])
    expect(modelSelect.props('options')).toEqual([
      { value: null, label: 'admin.usage.allModels' },
      { value: 'claude-opus', label: 'claude-opus' },
    ])
    expect(groupSelect.props('options')).toEqual([
      { value: null, label: 'admin.usage.allGroups' },
      { value: 3, label: 'Claude' },
    ])

    vi.mocked(usageAPI.getDashboardTrend).mockClear()
    groupSelect.vm.$emit('update:modelValue', 3)
    typeSelect.vm.$emit('update:modelValue', 'sync')
    typeSelect.vm.$emit('change', 'sync', null)
    await flushPromises()

    const calls = vi.mocked(usageAPI.getDashboardTrend).mock.calls.map((call) => call[0])
    expect(calls).toHaveLength(2)
    expect(calls[0]).toMatchObject({ group_id: 3, request_type: 'sync', stream: false })
    expect(calls[0]).not.toHaveProperty('model')
    // 模型排行按筛选重新取数，候选项沿用不带筛选的结果
    const modelCalls = vi.mocked(usageAPI.getDashboardModels).mock.calls
    expect(modelCalls).toHaveLength(2)
    expect(modelCalls[1][0]).toMatchObject({ group_id: 3, request_type: 'sync' })
    expect(modelSelect.props('options')).toHaveLength(2)
  })

  it('没有筛选条件时，模型排行与模型候选共用一次请求', async () => {
    vi.mocked(usageAPI.getDashboardTrend).mockResolvedValue(trendOf([]) as any)
    await mountChart()
    expect(usageAPI.getDashboardModels).toHaveBeenCalledTimes(1)
    expect(vi.mocked(usageAPI.getDashboardModels).mock.calls[0][0]).not.toHaveProperty('group_id')
  })

  it('工具栏刷新按钮通知页面刷新，刷新中禁用', async () => {
    vi.mocked(usageAPI.getDashboardTrend).mockResolvedValue(trendOf([]) as any)
    const wrapper = await mountChart()
    const button = wrapper.get('[data-testid="usage-refresh"]')
    await button.trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)
    await wrapper.setProps({ refreshing: true })
    expect(button.attributes('disabled')).toBeDefined()
  })

  it('Token 画成四条不堆叠的折线，未结束的末段画虚线，并挂上竖线和描线插件', async () => {
    const today = formatDayKey(new Date())
    vi.mocked(usageAPI.getDashboardTrend).mockResolvedValue(
      trendOf([{ date: today, requests: 3, actual_cost: 1 }]) as any,
    )
    const wrapper = await mountChart()
    const line = wrapper.findComponent(Line)

    const datasets = line.props('data').datasets
    expect(datasets.map((item: { label: string }) => item.label)).toEqual([
      'dashboard.usageChart.series.input',
      'dashboard.usageChart.series.output',
      'dashboard.usageChart.series.cacheCreation',
      'dashboard.usageChart.series.cacheRead',
    ])
    expect(datasets[0].cubicInterpolationMode).toBe('monotone')
    // 线下不填充，纵轴不堆叠
    expect(datasets.every((item: { fill: boolean }) => item.fill === false)).toBe(true)
    expect(line.props('options').scales.y.stacked).toBeUndefined()

    // 默认 7 天范围包含今天，最后一段是虚线，之前的段是实线
    const lastIndex = line.props('data').labels.length - 1
    expect(datasets[0].segment.borderDash({ p1DataIndex: lastIndex })).toEqual([4, 4])
    expect(datasets[0].segment.borderDash({ p1DataIndex: lastIndex - 1 })).toBeUndefined()

    expect(line.props('plugins').map((plugin: { id: string }) => plugin.id)).toEqual(['usageCrosshair', 'usageReveal'])
  })

  it('切换指标时重建图表，与首次打开播放同一套入场动画', async () => {
    const today = formatDayKey(new Date())
    vi.mocked(usageAPI.getDashboardTrend).mockResolvedValue(trendOf([{ date: today, requests: 3 }]) as any)
    const wrapper = await mountChart()
    const before = wrapper.findComponent(Line).vm

    await wrapper.get('[data-testid="usage-metric-requests"]').trigger('click')
    expect(wrapper.findComponent(Line).vm).not.toBe(before)
  })

  it('单指标叠加上一周期虚线，可以从图例关闭', async () => {
    const today = formatDayKey(new Date())
    vi.mocked(usageAPI.getDashboardTrend)
      .mockResolvedValueOnce(trendOf([{ date: today, requests: 30 }]) as any)
      .mockResolvedValueOnce(trendOf([{ date: '2000-01-01', requests: 20 }]) as any)
    const wrapper = await mountChart()
    await wrapper.get('[data-testid="usage-metric-requests"]').trigger('click')

    const datasets = () => wrapper.findComponent(Line).props('data').datasets
    expect(datasets()).toHaveLength(2)
    expect(datasets()[1]).toMatchObject({ previous: true, borderDash: [4, 4], order: 1 })
    // 上一周期只取本期已有的时段数
    expect(datasets()[1].data).toHaveLength(datasets()[0].data.length)

    await wrapper.get('[data-testid="usage-series-previous"]').trigger('click')
    expect(datasets()).toHaveLength(1)
  })

  it('模型排行跟随当前指标排序，点击后按模型筛选，再点一次取消', async () => {
    vi.mocked(usageAPI.getDashboardModels).mockResolvedValue({
      models: [
        { model: 'gpt-4o', requests: 5, total_tokens: 900, actual_cost: 1, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, cost: 1 },
        { model: 'claude-opus', requests: 10, total_tokens: 100, actual_cost: 3, input_tokens: 0, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, cost: 3 },
      ],
      start_date: '',
      end_date: '',
    })
    vi.mocked(usageAPI.getDashboardTrend).mockResolvedValue(trendOf([]) as any)
    const wrapper = await mountChart()

    const order = () => wrapper.findAll('[data-testid^="top-model-"]').map((row) => row.attributes('data-testid'))
    // 默认指标为 Token
    expect(order()).toEqual(['top-model-gpt-4o', 'top-model-claude-opus'])
    await wrapper.get('[data-testid="usage-metric-requests"]').trigger('click')
    expect(order()).toEqual(['top-model-claude-opus', 'top-model-gpt-4o'])

    vi.mocked(usageAPI.getDashboardTrend).mockClear()
    await wrapper.get('[data-testid="top-model-claude-opus"]').trigger('click')
    await flushPromises()
    expect(vi.mocked(usageAPI.getDashboardTrend).mock.calls[0][0]).toMatchObject({ model: 'claude-opus' })
    expect(wrapper.get('[data-testid="top-model-claude-opus"]').attributes('aria-pressed')).toBe('true')

    await wrapper.get('[data-testid="top-model-claude-opus"]').trigger('click')
    await flushPromises()
    expect(vi.mocked(usageAPI.getDashboardTrend).mock.calls.at(-1)?.[0]).not.toHaveProperty('model')
  })
})

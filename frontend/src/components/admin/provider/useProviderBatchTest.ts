import { computed, ref } from 'vue'
import {
  createProviderTestRun,
  executeProviderTest,
  type ProviderTestRequestBody,
  type ProviderTestRun
} from './providerTestRun'

/** 每批测试的模型数量上限。 */
export const MAX_BATCH_MODELS = 50
/** 可选的并发数，数值越大越容易触发上游限流。 */
export const BATCH_CONCURRENCY_OPTIONS = [1, 2, 3] as const

export type ProviderBatchRowState = 'queued' | 'running' | 'success' | 'failed' | 'stopped'

export interface ProviderBatchRow {
  model: string
  state: ProviderBatchRowState
  run: ProviderTestRun
}

type Translate = (key: string, params?: Record<string, unknown>) => string

/**
 * 批量模型测试的状态与调度：按并发数依次取模型执行连接测试。
 * 停止后暂停后续模型的测试，已发出的请求继续等待结果。关闭弹窗时中止全部请求。
 */
export function useProviderBatchTest(t: Translate) {
  const baseModels = ref<string[]>([])
  const customModels = ref<string[]>([])
  const selected = ref(new Set<string>())
  const rows = ref<Record<string, ProviderBatchRow>>({})
  const running = ref(false)
  const stopping = ref(false)
  const concurrency = ref(2)
  const detailModel = ref<string | null>(null)
  let stopRequested = false
  let controller: AbortController | null = null

  const models = computed(() => [...new Set([...baseModels.value, ...customModels.value])])
  const entries = computed(() => Object.values(rows.value))
  const doneCount = computed(() => entries.value.filter((row) => row.state === 'success' || row.state === 'failed').length)
  const successCount = computed(() => entries.value.filter((row) => row.state === 'success').length)
  const failedModels = computed(() => entries.value.filter((row) => row.state === 'failed').map((row) => row.model))
  const stoppedCount = computed(() => entries.value.filter((row) => row.state === 'stopped').length)
  const progress = computed(() =>
    entries.value.length ? (100 * (doneCount.value + stoppedCount.value)) / entries.value.length : 0
  )
  const detailRow = computed(() => (detailModel.value ? rows.value[detailModel.value] ?? null : null))

  // 打开弹窗或切换提供商后，由管理员选择本次测试的型号。
  function reset(nextModels: string[]) {
    abort()
    baseModels.value = nextModels
    customModels.value = []
    selected.value = new Set()
    rows.value = {}
    detailModel.value = null
  }

  function toggle(model: string, checked: boolean) {
    const next = new Set(selected.value)
    if (checked && next.size < MAX_BATCH_MODELS) next.add(model)
    if (!checked) next.delete(model)
    selected.value = next
  }

  function setMany(targets: string[], checked: boolean) {
    const next = new Set(selected.value)
    for (const model of targets) {
      if (!checked) next.delete(model)
      else if (next.size < MAX_BATCH_MODELS) next.add(model)
    }
    selected.value = next
  }

  function clearSelection() {
    selected.value = new Set()
  }

  // 列表里没有的模型 ID 作为自定义项加入并选中。
  function addModel(model: string) {
    if (!model || models.value.includes(model)) return
    customModels.value = [...customModels.value, model]
    toggle(model, true)
  }

  async function start(options: {
    targets: string[]
    retry: boolean
    providerId: number
    providerName: string
    buildBody: (model: string) => ProviderTestRequestBody
  }) {
    if (running.value || options.targets.length === 0) return
    stopRequested = false
    stopping.value = false
    running.value = true
    detailModel.value = null
    controller = new AbortController()
    const signal = controller.signal

    const queued: Record<string, ProviderBatchRow> = {}
    for (const model of options.targets) {
      queued[model] = { model, state: 'queued', run: createProviderTestRun() }
    }
    rows.value = options.retry ? { ...rows.value, ...queued } : queued

    const targets = [...options.targets]
    let index = 0
    const worker = async () => {
      while (!stopRequested && index < targets.length) {
        const row = rows.value[targets[index++]]
        row.state = 'running'
        const finished = await executeProviderTest({
          providerId: options.providerId,
          providerName: options.providerName,
          body: options.buildBody(row.model),
          run: row.run,
          signal,
          t
        })
        if (!finished) {
          row.state = 'stopped'
        } else {
          row.state = row.run.status === 'success' ? 'success' : 'failed'
        }
      }
    }

    try {
      await Promise.all(Array.from({ length: Math.min(concurrency.value, targets.length) }, worker))
      // 停止后尚未开始的模型标记为未执行。
      for (; index < targets.length; index++) {
        rows.value[targets[index]].state = 'stopped'
      }
    } finally {
      running.value = false
      stopping.value = false
      controller = null
    }
  }

  // showDetail 传 null 时回到模型列表。
  function showDetail(model: string | null) {
    detailModel.value = model
  }

  function stop() {
    if (!running.value) return
    stopRequested = true
    stopping.value = true
  }

  // abort 立即中止所有进行中的请求，用于关闭弹窗。
  function abort() {
    stopRequested = true
    controller?.abort()
  }

  return {
    models,
    selected,
    rows,
    running,
    stopping,
    concurrency,
    detailModel,
    detailRow,
    entries,
    doneCount,
    successCount,
    failedModels,
    stoppedCount,
    progress,
    reset,
    toggle,
    setMany,
    clearSelection,
    addModel,
    showDetail,
    start,
    stop,
    abort
  }
}

export type ProviderBatchTest = ReturnType<typeof useProviderBatchTest>

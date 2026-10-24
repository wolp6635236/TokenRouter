# 降智探测

本文记录 OpenAI 提供商的降智探测、连续失败后的临时停调和分组保底规则。调度评分和 502 处理见提供商维护与调度文档。

## 章节导航

- [探测范围](#探测范围)：修改覆盖平台、模型和开关时读取。
- [判定](#判定)：修改糖果题或 ModelTrace 通过条件时读取。
- [连续失败](#连续失败)：修改 5 分钟复测、三次上限和邮件时读取。
- [分组保底](#分组保底)：修改“最后一台不停调”时读取。
- [代码位置](#代码位置)：修改模块归属、装配或调度接入方式时读取。

<a id="quality_probe_scope"></a>
## 探测范围

运行时设置键是 `quality_probe_settings`。总开关默认关闭。开启后只探测 `platform=openai` 的提供商，包括 OAuth 和 API Key。定时探测的账号还要满足：`status=active`、打开了「参与调度」，至少属于一个启用中的分组。`group_ids` 限制自动探测覆盖哪些分组，多选；空数组表示全部启用中的 OpenAI 分组。禁用分组不进入定时探测。设置里的 `models` 是多选列表，候选值来自这些启用分组的请求模型并去重；空分组时取全部启用分组的模型。每个账号只实际探测自己可用模型里的候选，缺少的模型跳过且不写 history。提供商菜单的手动探测仍针对任意 OpenAI 账号。默认周期 30 分钟，可改；默认模型列表只有 `gpt-6-astra`。旧的单值 `model` 设置继续读取为一个模型。

`schedule_enabled` 默认开启，配合 `schedule_start` / `schedule_end`（本地时区 `HH:MM`，默认 `08:00`–`00:00`）限制自动探测时段；窗口是左闭右开，起点大于终点时跨午夜。总开关是管理员保存的意图，进出时段只改变自动探测是否运行。时段外自动探测直接返回 `skip_reason=schedule`，history 保持原样；手动探测仍只检查总开关。关掉 `schedule_enabled` 后，只要总开关打开就全天自动探测。

`unschedule_on_degraded` 默认开启。开启时降智会写入 `quality_degraded` 临时停调（分组保底仍生效）。关闭后仍判定降智、累计次数、发邮件和停自动循环，账号保持可调度；保存为关闭时立即清掉 OpenAI 账号上已有的 `quality_degraded` 临时停调，其它原因的临时停调继续由原有恢复路径处理。

探测走提供商测试通道，费用记在上游账号上。循环状态和最近 50 条记录写在提供商 Extra 的 `quality_probe` 键，`history` 同时收录自动探测和手动探测，每条带上测号的提问和截断后的回答。后台每分钟扫描到期且命中上述过滤的提供商，最多同时探测 4 台；每台通常选 2 到 3 个模型并行测号，单个模型内部依次执行糖果题和 ModelTrace。所有实际探测模型完成后再一次性更新该账号的连续失败、临时停调和邮件状态。手动多选仍按模型并行请求，并分别写记录。通过后的下次自动探测按设置的周期（默认 30 分钟），失败后按冷却（默认 5 分钟）。

管理接口：`GET`/`PUT /api/v1/admin/quality-probe/settings`，`GET /api/v1/admin/quality-probe/models`，`GET /api/v1/admin/quality-probe/logs`，`GET`/`POST /api/v1/admin/providers/:id/quality-probe`。模型接口接收重复的 `group_ids` 查询参数，空值覆盖全部启用分组。管理侧栏「降智探测」按时间分页列出记录，展示账号所属分组并可打开该轮问答。提供商列表在分组列后读 `extra.quality_probe` 显示最近一轮通过、降智、上游报错或跳过。管理员在提供商菜单悬停「降智探测」时列出该账号可用模型，每行一个复选框，打开时全部勾选。点「开始探测」后，勾选的模型各发一次 POST，结果对话框按模型列出进度。自动循环停止后，菜单里的手动探测仍会执行。POST 可带 `model`；没带时用设置里的第一个模型。POST 会连续打两次测试通道，前端超时 180 秒；关掉弹窗或浏览器取消请求后，服务端仍把这一轮跑完并写入 Extra。总开关关闭时立即返回 `skip_reason=disabled`。账号未开启时自动探测返回 `skip_reason=account`。自动探测未命中启用中的所选分组时返回 `skip_reason=group`。设置模型不在该账号可用模型里时跳过该模型；所有候选都缺失时账号返回 `skip_reason=model`。时段外自动探测返回 `skip_reason=schedule`。history 保存实际跑完的探测，以及手动触发且需要展示的跳过（例如总开关关闭）。

<a id="quality_probe_verdict"></a>
## 判定

一轮探测包含糖果题和 ModelTrace。糖果题原文与 CPA 插件相同，回答里出现独立的 `21` 算通过。ModelTrace 发一道数值选择题，把有效回答送进内置的 [ModelTrace](https://github.com/xqy2006/ModelTrace) `unified_bank.json` 做归因；最可能模型与本次请求的模型为同一条算通过。请求名带日期后缀时（例如 `gpt-6-astra-2026-10-01`）与指纹库中的 `gpt-6-astra` 视为同一条。请求 `gpt-6-astra`、归因 `gpt-6-luna` 时 ModelTrace 未通过。回答里 1 到 355 的整数少于 80 个，或不足题目数量的 55%，这次不进入归因，ModelTrace 未通过。测号通道返回错误时（例如 403 余额不足、超时）记为上游报错；失败计数、临时停调和自动循环保持原样，下次按通过后的间隔再测。单个模型在糖果题和 ModelTrace 都未通过时记为降智。一次自动多模型探测只有全部实际探测模型都降智时，才把账号记为降智并触发连续失败、临时停调和邮件；任一模型通过会清掉账号降智状态。

<a id="quality_probe_cycle"></a>
## 连续失败

连续失败次数记在提供商探测状态上。`unschedule_on_degraded` 开启时，每次降智后临时停调一段冷却（默认 5 分钟），到期再测；关闭时跳过临时停调，冷却只决定下次自动再测的时间。第 3 次仍失败时发邮件到设置里的收件人（默认 `295783453@qq.com`），并停止自动循环。管理员手动探测在停循环之后仍可执行；本轮通过则清临时停调、清失败计数，自动循环重新打开。邮件只在失败次数从 2 变成 3 时发一次。

<a id="quality_probe_keep_one"></a>
## 分组保底

`unschedule_on_degraded` 开启且准备停调前，检查该提供商所在的全部分组。只要有一个分组里它已经是仍可调度的最后一个成员，就保持可调度，仍累计失败次数和发邮件。可调度指 active、schedulable、未到期、且没有其他原因的临时停调。

<a id="quality_probe_placement"></a>
## 代码位置

核心在 `backend/internal/qualityprobe`。管理 HTTP 在 `qualityprobe/httpapi`。app 把提供商存储、测号服务和邮件适配进去，并登记每分钟扫描的 runner。调度筛选继续读提供商上的临时停调字段。引用本模块的是 app 适配器、`qualityprobe/httpapi`，以及管理前端列表徽章。Agent 改这项能力时的约束见仓库根目录 `AGENTS.md`。

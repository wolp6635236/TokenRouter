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

运行时设置键是 `quality_probe_settings`。总开关默认关闭。开启后只探测 `platform=openai` 的提供商，包括 OAuth 和 API Key。定时探测的账号还要满足：`status=active`、打开了「参与调度」，并且至少属于一个启用中的分组。`group_ids` 再限制自动探测覆盖哪些分组，多选；空数组表示全部启用中的 OpenAI 分组。禁用分组不进入定时探测。提供商菜单的手动探测仍针对任意 OpenAI 账号。默认周期 30 分钟，可改。默认模型是 `gpt-6-astra`；配置为 `latest` 时，从管理测试用的模型目录里取字典序最大的 Astra 名称，目录没有 Astra 时仍用 `gpt-6-astra`。

探测走提供商测试通道，费用记在上游账号上。循环状态和最近 50 条记录写在提供商 Extra 的 `quality_probe` 键，`history` 同时收录自动探测和手动探测，每条带上四次测号的提问和截断后的回答。后台每分钟扫描到期且命中上述过滤的提供商；通过后的下次自动探测按设置的周期（默认 30 分钟），失败后按冷却（默认 5 分钟）。

管理接口：`GET`/`PUT /api/v1/admin/quality-probe/settings`，`GET /api/v1/admin/quality-probe/logs`，`GET`/`POST /api/v1/admin/providers/:id/quality-probe`。管理侧栏「降智探测」列出汇总记录，可打开该轮问答。管理员在提供商菜单点「降智探测」后弹出结果对话框。自动循环停止后，菜单里的手动探测仍会执行。POST 会连续打四次测试通道，前端超时 180 秒。总开关关闭时立即返回 `skip_reason=disabled`。账号未开启时自动探测返回 `skip_reason=account`。自动探测未命中启用中的所选分组时返回 `skip_reason=group`。history 保存实际跑完的探测，以及手动触发且需要展示的跳过（例如总开关关闭）。

<a id="quality_probe_verdict"></a>
## 判定

一轮探测包含糖果题和 ModelTrace。糖果题原文与 CPA 插件相同，回答里出现独立的 `21` 算通过。ModelTrace 发三道数值选择题，每道解析出的整数达到题目数量的七成算该道通过，三道都通过才算 ModelTrace 通过。两样都未通过才记为降智。

<a id="quality_probe_cycle"></a>
## 连续失败

连续失败次数记在提供商探测状态上。每次降智后临时停调 5 分钟，到期再测。第 3 次仍失败时发邮件到设置里的收件人（默认 `295783453@qq.com`），并停止自动循环。管理员手动探测在停循环之后仍可执行；本轮通过则清临时停调、清失败计数，自动循环重新打开。邮件只在失败次数从 2 变成 3 时发一次。

<a id="quality_probe_keep_one"></a>
## 分组保底

停调前检查该提供商所在的全部分组。只要有一个分组里它已经是仍可调度的最后一个成员，就保持可调度，仍累计失败次数和发邮件。可调度指 active、schedulable、未到期、且没有其他原因的临时停调。

<a id="quality_probe_placement"></a>
## 代码位置

核心在 `backend/internal/qualityprobe`。管理 HTTP 在 `qualityprobe/httpapi`。app 把提供商存储、测号服务和邮件适配进去，并登记每分钟扫描的 runner。调度筛选继续读提供商上的临时停调字段。引用本模块的是 app 适配器和 `qualityprobe/httpapi`。Agent 改这项能力时的约束见仓库根目录 `AGENTS.md`。

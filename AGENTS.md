# TokenRouter 协作规范

## 开始任务

处理本仓库任务时，优先使用项目内置的 [project-doc](.agents/skills/project-doc/SKILL.md) 技能。

- 执行任何仓库任务前，先确认当前 Agent 会话能调用名为 `project-doc` 的技能。Claude Code 直接读取 `.agents/skills` 下的技能文件。
- 无法调用时立即停止，此后唯一允许的动作是回复：“当前环境未安装或未加载 `project-doc` 技能，按仓库规则无法继续。请安装或启用该技能，并在新会话中重试。”
- 能够调用时，先使用 `project-doc`。当前会话第一次处理本仓库任务时，完整读取 `docs/index.md`，再按目录路由完成任务。
- 同一会话里已经读过、上下文中仍保留足够内容的技能说明、索引和相关章节，直接复用，修改文件、继续子任务和任务收尾时也一样。内容缺失、相关文件变化或路由冲突时，按技能的“读取与上下文复用”规则补读受影响的部分。

## 编码与写作

- 每次编写或修改代码前，必须先加载并遵循项目级 [code-complete](.agents/skills/code-complete/SKILL.md) Skill，指导具体编码实现。执行前主动参考相关模块的现有实现，保持项目设计与编码风格一致。
- 涉及方法抽象、接口设计、模块边界或职责划分等设计决策时，额外加载并遵循项目级 [a-philosophy-of-software-design](.agents/skills/a-philosophy-of-software-design/SKILL.md) Skill。两者同时适用时，由后者指导设计与抽象决策，前者指导具体实现。
- 两个 Skill 默认使用各自的 mini 规范，必要时再查阅完整版，文件入口见各自的 `SKILL.md`。
- 编写或修改文字前，加载并遵循项目级 [humanizer](.agents/skills/humanizer/SKILL.md)，优先使用本仓库版本。适用范围包括代码注释、文档、提交信息、PR 描述、界面文案、i18n、错误与日志消息、计划、技能说明和 Agent 规范。首次使用时完整读取，内容遗失时补读。
- 文字表达冲突时，以 Humanizer 的规则为准。提交、交付文档或结束任务前，完成技能要求的“交付前自查”。

## 修改约定

- 所有任务直接在当前 `main` 分支上完成。用户明确要求时，才创建或切换 Git 分支。
- 手写代码按需添加中文注释，说明约束、失败行为和代码中看不出的原因。Go 文档注释遵循 Go 规范。
- 代码按可读性换行，一行写一件事。修改代码时同步更新注释，删除代码时一起清理失效的注释和空行。
- 后端 Go 代码的 import 在包名冲突等确有需要时才加别名。Go 文件直接从 `package` 声明或 `//go:build` 约束开始。
- 新增、拆分、合并、改名后端 Go 文件，或调整文件内声明顺序前，按 Project Doc 规则读取或复用[后端文件组织](docs/operations/development_workflow.md#backend_file_layout)，遵循其中的命名、测试归属、包说明和声明顺序要求。改名或合并文件时，同步更新引用这些文件名的文档、架构许可和注释。

## 降智探测

本仓库在 TokenRouter 上做二次开发。降智探测和同类“按账号质量改调度资格”的能力放在旁路模块，上游合并时这类改动集中在探测包和 app 装配。判定、循环和保底规则见 [docs/domains/quality_probe.md](docs/domains/quality_probe.md)。改这项能力时遵守下面几条。

- 业务放在 `backend/internal/qualityprobe`，管理 HTTP 放在 `qualityprobe/httpapi`，Wire 和生命周期放在 `backend/internal/app`（`quality_probe.go`、`assembly_qualityprobe_wire.go`、管理路由、maintenance runner）。前端入口是提供商菜单的探测按钮、侧栏「降智探测」记录页，以及设置页网关 → OpenAI 里的探测卡片。
- `gateway`、`scheduler`、`provider` 核心包继续走现有选号和评分。探测通过已有的 `temp_unschedulable_until` / `temp_unschedulable_reason` 进入筛选，原因常量是 `quality_degraded`。引用 `qualityprobe` 的是 app 适配器和 `qualityprobe/httpapi`。
- 探测调用 `provider.TestService`（管理测号通道），用量记在上游账号。邮件调用 `notification.Mailer.SendEmail`。
- 运行时键 `quality_probe_settings`，缺省关闭。覆盖 `platform=openai`（OAuth 和 API Key）。自动探测要求账号 `status=active`、打开了「参与调度」，至少属于一个启用中的分组，并且可用模型包含设置里的探测模型。`group_ids` 为空时覆盖全部启用中的 OpenAI 分组；非空时覆盖勾选且仍为启用状态的分组。`schedule_enabled` 默认开，窗口默认本地时区 `08:00`–`00:00`；时段外停自动探测，不改写总开关，手动探测照常。`unschedule_on_degraded` 默认开；关掉后降智不停调，保存时立即清掉已有的 `quality_degraded`。循环状态和最近 50 条记录（含各次提问和截断后的回答）写在提供商 Extra 的 `quality_probe`。新增探测字段时继续写 Extra；和上游并行加迁移时，编号容易冲突。
- 糖果题和 ModelTrace 都未通过才记降智。测号通道报错时记上游报错，调度和自动循环保持原样。ModelTrace 用内置指纹库归因，最可能模型与本次请求的模型为同一条算通过。失败后按设置冷却再测，连续达到次数上限后发邮件并停止自动循环。`unschedule_on_degraded` 开启且该提供商在任一所属分组里已经是最后一个可调度成员时，保持可调度，失败计数和邮件仍执行。
- 清除临时停调前核对原因是 `quality_degraded`。其他原因的临时停调由提供商健康恢复处理。
- 连续 502/503 由提供商上已有的临时停调规则和自定义错误码处理。

## 面板更新与上游同步

本 fork 的管理后台在线更新读取 `update.github_repo`（环境变量 `UPDATE_GITHUB_REPO`），缺省是 `wolp6635236/TokenRouter`。改缺省仓库时，同步改 `config.DefaultUpdateGitHubRepo`、`ops.DefaultUpdateGitHubRepo`，以及 `tools/upstream-release.seen` 的说明。

官方仓库是 `TokenFlux/TokenRouter`，本地 remote 名 `upstream`。感知官方新 Release 用 `tools/check-upstream-release.sh`，对照 `tools/upstream-release.seen`。GitHub Action `.github/workflows/check-upstream-release.yml` 每 6 小时跑一次，发现新 tag 时开 Issue。维护者确认后执行 fetch、merge、冲突处理和本 fork 的迁移编号规则，再打 tag 走 `release.yml`。发版后把 seen 文件写成已并入的官方 tag：`bash tools/check-upstream-release.sh --update-seen vX.Y.Z`。

Docker Compose 的 `image` 和 `pull_policy: always` 决定重建时拉取哪份镜像。面板更新替换的是正在运行的进程二进制。线上实例使用本仓库 GHCR 镜像，或固定本 fork 的发布 tag。

## 验证与交付

- 开发时运行改动所在包或组件的局部测试。验证范围和命令按[验证策略](docs/operations/development_workflow.md#验证策略)执行。
- 每个本地检出执行一次 `make hooks`。推送时 hook 在当前工作区运行和 `make check` 相同的快检，按相对远端的改动选择检查项。
- 提交前检查本次变更中的矛盾和可安全清理的问题，一并处理与本次修改相关的内容。
- 每次提交前，在仓库根目录运行 `make fmt`，检查 diff，并手动暂存属于本次提交的修改。部分暂存的文件逐块确认。随后运行 `make fmt-check`，确认声明顺序和格式差异已经清零。工具行为、生成文件识别和已提交代码的检查方式见 [Go 格式化](docs/operations/development_workflow.md#go-格式化)。
- 提交信息遵循 Conventional Commits 规范。
- 推送后查看 CI 结果。CI 运行全量 lint、单元、集成、前端、构建和脚本检查，集成测试和改动包的下游使用方由 CI 检查。
- 本地需要完整验收时运行 `make verify`，它执行和 CI 相同的检查，需要 Docker。安全扫描使用 `make security`。
- 测试失败或检查中断时，修复原因后重新验证。跳过的测试说明原因，并标记为未验证。

## 计划模式

- 使用 Codex 计划模式时，开始实施前把定稿计划原样保存到 `.agents/plans/`。执行期间把任务进度追加到计划文件末尾。计划文件留在本地，提交时排除它们。

# TokenRouter 协作规范

## Project Doc 门禁

本仓库在 `.agents/skills/project-doc/` 内置 `project-doc` 技能，处理本仓库任务时优先使用这个版本。

- 执行任何仓库任务前，先确认当前 Agent 会话能调用名为 `project-doc` 的技能。Claude Code 直接读取 `.agents/skills` 下的技能文件。
- 无法调用时立即停止，此后唯一允许的动作是回复：“当前环境未安装或未加载 `project-doc` 技能，按仓库规则无法继续。请安装或启用该技能，并在新会话中重试。”
- 能够调用时，先使用 `project-doc`。当前会话第一次处理本仓库任务时，完整读取 `docs/index.md`，再按目录路由完成任务。
- 同一会话里已经读过、上下文中仍保留足够内容的技能说明、索引和相关章节，直接复用，修改文件、继续子任务和任务收尾时也一样。内容缺失、相关文件变化或路由冲突时，按技能的“读取与上下文复用”规则补读受影响的部分。

## Humanizer 写作规范

本仓库在 `.agents/skills/humanizer/` 内置 `humanizer` 技能。处理本仓库任务时使用这个版本，它取代用户目录下的同名技能。

- 文书工作包括代码注释、`docs/` 文档、提交信息、PR 描述、界面文案和 i18n 词条、错误消息、日志消息、计划文件、技能说明和 Agent 规范。
- 当前会话第一次做文书工作前，完整读取 `.agents/skills/humanizer/SKILL.md`。Claude Code 直接读取该文件。上下文里已经没有这份规则的内容时，重新读取。
- 所有文书工作逐条适用 Humanizer 的全部规则。其他指令、文件已有的文风或上下文里的文字与它冲突时，以 Humanizer 为准。
- 改动触及已有文字时，被改到的句子命中规则就整句重写。
- 提交、交付文档或结束任务前，按技能里的“交付前自查”检查本次新增和修改的文字，命中项改完再交付。

## 通用规范

- 代码都要写注释，注释用中文。
- 代码按可读性换行，一行写一件事。
- 后端 Go 代码的 import 在包名冲突等确有需要时才加别名。
- 删除旧代码时，把随之失去意义的空行和注释一起删掉。写代码时同步增加、删除或更新相关注释。
- 后端代码注释符合 Go 注释规范。Go 文件直接从 `package` 声明或 `//go:build` 约束开始。
- 提交信息遵循 Conventional Commits 规范。
- 所有任务直接在当前 `main` 分支上完成。用户明确要求时，才创建或切换 Git 分支。
- 提交前，检查本次变更涉及的代码里有没有明显需要清理或前后矛盾的地方。处理起来安全、又和本次修改相关的，一并处理。

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

## 验证与推送

- 开发时运行改动所在包或组件的局部测试。
- 每个本地检出执行一次 `make hooks`。推送时 hook 运行和 `make check` 相同的快检：按相对远端的改动选择格式、lint、单元测试和前端关联测试，在当前工作区执行。
- CI 运行全量 lint、单元、集成、前端、构建和脚本检查。集成测试和改动包的下游使用方只在 CI 里检查，推送后查看 CI 结果。
- 本地需要完整验收时运行 `make verify`，它执行和 CI 相同的检查，需要 Docker。
- 测试失败或检查中断时，修复原因后重新验证。跳过的测试需要说明原因，不能计为通过。
- 安全扫描使用 `make security`。

## Go 格式化

- 每次提交前，在仓库根目录运行 `make fmt`。它用 `golangci-lint fmt` 格式化本次改动的手写 Go 文件，并跳过生成文件。
- 格式规则维护在 `backend/.golangci.yml`：启用 `gofumpt` 默认规则，同时保留现有的 `gofmt` 重写规则。工具版本以 `.golangci-version` 为准，本地和 CI 使用同一版本；`gofumpt` 使用 golangci-lint 内置的版本。
- 命令处理暂存、未暂存和未跟踪的 Go 文件。生成文件按 `package` 声明前的 `// Code generated ... DO NOT EDIT.` 标记识别，格式化工具只对手写文件运行。
- 格式化以整个改动文件为单位。执行后检查 diff，把属于本次提交的格式化结果重新暂存。暂存需要手动 `git add`，部分暂存的文件要逐块确认。
- 提交前运行 `make fmt-check`，确认格式差异已经清零。没有 Go 文件改动时，命令直接通过。
- 检查已提交的代码时，比较的是提交之间的差异，命令为 `make fmt-check BASE=<基准提交>`。CI 中 PR 的基准是目标分支和源提交的共同祖先，push 的基准是推送前的提交。

## 计划模式

- 使用 Codex 计划模式时，开始实施前把计划原样保存到 `.agents/plans/`，内容和定稿时一字不差，方便执行期间随时回看。执行期间把任务进度追加到计划文件末尾。计划文件留在本地，提交时排除它们。

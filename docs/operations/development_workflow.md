# 开发、验证与上游同步

本文记录 TokenRouter 当前的工具链、代码生成、依赖规则、测试分层、发布和 fork 同步流程。具体的工具版本以 manifest 和 CI 为准；本地的凭据和临时的故障记录不写进本文。

## 章节导航

- [工具链与本地运行](#工具链与本地运行)：准备环境或更新依赖时读取。
- [依赖规则](#backend_dependency_rules)：新增后端模块、调整 import 或文件许可时读取。
- [编码约定](#编码约定)：写注释、修改前端时读取。
- [生成代码与迁移](#生成代码与迁移)：修改 Ent schema、Wire 或数据库时读取。
- [验证策略](#验证策略)：实现完成、提交之前读取。
- [提交与文档](#提交与文档)：形成提交或维护 Project Doc 时读取。
- [同步上游](#同步上游)：引入 upstream 的 PR 或 commit 时读取。
- [发布](#发布)：创建版本 tag 之前读取。

## 工具链与本地运行

| 工具 | 版本来源 | 当前要求 |
| --- | --- | --- |
| Go | `backend/go.mod`、CI | `1.27.0` |
| Node.js | `.github/workflows/backend-ci.yml` | `20` |
| pnpm | CI 和根 Makefile | `9`；根命令默认使用 `npx --yes pnpm@9` |
| golangci-lint | `.golangci-version` | 本地和 CI 使用同一个完整版本，配置在 `backend/.golangci.yml` |
| gofumpt | golangci-lint 内置 | 使用默认规则，不开启 extra，不单独维护版本 |
| arch-go | `tools/architecture/go.mod` | `v2.1.2`；通过 Go API 使用，由独立的工具模块运行 |
| PostgreSQL、Redis | Compose 和集成测试 | 生产必需；测试可以由 Testcontainers 或 Compose 提供 |

本地安装和 CI 相同版本的 lint，以免规则集不同，导致问题只在 CI 上出现：

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@"$(cat .golangci-version)"
```

在仓库根目录执行安装命令，并把 Go 安装目录里的二进制加入 PATH。`tools/golangci-lint.sh` 会验证实际的版本，版本不符的本地工具会被拒绝；CI 的 action 从同一个版本文件读取要安装的版本。

升级 Go 时，同时修改 `backend/go.mod`，以及 `backend-ci.yml`（两处）、`release.yml`（两处）和 `security-scan.yml` 里对 `go version` 的硬断言。workflow 都通过 `go-version-file: backend/go.mod` 安装工具链，漏改任何一处断言，版本校验步骤都会失败。

个人的数据库路径、固定的密码，或者某台机器的服务配置，不写进工程文档。开发配置使用不提交的环境文件或 `backend/config.yaml`；可以提交的样例在 `deploy/`。前端开发服务器默认通过 `VITE_DEV_PROXY_TARGET` 代理到后端，端口由 `VITE_DEV_PORT` 控制。

常用入口：

```bash
# 后端
cd backend
go run ./cmd/server

# 前端
cd frontend
pnpm install --frozen-lockfile
pnpm run dev

# 完整源码 Compose
docker compose -f deploy/docker-compose.dev.yml up --build
```

根 Makefile 里保留了 `build-datamanagementd` 和 `test-datamanagementd` 目标，但仓库里没有 `datamanagement/` 源码树；除非工作范围里明确提供了这部分可选源码，这两个目标不算在默认的通过条件里。

<a id="backend_dependency_rules"></a>
## 依赖规则

HTTP、用例、存储和后台资源，由 app 装配各模块的实现。业务测试放在实际负责的模块里，跨模块的接口测试放在 `tests/integration`。包的职责和依赖方向见[后端模块地图](../architecture/backend_modules.md)。通用的技术实现在 `internal/infra`，HTTP 工具在 `server/httpx` 和 `server/clientip`，纯工具在明确列出的 pkg 包里。

### 架构检查

架构检查在 `tools/architecture/`，和后端应用的依赖隔离：

- `catalog.go` 保存技术库、模块协作、纯叶子包、平台和必要的文件许可；`policy.go` 按目录角色复用约束；arch-go 执行依赖判断。
- 普通的辅助子包继承所属的角色。没有登记的顶层模块和平台，只能使用少量基础标准库，要声明业务或 I/O 依赖，需要先登记。新目录不能靠任意嵌套的 `postgres` 之类的名称，获得适配层的权限。
- 扫描器读取所有手写的 Go 文件，覆盖测试、构建标签、平台文件和 wireinject；生成代码不参与架构规则的判断。这是静态的 import 检查，所有构建组合实际编译、执行的结果，需要另外验证。

`make -C backend test-architecture` 用固定版本 arch-go 的 Go API 检查规则，并运行正反例、扫描覆盖和文件许可的使用方测试。工具要读取独立模块之外的源码，所以入口使用 `-count=1` 关闭测试缓存。后端的 `make test` 和 CI 的 lint job 都执行这个入口；CI 分别缓存后端和架构工具的 Go 依赖。修改角色和模块关系时，维护规则表，不另外引入生成配置的流程。golangci-lint 负责通用的代码质量检查，架构白名单不放在它里面。

### 通用规则

- 核心不依赖旧的业务实现，也不依赖框架和存储的实现；HTTP 适配层通过用例访问数据，不直接访问数据库。
- 具体的上游平台不依赖其他平台的实现；技术包不反过来读取完整的 config 或业务 service。旧的包路径始终禁止。
- app 是装配的位置，bootstrap 和 lifecycle 有各自独立的依赖范围。存储、任务队列和上游客户端在 app 的 wireinject 集合里绑定；旧的聚合包和 legacybridge 路径被全局规则拒绝。setup 只有实际的入口文件可以引用精简版 bootstrap；模块不能反向依赖 app。
- 综合设置新增字段时，在 app 的静态参与者里声明这个字段和键唯一的所有者，保持一次原子保存，提交后应用失败时明确标记为已持久化。HTTP 和 DTO 使用所属模块的能力，测试夹具只提供数据或 I/O 替身。
- protocol 和其他纯角色共用一份明确的标准库白名单，允许内存里的解析、编码、同步、测试，以及调用方提供的流接口。确实需要读取文件或构造 HTTP 输入的测试，按实际文件单独许可，其他纯文件不会继承。ipmatch 和 urlpolicy 只额外允许 `net` 的地址类型。`io`、`net` 和其他允许的包里，具体调用的副作用仍然需要代码审查。arch-go v2 把 `golang.org/x` 归为标准库，适配层要按实际的第三方库单独许可，不能随标准库一起放行。

### 文件级许可

保留路径上的旧依赖，按准确的源文件和 import 登记：许可不覆盖同目录的新文件，也不覆盖允许包的其他子包。每次添加路径或修改规则，分别用普通、unit、integration 三个集合，验证合法的依赖和违规的夹具；已有的失败不会自动变成白名单。

删除或迁移文件后，同步删除对应的文件许可，并检查源目录和目标目录的角色。文件许可的使用方测试，会拒绝已经没有实际 import 的过时条目。验证要覆盖：删除后恢复同名文件、旧文件新增禁止的 import、同目录的新文件和非法的子包；只检查迁出后的正向 lint 是不够的。迁出的文件按目标目录的角色规则检查，同目录新增的文件不会继承例外。

角色检查识别不了"通过接口绕过业务用例"，也识别不了"借着已有的 import 增加耦合"，这两项仍然需要代码审查。

### 各模块的依赖规则

- billing：缓存和提醒设置的读取，按核心角色和模块关系许可；app 负责组合动态设置、价格配置、通知、推广、提供商 outbox 和支付能力。公告和 billing 的用户读取直接使用 identity 的数据。PostgreSQL 和 Redis 适配层不互相继承存储客户端的许可；同连接的资金参与、跨存储的数据读取等窄权限，限定到具体的文件和目标包。
- identity、team、apikey：生产实例和同连接事务的参与工厂由 app 绑定。只剩测试在使用的私有转接，放进对应标签的 `_test.go`，生产代码里不保留算法副本。
- usage、audit、ops：各自使用自己的核心和适配层。用户、Key、团队的用量 SQL 参与函数复用调用方的连接，不能改成逐条查询或分页之后再排序。新的核心不导入旧实体、Gin 或具体的存储；纯包 `querycache`、`logevent` 和已经迁移的统计值有独立的职责规则。历史的构造、HTTP 上下文和测试适配的许可精确到文件和 import，普通的新文件不会继承。
- upstream：具体平台之间不能互相导入，也不接收旧的 Provider、Gin 或完整的 config。共享的 Google 认证原语在 `upstream/internal/googleauth`，纯报文和转换由 protocol 提供；提供商的授权会话和凭据持久化在 provider。app 整理参数，gateway 决定重试的时机。
- notification、site、moderation、search：核心、纯接口和适配层按职责匹配架构规则。邮件凭据留在 identity，阈值留在 billing，通知接收已经确定的事件；审核跨身份的事务使用同一个 SQL 连接；文件读取在 `site/filesystem`；搜索的 HTTP 和 Redis 分开。适配层不复制核心的状态或算法，文件移动时同步清理专用的许可和排除项。角色夹具覆盖新文件、精确的历史 import、非法子包和迁出后的同名文件。
- gateway：请求值和固定的 `Execute` 接口在 `gateway/execution`，这个叶子包不能反向导入根执行器、text 或适配层。构造 HTTP 时，app 先绑定完成记录器（只有一个），再构造执行器；请求调用只传入明确的状态和同步输出。内部的提供商循环、平台同提供商的恢复和协议转换各只有一个实现，不能再通过新增回调把旧的 handler 整个包起来。执行适配层只提供单步调用和数据整理。
- creative、batchimage：核心、HTTP、PostgreSQL、Redis 和平台适配层使用各自的角色门禁。任务资金引用的 scope 只在 app 注册，所属的存储参与者不开启、也不提交自己的事务。
- scheduler：核心、HTTP、Redis、PostgreSQL 分别使用角色门禁。评分、排序、会话和等待不接收旧实体、Gin 或存储客户端；需要的 ProviderSnapshot、RoutePlan、singleflight 和散列依赖，由核心角色和模块关系许可。`scheduler/rediscache/codec` 维护 `sched:v4` 的完整提供商和轻量数据。
- payment、promotion：依赖门禁覆盖新的核心、HTTP、PostgreSQL 和 app。payment 根包不直接依赖 Ent、config 或 Wire；billing 的值、套餐 HTTP 的复用和微信身份的辅助，分别按实际的文件和 import 许可，新文件和迁出的文件不继承许可。
- 纯规则包：协议哈希和 Gemini 迭代器使用纯角色的 `crypto/sha256` 和 `iter` 许可；clientmeta 的版本库按纯角色许可；app 的定价装配和目录 HTTP 的接口测试，按对应的角色和模块关系检查。pricing、capability、clientmeta 使用明确的标准库集合，不能增加文件或网络读取。
- 传输：传输测试随 `gateway/provider/transport` 运行，旧 repository 测试的许可不会随路径迁移被继承。

## 编码约定

所有手写代码都写必要的中文注释，生成文件只由生成命令更新。注释写约束、失败时的行为，或者代码里看不出来的原因。注释、文档、提交信息和界面文案按 `.agents/skills/humanizer/` 的规则书写。跨模块的不变量同步到 Project Doc，并在关键的手写入口添加唯一的 `@project-doc` 锚点。

前端使用 Vue 3、TypeScript、Pinia、Vue Router、Vue I18n 和项目自己的组件。修改界面时：

- 组件、样式和选择框的约定见[前端 UI 规范](../architecture/frontend_ui_conventions.md)。
- 用户可见的文案放进 `src/i18n/locales/`，中英文的 key 保持一致。
- API 的类型和调用放在 `src/api/`，跨页面的状态放进 store 或 composable，view 里不重复写协议。
- 修改依赖时同步更新 `frontend/pnpm-lock.yaml`，CI 使用 frozen lockfile。

支付页动态导入 Stripe 和 Airwallex SDK，`frontend/vite.config.ts` 将它们分别放进独立的 vendor 包。Airwallex 在模块加载时会预取远程支付脚本，因此它和同命名空间的依赖一起分包。调整分包规则后，检查生产构建的依赖关系，确认支付 SDK 由支付流程触发加载。

## 生成代码与迁移

`backend/ent/` 的大部分文件由 Ent 生成，`backend/internal/app/wire_gen.go` 由 Wire 生成。统一使用：

```bash
make -C backend generate
```

这个目标依次执行 `go generate ./ent` 和 `go generate ./cmd/server`。只改了 Wire 装配时，运行 `(cd backend && go generate ./cmd/server)` 即可；这个入口委托给 app 的生成位置，构建版本仍通过 `-X main.Version` 等变量注入。修改 `backend/ent/schema/`、生成 feature 或 Wire provider 之后，提交对应的生成差异，并检查差异里只有预期的 schema 和依赖变化。

Go 1.27 的 jsonv2 生成代码，可能把 Ent 的 JSON 字段表示为 `encoding/json/jsontext.Value`，这是预期的结果。

Ent schema 不是生产环境的迁移器。数据库的变更需要新建 `backend/migrations/*.sql`，不能依赖 Ent 的 auto-migrate，也不能修改已有的迁移。编号、`_notx.sql`、checksum 和 fork 同步上游时的重新编号规则，见[部署与数据库迁移](deployment_and_migrations.md)。

## 验证策略

验证的范围随风险扩大：先运行受影响的包或组件，再运行仓库的门禁。后端常用的命令：

```bash
# 受影响的包
(cd backend && GOTOOLCHAIN=go1.27.0 go test ./internal/provider/... ./internal/routing/... ./internal/egress/...)

# 架构检查覆盖全部标签，不需要按标签重复执行
make -C backend test-architecture

# 与 CI 一致的测试分层
make -C backend test-unit
make -C backend test-integration

# 架构检查、普通测试、lint 配置校验和 lint
make -C backend test
```

### 测试分层

普通、unit 和 integration 三组全量测试分别串行运行，避免多个 Ent schema loader 会话争用临时目录；用 go list 和 JSON 事件核对实际的标签、OS 文件和执行的测试。`make -C backend test-integration` 固定使用 `-p=4`，本地和 CI 共用这个入口，测试内部的并发和断言保持不变。测试事件、跳过、原始的失败和之后的通过，分别保存，不能只比较数量来代替逐项诊断。跳过和只编译的结果，不算行为通过。

集成测试可能启动 PostgreSQL 和 Redis 容器；环境里没有 Docker 时，明确报告没有运行，单元测试的结果代替不了它。涉及迁移时，还要运行 migration runner 和对应的 schema、数据回归测试。

外部 E2E 测试在 `backend/tests/integration`；`make -C backend test-e2e` 和 `test-e2e-local` 使用同一个 Go 测试入口，读取服务地址和测试凭据的环境变量。没有配置服务和供应商凭据时，只能报告测试被选中或通过编译，不能说行为已经验证。

这个目录也存放跨模块的装配测试，具体的执行集合由文件的构建标签决定：

- `tests/integration/pricing_contract` 保存价格配置和市场价卡、Key 快照、完成处理和资金分配的跨模块测试，使用 unit 标签；纯的提供商统计匹配和计算测试在 `billing/pricing`。这些测试直接调用模块和已有的存储替身，目录名不代表运行了真实的数据库。
- 身份注册和邮箱绑定，使用 identity 和 PostgreSQL 适配层，在 SQLite 夹具上验证规则；批量任务的运行时使用 batchimage 和 miniredis。这些 `unit` 测试不能代替真实 PostgreSQL 和 Redis 上的事务和竞争证据。
- Messages、Chat、Responses 和 Raw Chat 的协议测试，直接构造 `gateway/httpapi` 的单次执行器，共用实际的请求、响应和会话组件；纯流终态和用量 JSON 的断言在 `protocol/openai`。阻塞读取、响应关闭等 I/O 替身在 `gateway/testkit` 共用，测试不重建旧的网关应用图。
- 用量 HTTP、仪表盘和 DTO 的接口测试，直接构造 usage 和使用方的查询数据，不经过旧的 service 或完整的设置服务。日期测试明确指定 Calendar，分别覆盖用户时区的回退、夏令时和各入口的结束边界；清理任务的存储缺失错误，先由 PostgreSQL 适配层的测试核对，再用相同的错误链输入 HTTP 夹具。
- 团队所有权的两次有序 SQL 更新，由 `team/postgres` 同包的测试直接验证。sqlmock 夹具检查关闭错误时，同时登记关闭的预期，测试资源的清理不会被误判为业务 SQL 的失败。

### 各模块的验证要求

- 资金：使用真实的 PostgreSQL 事务确认回滚、持久化去重、8 位和 10 位的精度，以及 Redis 用户锁的交错；SQLite 和 mock 不能代替这些证据。
- 身份：SDK 验证、令牌消费和认证缓存，分别覆盖普通和 unit 构建的选择，以及真实的 PostgreSQL 和 Redis。
- 用量和观测：需要真实的队列、事务、取消事件和查询次数的证据，只编译或跳过都不能代替。
- 上游平台：分别验证 HTTP 提交、语义输出、可重试的窗口和已观测的用量。平台验证使用本地的 HTTP、TLS、WS 和隔离的存储夹具，这些结果不能当作对真实供应商的验证。
- 通知、站点、审核、搜索：真实的 SMTP 和 TLS 夹具、页面文件的访问范围、PostgreSQL 上审核的回滚、Redis 的预占和释放、配置的交错，以及有上限的关闭。
- 网关：分别核对实际的输出、HTTP 提交和重试窗口，不能只比较最终的字符串；失败可以带有已观测的用量，但完成资格按各入口的规则判断。对完成队列、模型快照、WS 和 Live、规则发布运行定向的 race 测试，真实的资金和 Redis 协议使用隔离的存储。不同的顶层测试如果共用第三方的全局测试设置，可以分开执行，内部的并发和断言保持不变，失败和补验的日志都要保留。
- 创作台和批量图片：真实的 PostgreSQL 测试要覆盖写任务表失败时整体回滚，以及旧请求 ID 的重放。平台和 GCS 的测试使用本地夹具，不用真实收费的生成做回归。成功的元数据、输出保存和资金变化分别核验：输出保存失败时，不会重新调用供应商；临时的 Redis 故障也不能直接认定结果永久丢失。
- 调度：测试使用兼容的报文验证存储格式。确认取得和故障放行、重复释放、锁过期后的继任和运行时的取消，要验证实际的资源数量，只检查返回码不够。
- 纯规则：测试直接传入值；平台选择、HTTP 的失败和取消、目录的热更新，还要验证实际的调用方。管理员目录完整的 JSON、24 项的顺序和 TypeScript 类型，由 app 的组合测试对照前端的 fixture，不能靠修改夹具掩盖输出的差异。
- 推广与支付：资金验证使用真实的 PostgreSQL，分别检查 Promo、返利转入、订单履约和退款的短事务；退款渠道使用本地夹具，不发起真实的付款或退款。回退新的退款代码之前，保留并核实 `REFUND_PREPARED` 记录，只替换二进制、再重新发起渠道退款是不行的。
- 备份和维护：只对隔离的 PostgreSQL、本地的 S3 和 HTTP 夹具，以及临时的可执行文件操作。恢复至少覆盖真实的成功提交、SQL 失败回滚、输入中断和取消；系统锁覆盖同一业务 ID 不同的认领版本。二进制替换的测试不能使用测试进程或部署实例的真实路径。初始化和两个维护命令，要验证退出前释放了连接，Wire 可以重复生成，Ent 和已发布的 SQL 保持不变。
- 装配：验证覆盖普通、unit、integration，以及 wireinject、embed 和 OS 的文件选择；lint 没有报错，不代表规则一定命中了。

### 其他环境要求

备份恢复的集成测试，还需要 PATH 里有 `pg_dump` 和 `psql`。目前的恢复夹具使用 PostgreSQL 18，CI 明确安装 PostgreSQL 18 的客户端；本地也使用同一个主版本，runner 自带的旧客户端可能无法备份测试数据库。断言时间戳不变时，比较操作前后从数据库读回的值；Ent 创建时返回的纳秒级内存值，和 PostgreSQL 保存的微秒值不能直接比较。

前端的门禁：

```bash
# CI 使用 lint、类型检查和关键的 Vitest 集
make test-frontend

# 变更涉及其他组件时，运行它们的测试或完整套件
npx --yes pnpm@9 --dir frontend run test:run
npx --yes pnpm@9 --dir frontend run build
```

部署文件变更时，运行 `.github/workflows/backend-ci.yml` 里对应的 shell 和 Compose 检查；依赖或安全相关的变更，还要运行 `make secret-scan`、`govulncheck` 或相应的审计。最后至少执行 `git diff --check`，并确认没有意外的生成物、环境文件或秘密。

CI 的安装器兼容测试在 Linux 上运行，依赖 Bash 4+ 和 `sha256sum`。Apple container 测试和其余的 shell、Compose 检查在 macOS 上运行，覆盖系统自带的 Bash 3.2。

## 提交与文档

提交信息遵循 Conventional Commits，例如 `feat(gateway): ...`、`fix(billing): ...`、`docs(project): ...`。一次提交围绕一个可以验证的目的，生成文件、迁移和接口测试，和它们的源码变更一起提交。

### Go 格式化

每次提交代码之前，在仓库根目录运行 `make fmt-go-changed`，再运行 `make check-fmt-go-changed`。这两个入口需要 Python 3 和指定版本的 golangci-lint。`tools/format_go.py` 筛选文件后，调用 `golangci-lint fmt --config backend/.golangci.yml`，检查模式加上 `--diff`。配置启用 gofumpt 的默认规则，并保留 gofmt 的 `interface{}` → `any`、`a[b:len(a)]` → `a[b:]` 两条重写规则；gofumpt 不单独维护版本。

命令处理暂存、未暂存和未跟踪的 Go 文件，按整个文件格式化，覆盖后端和仓库的工具模块；删除的文件、符号链接、vendor 和 node_modules，以及带标准生成标记的文件会被跳过。生成标记是 `package` 声明之前的 `// Code generated ... DO NOT EDIT.`，Ent schema 等手写的源文件照常参与格式化。脚本会预先排除生成文件，格式化配置也使用严格的生成文件识别。

格式化之后检查 diff，把属于这次提交的修改重新暂存。部分暂存的文件需要逐块核对，命令不会修改 Git 的暂存区。检查入口发现格式差异或工具执行失败时，返回非零状态；本地通过 AGENTS.md 要求执行，没有安装 Git hook。

检查已经提交的改动，使用 `make check-fmt-go-changed FMT_BASE=<基准提交>`，它按基准和 HEAD 的差异选择文件，工作区干净时也会检查。CI 的 PR 检出源提交，以目标分支和源提交的共同祖先为基准；普通 push 比较推送前后的提交，新分支第一次推送比较默认分支的共同祖先，默认分支第一次推送比较空树。基准无法解析时检查失败，不会悄悄跳过。

现有的全量 lint 保留 gofmt 和其他规则；配置里的 `linters.exclusions.rules` 只排除 gofumpt 的报告，新增代码的检查交给上面按改动文件执行的入口，历史文件不需要全部重新格式化。这条排除不影响 `golangci-lint fmt`。后端的 `make test` 使用同一个版本校验入口。

`PYTHONDONTWRITEBYTECODE=1 python3 tools/test_format_go.py` 在临时的 Git 仓库里，验证文件筛选、生成代码的排除、暂存区的保护、干净工作区下的提交差异，以及两种格式化规则同时生效；CI 安装指定版本后，也会执行这个测试。

### 本地文件和文档

`SYNC.md` 是本地的同步进度，受 `.gitignore` 保护，不要提交。`refactor/` 保存本地的重构计划和验证资料，整个目录不纳入版本控制；需要共享的工程说明维护在 `docs/`。使用 Codex 计划模式时，实施之前按项目指令把完整的计划保存到 `.agents/plans/`，执行进度追加到末尾。工作区里来源不明的修改不要覆盖；提交之前，按文件核对暂存的范围。

每次代码变更，都根据[工程文档目录](../index.md)判断相关的文档；已经读过、上下文里仍然保留足够内容的索引和章节，按 `project-doc` 的"读取与上下文复用"规则复用，每次改文件或收尾时不需要重读。持久的架构、领域不变量、对外接口或运维流程变化时，同步更新正文、分类目录和代码锚点；局部的实现细节，不需要扩写成新的文档。README 保持为简洁的项目入口，工程细节放在 `docs/`。

## 同步上游

同步以 upstream 的一个 PR 或 commit 为最小的审查单位，逐项理解变更，并保留 fork 在产品、计费、安全和部署上的设计。解决冲突后，运行这一项涉及的测试，再形成符合 Conventional Commits 的本地提交；`SYNC.md` 只记录本地的进度，不进入提交。

官方新 Release 由 `tools/check-upstream-release.sh` 对照 `tools/upstream-release.seen` 里记录的 tag。本地运行：

```bash
bash tools/check-upstream-release.sh
```

最新 tag 与对照文件相同时退出 0。官方更新时打印新 tag 并退出 2。GitHub Action `.github/workflows/check-upstream-release.yml` 每 6 小时执行一次，发现新 tag 时开 Issue。维护者确认后：`git fetch upstream`，按下面两条 fork 规则 merge，跑相关测试，再打本仓库 `v*` tag 走 `release.yml`。发版完成后把对照文件写成已并入的官方 tag：

```bash
bash tools/check-upstream-release.sh --update-seen vX.Y.Z
```

两条 fork 专属的规则：

1. 上游新增 `backend/migrations/` 文件时，按上游的顺序，把前缀重新编号为本 fork 当前最大迁移 ID 依次加一，并修复所有精确引用了文件名的地方；不能按原名照搬。
2. 上游在 `README.md` 里新增的工程文档，不直接并入 README；把内容归到 `docs/` 里合适的 Project Doc 或相关的用户手册，没有合适的位置时，再新建规范命名的文档。

同步之后，只检查 `git diff upstream/...` 不足以证明行为正确，还要核对本 fork 的迁移顺序、默认配置、i18n、生成文件、部署样例、文档链接和安全检查。fork 已有的修改，不能为了减少冲突而悄悄回退。

## 发布

`.github/workflows/release.yml` 由 `v*` tag 或手动 dispatch 触发。标准发布只构建一次前端，再把 Linux、Windows 和 macOS 的五个 Go 目标，分配到独立的 runner 并行编译；最后的 job 通过 `tools/goreleaser_prebuilt.sh` 把这些二进制导入 GoReleaser，统一生成 Release 归档、校验和、双架构镜像和 manifest。新旧品牌的两个 build ID 在 CI 里复制同一份预编译的二进制，两个归档都包含 `tokenrouter` 和 `sub2api` 两个普通文件，以兼容旧的更新器；镜像只使用主 build ID。

每个镜像架构只构建一次，同时打上 GHCR 和可选的 DockerHub 标签；没有配置 DockerHub 时，不会创建占位镜像。simple release 跳过二进制矩阵，只构建精简的镜像集合。workflow 从 annotated tag 的 body 读取 release notes，成功后把 `backend/cmd/server/VERSION` 同步回默认分支。

`release` 和 `sync-version-file` 成功后，`notify-discord` 向 Discord 发布英文版本公告，标准发布和 simple release 都会执行。仓库 Actions Secret `DISCORD_RELEASE_WEBHOOK_URL` 保存 `releases` 频道的专用 Webhook URL，地址使用 Discord 生成的完整值。通知步骤从 GitHub Release 读取版本名、更新说明和下载链接，`tools/notify_discord_release.py` 构造消息并附上本次 workflow 链接。草稿发布会被拒绝，长说明会截断并保留完整 Release 链接，消息关闭所有提及通知。

缺少 Secret 时通知步骤输出警告并跳过。发送失败会显示警告，发布结果仍为成功。脚本等待 Discord 返回消息回执；网络超时后由维护者检查频道，确认消息是否送达。重跑已成功的通知会再发送一条消息。GitHub feed 的提交动态使用仓库 push Webhook，发布公告由 Release workflow 发送。

发布之前，确认目标提交已经推送、CI 通过、数据库迁移可以滚动升级，并且备份已经验证。发布之后，检查 Release、镜像、二进制、VERSION 的回写和部署的冒烟测试；tag 只标识代码版本，迁移和恢复的检查仍然要做。

相关文档：[项目总览](../project_overview.md)、[系统架构](../architecture/system_architecture.md)、[配置](../interfaces/configuration.md)、[部署与数据库迁移](deployment_and_migrations.md)、[版本升级说明](upgrade_notes.md)、[运维目录](index.md)。

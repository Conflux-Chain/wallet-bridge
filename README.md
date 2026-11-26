# Wallet Bridge

让 CLI、AI agent 和本地应用通过浏览器扩展钱包连接账户、签名并发送交易。

## 当前状态

进度与待办见[当前迭代](docs/iterations/current.md)。

交付目标为单一可执行程序；运行时自动管理本地数据。[本机服务访问凭据](docs/local-credentials.md)说明令牌用途、自动生成与保存位置，以及用户需要准备的内容。

## 文档入口

| 文档 | 用途 |
|---|---|
| [AGENTS.md](AGENTS.md) | AI 开发与文档写作约定 |
| [首版需求确认稿](docs/requirements.md) | 产品需求基线 |
| [迭代开发计划](docs/roadmap.md) | 迭代顺序与开发前待澄清问题 |
| [迭代工作流](docs/iterations/README.md) | Spec、design doc 与 PR 拆分规则 |
| [架构概览](docs/architecture.md) | 组件职责、流程与信任边界 |
| [请求审阅交互](docs/approval-flow.md) | 页面信息、网络／账户修复与钱包确认流程 |
| [决策记录](docs/decisions.md) | 决策依据及待定事项 |
| [验收清单](docs/acceptance.md) | 验收场景及证据要求 |
| [本地服务测试](docs/local-service.md) | 前台／后台启动、查询、访问、停止及自动测试步骤 |

## 开发命令

使用 Codex 的开发者可按[Codex 接入指南](docs/codex.md)注册 MCP、发起首次连接，并验证中断后的结果查询。

需要长期使用时，运行下载或构建的程序并加上 `install`，将输出的绝对路径用于 CLI 和 Codex。安装成功后可清理下载副本。[安装位置与隔离测试](docs/iterations/06-release-upgrade/design.md#本机安装)

### 环境准备

先安装 [go.mod](go.mod)、[.node-version](.node-version) 和 [web/package.json](web/package.json) 指定的 Go、Node.js 和 npm。以下命令均在仓库根目录执行。首次检出或依赖锁文件更新后安装前端依赖：

```sh
npm ci --prefix web
```

### 验证首次连接

运行 `npm --prefix web run try:connection`，按终端提示用 MetaMask 与 Fluent 分别建立一次连接。前置条件、操作、核对及清理见[首次钱包连接指南](docs/first-connection.md)。

使用 `wallet-bridge connect --chain-id 71` 发起连接，可等待用户确认或异步返回。已有请求和 Session 可按 ID 管理。[发起连接](docs/first-connection.md#用-cli-发起连接) · [查询、取消与关闭](docs/first-connection.md#用-cli-管理已有连接) · [独立程序与模拟钱包回归](docs/first-connection.md#模拟钱包回归)

### 页面预览

在仓库根目录运行 `npm --prefix web run try:transaction` 可用 CLI 创建并审阅示例交易。使用 MetaMask 或 Fluent 核对账户与网络时，在命令末尾加上 `-- --wallet`。[逐字段发起交易](docs/transaction-review.md#用-cli-发起交易) · [查询与取消](docs/transaction-review.md#用-cli-查询和取消示例交易) · [钱包操作步骤](docs/transaction-review.md#核对插件钱包并体验发送)

```sh
npm --prefix web run dev
```

保持终端运行，在浏览器访问终端显示的 Local 地址，通常是 `http://127.0.0.1:5173`；端口被占用时以实际输出为准。应能看到「Wallet Bridge」、「开发预览」和钱包选择面板；[钱包发现测试指南](docs/wallet-discovery.md)提供扩展发现与模拟测试步骤。此地址不接入真实服务；完整授权流程见[浏览器授权页面测试](docs/browser-testing.md)。功能进度见[当前迭代](docs/iterations/current.md)。

页面入口需要 Vite 处理，通过上述 HTTP 地址访问；直接双击 `web/index.html` 或 `web/dist/index.html` 可能导致白屏。预览结束后，在启动终端按 `Ctrl+C` 停止。

### 构建与检查

在仓库根目录运行 `npm --prefix web run release:build -- v0.0.0-local.1` 可生成三平台独立程序和 SHA-256 清单。[本地发行构建与核对步骤](docs/iterations/06-release-upgrade/design.md#本地发行构建)

对已有版本运行 `npm --prefix web run release:review -- v0.0.0-local.1`，可在本地核对候选文件并查看版本、稳定／预览标记与清单摘要。[候选版本和发行步骤](docs/iterations/06-release-upgrade/release.md)

使用 `wallet-bridge update check` 查询最新稳定版，添加 `--include-preview` 可同时比较预览版。[版本查询与本地测试](docs/iterations/06-release-upgrade/design.md#查询可升级版本)

取得发行标签后，使用 `wallet-bridge update download <版本号>` 下载并核对本机候选程序。[下载命令与本地验证](docs/iterations/06-release-upgrade/design.md#下载候选版本) · [下载完整性测试](docs/iterations/06-release-upgrade/design.md#下载字节校验)

要验证旧 CLI 和已配置的 MCP 都能启动本机启用的服务，可运行[双版本隔离测试](docs/iterations/06-release-upgrade/design.md#启动已启用的服务)。

服务端升级前的钱包占用与并发领取可按[升级停机检查](docs/iterations/06-release-upgrade/design.md#升级停机检查)在临时数据库验证。

升级停机命令 `wallet-bridge service stop --for-upgrade` 的输出、重试方式及隔离测试见[客户端检查升级停机](docs/iterations/06-release-upgrade/design.md#客户端检查升级停机)。

升级备份的本地验证见[创建备份](docs/iterations/06-release-upgrade/design.md#创建升级备份)和[恢复与清理](docs/iterations/06-release-upgrade/design.md#恢复与清理升级备份)。

版本切换的各阶段与本地测试命令见[升级设计与复现步骤](docs/iterations/06-release-upgrade/design.md#版本切换与中断恢复)，最新用例见[候选失败后恢复数据](docs/iterations/06-release-upgrade/design.md#候选失败后恢复数据)。

验证 MCP 工具发现可运行下列 `bundle` 和 `smoke` 两项。连接与单笔交易可运行独立程序的端到端用例。[STDIO 握手与工具发现](docs/iterations/05-agent-integration/design.md#验证-stdio-接入) · [MCP 首次连接](docs/iterations/05-agent-integration/design.md#验证-mcp-连接) · [MCP 交易创建、查询与取消](docs/iterations/05-agent-integration/design.md#验证-mcp-交易)

```sh
npm --prefix web run bundle
go test ./...
go vet ./...
npm --prefix web run smoke
npm --prefix web run service:smoke
npm --prefix web run background:smoke
go run ./cmd/wallet-bridge version --json
```

预期：命令全部成功退出，三个 smoke 分别输出 `Smoke passed`、`Service smoke passed` 和 `Background smoke passed`；version 输出 JSON，其中 `uiEmbedded` 为 `true`。服务测试要求固定端口空闲，使用临时目录，停止测试服务后清理，详见[本地服务测试](docs/local-service.md)。

`bundle` 先构建页面，再将其内嵌到当前平台的 `dist/wallet-bridge-<os>-<arch>` 程序中（Windows 后缀为 `.exe`）。该程序支持 `version --json`；`smoke` 在空 PATH 下检查独立程序的命令与页面打包结果。构建检查与浏览器预览分别验证。

### 本地服务

构建后按[本地服务指南](docs/local-service.md)使用前台 `service run` 或后台 `service ensure`。指南包含查询／停止、页面地址、预期结果、凭据生成、常见错误及接口文档生成方式。

### 其他开发方式

- 交易接口验证：[共享 Go client](docs/iterations/03-single-transaction/design.md#go-client-交易测试) · [HTTP 创建、查询、取消](docs/iterations/03-single-transaction/design.md#交易-http-测试) · [浏览器读取交易及固定钱包](docs/iterations/03-single-transaction/design.md#浏览器交易读取测试) · [多页面协调与切链许可](docs/iterations/03-single-transaction/design.md#浏览器交易审阅协调测试) · [拒绝、发送许可及钱包回报](docs/iterations/03-single-transaction/design.md#浏览器交易决定测试)。
- 单笔交易开发验证：[交易输入校验](docs/iterations/03-single-transaction/design.md#本地验证) · [SQLite 创建与幂等重试](docs/iterations/03-single-transaction/design.md#创建与重试测试) · [取消、到期与 Session 关闭](docs/iterations/03-single-transaction/design.md#取消与到期测试) · [共享执行槽迁移](docs/iterations/03-single-transaction/design.md#共享执行槽测试) · [钱包占用提示](docs/iterations/03-single-transaction/design.md#钱包占用提示测试) · [交易审阅归属](docs/iterations/03-single-transaction/design.md#交易审阅归属测试) · [一次性发送许可](docs/iterations/03-single-transaction/design.md#一次性发送许可测试) · [钱包结果与重新授权](docs/iterations/03-single-transaction/design.md#钱包结果测试) · [发送中断与手动放行](docs/iterations/03-single-transaction/design.md#发送中断恢复测试)。
- 多 client 待审批请求：[管理首页体验与浏览器回归](docs/iterations/04-collaboration-recovery/design.md#验证管理首页) · [列表接口验证](docs/iterations/04-collaboration-recovery/design.md#本地验证)。
- 本机活动记录：[页面体验与浏览器回归](docs/iterations/04-collaboration-recovery/design.md#验证活动记录页面) · [分页查询接口验证](docs/iterations/04-collaboration-recovery/design.md#验证活动记录接口)。
- [多客户端与重启恢复验收](docs/iterations/04-collaboration-recovery/acceptance.md)验证手动放行、迟到交易哈希及活动移除后的结果查询。
- 三目标构建：`npm --prefix web run bundle:all`（Windows amd64、macOS amd64／arm64）。
- 无页面的后端开发：`go test -tags no_web ./...`、`go run -tags no_web ./cmd/wallet-bridge version --json`。

### 本机凭据测试

通过真实文件和多进程测试检查初始化及权限；在仓库根目录执行，前置环境同上，无需启动服务或页面：

```sh
go test -tags no_web ./internal/platform ./internal/credentials -count=1 -v
```

预期：单凭据初始化、旧目录复用、损坏文件保护、权限拒绝、失败清理和六进程并发初始化测试通过。测试使用临时目录，结束后自动清理，不创建正式用户凭据。Windows 无符号链接创建权限时该项显示 `SKIP`，需要在具备权限的原生环境补验；不必为运行测试申请管理员权限。

[服务接入兼容性测试](docs/local-service.md#服务接入兼容性测试)验证同一凭据的查询／管理能力、旧控制密钥拒绝、浏览器权限及旧服务不兼容；固定端口已有服务时也可运行。

### 授权存储测试

[授权核心与 SQLite 测试指南](docs/local-service.md#sqlite-存储测试与数据)提供一次性凭据、持久授权、迁移、到期／撤销、崩溃恢复和文件保护测试，以及正式数据库的位置与失败处理方式。

[连接核心与存储测试](docs/connection-storage.md)提供创建重试、Session 生命周期、执行协调、统一维护及迁移的独立验证步骤；无需启动正式服务或钱包。

[连接与 Session HTTP 测试](docs/connection-http.md)提供 HTTP、浏览器执行协调与连接决定的验证步骤，以及 Swagger 入口。[共用 Go 客户端验证](docs/connection-http.md#共用-go-客户端验证)可单独复现创建响应丢失后的重试及 Session 历史查询。

[授权 HTTP 测试](docs/local-service.md#浏览器授权-http-测试)提供接口行为测试、独立程序验证方式及当前交付边界。

[授权 CLI 指南](docs/local-service.md#浏览器授权-cli)说明 `ui`、`browser list／revoke`；[浏览器测试指南](docs/browser-testing.md)提供页面授权、退出、重启复用和 Playwright 自动测试步骤。钱包功能按[当前迭代](docs/iterations/current.md)继续交付。

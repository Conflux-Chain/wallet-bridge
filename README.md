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

### 环境准备

先安装 [go.mod](go.mod)、[.node-version](.node-version) 和 [web/package.json](web/package.json) 指定的 Go、Node.js 和 npm。以下命令均在仓库根目录执行。首次检出或依赖锁文件更新后安装前端依赖：

```sh
npm ci --prefix web
```

### 页面预览

```sh
npm --prefix web run dev
```

保持终端运行，在浏览器访问终端显示的 Local 地址，通常是 `http://127.0.0.1:5173`；端口被占用时以实际输出为准。应能看到「Wallet Bridge」、「开发预览」和钱包选择面板；[钱包发现测试指南](docs/wallet-discovery.md)提供扩展发现与模拟测试步骤。此地址不接入真实服务；完整授权流程见[浏览器授权页面测试](docs/browser-testing.md)。功能进度见[当前迭代](docs/iterations/current.md)。

页面入口需要 Vite 处理，通过上述 HTTP 地址访问；直接双击 `web/index.html` 或 `web/dist/index.html` 可能导致白屏。预览结束后，在启动终端按 `Ctrl+C` 停止。

### 构建与检查

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

[连接与 Session HTTP 测试](docs/connection-http.md)提供 client 侧接口、权限、重试与历史查询的验证步骤，以及 Swagger 入口。

[授权 HTTP 测试](docs/local-service.md#浏览器授权-http-测试)提供接口行为测试、独立程序验证方式及当前交付边界。

[授权 CLI 指南](docs/local-service.md#浏览器授权-cli)说明 `ui`、`browser list／revoke`；[浏览器测试指南](docs/browser-testing.md)提供页面授权、退出、重启复用和 Playwright 自动测试步骤。钱包功能按[当前迭代](docs/iterations/current.md)继续交付。

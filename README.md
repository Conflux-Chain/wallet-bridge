# Wallet Bridge

让 CLI、AI agent 和本地应用通过浏览器扩展钱包连接账户、签名并发送交易。

## 当前状态

进度与待办见[当前迭代](docs/iterations/current.md)。

## 文档入口

| 文档 | 用途 |
|---|---|
| [AGENTS.md](AGENTS.md) | AI 开发与文档写作约定 |
| [首版需求确认稿](docs/requirements.md) | 产品需求基线 |
| [迭代开发计划](docs/roadmap.md) | 迭代顺序与开发前待澄清问题 |
| [迭代工作流](docs/iterations/README.md) | Spec、design doc 与 PR 拆分规则 |
| [架构概览](docs/architecture.md) | 组件职责、流程与信任边界 |
| [决策记录](docs/decisions.md) | 决策依据及待定事项 |
| [验收清单](docs/acceptance.md) | 验收场景及证据要求 |

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

保持终端运行，在浏览器访问终端显示的 Local 地址，通常是 `http://127.0.0.1:5173`；端口被占用时以实际输出为准。应能看到「Wallet Bridge」标题和「由你确认的钱包操作」说明。当前为静态说明页，功能进度见[当前迭代](docs/iterations/current.md)。

页面入口需要 Vite 处理，通过上述 HTTP 地址访问；直接双击 `web/index.html` 或 `web/dist/index.html` 可能导致白屏。预览结束后，在启动终端按 `Ctrl+C` 停止。

### 构建与检查

```sh
npm --prefix web run bundle
go test ./...
go vet ./...
npm --prefix web run smoke
go run ./cmd/wallet-bridge version --json
```

预期：命令全部成功退出，smoke 输出 `Smoke passed`，version 输出 JSON，其中 `uiEmbedded` 为 `true`。

`bundle` 先构建页面，再将其内嵌到当前平台的 `dist/wallet-bridge-<os>-<arch>` 程序中（Windows 后缀为 `.exe`）。该程序支持 `version --json`；`smoke` 在空 PATH 下检查独立程序的命令与页面打包结果。构建检查与浏览器预览分别验证。

### 其他开发方式

- 三目标构建：`npm --prefix web run bundle:all`（Windows amd64、macOS amd64／arm64）。
- 无页面的后端开发：`go test -tags no_web ./...`、`go run -tags no_web ./cmd/wallet-bridge version --json`。

当前仅提供工程骨架与 `version`；本地服务及钱包功能按[迭代计划](docs/iterations/01-local-service/plan.md)继续交付。

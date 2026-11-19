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

工具版本见 [go.mod](go.mod)、[.node-version](.node-version) 和 [web/package.json](web/package.json)。在仓库根目录执行：

```sh
npm ci --prefix web
npm --prefix web run bundle
go test ./...
go vet ./...
npm --prefix web run smoke
```

`bundle` 先构建页面，再将其内嵌到当前平台的 `dist/wallet-bridge-<os>-<arch>` 程序中（Windows 后缀为 `.exe`）。运行该程序的 `version --json` 查看构建信息；`smoke` 在空 PATH 下检查命令与页面打包结果。

- 三目标构建：`npm --prefix web run bundle:all`（Windows amd64、macOS amd64／arm64）。
- 前端开发预览：`npm --prefix web run dev`。
- 无页面的后端开发：`go test -tags no_web ./...`、`go run -tags no_web ./cmd/wallet-bridge version --json`。

当前仅提供工程骨架与 `version`；本地服务及钱包功能按[迭代计划](docs/iterations/01-local-service/plan.md)继续交付。

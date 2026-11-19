# 迭代 1 PR 计划

状态：2026-11-19 PR 1-2 已合并；PR 1-3 已实现，本地检查通过，待提交评审及原生 CI。范围见 [spec](spec.md)，方案见 [design](design.md)。每次只推进一个评审单元。

## 拆分

| 步骤 | 单一目标与边界 | 检查与验收 | 状态 |
|---|---|---|---|
| PR 1-0：需求与设计 | 确认本轮行为、凭据交付、系统版本及分步计划；同步全局资料 | 文档一致、链接有效，未实现与待验证项明确 | [#4](https://github.com/Conflux-Chain/wallet-bridge/pull/4) 已合并 |
| PR 1-1：工程骨架 | Go module、最小 CLI version、React／Vite 页面、资源封装、构建脚本与基础 CI；固定所需工具版本 | 前端检查、完整构建、三目标构建及 version smoke；A-01 基础 | [#5](https://github.com/Conflux-Chain/wallet-bridge/pull/5) 已合并 |
| PR 1-2：本机凭据 | 用户数据目录、受限凭据文件与原子初始化；不接 HTTP 或 SQLite | 目录及文件权限、并发初始化、不覆盖已有密钥、失败清理；三个目标构建 | [#6](https://github.com/Conflux-Chain/wallet-bridge/pull/6) 已合并 |
| PR 1-3：前台 HTTP 服务 | 进程锁、service run、版本／状态接口、鉴权与 Host／Origin 边界、正常停止；引入代码注释生成文档 | 并发锁、真实监听、占用、权限隔离、错误映射和正常退出；A-02～04 基础 | 本地检查通过，待评审及 CI |
| PR 1-4：后台启动与复用 | 共享 client、后台进程、ensure／status／stop、身份探测、就绪与版本兼容；不增加页面授权入口 | 多进程并发、client 退出后常驻、假服务／不兼容／启动超时；A-02～03 | 待开发 |
| PR 1-5：授权存储 | SQLite 驱动、schema 迁移与浏览器授权存储；不接页面或授权交换接口 | 迁移原子性、记录读写／重开、到期／撤销持久化；三个目标构建 | 待开发 |
| PR 1-6：浏览器授权 | ui、一次性授权交换、管理 cookie、撤销与状态页；不调用钱包 | 重放／过期／跨站拒绝，重启保留、固定到期和撤销；A-04 本轮范围 | 待开发 |
| PR 1-7：组合验收 | 三平台本轮组合流程、无工具链运行及交接资料；不补交此前 PR 应有的基础测试 | 汇总 A-01～04 证据，明确最低系统版本与缺失环境的待验收项 | 待开发 |

PR 1-1～1-7 按顺序依赖；每个实现 PR 只引入当前能力需要的目录和依赖。PR 合并、自动检查、真实环境及用户验收分别记录。

## 当前步骤

- PR 1-2 已按用户“继续”的指令 Squash 合并为 `35c1bdb`，并清理其本地／远端分支；本地 main 已同步。
- 当前分支：`feat/foreground-service`，从上述 main 创建，仅交付 PR 1-3。
- 本 PR 实现前台服务、锁和 HTTP 基础接口；后台复用、SQLite、浏览器授权、钱包及 MCP 按后续 PR 交付。

## 验证与限制

- PR 1-0：15 份 Markdown、82 个本地链接及 23 个标题锚点检查通过，无行尾空白；Git 空白检查通过。
- PR 1-1 本地 Windows amd64：npm ci、前端类型检查与生产构建、完整 Go 测试（含内嵌资源引用检查）、no_web 构建／运行、go vet、原生打包与空 PATH 的 CLI smoke 均通过。
- Windows amd64、macOS amd64／arm64 三目标交叉构建通过。Go 按 go.mod 自动下载并使用 1.27.4；前端依赖审计未发现已知漏洞。
- PR 1-1：15 份 Markdown、88 个本地链接及 24 个标题锚点检查通过；Git 暂存区空白检查通过。
- CI 配置 Windows x64、macOS Intel／Apple Silicon 原生 runner；实际执行结果随 PR checks 记录，交叉编译不算原生运行证据。
- 用户已确认按 Vite 开发服务器方式预览时页面正常显示；本地测试步骤、预期结果及停止方式已补齐到 [README](../../../README.md#开发命令)。
- 最低系统版本、完整浏览器／钱包兼容性及真实 agent 尚未验收；数据库、浏览器授权按后续 PR 验证。

### PR 1-2

- Windows 本地完整／no_web Go 测试、go vet、前端／三目标程序构建及空 PATH smoke 通过；两种 macOS 架构的新增测试程序交叉编译通过。
- 真实文件测试覆盖权限拒绝、重复／部分初始化、损坏文件保留、独立读取、随机源失败清理及六进程同时初始化。测试在临时目录执行，步骤与预期见 [README](../../../README.md#本机凭据测试)。
- 三平台[原生 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44483555802) 通过（`1d3267f`）；Windows 的 ACL／符号链接，以及 macOS 两种架构的权限／扩展 ACL／符号链接测试均实际执行通过，六进程测试也通过。
- 本机 Windows 符号链接测试因缺少创建权限跳过，已由上述 CI 补验。最低系统版本、以其他普通用户身份的实际访问隔离仍待验收。
- 按用户反馈补充[凭据说明](../../local-credentials.md)，明确用途、安装与运行数据的区别、自动生成和保存、权限边界及当前实现范围；README 提供入口。文档本地链接／锚点及 Git 空白检查通过。

### PR 1-3

- Windows amd64：完整／no_web Go 测试、go vet、前端生产构建和 version 空 PATH smoke 通过；三目标程序交叉构建通过。
- 临时目录测试验证跨进程锁竞争、正常／异常退出后锁释放、实际 HTTP 监听及停止、启动失败的资源释放；接口测试覆盖 Host／Origin、凭据权限、challenge proof、请求体限制和静态资源路径。
- 原生独立程序在空 PATH 下的 service smoke 通过：固定端口与同目录竞争、HTML／JavaScript 资源、鉴权、控制停止、凭据保留及重启。修正 Windows 独占端口冲突返回 WSAEACCES 时的错误映射。
- handler 注释生成 Swagger 2.0 JSON／YAML；[本地服务指南](../../local-service.md)提供构建、自动测试、手动启动、访问与停止步骤，README 链接该入口。
- Windows 终端实际启动、Ctrl+C 退出码 0、同目录再次启动及再次正常停止通过；文档链接／锚点与行尾空白检查通过。
- macOS 两种架构的原生运行及 SIGTERM 退出待本 PR 的 CI；最低系统版本、浏览器呈现和用户验收尚未补验。HTTP 资源检查不代替浏览器呈现验收。

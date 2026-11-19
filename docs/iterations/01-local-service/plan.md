# 迭代 1 PR 计划

状态：2026-11-19 PR 1-6a 已合并；PR 1-6b 已提交，待评审。范围见 [spec](spec.md)，方案见 [design](design.md)。每次只推进一个评审单元。

## 拆分

| 步骤 | 单一目标与边界 | 检查与验收 | 状态 |
|---|---|---|---|
| PR 1-0：需求与设计 | 确认本轮行为、凭据交付、系统版本及分步计划；同步全局资料 | 文档一致、链接有效，未实现与待验证项明确 | [#4](https://github.com/Conflux-Chain/wallet-bridge/pull/4) 已合并 |
| PR 1-1：工程骨架 | Go module、最小 CLI version、React／Vite 页面、资源封装、构建脚本与基础 CI；固定所需工具版本 | 前端检查、完整构建、三目标构建及 version smoke；A-01 基础 | [#5](https://github.com/Conflux-Chain/wallet-bridge/pull/5) 已合并 |
| PR 1-2：本机凭据 | 用户数据目录、受限凭据文件与原子初始化；不接 HTTP 或 SQLite | 目录及文件权限、并发初始化、不覆盖已有密钥、失败清理；三个目标构建 | [#6](https://github.com/Conflux-Chain/wallet-bridge/pull/6) 已合并 |
| PR 1-3：前台 HTTP 服务 | 进程锁、service run、版本／状态接口、鉴权与 Host／Origin 边界、正常停止；引入代码注释生成文档 | 并发锁、真实监听、占用、权限隔离、错误映射和正常退出；A-02～04 基础 | [#7](https://github.com/Conflux-Chain/wallet-bridge/pull/7) 已合并 |
| PR 1-4：后台启动与复用 | 共享 client、后台进程、ensure／status／stop、身份探测、就绪与版本兼容；不增加页面授权入口 | 多进程并发、client 退出后常驻、假服务／不兼容／启动超时；A-02～03 | [#8](https://github.com/Conflux-Chain/wallet-bridge/pull/8) 已合并 |
| PR 1-5：授权存储 | SQLite 驱动、schema 迁移与浏览器授权存储；不接页面或授权交换接口 | 迁移原子性、记录读写／重开、到期／撤销持久化；三个目标构建 | [#9](https://github.com/Conflux-Chain/wallet-bridge/pull/9) 已合并 |
| PR 1-6a：授权核心 | 一次性凭据签发／消费、持久授权校验和撤销；暂不接 HTTP、CLI 或页面 | 重放／到期／并发消费、重启边界、存储失败、单个／全部撤销 | [#10](https://github.com/Conflux-Chain/wallet-bridge/pull/10) 已合并 |
| PR 1-6b：授权 HTTP | 接入授权核心，提供交换／cookie／管理接口，同步生成接口文档 | 权限分离、同 origin、cookie 属性、受保护状态、错误和凭据不泄漏 | [#11](https://github.com/Conflux-Chain/wallet-bridge/pull/11) 待评审 |
| PR 1-6c：授权 CLI | ui、浏览器启动及授权管理命令，共用已验证身份的 HTTP client | 一次性链接仅显式输出、打开失败处理、CLI 管理、三平台运行 | 待开发 |
| PR 1-6d：授权页面 | 未授权说明、交换／状态及退出页面 | 地址 fragment 清理、30 天授权复用、到期／撤销反馈、真实浏览器呈现 | 待开发 |
| PR 1-7：组合验收 | 三平台本轮组合流程、无工具链运行及交接资料；不补交此前 PR 应有的基础测试 | 汇总 A-01～04 证据，明确最低系统版本与缺失环境的待验收项 | 待开发 |

PR 1-1～1-7 按顺序依赖，其中 PR 1-6 按 a～d 分步交付，保持评审单元集中；每个实现 PR 只引入当前能力需要的目录和依赖。PR 合并、自动检查、真实环境及用户验收分别记录。

## 当前步骤

- PR 1-6a 已按用户“继续”的指令 Squash 合并为 `6583dec`，并清理其本地／远端分支；本地 main 已同步。
- 当前分支：`feat/browser-authorization-http`，从上述 main 创建；PR 1-6b 已提交为 #11，待评审；撤销入口已按 spec 第 6 节确认。
- 授权 HTTP、CLI 和页面分别交付，钱包及 MCP 沿用后续迭代安排。

## 验证与限制

- PR 1-0：15 份 Markdown、82 个本地链接及 23 个标题锚点检查通过，无行尾空白；Git 空白检查通过。
- PR 1-1 本地 Windows amd64：npm ci、前端类型检查与生产构建、完整 Go 测试（含内嵌资源引用检查）、no_web 构建／运行、go vet、原生打包与空 PATH 的 CLI smoke 均通过。
- Windows amd64、macOS amd64／arm64 三目标交叉构建通过。Go 按 go.mod 自动下载并使用 1.27.4；前端依赖审计未发现已知漏洞。
- PR 1-1：15 份 Markdown、88 个本地链接及 24 个标题锚点检查通过；Git 暂存区空白检查通过。
- CI 配置 Windows x64、macOS Intel／Apple Silicon 原生 runner；实际执行结果随 PR checks 记录，交叉编译不算原生运行证据。
- 用户已确认按 Vite 开发服务器方式预览时页面正常显示；本地测试步骤、预期结果及停止方式已补齐到 [README](../../../README.md#开发命令)。
- 最低系统版本、完整浏览器／钱包兼容性及真实 agent 尚未验收；浏览器授权流程按后续 PR 验证。

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
- 三平台[原生 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44488957047) 通过（`8077040`），含 macOS 两种架构的实际运行及 SIGTERM 退出；最低系统版本、浏览器呈现和用户验收尚未补验。HTTP 资源检查不代替浏览器呈现验收。

### PR 1-4

- Windows amd64：完整／no_web Go 测试、go vet、前端及三目标程序构建通过；client／platform／service 的 Go race 检查通过。
- client 测试验证同连接身份确认后鉴权、伪造／重放／篡改响应拒绝、接口与能力不兼容、重定向与响应大小限制、查询不创建数据、启动／停止超时和取消；认证连接关闭后不向替换连接发送令牌。
- 空 PATH 的 version、前台 service、后台 background smoke 通过。六个原生 CLI 并发 ensure 复用同一 `startedAt`，调用者退出后可继续查询，正常停止、重启保留凭据、假服务不接收令牌均通过。
- [本地服务指南](../../local-service.md)补充后台 ensure／status／stop、JSON 与退出码、预期结果、停止／清理及错误处理；README 提供入口。
- 三平台[原生 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44492473941) 通过（`c45ee4e`）；最低系统版本、实际终端关闭、浏览器／钱包及 agent 仍待对应验收。

### PR 1-5

- 采用 `modernc.org/sqlite v1.62.0`，实际引擎为 SQLite 3.54.1。Windows amd64 完整／no_web Go 测试、go vet、store／service／client 的 race 检查、前端及三目标无 CGO 程序构建通过。
- 真实临时数据库验证事务迁移回滚、较新／未知 schema 拒绝且保留数据、重复／并发写入、固定到期、撤销持久化及权限拒绝。强制终止自身测试子进程后，从真实 hot journal 恢复，未提交的新增和撤销均回滚，已提交记录及日志保护保留。
- 空 PATH 的三组 smoke 通过；正式程序启动完成 SQLite 初始化后才报告就绪，损坏数据库保持原内容，后台 ensure 返回 `storage_error`。
- [SQLite 存储指南](../../local-service.md#sqlite-存储测试与数据)提供可复现命令、预期、临时数据清理、正式数据库位置及失败处理；README 提供入口。
- 依赖校验、17 份 Markdown 的 118 个本地链接／37 个标题锚点及 Git 空白检查通过。
- 三平台[原生 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44497310341) 通过（最终 head `0e793c4`），含真实读写、崩溃恢复、文件权限和数据库／日志符号链接拒绝；本机 Windows 跳过项已补验。最低系统版本及真实浏览器授权尚未验收。

### PR 1-6a

- 核心行为和真实 SQLite 组合测试通过：一次性凭据到期／限额／并发消费、交换失败不能重放、每次校验持久状态、重启丢弃临时凭据并保留授权、退出／单个／全部撤销及历史保留。
- Windows 完整／no_web Go 测试、go vet 和 app／store 的 race 检查通过；本机数据库符号链接测试因权限不足跳过，原生补验以本 PR checks 为准。
- 前端与三目标无 CGO 构建、空 PATH 的三组 smoke，以及 17 份 Markdown 的 119 个本地链接／38 个标题锚点和 Git 空白检查通过。
- 本 PR 只提供内部授权核心；HTTP、CLI 和页面分别在后续 PR 接入。撤销入口已确认，接入前通过[存储与核心测试指南](../../local-service.md#sqlite-存储测试与数据)验证。
- 三平台[原生 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44503787810) 通过（最终 head `6a2e2e7`），含本机跳过的符号链接测试。

### PR 1-6b

- Windows 完整／no_web Go 测试、go vet、api／app／service／client 的 race 检查、前端及三目标无 CGO 构建通过。
- 真实 SQLite 的 HTTP 测试验证交换／重放、同 origin、cookie 属性、权限分离、重复 cookie、失败输出、到期／重启／撤销、分页及链接限额；既有 client 的身份和状态／停止测试通过。
- 空 PATH 的原生 service smoke 验证签发链接、交换 cookie、重放拒绝、重启保留期限、临时链接失效、退出及撤销持久化。Swagger 已按类型／注释生成；本地复现与清理见[授权 HTTP 测试](../../local-service.md#浏览器授权-http-测试)。
- version／background smoke、17 份 Markdown 的 123 个本地链接／42 个标题锚点及 Git 空白检查通过。
- 三平台原生结果见 [#11 checks](https://github.com/Conflux-Chain/wallet-bridge/pull/11/checks)；真实浏览器 cookie／页面验收、授权 CLI 及最低系统版本仍待相应 PR／环境验证。

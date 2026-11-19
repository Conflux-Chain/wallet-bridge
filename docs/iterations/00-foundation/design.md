# 迭代 0 技术设计

状态：2026-11-19 本轮技术方向已确认，驱动版本、凭据机制及构建兼容性在对应迭代细化与验证。尚未创建工程。范围见 [spec](spec.md)，实施与评审安排见 [plan](plan.md)。

## 1. 语言与工具

| 方案 | 优势 | 主要代价 |
|---|---|---|
| Go + TypeScript | Go 适合 CLI、常驻服务与独立程序交付；浏览器用 TS 对接钱包 | 两套构建工具；跨语言协议类型需要保持一致 |
| 全 TypeScript | 服务、MCP 与页面可共享语言和类型，钱包生态接入方便 | 无需用户安装 Node.js 的打包、资源及依赖兼容性需要专门验证 |
| Rust + TypeScript | 适合独立程序与系统集成 | 工具链与异步代码的开发维护复杂度较高 |

用户已确认 **Go 编写 CLI／HTTP 服务／MCP，TypeScript 编写浏览器页面**。主要考虑 Windows／macOS 的常驻服务与免额外运行时交付。

选择依据：

- 首版主要处理请求、进程、状态和持久化，没有已知的高吞吐计算需求；Go 的系统工具交付方式比较契合。
- 全 TS 也能满足用户无需预装 Node.js，但要验证内置运行时、依赖及资源在两个系统上的打包行为。
- HTTP 文档从 Go 类型与 handler 注释生成；TS 类型生成与接口一致性检查在接口设计时确定。

已确认的配套方案：

- MCP 使用[官方 Go SDK](https://github.com/modelcontextprotocol/go-sdk)，通过 STDIO 接入，作为同一可执行程序的子命令运行。
- 页面采用 React + TypeScript + Vite；React 用于维护请求及钱包事件驱动的界面状态。

构建采用以下技术方向：

- 静态页面通过 [Go embed](https://pkg.go.dev/embed) 嵌入程序，沿用第 4 节的资源封装。
- 开发者需要 Go 与前端构建工具；发行包用户无需安装这些工具。

具体依赖版本在工程初始化时固定，构建与兼容性尚未验证。

## 2. 进程与打包方向

已确认：同一仓库使用一个 Go module，浏览器页面作为独立的 TypeScript 前端工程；CLI／MCP 共用 HTTP client，后台统一管理状态，浏览器负责钱包调用。

运行与打包方案：

- 同一程序通过子命令提供 CLI、后台 HTTP 服务和 MCP 运行入口；具体命令在迭代 1／5 细化。
- 后台 HTTP 服务独立常驻，统一管理 Session、请求、排队与持久化。
- CLI 和 MCP 通过本地 HTTP client 复用该服务，避免各自维护一份交易状态。
- 浏览器页面通过 HTTP 服务获取请求，选定钱包 Provider 后调用钱包并回传结果。
- 原生平台构建与验收目标见第 8 节；最低系统版本和签名分发方式在对应迭代明确。

## 3. 模块职责

模块职责与依赖已确认；下表路径对应下一节的目录布局。

| 路径 | 职责 |
|---|---|
| `cmd/wallet-bridge/` | 程序入口与依赖装配 |
| `internal/cli/` | 命令、参数和输出；启动／复用服务并调用 HTTP client |
| `internal/client/` | CLI／MCP 共用的本地 HTTP client，封装鉴权、查询与错误 |
| `internal/api/` | HTTP 路由、handler、中间件、请求／响应类型与转换、页面与静态资源入口 |
| `internal/app/` | Session、请求状态、操作权限、幂等、排队与流程编排 |
| `internal/store/` | 持久化、原子状态写入与迁移 |
| `internal/mcp/` | Tool 映射、参数与结果转换，通过 client 访问共享服务 |
| `internal/platform/` | 进程管理、互斥锁、数据路径与打开浏览器等平台差异 |
| `web/` | 浏览器界面、钱包发现／调用、请求与结果回传 |

依赖方向：CLI／MCP → client → HTTP；HTTP → app → 存储接口。app 不依赖具体 CLI、MCP 或钱包品牌，store 实现存储接口。

交易请求的可变状态集中由共享服务管理；CLI／MCP 负责接入与呈现，浏览器负责钱包交互。请求入口校验与应用操作权限的具体规则另行设计。

依赖约束：

- 存储接口由使用方 app 定义，store 实现，由程序入口装配；app 不导入具体存储实现。
- app 只管理会话／请求流程；命令解析、HTTP 转换、SQL 和钱包 Provider 调用各归所属模块。
- platform 只封装操作系统差异；服务就绪与兼容判断由共享接入代码处理，平台代码不反向调用应用流程。
- HTTP 层接收 `fs.FS` 提供页面资源，资源嵌入在前端目录封装，由入口注入。

## 4. 目录结构

用户已确认经自审修订后的目录与接口文档方案；仅在对应功能需要时创建。源码按模块归属，构建产物单独管理。

```text
wallet-bridge/
├── go.mod / go.sum
├── cmd/wallet-bridge/main.go
├── internal/
│   ├── cli/
│   ├── client/
│   ├── api/
│   ├── app/
│   ├── store/
│   ├── mcp/
│   └── platform/
├── web/
│   ├── package.json
│   ├── embed.go             # 页面资源的 Go 封装
│   ├── src/
│   │   ├── ui/              # 页面与交互
│   │   ├── wallet/          # Provider 发现与调用
│   │   └── api/             # 浏览器侧 HTTP 接入
│   ├── public/              # 静态源资源
│   ├── dist/                # 前端构建产物，忽略
│   └── tests/e2e/           # 浏览器端到端测试
├── tests/integration/       # Go 跨模块／跨进程测试
├── scripts/                 # 跨平台构建、检查与打包
├── docs/
│   ├── swagger/             # 从 Go 代码注释生成的接口文档
│   └── ...                  # 现有需求、设计与迭代文档
├── dist/                    # 发行产物，忽略
├── .gitignore
├── README.md
└── AGENTS.md
```

- Go 单元测试与代码同包，使用 `*_test.go`；TS 单元测试就近放置，测试数据归属相关模块。
- `internal/api/` 专指 HTTP 接口层；MCP 适配仍归 `internal/mcp/`，业务流程归 `internal/app/`。`web/src/api/` 是浏览器侧调用代码。
- 前端按惯例输出到 `web/dist/`；采用 embed 时，由 `web/embed.go` 包装成 `fs.FS`，通过入口注入 HTTP 层。
- 后端测试使用内存文件系统及明确的无 UI 构建模式，例如 `no_web` build tag；全量默认构建仍需前端产物。发行构建先生成页面再编译，必须检查完整资源，不能把无 UI 测试模式当作发行包。
- 前端／发行构建产物、依赖目录和本地运行数据不提交 Git；固定依赖版本的清单与锁文件提交。生成接口文档按下述规则维护。

### 接口文档

- 采用代码优先：以 `internal/api/` 的请求／响应类型和 handler 的 Swagger 注释生成文档，不另建根目录 `api/` 手写规范。
- 使用 [swaggo/swag](https://github.com/swaggo/swag) 生成到 `docs/swagger/`，避免与现有 Markdown 混放。默认输出为 `docs.go`、`swagger.json`、`swagger.yaml`；具体版本与输出类型在接口迭代引入并验证，Swagger UI 按需接入。
- 生成文档随接口变更提交，不手改；引入时固定生成器版本并增加重新生成的差异检查。注释中的路由、响应及鉴权说明仍需与实现核对；生成文档不能替代接口测试。
- 当前查阅的 swag 主线说明生成 Swagger 2.0（OpenAPI 2.0），不视为 OpenAPI 3.x 方案；如后续需要 3.x，再验证对应工具支持。Go／TS 类型共享方式在接口设计时确定。

## 5. 架构与目录自审

| 检查项 | 结论与处理 |
|---|---|
| 目录通用性 | `cmd/`、`internal/`、根 go.mod 符合 Go 官方服务项目指引；`internal/api/`、web／scripts／tests 及 `docs/swagger/` 的具体名称是项目约定 |
| 分层与依赖 | 保留共享服务与薄 CLI／MCP；明确接口归属及入口装配，避免 app 与具体 store 循环依赖 |
| 包职责扩张 | 为 app／platform 限定职责，按实际变化再拆包，不为目录整齐预先铺空包 |
| 前端资源耦合 | 前端目录封装资源、HTTP 接收 fs.FS；补充后端测试与完整构建的区别 |
| 验证边界 | 当前只完成职责、依赖与构建路径的静态评估；持久化原子性、访问隔离、并发调度及真实钱包仍需后续设计与验证 |

参考：[Go 官方布局](https://go.dev/doc/modules/layout)、[Go 包命名指引](https://go.dev/blog/package-names)、[PocketBase 资源封装](https://github.com/pocketbase/pocketbase/blob/master/ui/embed.go)及[无 UI 模式](https://github.com/pocketbase/pocketbase/blob/master/ui/embed_no_ui.go)、[AdGuard Home](https://github.com/AdguardTeam/AdGuardHome)。这些资料支持所采用的惯例，不表示存在一套所有项目通用的固定目录树。

## 6. 持久化方案

用户已选择 **SQLite，通过 `database/sql` 访问**。驱动优先评估 [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)，避免 CGO 工具链要求；具体版本与目标平台构建尚未验证。

| 方案 | 适用性与取舍 |
|---|---|
| SQLite（已选） | 嵌入式，无需安装数据库服务；事务、约束和 SQL 适合关联状态及活动日志查询；需要管理迁移和连接配置 |
| bbolt | 纯 Go 事务型 KV，也能原子更新多项记录；筛选及二级索引需自行组织 |
| JSON 文件 | 便于查看，但一致性、并发写入与崩溃恢复需额外设计，不作为状态存储方案 |

- 保存范围与恢复行为沿用[需求 §9](../../requirements.md#9-持久化日志与重启恢复)。请求创建与幂等记录、相关状态与活动记录应以事务一致更新；具体存储接口随对应流程设计。
- 只有共享后台服务直接访问数据库，CLI／MCP 与浏览器经 HTTP 访问。迁移在接收业务请求前完成，失败明确报错；迁移与升级回退须保持兼容。
- SQLite 事务只覆盖数据库内更新；不能将数据库提交与钱包调用合并为原子操作。交接标记、取消竞争及不确定结果在交易迭代设计。
- 表结构、日志／同步模式、连接与锁等待配置随实现验证；不在本轮提前固定。

## 7. 基础访问模型

用户已确认共享服务、按请求／Session 凭据隔离；产品范围见[需求 §2](../../requirements.md#2-面向的-client-与交付方式)。

技术方向（具体机制在迭代 1～4 随对应功能设计）：

- 区分服务接入、请求／Session 操作、浏览器审批及钱包结果回传权限。普通 client 凭据不能直接批准请求或伪造钱包回传。
- 采用不可猜测的访问凭据，以持有的权限校验资源访问；自报应用名称与资源 ID 均不作为授权依据，不引入强制应用注册。
- 服务仅监听固定 loopback 地址；浏览器请求检查 Host／Origin，凭据与操作权限另行校验。Origin 缺失不能作为可信依据。
- 本机数据与凭据使用操作系统用户权限保护；浏览器审批授权与 client 访问凭据分开交付。交付、保存、撤销与重启后的有效期在迭代 1 明确。
- Session 失效后的结果查询及迟到回传沿用需求，不简单地随 Session 一起删除凭据和结果。

本模型用于限制普通 client 的资源访问及网页跨站操作；不承诺隔离已控制当前系统用户账户、可读取其凭据的程序。页面执行归属与真实钱包确认仍需独立验证。

## 8. 构建与平台范围

原生平台与架构已确认，见[需求 §2](../../requirements.md#2-面向的-client-与交付方式)：Windows amd64、macOS amd64／arm64。

- 完整构建先构建前端，再将页面资源嵌入 Go 单一可执行程序；开发用 Node.js 不作为用户运行依赖。
- 迭代 1 固定 Go／Node.js 与依赖版本，明确最低系统版本，验证三个目标的构建及无开发工具链运行。交叉编译成功不能代替各系统的启动、锁、目录权限及浏览器交互验证。
- 迭代 6 完成发行包、签名／校验和升级回退；真实 MetaMask／Fluent、Codex 按关联验收项分别验证。
- WSL／容器兼容范围仍待细化，不因原生平台构建成功就视为已支持；远程连接不属于已确认的同机运行范围。

## 9. 本轮结论与后续细化

1. **开发语言**：已确认 Go + TypeScript。
2. **工程与模块**：已确认一个 Go module、独立前端工程及各模块职责。
3. **目录与接口文档**：已确认，具体目录随功能引入。
4. **持久化与访问模型**：SQLite 与按资源凭据隔离已确认；驱动和凭据机制按上述边界在对应迭代验证。
5. **构建与配套工具**：已确认三个原生平台目标、MCP STDIO 子命令和 React／TS／Vite；版本与完整构建在迭代 1 验证。

具体 API、表结构、锁实现和交易状态机在对应迭代设计；凭据交付与最低系统版本须在迭代 1 的相关实现前明确。本轮只完成方案文档，不代表功能与运行环境已验证。

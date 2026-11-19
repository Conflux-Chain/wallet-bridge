# 迭代 1 技术设计

状态：2026-11-19 已确认，实现与验证进度见 [plan](plan.md)。范围见 [spec](spec.md)，模块与目录沿用[迭代 0 设计](../00-foundation/design.md)。本轮只细化本地服务。

## 1. 默认值与工具

| 项目 | 本轮方案 |
|---|---|
| Origin | 固定 `http://127.0.0.1:18765`；占用时报错 |
| 数据目录 | 见[本机服务访问凭据](../../local-credentials.md#用户需要准备什么) |
| 就绪／停止等待 | ensure 最多 15 秒；正常停止最多等待 5 秒，超时不自动强杀 |
| 浏览器授权 | 一次性链接 2 分钟有效；浏览器授权固定有效 30 天 |
| Go | 1.27.4，固定于 go.mod |
| 前端工具 | Node.js 24.19.0、npm 11.17.0；依赖固定于 web/package.json 和 package-lock.json |

系统版本下限由用户确认，依据 [Go 最低系统要求](https://go.dev/wiki/MinimumRequirements)。Vite 的 Node.js 要求已按[官方说明](https://vite.dev/guide/)及实际依赖版本核对；构建结果不代替最低系统版本验收。

## 2. 进程与就绪

CLI 拟提供 `version`、`service run`、`service ensure`、`service status`、`service stop`、`ui`。version／ensure／status 支持 JSON 输出；成功返回 0，执行失败返回 1，参数错误返回 2。结构化错误包含稳定的 code 和可操作说明。

服务启动顺序：取得用户数据目录的进程锁 → 独占绑定固定端口 → 初始化权限、凭据及数据库迁移 → 启动 HTTP → 报告就绪。失败时释放资源，不留下可使用的半初始化服务。

- Windows 使用系统文件锁，macOS 使用 flock 类互斥；持锁句柄随进程存活，PID 仅作诊断信息。
- 后台进程与调用者终端脱离，不继承 MCP STDIO，不额外弹出控制台窗口。
- ensure 先探测并验证已有服务；尚未就绪时有界等待。需要启动时允许候选进程竞争锁；失败不能以 PID 文件存在就判断成功。
- 复用前通过随机 challenge 与本机 client 密钥的 HMAC 响应确认服务身份，再发送 Bearer 凭据；仅返回相同名称或版本不构成证明。
- 本地 HTTP client 只向固定 origin 直连，不跟随重定向或经代理转发访问凭据。
- 发行版本用于诊断；API major／minor 与能力列表用于兼容判断。major 不同或缺少必需能力时失败，不自动替换已有服务。

就绪与复用在 `internal/client/` 协调；锁、目录和进程机制在 `internal/platform/`；入口组装依赖。具体错误 code 和 JSON 字段随 PR 1-3／1-4 的接口定义固定。

### PR 1-3：前台服务

- `internal/service/` 编排前台生命周期；入口注入构建信息、页面资源及就绪输出。`service run [--data-dir <绝对路径>]` 默认使用用户数据目录；显式目录便于隔离测试，端口仍固定。
- 顺序为校验／创建私有目录 → 取得 `service.lock` 内核锁 → 独占监听 IPv4 loopback → 初始化凭据 → 开始接收 HTTP 请求。任一步失败释放已取得的资源；锁文件保留，文件存在不表示服务存活。
- Windows 使用 `LockFileEx`，macOS 使用非阻塞 `flock`；句柄随进程存活，正常关闭或异常退出均释放锁。锁竞争返回 `already_running`，端口占用或 Windows 端口保留返回 `port_in_use`；不终止占用者或换端口。
- 支持 Ctrl+C／终止信号及 HTTP 管理停止，先停止接收请求并有界等待完成；关闭失败报错并清理本进程的 HTTP 资源，不强杀其他进程。后台启动及客户端停止等待在 PR 1-4 接入。

### PR 1-4：后台启动与复用

- `internal/client/` 提供 Ensure／Status／Stop。每次调用直连固定 IPv4 地址，使用新随机 challenge 验证 HMAC、API major 和必需能力；同一连接通过身份校验后才发送 Bearer。连接关闭后不在新连接上重试带凭据的请求，不使用代理、不跟随重定向；响应头及 JSON 大小有界。
- `service ensure|status|stop [--data-dir <绝对路径>] [--json]` 共用该 client。status 不创建目录／凭据或启动进程；无监听且目录未被持锁时返回 `stopped`，成功退出。stop 对已停止服务同样成功；监听不可验证或仍被持锁时不能报已停止。JSON 成功结果写 stdout，错误 `{code,message}` 写 stderr；参数错误仍返回 2。
- ensure 最多等待 15 秒，仅在服务未运行且锁空闲时启动一个候选进程；锁竞争输家继续等待胜者就绪。身份不符、接口不兼容、权限和启动错误明确失败；超时或调用者退出不强杀已启动的服务。
- `internal/platform/` 启动当前可执行程序的 `service run`：macOS 使用独立 session，Windows 使用 detached process／独立进程组并隐藏窗口。stdin／stdout 接空设备，stderr 使用独立、有界的启动诊断管道，不继承调用者 STDIO，也不写入持久启动日志。调用者结束时关闭诊断管道，服务继续运行。
- stop 仅在身份／兼容校验后读取 control 凭据并请求正常关闭；最多等 5 秒确认该目录的内核锁释放。HTTP 202 只表示已接收停止请求；超时返回 `stop_timeout`，不再次请求或按 PID 强杀。
- 默认单次 HTTP 操作限时 2 秒，JSON 最多 16 KiB、响应头最多 8 KiB。`identity_unverified`、`incompatible_service`、`service_unavailable`、`credential_error`、`startup_failed`／`startup_timeout` 和 `stop_timeout` 区分故障；完整 code 以实现及测试为准。

## 3. HTTP 边界

拟采用 `/api/v1/`，接口由 `internal/api/` 实现并通过注释生成文档。下表仅记录本轮协议方向，实际字段以相关 PR 的 Go 类型及注释为准。

| 接口 | 权限与作用 |
|---|---|
| `GET /api/v1/info` | 最小就绪、版本、能力和 challenge 响应；不返回目录、凭据或业务数据 |
| `GET /api/v1/status` | 本机 client 或浏览器管理权限；服务状态 |
| `POST /api/v1/control/stop` | 仅本机控制权限；正常停止 |
| `POST /api/v1/control/browser-grants` | 仅本机控制权限；创建一次性浏览器授权 |
| `POST /api/v1/browser/authorize` | 同 origin 且持有有效一次性凭据；换取浏览器管理 cookie |
| `POST /api/v1/browser/logout` | 浏览器管理权限；撤销当前浏览器授权 |
| 浏览器授权管理接口 | 本机控制权限；枚举非敏感授权标识、撤销授权，字段在 PR 1-6 固定 |

生产环境仅接受固定 Host；浏览器有 Origin 时必须精确匹配，拒绝 null 或其他 origin。无 Origin 的原生 client 仍需相应 Bearer 权限；以 cookie 执行变更的接口必须校验同 origin。静态页面可公开加载，业务／管理接口逐项鉴权，不启用跨站 CORS。

JSON 错误使用 `code`、`message`；校验失败、未认证、权限不足、冲突及内部错误分别映射适当 HTTP 状态。限制请求大小与读取时间，诊断错误不回传内部栈、密钥或带权限链接。

PR 1-3 实现前三个接口：info 公开最小就绪信息，status 接受 client 或 control Bearer，stop 仅接受 control Bearer。授权缺失／错误为 401，持有 client 却请求管理操作为 403。页面资源可公开加载；本 PR 不支持浏览器管理 cookie。

info 可携带一次 32 字节小写十六进制 `challenge`，响应以 client 密钥的原始 32 字节，对域标记 `wallet-bridge.info.v1\n`（末尾实际为换行）加不含 proof 的响应 JSON 计算 HMAC-SHA256。签名 JSON 使用 Go `encoding/json.Marshal` 的紧凑编码，字段按 `InfoResponse` 声明顺序，保留数组顺序，不含末尾换行。响应包含 challenge、接口版本与能力列表，proof 为小写十六进制；客户端校验在 PR 1-4 接入。字段与错误以 Go 类型／注释和生成的 Swagger 2.0 为准。

控制请求体最多 1 KiB；限制 Header 大小及读写时间，不启用 CORS，也不接受 URL 中的访问令牌。使用固定的 swag v1.16.6，从 `internal/api/` 注释生成 JSON／YAML 到 `docs/swagger/`，CI 重新生成并检查差异；本 PR 不接 Swagger UI。

## 4. 本机与浏览器凭据

| 凭据 | 保存与权限 |
|---|---|
| 本机 client 密钥 | 用户专属凭据文件；服务探测及普通接入。后续创建请求后另发资源凭据，不授予全局管理权 |
| 本机 control 密钥 | 独立的用户专属文件；停止服务、签发浏览器授权及撤销授权。普通 HTTP client 不接收此密钥 |
| 浏览器管理凭据 | 随机 cookie，HttpOnly、SameSite=Strict、host-only；服务端只保存摘要与到期、撤销信息 |

使用密码学安全随机值。macOS 数据目录 0700、凭据文件 0600；Windows 设置当前用户及 SYSTEM 的受限 ACL，不能用 chmod 成功代替 ACL 验证。初始化、文件替换和失败清理必须在对应权限下完成；不能保护凭据时失败。

本机密钥跨服务重启保留；浏览器授权在 SQLite 保存并跨重启复用。不会将管理密钥传入页面或 MCP 输出；资源访问权限按已确认的隔离方向在迭代 2～4 引入。

本轮防护覆盖普通网页跨站操作与无权限的 HTTP 接入；已控制当前系统账户的程序不在隔离承诺内。challenge 用于 CLI 识别端口服务，不为浏览器提供 HTTPS 级身份认证。

### PR 1-2：凭据文件

- `internal/credentials/` 负责独立的 `client.key`、`control.key`：各使用 32 字节安全随机值，保存为十六进制加换行；按类型单独读取，错误不包含密钥。
- `internal/platform/` 负责默认路径及受限目录／文件。创建时即设置权限，打开句柄后校验所有者与权限；拒绝目录本身或凭据文件为符号链接／Windows reparse point、错误类型和已有的不安全权限，不自动修复或轮换密钥。Windows API 使用 `golang.org/x/sys/windows` v0.50.0；macOS 同时拒绝扩展 ACL。
- 在同目录写入受限临时文件并同步、关闭，再以不可覆盖的硬链接原子发布；并发输家读取已发布的文件。不支持硬链接时明确失败，不退回可能暴露半写内容的方式。
- 原子性以单个文件为界；中断后只补缺失文件，保留有效文件。损坏文件报错，失败时清理本次临时文件，不删除其他文件。正常服务重启复用现有密钥。
- PR 1-2 仅提供内部 API；使用临时目录的真实文件及多进程测试验收。PR 1-3 的 `service run` 接入自动初始化；不增加凭据管理 CLI。

## 5. 浏览器授权流程

1. `ui` 确保服务就绪，使用本机 control 权限申请一次性凭据；默认打开外部浏览器，只有显式选择输出链接时才将带权限链接交给用户。
2. 一次性凭据放 URL fragment，避免进入 HTTP 路径／查询及 Referer。页面读取后立即清理地址，再以同 origin POST 交换。
3. 服务原子地消费一次性凭据，成功后创建管理授权并设置 cookie；过期、重放、服务重启后失效的临时凭据均拒绝。
4. 浏览器管理授权有效期从签发起固定 30 天，不滑动续期；到期、退出或本机撤销后重新执行授权流程。
5. 普通请求链接只定位资源；未授权浏览器显示本机授权入口说明，拥有请求查询凭据不能据此换取管理授权。

一次性凭据仅存在服务内存，设置数量上限并及时过期；持久化的浏览器授权独立编号，撤销不删除业务历史。清理网站数据或换浏览器配置后须重新授权。

### PR 1-6a：授权核心

- `internal/app/` 编排签发、交换、校验、列举与撤销；通过已有存储接口操作摘要和元数据，不依赖 HTTP／CLI／页面。入口权限沿用第 3 节，已确认的撤销入口见 spec 第 6 节。
- 一次性凭据和持久凭据分别使用 32 字节安全随机值，授权 ID 使用 16 字节随机值，均编码为小写十六进制。内存和数据库只保留凭据的 SHA-256 摘要；原值仅在签发／交换成功时返回给调用层。
- 一次性凭据有效 2 分钟，内存最多保留 64 个未到期记录；签发和交换时回收过期记录，满额明确报错。单个锁串行化签发、消费和撤销全部；交换在写入持久授权前消费凭据，存储失败或结果丢失后重新申请链接，不重放旧凭据。
- 授权期限从成功交换时起固定 30 天。单个撤销保留首次时间；全部撤销用一个 SQL 更新持久授权，成功后同时清空未消费的一次性凭据，避免旧链接重新授予管理权限。已有历史记录保留，其他实例在重启后也须查询持久撤销状态。
- 本 PR 用核心行为测试和真实 SQLite 组合测试验证上述边界；HTTP 接线、cookie 属性、命令和页面分别在后续三个 PR 交付。

### PR 1-6b：授权 HTTP

- 服务启动时构造一个授权核心并注入 API。接口版本为 1.1，增加 `browser.authorization` capability；既有 client 的状态／停止协议保持兼容。
- `POST /control/browser-grants` 使用 control 权限，返回一次性 `url`（`/#authorize=<ticket>`）和 `expiresAt`；`GET` 同路径分页列举授权元数据，`limit` 默认／最多 50，`after` 为上一页最后的授权 ID。响应不含 cookie 或摘要。
- `POST /control/browser-grants/revoke` 使用 control 权限，JSON 体的 `id` 与 `all: true` 二选一；成功返回 `state: revoked`。全部撤销同时作废未消费链接，沿用核心的串行化保证。
- `POST /browser/authorize` 以 JSON `ticket` 交换管理 cookie，只返回授权元数据；`POST /browser/logout` 只使用当前 cookie 撤销并清除它。两者要求精确同 origin，缺失 Origin 也拒绝；变更请求最多 1 KiB，拒绝查询参数、未知字段及多余 JSON。
- cookie 名 `wallet_bridge_browser`，Path 为 `/api/v1`，HttpOnly、SameSite=Strict、不设置 Domain；固定 HTTP loopback 下不设置 Secure。到期时间来自持久授权，查询不重发 cookie；服务端到期／撤销检查是最终依据。
- `/status` 接受本机 Bearer 或有效浏览器 cookie，使用 cookie 时附带当前授权元数据。显式 Authorization 优先，错误 Bearer 不回退 cookie；重复同名 cookie 拒绝。control 接口始终只接受 control Bearer。
- HTTP 边界、真实数据库和空 PATH 的原生服务测试覆盖交换／重放、权限、跨站、cookie 属性、重启和撤销；浏览器实际 cookie／页面行为在 PR 1-6d 验证。字段和错误 code 随 Go 类型／注释生成到 Swagger。

## 6. 存储、页面与构建

- SQLite 本轮仅引入 schema 版本、浏览器授权所需表及迁移；不预建 Session／交易表。采用 `modernc.org/sqlite v1.62.0`（纯 Go、`database/sql`），三个目标构建与真实读写证据记入 plan。
- React 页面先实现未授权说明、服务状态和退出；经注入的 `fs.FS` 由 HTTP 层提供。管理内容须经后端鉴权，前端路由不能充当权限边界。
- 完整构建先构建前端，再编译带页面资源的 Go 程序；后端独立测试使用明确的无 UI 构建模式。开发预览不通过放宽生产 Origin 规则接入真实服务。
- `internal/buildinfo/` 提供发行版本、Go／平台与 VCS 信息；CLI 的 `version --json` 同时报告是否内嵌页面。`web.Assets()` 返回页面资源，`no_web` 构建模式返回空资源集。
- 各功能 PR 随带必要测试。并发启动、权限失败、假服务探测、授权重放和跨站访问需要行为验证；工程骨架以构建及命令 smoke 验证，不为简单脚手架增加镜像式单元测试。
- 三平台 CI 覆盖实际可用的原生 runner；最低版本及缺失的目标环境另记实测证据。新系统 runner 和交叉编译均不能代替最低系统版本验收。

### PR 1-5：授权存储

- `internal/app/` 定义授权记录和使用方存储接口，`internal/store/` 实现 SQLite；本 PR 不增加授权流程、HTTP 或页面。记录仅含随机非敏感 ID、签发／到期／撤销时间；摘要作为写入／查找参数，不进入列表结果。原始 cookie 和一次性授权不落库。
- 数据保存为私有目录下的 `bridge.db`，启动先预建／验证数据库及 `bridge.db-journal` 的受限权限，再以 `mode=rw` 打开。单连接、`journal_mode=PERSIST`、`synchronous=FULL`、1 秒 busy timeout，临时表使用内存；保留回滚日志减少重建文件。SQLite 崩溃恢复可能删除已消费的日志，应用写入前重新预建／验证保护。当前不接 WAL，已有 WAL／SHM 文件或不安全路径明确拒绝，不自动删除／修复。
- schema 1 仅含 `browser_grants`：唯一 ID、唯一 SHA-256 token 摘要、UTC 毫秒时间戳。授权固定 30 天；查找要求签发时间已到、未撤销且尚未到期，不更新期限。撤销保留记录并保持首次撤销时间；用户入口在 PR 1-6 细化。
- 用 SQLite `application_id` 标识本项目，`user_version` 记录 schema；迁移与版本更新在同一事务提交。拒绝较新 schema、其他应用或未标记的非空数据库；损坏文件保留，失败不报告服务就绪。只追加迁移，不自动降级。
- 服务持锁且取得端口后，完成凭据及数据库初始化才接收 HTTP；数据库在释放服务锁前关闭。初始化失败返回 `storage_error`，后台 ensure 保留该 code。本 PR 用真实临时数据库验证迁移回滚、重开、到期／撤销、并发及文件保护，既有服务 smoke 同时验证无工具链启动。

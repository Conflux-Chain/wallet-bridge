# 本地服务开发与测试

当前提供前台／后台服务、进程锁、浏览器授权 HTTP／CLI／页面及授权存储。钱包接入按[迭代计划](iterations/01-local-service/plan.md)交付；页面手动与自动测试见[浏览器测试指南](browser-testing.md)，本轮环境证据与待办见[验收记录](iterations/01-local-service/acceptance.md)。

## 前置条件

使用 Windows amd64 或 macOS amd64／arm64，按 [README 环境准备](../README.md#环境准备)安装工具和前端依赖。以下命令均在仓库根目录运行；固定端口 `127.0.0.1:18765` 须空闲。

## 自动验证独立程序

```sh
npm --prefix web run bundle
npm --prefix web run service:smoke
npm --prefix web run background:smoke
```

预期分别输出 `Service smoke passed`、`Background smoke passed`，退出码均为 0。测试在空 PATH 下运行打包程序：前者检查单服务密钥、HTTP、资源、锁、SQLite、一次性授权交换／重放拒绝、cookie 跨重启复用／退出及停止；后者检查六个 CLI 并发 ensure、常驻、复用、重启、停止、授权 CLI、损坏数据库保留及假服务拒绝。macOS 另验前台 SIGTERM 正常退出。

脚本使用独立临时数据目录，结束时停止自身服务并清理目录，不初始化正式用户凭据。遇到已有端口占用时明确失败，不停止占用者。后台测试如无法确认自身服务已停止，会保留目录并报告错误，供排查。

## 服务接入兼容性测试

固定端口已有服务时，可独立运行此组测试。仅需上述 Go 环境，在仓库根目录执行，无需构建页面、启动服务或准备密钥：

```sh
go test -tags no_web ./internal/credentials ./internal/api ./internal/client ./internal/service -count=1 -v
```

预期退出码 0：新装只生成 `client.key`，重启／并发初始化复用；旧目录保留原密钥及遗留控制文件，但控制密钥不能鉴权。查询、停止、浏览器授权管理使用同一服务凭据；浏览器 cookie 不能停止服务或管理全部授权，错误 Host／Origin 仍被拒绝。模拟旧版服务通过身份验证后仍返回 `incompatible_service`，不收到 Bearer。

测试使用自动清理的私有临时目录、随机空闲端口及模拟 HTTP，不接触正式数据和固定端口；命令结束即完成停止与清理。这是程序及接口验证，不包含真实钱包连接。API 1.2 增加 `service.auth.shared`；跨版本切换须先使用服务配套程序正常停止旧服务，见[凭据说明](local-credentials.md#保存与恢复)。

## 后台启动、查询与停止

完成构建后，以 Windows PowerShell 为例：

```powershell
.\dist\wallet-bridge-windows-amd64.exe service status --json
.\dist\wallet-bridge-windows-amd64.exe service ensure --json
.\dist\wallet-bridge-windows-amd64.exe service status --json
.\dist\wallet-bridge-windows-amd64.exe service stop --json
```

macOS 将程序路径替换为 `./dist/wallet-bridge-darwin-arm64`（Intel 为 `darwin-amd64`），其余参数相同。

预期：原本未运行时，首个 status 返回 `state: stopped`，且不创建目录／凭据；ensure 返回 `running` 及服务信息，CLI 随后退出。再次 status 仍为 `running`，重复 ensure 保持相同 `startedAt`。stop 在确认服务锁释放后返回 `stopped`；对已停止的服务再次 stop 仍成功。上述命令退出码均为 0。

命令可追加 `--data-dir <绝对路径>`；同一组操作使用同一目录，端口始终固定。停止后保留数据，重启复用凭据。只执行后台 ensure／status 的终端可直接关闭，服务继续运行；使用 stop 明确停止。

ensure 最多等待 15 秒，stop 最多 5 秒。超时或调用者退出不会强杀候选／服务；之后先查询 status。运行错误返回 1；带 `--json` 时，错误 `{code,message}` 只写 stderr，成功结果只写 stdout；参数错误返回 2。

`identity_unverified` 表示无法确认端口服务身份；`incompatible_service` 表示接口或能力不兼容，须用匹配版本程序的 stop（或原前台终端 Ctrl+C）正常停止，再启动新版本。这两类失败均不发送访问令牌或自动替换服务。`service_unavailable` 表示仍持锁或暂时无法应答；后台初始化失败可用同目录的 `service run` 查看错误。

## 手动启动与访问

先完成上述构建，按当前系统运行一个命令：

```powershell
# Windows PowerShell
.\dist\wallet-bridge-windows-amd64.exe service run
```

```sh
# macOS Apple Silicon；Intel 使用 wallet-bridge-darwin-amd64
./dist/wallet-bridge-darwin-arm64 service run
```

保持终端运行，预期输出 `wallet-bridge ready at http://127.0.0.1:18765`。浏览器打开：

- [页面](http://127.0.0.1:18765/)：未授权时显示「需要浏览器授权」及 CLI 命令；授权后显示服务状态与当前授权信息。
- [公开服务信息](http://127.0.0.1:18765/api/v1/info)：JSON 中 `name` 为 `wallet-bridge`、`ready` 为 `true`，并含版本与能力。
- [受保护状态接口](http://127.0.0.1:18765/api/v1/status)：未授权浏览器直接打开返回 HTTP 401 和 `unauthenticated`，属于预期鉴权行为；有效浏览器 cookie 可读取当前授权及服务状态。

在启动终端按 `Ctrl+C` 正常停止。手动启动会在[用户数据目录](local-credentials.md#用户需要准备什么)自动初始化并保留凭据及 `service.lock`；重启复用凭据，锁文件存在不代表服务仍在运行。需要隔离数据时，可追加 `--data-dir <尚未创建的绝对目录>`，其父目录须已存在；停止后该目录仍保留。端口不随目录变化。

启动失败显示错误 code；`already_running` 表示该数据目录正在被另一服务持锁，`port_in_use` 表示固定端口被占用或保留。应先自行检查占用原因，再重新启动。

## 接口文档与后端检查

[Swagger JSON](swagger/swagger.json)／[YAML](swagger/swagger.yaml)由 `internal/api/` 的类型与 handler 注释生成，包含服务、一次性链接、授权交换／退出、授权列表及撤销接口；暂不提供 Swagger UI。

```sh
npm --prefix web run swagger
go test -tags no_web ./internal/api ./internal/platform ./internal/service ./internal/client ./internal/cli -count=1 -v
```

生成步骤应成功退出；行为测试应通过，临时目录自动清理。Windows 符号链接测试可能因创建权限不足而跳过，规则同 [README 凭据测试](../README.md#本机凭据测试)。修改接口注释后重新生成并提交 JSON／YAML；CI 检查是否同步。

## 浏览器授权 CLI

完成构建后，在仓库根目录运行（Windows PowerShell）：

```powershell
.\dist\wallet-bridge-windows-amd64.exe ui --print-url
.\dist\wallet-bridge-windows-amd64.exe browser list --limit 1 --json
.\dist\wallet-bridge-windows-amd64.exe service stop --json
```

macOS 使用 `./dist/wallet-bridge-darwin-arm64`（Intel 为 `darwin-amd64`），其余参数相同。`ui` 自动启动或复用服务；`--print-url` 只输出两分钟内有效的一次性链接。省略该选项时向系统默认浏览器提交打开请求，终端不显示链接。需要用特定 Chrome 配置时，可将显式输出的链接粘贴到对应窗口。

预期三个命令均返回 0：链接使用固定 origin 和 `#authorize=` fragment；list 返回至多一条授权元数据及可选 `nextAfter`；stop 返回 `stopped`。单纯签发链接不会新增授权；在服务运行时将链接粘贴到目标 Chrome 配置，页面清除 fragment 并交换授权，随后显示「浏览器已授权」。完整顺序与退出验证见[浏览器测试指南](browser-testing.md#手动验证授权页面)。

`browser list` 省略 `--json` 时显示表格；使用 `--after <nextAfter>` 取下一页。`browser revoke <id>` 撤销单个授权，`browser revoke --all` 撤销全部授权及未消费链接；都支持 `--json`。这些管理命令不启动服务，未运行时返回 `service_not_running`、退出码 1。运行错误写 stderr；参数错误返回 2。浏览器启动失败返回 `browser_open_failed`，按提示重新运行 `ui --print-url`；链接过期时重新签发，达到限额时等待两分钟再试。

上述命令可追加同一个绝对 `--data-dir` 以隔离数据；默认数据位置见[凭据说明](local-credentials.md#用户需要准备什么)。后台服务不会随 CLI 退出而停止，结束时使用同目录的 `service stop`；数据保留以供重启复用。

开发者可运行以下测试，无需启动服务或浏览器，临时数据库自动清理：

```sh
go test -tags no_web ./internal/client ./internal/cli -run 'Test(Browser|UI)' -count=1 -v
```

预期身份／能力校验、链接输出隔离、启动失败、分页与撤销测试通过。上述 `background:smoke` 另用独立程序验证 `ui --print-url` 自动启动、真实 HTTP 交换、分页、重启及单个／全部撤销；不会打开用户浏览器。实际页面测试使用 Playwright，见[浏览器测试指南](browser-testing.md#自动浏览器测试)；系统默认浏览器的实际启动另行验收。

## 浏览器授权 HTTP 测试

前置环境与工作目录同上；无需启动服务、浏览器或准备凭据：

```sh
go test -tags no_web ./internal/api -run '^TestBrowserHTTP' -count=1 -v
```

预期：真实 SQLite 上的交换／重放拒绝、Origin／Host、Bearer／cookie 权限、重复 cookie、到期／撤销、重启、分页、链接数量上限及存储失败测试通过。测试自动清理临时数据库；授权 30 天到期用过期记录验证，无需等待。独立可执行程序的真实 HTTP 流程另由上述 `service:smoke` 验证，停止与清理规则相同。

HTTP、CLI 和页面均已接入。HTTP 客户端测试与[浏览器测试](browser-testing.md)分别记录，不代替实际钱包或用户验收。

## SQLite 存储测试与数据

前置环境与工作目录同上，无需启动服务或浏览器：

```sh
go test -tags no_web ./internal/app ./internal/store -count=1 -v
```

预期：授权核心的一次性凭据到期／限额／并发交换、失败不能重放、持久授权重开／退出／撤销，以及存储层的迁移回滚、schema 拒绝和真实子进程崩溃恢复测试通过，输出实际 SQLite 版本。测试使用可控时钟与临时数据库，无需等待 30 天，结束自动清理；Windows 符号链接权限不足时该项跳过，其他权限检查仍执行。

正式服务会在用户数据目录生成 `bridge.db` 和 `bridge.db-journal`，启动迁移完成后才报告就绪。schema v4 的[连接、Session 及执行记录](connection-storage.md)按 ID 定位，不保存资源凭据；浏览器授权仍保存 cookie 摘要及元数据，不保存钱包私钥或原始 cookie。当前 CLI／页面尚不创建连接，正常停止和重启保留数据。

`storage_error` 表示数据库初始化、迁移或状态维护失败；启动失败不报告就绪，运行中维护失败会停止服务并保留原数据。统一恢复与维护规则见[连接测试指南](connection-storage.md#数据与交付边界)。损坏文件、不安全权限、较新 schema 或不支持的日志文件不会被自动清理或降级；先用同目录 `service run` 排查，保留原数据。恢复或替换数据文件须在服务停止后进行。

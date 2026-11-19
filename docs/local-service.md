# 本地服务开发与测试

当前提供前台／后台服务、进程锁、HTTP 基础接口、内嵌页面及授权存储。浏览器授权流程及钱包接入按[迭代计划](iterations/01-local-service/plan.md)交付。

## 前置条件

使用 Windows amd64 或 macOS amd64／arm64，按 [README 环境准备](../README.md#环境准备)安装工具和前端依赖。以下命令均在仓库根目录运行；固定端口 `127.0.0.1:18765` 须空闲。

## 自动验证独立程序

```sh
npm --prefix web run bundle
npm --prefix web run service:smoke
npm --prefix web run background:smoke
```

预期分别输出 `Service smoke passed`、`Background smoke passed`，退出码均为 0。测试在空 PATH 下运行打包程序：前者检查 HTTP、资源、锁、SQLite 初始化与控制停止；后者检查六个 CLI 并发 ensure、常驻、复用、重启、停止、损坏数据库保留及假服务拒绝。macOS 另验前台 SIGTERM 正常退出。

脚本使用独立临时数据目录，结束时停止自身服务并清理目录，不初始化正式用户凭据。遇到已有端口占用时明确失败，不停止占用者。后台测试如无法确认自身服务已停止，会保留目录并报告错误，供排查。

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

- [页面](http://127.0.0.1:18765/)：显示「Wallet Bridge」静态说明页。
- [公开服务信息](http://127.0.0.1:18765/api/v1/info)：JSON 中 `name` 为 `wallet-bridge`、`ready` 为 `true`，并含版本与能力。
- [受保护状态接口](http://127.0.0.1:18765/api/v1/status)：直接打开返回 HTTP 401 和 `unauthenticated`，属于预期鉴权行为。

在启动终端按 `Ctrl+C` 正常停止。手动启动会在[用户数据目录](local-credentials.md#用户需要准备什么)自动初始化并保留凭据及 `service.lock`；重启复用凭据，锁文件存在不代表服务仍在运行。需要隔离数据时，可追加 `--data-dir <尚未创建的绝对目录>`，其父目录须已存在；停止后该目录仍保留。端口不随目录变化。

启动失败显示错误 code；`already_running` 表示该数据目录正在被另一服务持锁，`port_in_use` 表示固定端口被占用或保留。应先自行检查占用原因，再重新启动。

## 接口文档与后端检查

[Swagger JSON](swagger/swagger.json)／[YAML](swagger/swagger.yaml)由 `internal/api/` 的类型与 handler 注释生成，包含三个基础接口；暂不提供 Swagger UI。

```sh
npm --prefix web run swagger
go test -tags no_web ./internal/api ./internal/platform ./internal/service ./internal/client ./internal/cli -count=1 -v
```

生成步骤应成功退出；行为测试应通过，临时目录自动清理。Windows 符号链接测试可能因创建权限不足而跳过，规则同 [README 凭据测试](../README.md#本机凭据测试)。修改接口注释后重新生成并提交 JSON／YAML；CI 检查是否同步。

## SQLite 存储测试与数据

前置环境与工作目录同上，无需启动服务或浏览器：

```sh
go test -tags no_web ./internal/store -count=1 -v
```

预期：迁移回滚、较新／未知 schema 拒绝、重复／并发写入、固定到期、撤销重开和真实子进程崩溃恢复测试通过，输出实际 SQLite 版本。测试使用临时数据库并自动清理；Windows 符号链接权限不足时该项跳过，其他权限检查仍执行。

正式服务会在用户数据目录生成 `bridge.db` 和 `bridge.db-journal`，启动迁移完成后才报告就绪。当前只存浏览器授权摘要及元数据，授权交换接口尚未接入；不会保存钱包私钥或原始 cookie。正常停止和重启保留数据。

`storage_error` 表示数据库初始化／迁移失败，ensure 不会将其当作就绪。损坏文件、不安全权限、较新 schema 或不支持的日志文件不会被自动清理或降级；先用同目录 `service run` 排查，保留原数据。恢复或替换数据文件须在服务停止后进行。

# 本地服务开发与测试

PR 1-3 提供前台服务、进程锁、HTTP 基础接口及内嵌页面。后台 ensure／status／stop 命令、浏览器授权及钱包接入按[迭代计划](iterations/01-local-service/plan.md)交付。

## 前置条件

使用 Windows amd64 或 macOS amd64／arm64，按 [README 环境准备](../README.md#环境准备)安装工具和前端依赖。以下命令均在仓库根目录运行；固定端口 `127.0.0.1:18765` 须空闲。

## 自动验证独立程序

```sh
npm --prefix web run bundle
npm --prefix web run service:smoke
```

预期输出 `Service smoke passed`，退出码为 0。测试在空 PATH 下运行打包程序，检查 HTTP 鉴权、Host／Origin、页面与 JavaScript 资源、锁竞争、端口冲突、重启复用凭据及控制停止；macOS 另验 SIGTERM 正常退出。

脚本使用独立临时数据目录，结束时关闭自身启动的进程并清理该目录，不初始化正式用户凭据。遇到已有端口占用时明确失败，不停止占用者。

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
go test -tags no_web ./internal/api ./internal/platform ./internal/service ./internal/cli -count=1 -v
```

生成步骤应成功退出；行为测试应通过，临时目录自动清理。Windows 符号链接测试可能因创建权限不足而跳过，规则同 [README 凭据测试](../README.md#本机凭据测试)。修改接口注释后重新生成并提交 JSON／YAML；CI 检查是否同步。

# 发布与升级设计草案

## 已有实现

现有构建脚本支持三种原生目标，嵌入页面及版本信息。`version --json` 可读取平台、发行版本、源码修订和页面打包状态。每个平台已有独立程序 smoke，可用于检验发行产物。[构建脚本](../../../scripts/build.mjs) · [版本信息](../../../internal/buildinfo/info.go)

HTTP client 已验证服务身份、API 兼容性和所需能力。服务停止时，`Ensure` 会启动当前 client 的配套程序。SQLite 在服务就绪前执行迁移，并拒绝过新的 schema。升级需补充启用版本的选择及跨版本恢复。[服务启动](../../../internal/client/lifecycle.go) · [数据库打开与迁移](../../../internal/store/store.go)

## 本地发行构建

在 Windows 或 macOS 的仓库根目录按 [README 环境准备](../../../README.md#环境准备)安装依赖，然后运行：

```sh
npm --prefix web run release:build -- v0.0.0-local.1
```

版本参数使用带 `v` 前缀的语义化版本。命令在 `dist/releases/v0.0.0-local.1/` 生成三个独立程序和 `SHA256SUMS`。清单每行包含十六进制 SHA-256、两个空格及文件名。构建完成后整组移入版本目录，已有同名目录会提示冲突。

在 Windows PowerShell 中从仓库根目录核对：

```powershell
Push-Location dist/releases/v0.0.0-local.1
try {
  foreach ($line in Get-Content SHA256SUMS) {
    $digest, $name = $line -split '  ', 2
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $name).Hash -ne $digest) {
      throw "Checksum mismatch: $name"
    }
  }
  .\wallet-bridge-windows-amd64.exe version --json
} finally { Pop-Location }
```

在 macOS 的仓库根目录运行下列命令。Intel 机器将 `arm64` 换为 `amd64`：

```sh
(cd dist/releases/v0.0.0-local.1 && shasum -a 256 -c SHA256SUMS && ./wallet-bridge-darwin-arm64 version --json)
```

预期三个文件摘要都匹配，本机程序输出 `version: v0.0.0-local.1`、对应 `platform` 和 `uiEmbedded: true`。命令退出即完成测试。清理时删除此次生成的版本目录即可。[首版下载信任来源](spec.md#下载校验)

## 查询可升级版本

`wallet-bridge update check [--include-preview] [--json]` 比较当前 CLI 的内嵌版本与本仓库公开的 GitHub Releases。分页读取完整列表后按语义化版本选择，默认只看稳定版。查询独立运行，访问网络超时或 GitHub 返回限流时以退出码 1 报错。

开发者在 Windows 或 macOS 仓库根目录，使用 [go.mod](../../../go.mod) 指定的 Go 版本运行：

```sh
go run -tags no_web ./cmd/wallet-bridge update check --json
go run -tags no_web ./cmd/wallet-bridge update check --include-preview
go test ./internal/update ./internal/cli -run 'TestRelease|TestUpdateCheck'
```

前两条需要能访问 GitHub API。仓库暂无发行版时，第一条返回 `state: no_release`，第二条显示 `No published release available.`。已有候选时，`latest` 为选出的版本，`update_available` 表示比当前版本新，`up_to_date` 表示无需升级。当前为 `dev` 时仍可查看最新版本，状态为 `current_unversioned`。最后一条用模拟发行列表核对分页、版本排序、草稿排除与查询错误。命令退出即结束，无需启动或清理本机服务。

## 下载字节校验

按指定标签读取本仓库的公开 Release，从其元数据取得清单和程序的附件 ID、长度与 SHA-256。通过官方资产 API 下载文件，二进制下载允许跳转到 GitHub 附件 CDN。先核对清单的字节长度与摘要，找到目标程序唯一对应的一行。两处程序摘要一致后流式读取程序，并核对实际长度和摘要。调用方须丢弃失败的临时输出。

在仓库根目录用项目指定的 Go 版本运行 `go test ./internal/update -run TestDownload -count=1 -v`。测试以模拟 GitHub 响应验证成功、截断、超长、文件被改、重复条目、错误标签和跳转范围。预期所有用例通过，进程退出即完成测试。

## 下载候选版本

从 `update check` 的 `latest` 取得目标标签后，运行 `wallet-bridge update download <版本号> [--data-dir <绝对路径>] [--json]`。成功输出版本、文件路径与 SHA-256。命令读取 Windows PE 或 macOS Mach-O 的平台信息，核对通过后将文件保存到用户数据目录下的 `versions/<版本号>/`。启用版本由后续切换步骤处理。

再次下载时，先从官方仓库重取清单，再核对本地候选。内容相同可直接复用，已有文件内容冲突会报错。并发下载通过同一文件系统的硬链接提交完整文件。

在 Windows 或 macOS 仓库根目录使用项目指定的 Go 版本运行：

```sh
go run -tags no_web ./cmd/wallet-bridge update download --help
go test ./internal/update ./internal/cli -run 'TestStage|TestUpdateDownload' -count=1 -v
```

第一条输出命令用法。第二条用模拟下载源和真实原生测试程序验证并发、复用和冲突，并检查错误架构的候选无法保存。用例自动清理临时目录。首次公开发布前，向真实仓库请求不存在的标签会返回 HTTP 404，退出码为 1。

## 本机安装

`wallet-bridge install [--data-dir <绝对路径>] [--json]` 将当前程序复制到用户数据目录的 `bin/wallet-bridge`（Windows 加 `.exe`）。普通输出为绝对路径，JSON 输出使用 `path` 字段。先将完整副本写入同目录的临时文件，再用硬链接提交。重复安装内容相同的程序返回已有路径，内容冲突时报告错误并保留原文件。

默认路径为 Windows 的 `%LOCALAPPDATA%\WalletBridge\bin\wallet-bridge.exe` 或 macOS 的 `~/Library/Application Support/WalletBridge/bin/wallet-bridge`。用输出的路径运行 `version --json` 可检查版本。[Codex 配置示例](../../codex.md#注册-mcp)

在 Windows 或 macOS 的仓库根目录完成 [README 环境准备](../../../README.md#环境准备)，运行以下隔离测试。

```sh
npm --prefix web run bundle
npm --prefix web run smoke
go test ./internal/update -run TestInstall -count=1 -v
```

`smoke` 在临时目录安装打包程序、删除原副本，再从固定路径查询版本并通过 STDIO 列出 MCP 工具。预期输出 `Installed CLI and MCP discovery passed`。Go 用例检查默认路径、并发复用与内容冲突。两个测试退出后自动清理临时目录。

## 拟实施的切换顺序

1. **准备版本。** 根据发行渠道与本机平台选择目标，按 D-071 校验后置于独立版本目录。确认启用前保留当前版本。
2. **取得切换权。** 服务端在同一协调边界内检查钱包占用并暂停新工作，防止检查后又领取请求。保留已交给钱包的回报入口。两个升级者并发或旧 CLI 同时启动时，只允许一次服务切换。
3. **切换与恢复。** 旧服务停止写入后形成可还原的数据状态。新版完成迁移及就绪检查，再开放业务并记录启用版本。开放前失败可恢复旧程序和配套数据。开放后已有新请求或迟到哈希时，恢复不能用旧快照覆盖这些记录。具体持久化顺序和断电恢复点在切换 PR 前评审。
4. **重连。** CLI／MCP 从启用版本启动服务，在原地址重新校验身份与能力。浏览器重读原请求及钱包调用状态，已有发送许可的请求禁止再次调用钱包。固定的 Codex 命令路径应在升级后仍能接入。

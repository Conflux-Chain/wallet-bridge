# 发布与升级设计草案

## 已有实现

现有构建脚本支持三种原生目标，嵌入页面及版本信息。`version --json` 可读取平台、发行版本、源码修订和页面打包状态。每个平台已有独立程序 smoke，可用于检验发行产物。[构建脚本](../../../scripts/build.mjs) · [版本信息](../../../internal/buildinfo/info.go)

HTTP client 已验证服务身份、API 兼容性和所需能力。SQLite 在服务就绪前执行迁移，并拒绝过新的 schema。[服务启动](../../../internal/client/lifecycle.go) · [数据库打开与迁移](../../../internal/store/store.go)

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

## 启动已启用的服务

CLI 和 MCP 先复用兼容的运行中服务。需要启动时，共用同一选择顺序。

1. 有 `active/` 时，从私有文件 `active/selection.json` 读取发行标签和 SHA-256，在 `versions/<标签>/` 校验对应平台的程序。记录或目标损坏时显示错误。
2. 已执行首次安装时使用 `bin/` 中的固定程序。
3. 直接运行且从未启用版本时，使用当前程序。仅下载候选文件不会启用它。

服务取得数据目录锁后，在监听端口和打开数据库前复核程序。当前程序与启用程序内容不同时，前台调试显示可执行的正确路径。写入或变更启用记录由 6-8 的切换流程负责。

在 Windows 或 macOS 的仓库根目录完成 [README 环境准备](../../../README.md#环境准备)，确保本机端口 18765 空闲，运行以下命令。

```sh
npm --prefix web run bundle
go test ./internal/update -run TestServiceProgramSelection -count=1
go test -tags integration ./internal/update -run TestPackagedServiceStartup -count=1 -v
```

集成测试生成第二份带版本号的程序，在临时目录模拟下载和启用记录。`foreground` 检查旧程序给出正确路径且数据库尚未创建，`background` 检查旧 CLI 与固定路径的 MCP 分别启动所选版本。预期两项均为 `PASS`，退出时停止测试服务并清理临时目录。端口已占用时可在最后一条的测试名后追加 `/foreground`，单独验证前台调试。

## 升级停机检查

本机升级客户端使用服务凭据调用 `POST /api/v1/control/stop-for-upgrade`。钱包被连接或交易占用时返回 HTTP 409、请求类型、ID 和阶段，服务继续接单。空闲时暂停新请求与钱包领取，返回 HTTP 202 并进入现有的优雅停机流程。调用结果以这次检查为准，后续升级者须重新发起请求。

同一把读写锁协调请求创建、钱包领取／恢复及升级检查。占用依据与管理页一致，排队等待审阅的请求仍可在重启后处理。已发起的结果回报沿用服务的优雅退出流程。

在 Windows 或 macOS 仓库根目录使用项目指定的 Go 版本运行 `go test ./internal/api -run TestUpgradeStop -count=20 -v`。用例使用隔离的 SQLite 和内存 HTTP 请求，核对占用信息、用户主动重试、并发领取与停机，以及停机期间的迟到哈希回报。预期全部通过，退出时自动清理。用户主动升级命令及跨进程切换在 6-8 接入。

## 客户端检查升级停机

`service stop --for-upgrade` 显式执行升级的停机步骤。客户端先确保服务就绪，再验证 API 1.15 和 `service.upgrade-stop` 能力。钱包占用时输出请求类型、ID、阶段和处理链接。停机应答丢失或等待超时时，本次升级检查以错误结束。执行普通停机仍使用 `service stop`。

脚本可加 `--json`。占用时退出 1，stderr 的 `code` 为 `wallet_busy`，`activeWalletRequest` 包含 `kind`、`id` 和 `phase`。处理后使用原命令重试。退出 0 且 `state` 为 `stopped` 表示已收到应答并观察到锁释放。独占整个升级过程由后续切换步骤负责。

在 Windows 或 macOS 的仓库根目录，运行 `go test ./internal/client -run TestUpgradeStop -count=1 -v` 验证旧服务、无效响应和停机超时。测试使用临时目录并自动清理。端口 18765 空闲时，按 [README 环境准备](../../../README.md#环境准备)安装依赖，运行以下打包程序回归。

```sh
npm --prefix web run bundle
npm --prefix web run background:smoke
```

用例模拟一次尚未返回的连接钱包调用，停止测试服务，再让升级命令恢复服务。预期能按原请求 ID 找到占用，回报原调用结束后主动重试可停机。成功输出 `Background smoke passed`，最后清理测试服务及数据目录。

## 创建升级备份

备份期间持有服务数据目录锁。确认回滚日志已失效或不存在后，将原凭据和数据库逐字节复制到本次升级的私有临时目录。用 SQLite 校验副本的应用标识、schema 版本、完整性和外键，写入 `SHA256SUMS` 后将目录改名为 `backups/<升级 ID>/`。发现 WAL、SHM 或待恢复的回滚日志时终止。重试可重建同一 ID 的临时目录，已有完整备份始终保留。

在 Windows 或 macOS 仓库根目录使用 `go.mod` 指定的 Go 版本运行：

```sh
go test ./internal/store ./internal/update -run 'TestUpgradeBackup|TestCrashRecoversUncommittedAuthorizationChanges' -count=1 -v
```

预期两个包均为 `ok`。用例从副本查询浏览器授权、Session 和已记录的交易哈希，比较旧 schema 的原始字节，并检查锁占用、写入中断、损坏数据与重复备份。所有文件均在测试临时目录内，退出时自动清理。

## 恢复与清理升级备份

回退先在本次备份的私有工作目录还原凭据和数据库，重新校验 SQLite 和两份 SHA-256。版本切换流程负责确认仍可回退、接入已验证的数据，并在旧版就绪且回退结果已持久化后报告成功。准备失败或回退中断时保留原备份。成功后先将备份目录改名为待清理目录，再删除；删除中断后可继续清理。新版成功启用后走相同的清理步骤。

在 Windows 或 macOS 仓库根目录使用项目指定的 Go 版本运行：

```sh
go test ./internal/update ./internal/api -run TestUpgradeRollback -count=1 -v
```

预期两个包均为 `ok`。用例在工作副本中使用原浏览器授权和请求 ID 查回交易，模拟回退进程退出、篡改备份和清理中断。测试自动移除临时数据，全程无需启动本机服务或连接钱包。实际版本切换与服务开放时点在 6-8 验证。

## 版本切换与中断恢复

升级命令独占本次切换。普通服务启动在持有数据锁、打开数据库前核对升级阶段。恢复时沿用原升级 ID、旧版程序及目标摘要。直接运行的旧版也须在停机前保存可校验的程序副本。

| 阶段 | 动作与恢复依据 |
|---|---|
| 准备停机 | 校验候选和可回退程序。持久记录切换意图，允许升级命令启动旧版以检查钱包。占用时撤销本次意图，普通请求继续使用旧版。停机应答丢失时重新取得占用检查结果。 |
| 已停机 | 确认旧服务退出后持锁创建备份。重启时校验同一 ID 的已有备份，完整备份就绪后才允许迁移。 |
| 验证新版 | 候选进程独占数据锁，在原地址绑定端口、迁移并核对服务身份与能力。业务入口保持关闭，包括交易结果回报。验证失败时在此门限内恢复配套数据和旧程序，核实旧版可用后执行 D-078。 |
| 允许接单 | 先发布启用版本记录，再持久提交此次接单决定，之后才开放业务入口。两次写入之间中断，仍按开放前阶段恢复。接单决定一旦提交，只可继续启动该版本、重连和清理，防止旧快照覆盖迟到哈希。 |

接单决定提交前中断，普通连接按 D-077 给出恢复命令。恢复命令从持久阶段继续。接单决定提交后，启动时按该决定复核启用记录。验收须覆盖进程在相邻两步之间退出的情形。

## 写入启用版本记录

启用记录只保存发行标签与 SHA-256。读写双方按相同规则从本机版本目录定位并检查平台程序。首次发布时把写好的私有记录连同 `active/` 一次移入，已有记录则用完整文件替换。候选校验失败时保留先前选择。切换流程负责协调写入者及业务开放时点。

在 Windows 或 macOS 仓库根目录使用 `go.mod` 指定的 Go 版本运行：

```sh
go test ./internal/update -run 'TestPublishSelection|TestServiceProgramSelection' -count=1 -v
```

预期两项均通过。用例模拟首次写入中断、更换及重复选择版本、校验失败和读取过程中记录被替换。测试退出时自动清理临时目录。[打包 CLI 与 MCP 启动验证](#启动已启用的服务)

## 接单前的升级协调

升级命令持有独立的本机进程锁。停机前写入私有的 `upgrade.json`，保留升级 ID、旧版和目标程序的版本与 SHA-256。记录依次进入准备停机、已停机、验证新版三个阶段，每次替换完整记录后才执行下一阶段。升级进程退出会释放锁，后继升级者先读取原记录。

普通 CLI／MCP 建连在探测服务前检查，前台和后台服务持有数据目录锁后再次检查。正在升级返回 `upgrade_in_progress`，中断且仍有记录返回 `upgrade_recovery_required`。记录损坏时报告读取错误。接单决定和回退结果分别在 6-8d、6-8e 接入。

在 Windows 或 macOS 仓库根目录完成 [README 环境准备](../../../README.md#环境准备)，运行：

```sh
npm --prefix web run bundle
go test ./internal/update -run '^TestUpgradeCoordination$' -count=1 -v
go test -tags integration ./internal/update -run '^TestPackagedServiceStartupDuringUpgrade$' -count=1 -v
```

预期两项 Go 测试均通过。第一项让写入升级阶段的子进程退出，再核对原 ID、版本与阶段。第二项通过打包程序运行 `connect`、`service run` 及 STDIO MCP 连接工具，检查两种提示。测试在临时数据目录运行，退出时自动清理，无需空出钱包服务端口。

## 停机前留存旧程序

准备升级时，先按记录摘要校验候选，再按普通启动顺序定位当前程序。将旧程序独立复制到私有的 `rollback-program/`，核对平台和 SHA-256，并运行副本的 `version --json` 取得实际版本。已有启用记录时还须与其版本和摘要一致。两份程序就绪后才记录升级意图。同一次升级恢复时按原 ID 和摘要读取副本，用户可清理原下载文件。

在 Windows 或 macOS 仓库根目录完成 [README 环境准备](../../../README.md#环境准备)，运行：

```sh
npm --prefix web run bundle
go test -tags integration ./internal/update -run '^TestPackagedServiceStartupRollbackProgram$' -count=1 -v
```

预期三组用例均为 `PASS`。测试删除临时下载文件后执行保留的副本，使用两个版本核对中断恢复，并检查损坏副本和错误版本标签。测试自行清理临时目录，无需启动钱包服务或关闭已打开的钱包。

## 从钱包占用检查到备份

升级者在准备停机阶段可启动留存的旧程序。子进程取得数据锁后复核升级锁、升级 ID、阶段及自身摘要。客户端在已验证身份的同一连接上核对服务版本，再请求检查钱包。钱包占用或服务不兼容时结束本次升级，普通客户端恢复访问。停机应答丢失时保留准备阶段，显式重试会重新检查钱包。

确认停机后记录已停机阶段，持数据锁创建备份。备份已写完而阶段更新中断时，恢复会校验同一 ID 的原备份，成功后才进入验证新版阶段。

在 Windows 或 macOS 仓库根目录完成 [README 环境准备](../../../README.md#环境准备)，运行：

```sh
npm --prefix web run bundle
go test -tags integration ./internal/update -run '^TestPackagedServiceStartupBackupResume$' -count=1 -v
go test -tags integration ./internal/update -run '^TestPackagedServiceStartupWalletCheckAndBackup$' -count=1 -v
```

第一项 Go 测试模拟停机应答丢失和备份发布后中断。第二项要求本机 18765 端口空闲，在临时目录启动打包服务，恢复一笔模拟钱包占用，再验证用户重试后停机并保存备份。预期两项均输出 `PASS`，退出时清理测试服务及临时数据。

## 验证候选服务

升级者校验目标文件后，用显式启动参数传入升级 ID。候选取得数据锁，核对升级阶段、版本和自身摘要，在固定地址加载原凭据、打开并迁移数据库。API 1.16 的身份应答在验证期间包含 `verifyingUpgrade` 和 `ready: false`。升级者验证签名、原升级 ID、目标版本及所需能力。此时仅允许身份查询和经服务凭据认证的停机请求，业务调用及迟到的交易结果回报均返回 HTTP 503。

升级者在提交接单决定前退出，候选会释放端口和数据锁，原升级记录保留。接单决定的提交与后续开放由 6-8d-2 接入。

在 Windows 或 macOS 仓库根目录完成 [README 环境准备](../../../README.md#环境准备)，运行：

```sh
go test ./internal/api ./internal/client -run '^TestCandidate(Admission|Probe)$' -count=1 -v
npm --prefix web run bundle
go test -tags integration ./internal/update -run '^TestPackagedServiceStartupCandidate$' -count=1 -v
```

第一条命令使用临时 HTTP 服务。最后一条需本机 18765 端口空闲，测试会启动并重复识别同一候选程序，检查交易结果回报受到阻止，并确认升级者退出后可按原记录恢复。预期均输出 `PASS`，测试自行清理进程和临时数据。

## 提交接单决定

升级者验证候选后，先发布启用版本，再将同一升级记录写为 `admitted`。候选取得升级锁的共享读锁，检查已提交的决定和启用记录，再开放原地址上的业务入口。升级锁仍被写入者持有时，普通 CLI／MCP 沿用 D-079。

两次写入之间中断，普通启动按 D-077 提示恢复。决定已提交后，重启使用已启用的新版本和现有数据库，旧快照失去回退资格。启用记录丢失或内容不符时，可从已提交的目标版本及摘要重建选择。升级命令的成功收尾与备份清理在后续步骤接入。

在 Windows 或 macOS 仓库根目录完成 [README 环境准备](../../../README.md#环境准备)，运行：

```sh
go test ./internal/update -run '^TestAdmissionRecovery$' -count=1 -v
npm --prefix web run bundle
go test -tags integration ./internal/update -run '^TestPackagedServiceStartupAdmission$' -count=1 -v
```

第一项使用临时目录模拟写入边界和启用记录丢失。最后一项要求本机 18765 端口空闲，验证运行中的候选开放接单，并在重启新版后按原 ID 查回新建请求及原期限。预期两项均输出 `PASS`，退出时清理测试服务和数据。

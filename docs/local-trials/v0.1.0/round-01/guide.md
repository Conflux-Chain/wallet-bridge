# v0.1.0 第 1 轮用户体验指南

准备一台 Windows 10+ x64 或 macOS 13+ 电脑、装有 MetaMask 或 Fluent（EVM 模式）的 Chrome，以及一个已配置 Conflux eSpace Testnet（链 ID 71）的测试账户。连接和拒绝交易都可以用零余额账户。体验 Codex 时需要已安装 Codex CLI。钱包操作请在同一个 Chrome 测试配置里完成。

## 1. 打开终端，核对程序

本次体验包在 `dist/releases/v0.1.0`。用终端进入**放有程序和 SHA256SUMS 的目录**，保持该终端打开。以下选一组执行。

**Windows PowerShell**

```powershell
$bridge = (Resolve-Path .\wallet-bridge-windows-amd64.exe).Path
(Get-FileHash .\SHA256SUMS -Algorithm SHA256).Hash
(Get-FileHash $bridge -Algorithm SHA256).Hash
Get-Content .\SHA256SUMS
& $bridge version --json
$trialData = Join-Path $env:LOCALAPPDATA 'WalletBridge-UAT-v0.1.0'
function wb { & $bridge @args --data-dir $trialData }
```

**Windows Git Bash**

```bash
bridge="$(cygpath -aw ./wallet-bridge-windows-amd64.exe)"
sha256sum SHA256SUMS ./wallet-bridge-windows-amd64.exe
cat SHA256SUMS
"$bridge" version --json
trialData="$(cygpath -aw "$LOCALAPPDATA/WalletBridge-UAT-v0.1.0")"
wb() { "$bridge" "$@" --data-dir "$trialData"; }
```

后续 `wb` 命令均在这个 Git Bash 窗口运行。传给 Windows 程序和 Codex 的路径已转换为 Windows 绝对路径，目录名可以包含空格。

**macOS Terminal（zsh）**

```sh
bridge="$PWD/wallet-bridge-darwin-arm64"   # Intel 使用 wallet-bridge-darwin-amd64
shasum -a 256 SHA256SUMS "$bridge"
cat SHA256SUMS
chmod u+x "$bridge"
"$bridge" version --json
trialData="$HOME/Library/Application Support/WalletBridge-UAT-v0.1.0"
wb() { "$bridge" "$@" --data-dir "$trialData"; }
```

清单的 SHA-256 应为 `b68ae442c573fd9fc9e47d3d6a26f055cc1bfd089a3baea5446ca900d5882f88`。将程序摘要与清单中**同名文件**的一行对照，忽略大小写。版本输出应为 `v0.1.0`、修订以 `b80f843` 开头、`modified: false`、`uiEmbedded: true`。摘要或版本不同先核对文件。

## 2. 启动服务并授权浏览器

`wb` 会给每条命令带上同一个体验数据目录。若以前跑过 Bridge，先处理完钱包弹窗、暂停使用旧服务的 CLI／agent，再用**旧程序和原数据目录**执行 `service stop --json`。新旧服务共用 `127.0.0.1:18765`。本仓库 Windows 开发版的默认目录示例如下，看到 `stopped` 后继续。

```powershell
& ..\..\wallet-bridge-windows-amd64.exe service stop --json
```

Git Bash 中，从候选目录调用上述旧开发程序的命令是 `../../wallet-bridge-windows-amd64.exe service stop --json`。使用自定义目录的旧服务时，需带上它原来的 `--data-dir`。

可选安装：运行 `wb install` 会在体验目录下安装程序并打印固定路径，可用于长期配置。想体验固定路径，将打印的绝对路径赋给 `bridge` 后继续；直接使用下载文件也可以。

```text
wb service ensure --json
wb ui --print-url
```

第一条应返回 `running`，`info.version` 为 `v0.1.0`。将第二条打印的一次性链接粘贴到目标 Chrome 配置，看到「浏览器已授权」。如果链接过期，重跑第二条。管理首页地址是 [127.0.0.1:18765](http://127.0.0.1:18765/)。

## 3. 用 CLI 核对钱包并审阅一笔交易

```text
wb connect --chain-id 71 --client-name "UAT CLI" --intent "核对测试账户" --no-open
```

在已授权的 Chrome 中打开终端的 `Open` 链接。核对调用方和目标网络，选择钱包，读取并授权账户，按需切链。核对最终地址和网络后点「同意连接」。终端会打印 Session ID、Account 和 Chain（`0x47`）。在五分钟内把下列三处占位文字换成刚返回的实际值，收发地址都填**自己的同一个测试账户**。

```text
wb transaction send --session-id "<Session ID>" --chain-id 71 --from "<Account>" --to "<Account>" --value 0 --client-name "UAT CLI" --intent "在测试网向自己发送 0 CFX" --no-open
```

打开新的 `Open` 链接，记下交易请求 ID。核对调用方、意图、`0 CFX`、收发地址、网络和原始 `data=0x`。费用由钱包估算。可任选一种结果体验：

- **拒绝**：在 Bridge 页点击「拒绝这笔交易」。也可点击「在钱包中确认并发送」，然后在钱包内拒绝。终端应显示 `rejected`，退出码 1。
- **广播到测试网**：账户需有测试 CFX 支付 gas。亲自在钱包核对网络、地址、金额和费用后确认。终端应显示 `sent` 和 `txHash`。用同一哈希在 [Conflux eSpace 测试网浏览器](https://evmtestnet.confluxscan.org/)核对回执。
- 若显示 `uncertain`，交易可能已广播。保留原钱包标签页，稍后按原请求 ID 查询迟到的哈希。等待命令此时退出 3。

```text
wb transaction get "<交易请求 ID>" --json
wb session close "<Session ID>" --json
```

第一次查询应与页面结果一致。关闭 Session 后可重复查询原交易。想换另一个钱包，从 `wb connect` 建立新 Session。

## 4. 从 Codex 发起请求

注册专用测试名称。若已有同名配置，选一个空闲名称，并在提示词和清理命令中同步替换。

Windows Git Bash 先按 [Codex CLI 定位步骤](../../../codex.md#windows-git-bash-定位-codex-cli)确认版本，再使用第 1 步取得的 Windows 绝对路径注册。

```bash
"$codex_cli" mcp add wallet_bridge_uat -- "$bridge" mcp --data-dir "$trialData"
"$codex_cli" mcp get wallet_bridge_uat --json
```

PowerShell 或 macOS 终端使用以下命令。

```text
codex mcp add wallet_bridge_uat -- "$bridge" mcp --data-dir "$trialData"
codex mcp get wallet_bridge_uat --json
```

核对命令为程序绝对路径、参数包含本次体验目录，打开新的 Codex 会话。可依次输入：

1. 「用 wallet_bridge_uat 连接 Conflux eSpace 测试网 71，给我审批链接并等待结果。」在 Chrome 选择钱包并同意。核对 Codex 返回的钱包、账户和网络。
2. 「用刚才的 Session 创建一笔测试交易，向我自己的同一账户发送 0 CFX，给我审阅链接。」核对页面显示的调用方和原始交易，在页面拒绝。让 Codex 按**原交易请求 ID**查询，应返回 `rejected`。
3. 「关闭刚才的 Session。」需要体验中断恢复时，也可在取得链接后中断等待，浏览器完成审批，再回到同一会话说「继续查询刚才的请求」。

## 5. 体验升级中断恢复

先退出测试会话并移除测试 MCP。Git Bash 用 `"$codex_cli" mcp remove wallet_bridge_uat`，PowerShell 和 macOS 用 `codex mcp remove wallet_bridge_uat`。再运行：

```text
wb service stop --json
```

看到 `stopped` 后继续。首版公开前，正常运行 `wb upgrade --json` 通常返回 `no_release`。要亲自体验切换，使用交付时在本机另行准备的 `dist/upgrade-trials-v0.1.0`。其中两个一次性目录均使用新建的空数据库、测试凭据和标记为 `v0.0.0-trial` 的程序，从旧服务已停止、备份已完成处继续。每个目录分别执行一次下列步骤。

Windows 在当前终端先选 `resume`：

```powershell
$scenario = (Resolve-Path ..\..\upgrade-trials-v0.1.0\resume).Path
function trial { & $bridge @args --data-dir $scenario }
```

Git Bash 在候选目录使用：

```bash
scenario="$(cygpath -aw ../../upgrade-trials-v0.1.0/resume)"
trial() { "$bridge" "$@" --data-dir "$scenario"; }
```

macOS 若已在本机准备演练目录，可使用：

```sh
scenario="$(cd ../../upgrade-trials-v0.1.0/resume && pwd)"
trial() { "$bridge" "$@" --data-dir "$scenario"; }
```

```text
trial service ensure --json
trial upgrade --json
trial service status --json
trial service stop --json
```

首条预期提示 `upgrade_recovery_required`。在 `resume` 中，`upgrade` 返回 `upgraded`，随后 `status` 显示 `v0.1.0`。等到 `stopped`，再将上面路径末尾的 `resume` 换为 `rollback` 并重复。该目录预设目标副本缺失，`upgrade` 会退出 1，报告 `upgrade_rolled_back`、原因和已恢复版本。随后 `status` 应显示运行中的 `v0.0.0-trial`。这里验证的是离线恢复；从 GitHub 下载真实附件需等首次公开发行。[演练目录的准备方式](../../../iterations/06-release-upgrade/release.md#准备升级演练目录)

## 6. 结束并记录反馈

关闭测试标签页。若第 2 步暂停了本仓库的 Windows 开发服务，在候选目录运行 `& ..\..\wallet-bridge-windows-amd64.exe service ensure --json`，应看到 `running`。使用其他旧程序时换成它的路径和原数据目录。需要复查交易时保留 `$trialData`。确认结束后，可在文件管理器中删除该体验目录及已停止的 `upgrade-trials-v0.1.0`。保留候选程序和 `SHA256SUMS` 供发布核对。

Git Bash 可在候选目录用 `../../wallet-bridge-windows-amd64.exe service ensure --json` 恢复第 2 步暂停的默认目录开发服务。用 `printf '%s\n' "$trialData"` 查看本轮数据目录。

请在[本轮体验反馈](feedback.md)中记录操作系统、Chrome 和钱包版本、完成到哪一步及实际结果。发生问题时附错误码或截图，跳过的步骤标记「未测」。一次性浏览器授权链接留在本机。

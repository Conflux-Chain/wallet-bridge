# 在 Codex 中连接钱包

在同一台 Windows 或 macOS 机器上运行 Codex、Wallet Bridge 和装有钱包的 Chrome。下例连接 Conflux eSpace 测试网（chainId 71），只需一个测试账户，无需余额。

## Windows Git Bash 定位 Codex CLI

在 Git Bash 运行以下命令。如果 PATH 中找不到 `codex`，则从已安装的 Codex 桌面应用定位自带的 CLI。保留当前终端供后续注册使用。

```bash
codex_cli="$(command -v codex || true)"
if [ -z "$codex_cli" ]; then
  package="$(powershell.exe -NoProfile -Command '(Get-AppxPackage OpenAI.Codex).InstallLocation' | tr -d '\r')"
  if [ -n "$package" ]; then
    codex_cli="$(cygpath -u "$package")/app/resources/codex.exe"
  fi
fi
if [ -x "$codex_cli" ]; then
  "$codex_cli" --version
else
  echo 'Codex CLI unavailable. Install Codex CLI and reopen Git Bash.' >&2
fi
```

看到 `codex-cli` 及版本号后继续。两个位置都找不到程序时，按[官方 CLI 指南](https://developers.openai.com/codex/cli/)安装，再打开 Git Bash 重试。

## 注册 MCP

下面以本机构建为例。在仓库根目录按 [README 环境准备](../README.md#环境准备)安装依赖，然后运行 `npm --prefix web run bundle`。已有独立程序时可直接使用其路径。Codex 通过 STDIO 启动程序，长期使用可先运行 `install` 并注册它输出的固定路径。若选择直接运行，将 `bridge` 设为原程序的绝对路径并保留该文件。

Windows PowerShell：

```powershell
$bridge = .\dist\wallet-bridge-windows-amd64.exe install
if ($LASTEXITCODE -ne 0) { throw 'Wallet Bridge install failed' }
codex mcp add wallet_bridge -- "$bridge" mcp
```

Windows Git Bash 在仓库根目录执行下列命令。先完成上面的 CLI 定位，再将完整程序路径写入 MCP 配置。

```bash
bridge="$(./dist/wallet-bridge-windows-amd64.exe install)" &&
  "$codex_cli" mcp add wallet_bridge -- "$(cygpath -aw "$bridge")" mcp
"$codex_cli" mcp get wallet_bridge --json
```

macOS（Intel 将 `arm64` 换为 `amd64`）：

```sh
bridge="$(./dist/wallet-bridge-darwin-arm64 install)" &&
  codex mcp add wallet_bridge -- "$bridge" mcp
```

运行 `codex mcp get wallet_bridge --json`，核对 `enabled` 为 `true`、`transport.type` 为 `stdio`、命令为上述绝对路径且参数为 `["mcp"]`。然后打开新的 Codex 会话。首次钱包调用会启动或复用本机服务。[Codex MCP 配置](https://developers.openai.com/codex/mcp/)

## 验证连接和恢复

1. 在 Codex 输入「用 wallet_bridge 连接 Conflux eSpace 测试网 71，先给我审批链接，再帮我等结果」。若 Codex 请求工具执行许可，按提示确认。应看到含 `?connection=…` 的可点击链接和原请求 ID。
2. 在装有 MetaMask 或 Fluent 的 Chrome 配置中打开该链接。如果链接进入 Codex 内置浏览器，点击「在系统默认浏览器继续」，核对新页面地址保留原请求 ID。若钱包安装在另一个浏览器配置，可将原链接粘贴到该配置中。若提示「需要浏览器授权」，用上面的程序运行 `ui`，完成授权后再打开**原连接链接**。在页面核对调用方和网络，选择钱包，核对最终账户后点「同意连接」。[浏览器授权步骤](browser-testing.md#手动验证授权页面) · [钱包操作](first-connection.md)
3. 保持 Codex 任务运行。它会间隔调用 `get_connection`，同意后用返回的 Session ID 调用 `get_session`。核对 Codex 展示的钱包、账户和网络与页面一致。
4. 要验证稍后继续，可在拿到链接后中断 Codex 等待，在原五分钟期限内处理页面，再回到同一对话输入「继续查询刚才的连接」。应查回同一请求的结果。超过期限且未作决定时应显示已到期。上下文缺失时可直接提供原请求 ID。

非交互的 `codex exec` 若提示 `MCP tool call requires approval, but approval policy is never`，这次调用尚未执行。在可确认工具调用的 Codex 会话继续，重试时沿用原 ID 和原输入。

## 结束测试

仍待审批时告诉 Codex「取消刚才的连接请求」，已建立 Session 时告诉它「关闭刚才的 Session」。移除 Codex 配置用 `codex mcp remove wallet_bridge`，Git Bash 用 `"$codex_cli" mcp remove wallet_bridge`。如果本机服务是本次测试专门启动的，可用同一程序运行 `service stop --json`。[服务管理](local-service.md#后台启动查询与停止)

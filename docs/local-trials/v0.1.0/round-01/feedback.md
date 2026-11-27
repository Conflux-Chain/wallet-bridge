# v0.1.0 第 1 轮体验反馈

本轮正在收集反馈。用户已在 Windows 体验 CLI 和 Codex，并确认使用 `connect` 返回的 Session ID 后，CLI 能成功发送交易。同事已在 macOS 配置 MCP 后体验 Codex，具体观察待补充。[本轮指南与候选校验信息](guide.md)

### 01 · Session ID 格式错误时只显示 usage

**已复现。** 在 Windows 运行 `transaction send --session-id aaa`，同时传入有效的收发地址和 `--chain-id 71`，CLI 打印整段用法并退出 2。用户无法判断缺少参数还是输入有误。期望提示具体字段、所需格式，以及从哪里取得有效 ID。

### 02 · 发送交易时需要手动传入 Session ID

**建议，待讨论。** 用户希望 `transaction send` 可以省略 `--session-id`。现有显式传入 ID 的流程已体验通过。省略后如何选择钱包连接，仍待确认。

### 03 · Windows 终端找不到指南中的 codex 命令

**已定位，有临时用法。** 本机安装了 Codex 桌面版，内置 CLI 所在目录未加入用户或系统 PATH。用户在配置 MCP 时卡在 `codex mcp add`，平时使用 Git Bash。通过桌面版安装目录可找到 `codex.exe`，PowerShell 已验证可调用。期望指南提供从 Git Bash 定位和调用 CLI 的步骤。

## 追加反馈

新反馈从 04 开始。复测已有问题时直接引用原编号，写明轮次和结果。

```markdown
### 04 · 一句话概括问题

- 环境：操作系统、CLI 或 Codex、所用钱包
- 操作：执行的命令、给 Codex 的原话或指南步骤
- 实际：
- 期望：
```

# 迭代 7 · 第 1 轮反馈处理方案

编号沿用第 1 轮反馈。每项只记录本轮决定和仍待确认的部分，修复 PR 与本地验收结果见[分步计划](plan.md)。

| 原反馈 | 方案与确认范围 |
|---|---|
| [1 · Session ID 报错](../../local-trials/v0.1.0/round-01/feedback.md#1-session-id-格式错误时只显示-usage) | 原 CLI 把 ID 校验与其他参数错误合并处理，统一打印用法。用户已同意合并 #96。该修复会区分缺少 ID 和格式错误，指出 `--session-id` 需要 32 位小写十六进制字符，并提示从已批准的 `wallet-bridge connect` 结果取得。 |
| [2 · 能否省略 Session ID](../../local-trials/v0.1.0/round-01/feedback.md#2-发送交易时需要手动传入-session-id) | **已确认暂缓。** 本轮继续显式传入 `--session-id`。用户已用有效 ID 完成发送，等帮助和报错改进后再体验，届时决定是否简化。 |
| [3 · 找到 Codex CLI](../../local-trials/v0.1.0/round-01/feedback.md#3-windows-终端找不到指南中的-codex-命令) | **已确认。** Git Bash 优先使用 PATH 中的 Codex CLI。找不到时查询 Windows 桌面应用的安装位置，运行其中的 CLI，用实际版本、MCP 注册和配置查询验收。 |
| [4 · Git Bash 指南](../../local-trials/v0.1.0/round-01/feedback.md#4-windows-体验指南缺少-git-bash-命令示例) | **已确认。** 体验指南增加 Windows Git Bash 命令，覆盖候选文件校验、独立数据目录、CLI 调用与清理。按本机 Git Bash 实测验收，WSL 免测。 |
| [5 · CLI 帮助与报错](../../local-trials/v0.1.0/round-01/feedback.md#5-cli-命令行诸多问题反馈) | **帮助版式已确认。** 顶层按任务列出命令及用途。子命令先以 `Required arguments` 展示必填选项和位置参数，再以 `Options` 展示可选项及默认行为。无必填项时只显示 `Options`。参数报错见下文分项方案。 |
| [6 · Codex 首次连接](../../local-trials/v0.1.0/round-01/feedback.md#6-其他用户-codex-首次体验不流畅) | **已确认交互目标。** 用户点击 Codex 审批链接，在系统默认浏览器继续同一请求。如何将链接交给默认浏览器仍待验证和确认。简化首次浏览器授权是另一个待讨论的问题。 |

### 反馈 5 · 连接时的网络参数

**已确认。** `connect` 曾将链 ID 与其他参数一起校验，失败就打印用法。缺少 `--chain-id` 时指出缺参，格式错误时指出该字段并给出十进制 `71`、十六进制 `0x47` 两个例子。两类错误都在启动服务前写入 stderr，退出 2。使用 `--json` 时返回 `bad_request`。

### 反馈 5 · 管理命令的 ID

**已确认。** 连接、Session 和交易的管理命令共用一段位置参数校验。缺少 ID、格式有误、多给 ID 时分别说明原因，并提示只传一个 32 位小写十六进制 ID。连接请求 ID 来自 `connect`，Session ID 来自批准连接后的结果，交易请求 ID 来自 `transaction send`。错误写入 stderr，退出 2，`--json` 返回 `bad_request`。

### 反馈 5 · 其余命令行参数错误

**已确认。** 公共参数和子命令各自校验，过去多个失败分支直接打印用法。相对数据目录、空调用方名称、未知选项、多余参数、浏览器分页／撤销和下载版本等错误，应指出出错位置及修正方法。沿用文本和 JSON 输出、stderr 及退出码，用打包 CLI 自动验收。

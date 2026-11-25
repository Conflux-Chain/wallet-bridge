# 迭代 5 PR 计划

5-0 规划已合并为 `958cf0b`，三平台 CI 通过。Windows 首次在连接维护测试观察 SQLite 状态时遇到 `SQLITE_BUSY`，本机该用例连续 15 次通过，同一提交重跑通过。[PR #65](https://github.com/Conflux-Chain/wallet-bridge/pull/65) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45543399271)

5-1 已合并为 `1e6c916`，Windows amd64、macOS amd64／arm64 CI 均通过独立程序握手与工具发现。[PR #66](https://github.com/Conflux-Chain/wallet-bridge/pull/66) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45545457113)

5-2 已合并为 `8ddb439`。三平台 CI 均通过 MCP 重试、浏览器同意与原结果查询。[PR #67](https://github.com/Conflux-Chain/wallet-bridge/pull/67) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45555450734)

5-3 已合并为 `f40b396`。三平台 CI 均通过服务重启后取消连接、Session 查询与重复关闭。[PR #68](https://github.com/Conflux-Chain/wallet-bridge/pull/68) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45556987685)

5-4 已合并为 `dfddcf2`，三平台 CI 均通过独立程序的原始交易创建、重试和审阅用例。[PR #69](https://github.com/Conflux-Chain/wallet-bridge/pull/69) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45560131376)

5-5 已合并为 `0537a32`。三平台 CI 均通过查询、取消、服务重启和新 MCP 进程查回迟到哈希的模拟钱包用例。[PR #70](https://github.com/Conflux-Chain/wallet-bridge/pull/70) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45563009042)

5-6 已合并为 `b96cf68`，本轮规划和六个能力 PR 全部合并。Windows 首次在连接维护测试遇到 `SQLITE_BUSY`，本机连续 15 次通过，同一提交重跑后三平台 CI 通过。实测范围见下方 A-18 记录。[PR #71](https://github.com/Conflux-Chain/wallet-bridge/pull/71) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45566225167) · [Codex 接入指南](../../codex.md)

| 步骤 | 单一交付 | 验证 |
|---|---|---|
| 5-1 | STDIO 子命令与工具发现 | 三平台可构建，独立进程可握手、列出工具并正常退出，发现阶段不启动 HTTP 服务 |
| 5-2 | 创建及查询连接请求 | 首次按需启动服务，取得可点击的原链接，同 ID 重试只产生一项请求，稍后查询返回原结果 |
| 5-3 | 连接取消与 Session 管理 | 取消待审批连接，查询绑定账户／网络，显式关闭后仍可读取历史 |
| 5-4 | 创建单笔交易审阅请求 | 复用固定 Session，展示原始字段和意图，同 ID 重试返回原记录 |
| 5-5 | 查询及取消交易请求 | 区分等待钱包、已发送、拒绝和结果未确定，取消与迟到哈希沿用现有规则 |
| 5-6 | Codex 配置和真实 agent 验收 | 从配置到展示原链接、自动查询、稍后继续与清理均可复现，记录版本与实测边界 |

## A-18 验收记录

2026-11-25 在 Windows amd64 使用 Codex CLI `0.171.0-alpha.13`、Bridge MCP `0537a32` 和本机已有服务（API 1.11）。临时配置验证了 `codex mcp add/get/remove`。实际对话以单次配置接入，使用 `codex exec --approve-for-me` 审核工具调用。

- **展示审批链接。** `create_connection` 返回目标网络 `0x47`、调用方 `Codex` 和普通审批链接。Codex 先展示原链接，再两次调用 `get_connection` 得到 `pending`。独立的 Chrome 157 配置打开同一链接，显示浏览器授权提示。
- **恢复并取得账户。** 新的 `codex exec resume` 进程恢复同一对话，沿用上下文里的原请求 ID 自动查询，先得到 `pending`，随后得到 `succeeded`。Codex 调用 `get_session` 并展示 MetaMask、固定账户和 Conflux eSpace 测试网 71。
- **重试与清理。** 再次恢复对话，以相同 ID 和原输入调用 `create_connection`。逐项比对工具结果，请求 ID、链接、原期限与已建立的 Session 均相同。`close_session` 返回该 Session 的 `closed` 状态。
- **覆盖范围。** 本次真实 agent 验收覆盖 Windows 的 MetaMask 连接。macOS、Fluent 的真实 agent 组合及实际钱包广播未在本轮验证。交易中断与迟到哈希的独立程序／模拟钱包证据见 5-5。

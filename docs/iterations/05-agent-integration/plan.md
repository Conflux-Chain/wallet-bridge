# 迭代 5 PR 计划

5-0 规划已合并为 `958cf0b`，三平台 CI 通过。Windows 首次在连接维护测试观察 SQLite 状态时遇到 `SQLITE_BUSY`，本机该用例连续 15 次通过，同一提交重跑通过。[PR #65](https://github.com/Conflux-Chain/wallet-bridge/pull/65) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45543399271)

5-1 已合并为 `1e6c916`，Windows amd64、macOS amd64／arm64 CI 均通过独立程序握手与工具发现。[PR #66](https://github.com/Conflux-Chain/wallet-bridge/pull/66) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45545457113)

5-2 已合并为 `8ddb439`。三平台 CI 均通过 MCP 重试、浏览器同意与原结果查询。[PR #67](https://github.com/Conflux-Chain/wallet-bridge/pull/67) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45555450734)

5-3 已合并为 `f40b396`。三平台 CI 均通过服务重启后取消连接、Session 查询与重复关闭。[PR #68](https://github.com/Conflux-Chain/wallet-bridge/pull/68) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45556987685)

5-4 已合并为 `dfddcf2`，三平台 CI 均通过独立程序的原始交易创建、重试和审阅用例。[PR #69](https://github.com/Conflux-Chain/wallet-bridge/pull/69) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45560131376)

5-5 待评审，覆盖查询、取消与迟到哈希。5-6 待开发。[复现步骤](design.md#验证-mcp-交易)

| 步骤 | 单一交付 | 验证 |
|---|---|---|
| 5-1 | STDIO 子命令与工具发现 | 三平台可构建，独立进程可握手、列出工具并正常退出，发现阶段不启动 HTTP 服务 |
| 5-2 | 创建及查询连接请求 | 首次按需启动服务，取得可点击的原链接，同 ID 重试只产生一项请求，稍后查询返回原结果 |
| 5-3 | 连接取消与 Session 管理 | 取消待审批连接，查询绑定账户／网络，显式关闭后仍可读取历史 |
| 5-4 | 创建单笔交易审阅请求 | 复用固定 Session，展示原始字段和意图，同 ID 重试返回原记录 |
| 5-5 | 查询及取消交易请求 | 区分等待钱包、已发送、拒绝和结果未确定，取消与迟到哈希沿用现有规则 |
| 5-6 | Codex 配置和真实 agent 验收 | 从配置到展示原链接、自动查询、稍后继续与清理均可复现，记录版本与实测边界 |

各能力 PR 随附本地验证命令。5-6 汇总真实 Codex 使用步骤与 A-18 结果。[验收索引](../../acceptance.md)

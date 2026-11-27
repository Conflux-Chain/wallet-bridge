# 迭代 7 · PR 计划

| 顺序 | 单个 PR 的交付 | 验证方式 | 状态 |
|---|---|---|---|
| 7-1 | 交易发送的 Session ID 诊断（反馈 1） | [复测步骤](../../transaction-review.md#检查-session-id-报错)，三平台 CI 通过 | 已合并 [#96](https://github.com/Conflux-Chain/wallet-bridge/pull/96) |
| 7-2 | CLI 帮助的命令用途、参数与默认行为（反馈 5） | Windows Git Bash 用户本地验收与三平台 CI 通过 | 已合并 [#97](https://github.com/Conflux-Chain/wallet-bridge/pull/97) |
| 7-3a | `connect --chain-id` 错误提示（反馈 5） | 用户已本地验收 `a62572c`，三平台 CI 通过 | 已合并 [#99](https://github.com/Conflux-Chain/wallet-bridge/pull/99) |
| 7-3b | 连接、Session 和交易管理命令的 ID 报错（反馈 5） | 六个命令的参数回归，独立程序验证文本／JSON、退出码和 ID 来源 | [#100](https://github.com/Conflux-Chain/wallet-bridge/pull/100) 由自动化验收 |
| 7-4a | 在 Windows Git Bash 中找到 Codex CLI（反馈 3） | 定位 CLI、注册并核对 MCP | 待分析 |
| 7-4b | 用 Git Bash 运行 Windows 候选程序（反馈 4） | 从新终端按示例运行 Bridge，核对程序与数据目录 | 待分析 |
| 7-5 | Codex 审批链接（反馈 6） | 先验证宿主能力、记录设计，再单独实现。用连接和交易原 ID 验证默认浏览器交接 | 待开发 |
| 7-6 | 首次授权体验（反馈 6） | 结合 7-5 的实测确认交互，再实现并用新浏览器配置复测 | 待确认 |
| 7-7 | 下一轮本地候选与体验指南 | 记录源码和产物摘要，按原编号收集用户复测结果，回访反馈 2 | 待开发 |

各 PR 从已合并的 `main` 开始。产品决定见[反馈处理方案](design.md)，本轮验收以[spec](spec.md)为准。

各问题按[体验反馈修复流程](../README.md#体验反馈修复)交付。

7-1 已用 Windows Git Bash 和本地构建的程序复现并对照反馈 1。修复后，错误写入 stderr、退出 2，stdout 为空。Go 回归覆盖省略、空值、漏填参数值、短 ID、大写 ID 及文本／JSON 输出。

7-2 已检查顶层菜单、6 个命令组和 21 个公开子命令。用户使用 `4535bec` 的 Windows 预览程序本地验收通过。交易帮助在 `Required arguments` 下列出 4 个必填字段，`Options` 说明金额、calldata 和费用的默认行为。

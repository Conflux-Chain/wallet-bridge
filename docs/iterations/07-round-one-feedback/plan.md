# 迭代 7 · PR 计划

| 顺序 | 单个 PR 的交付 | 验证方式 | 状态 |
|---|---|---|---|
| 7-1 | 交易发送的 Session ID 诊断（反馈 1） | [复测步骤](../../transaction-review.md#检查-session-id-报错)，三平台 CI 通过 | 已合并 [#96](https://github.com/Conflux-Chain/wallet-bridge/pull/96) |
| 7-2 | CLI 帮助的命令用途、参数与默认行为（反馈 5） | Windows Git Bash 用户本地验收与三平台 CI 通过 | 已合并 [#97](https://github.com/Conflux-Chain/wallet-bridge/pull/97) |
| 7-3a | `connect --chain-id` 错误提示（反馈 5） | 用户已本地验收 `a62572c`，三平台 CI 通过 | 已合并 [#99](https://github.com/Conflux-Chain/wallet-bridge/pull/99) |
| 7-3b | 连接、Session 和交易管理命令的 ID 报错（反馈 5） | 六个命令的参数回归，独立程序验证文本／JSON、退出码和 ID 来源 | 已合并 [#100](https://github.com/Conflux-Chain/wallet-bridge/pull/100)，三平台 CI 通过 |
| 7-3c | 其余命令行参数报错（反馈 5） | 打包程序覆盖 16 组输入的文本／JSON 提示、stderr 和退出码 2 | 已合并 [#101](https://github.com/Conflux-Chain/wallet-bridge/pull/101)，三平台 CI 通过 |
| 7-4a | 在 Windows Git Bash 中找到 Codex CLI（反馈 3） | Windows Git Bash 验证 PATH、桌面版 CLI 和独立配置中的 MCP 注册、查询、移除 | 已合并 [#102](https://github.com/Conflux-Chain/wallet-bridge/pull/102) |
| 7-4b | 用 Git Bash 运行 Windows 候选程序（反馈 4） | Git Bash 核对原候选和清单摘要、版本、独立目录安装、MCP 配置与清理 | 已合并 [#103](https://github.com/Conflux-Chain/wallet-bridge/pull/103) |
| 7-5 | Codex 审批链接（反馈 6） | 用户在 Codex 验证链接旁的提醒、跳转至系统默认浏览器及原连接请求 ID。Windows 和两种 macOS 的 CI 通过 | 已合并 [#104](https://github.com/Conflux-Chain/wallet-bridge/pull/104) |
| 7-6a | 首次使用与授权到期的产品要求（反馈 6） | 核对全局需求、当轮设计和验收场景 | 需求已确认，文档待评审 |
| 7-6b | 单笔请求的浏览器访问 | 验证可信入口发起的访问仅适用于原请求，以及到期、重试与旧链接行为 | 待细化设计 |
| 7-6c | CLI 与 Codex 的连接、交易审阅入口 | 分步接入两个客户端。新配置与授权到期后，直接打开、从内置浏览器继续均保留原 ID | 待开发 |
| 7-6d | CLI 与 Codex 查看历史 | 核对空列表、已有记录和分页，无需运行授权命令或连接钱包 | 待开发 |
| 7-7 | 下一轮本地候选与体验指南 | 记录源码和产物摘要，按原编号收集用户复测结果，回访反馈 2 | 待开发 |

各 PR 从已合并的 `main` 开始。产品决定见[反馈处理方案](design.md)，本轮验收以[spec](spec.md)为准。

各问题按[体验反馈修复流程](../README.md#体验反馈修复)交付。

7-1 已用 Windows Git Bash 和本地构建的程序复现并对照反馈 1。修复后，错误写入 stderr、退出 2，stdout 为空。Go 回归覆盖省略、空值、漏填参数值、短 ID、大写 ID 及文本／JSON 输出。

7-2 已检查顶层菜单、6 个命令组和 21 个公开子命令。用户使用 `4535bec` 的 Windows 预览程序本地验收通过。交易帮助在 `Required arguments` 下列出 4 个必填字段，`Options` 说明金额、calldata 和费用的默认行为。

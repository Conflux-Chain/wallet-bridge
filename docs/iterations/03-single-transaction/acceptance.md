# 迭代 3 · 单笔交易验收记录

2026-11-24。独立 CLI 已通过 Chrome 和模拟钱包走通连接、页面选定账户、零金额自转、取得 hash、关闭 Session 和再次查询。MetaMask、Fluent EVM 的本轮测试网发送与回执待人工验收。[组合用例](../../../web/tests/e2e/transaction-flow.spec.ts) · [本地复现](../../transaction-review.md#浏览器回归)

| 验收项 | 已验证的行为 | 证据 |
|---|---|---|
| A-07 固定绑定 | 创建时校验账户和网络。交接时账户、网络变化或钱包有歧义会阻止发送 | [输入校验](../../../internal/app/transaction_test.go) · [钱包状态](../../../web/tests/e2e/transaction-wallet.spec.ts) · [发送](../../../web/tests/e2e/transaction-send.spec.ts) |
| A-08 审阅 | 页面展示来源、意图、金额和费用，可复制完整地址和 data。未知网络显示精确最小单位 | [审阅页面](../../../web/tests/e2e/transaction-review.spec.ts) |
| A-09 发送 | 模拟中仅选定钱包收到审阅过的交易，CLI 和原请求返回同一 hash。真实扩展见下表 | [组合用例](../../../web/tests/e2e/transaction-flow.spec.ts) |
| A-10 幂等 | 相同输入和 ID 取回原请求，改变输入冲突。重复点击、丢失许可或回报重试不会再发送 | [存储](../../../internal/store/transaction_requests_test.go) · [共享 client](../../../internal/client/transactions_test.go) · [浏览器](../../../web/tests/e2e/transaction-send.spec.ts) |
| A-11、12 原链接、关闭与到期 | 原链接可重新打开。交接前关闭 Session 会取消待发交易，期限先到保留到期结果。待处理交易保护 Session，轮询保留原期限 | [页面](../../../web/tests/e2e/transaction-review.spec.ts) · [生命周期](../../../internal/store/transaction_lifecycle_test.go) |
| A-13、14 结果与迟到 hash | CLI 区分已发送、拒绝、取消、到期与结果未确定。未确定时退出 3，按原 ID 可取回迟到 hash | [CLI 退出码](../../../internal/cli/send_test.go) · [端到端查询](../../../web/tests/e2e/transaction-review.spec.ts) |
| A-15、16 历史与重启 | 关闭 Session 后仍可查交易，活动与结果事务写入。已交接的请求重启后进入未确定，保留原调用补交结果的能力 | [结果与活动](../../../internal/store/transaction_results_test.go) · [中断恢复](../../../internal/store/transaction_interruption_test.go) |

完整队列及日志清理归迭代 4。[全局验收清单](../../acceptance.md) · [各 PR 验证记录](plan.md)

## 真实钱包与链上结果

| 钱包 | 当前证据 | 待补 |
|---|---|---|
| MetaMask | [首次连接已人工确认](../02-connection-session/acceptance.md) | 本次发送的环境、钱包版本、hash、成功回执 |
| Fluent EVM | [首次连接已人工确认](../02-connection-session/acceptance.md) | 本次发送的环境、钱包版本、hash、成功回执 |

回执脚本已在 `chainId=0x47` 使用一笔既有公开交易读到 `status=0x1` 和区块 `0x1009119e`，未知 hash 返回无回执。两项真实钱包验收使用各自在本轮发送的 hash 核对。[测试步骤](../../transaction-review.md#验证测试网回执)

# 迭代 4 · 协作与恢复验收

## 本地复现

在 Windows 或 macOS 的仓库根目录按 [README 环境准备](../../../README.md#环境准备)安装依赖，确认本机端口 18765 空闲后运行：

```sh
npm --prefix web run bundle
npm --prefix web run browser:install
npm --prefix web run test:e2e -- collaboration-recovery.spec.ts
```

预期为 `1 passed`。测试使用独立可执行程序、临时 SQLite 和模拟钱包，自动打开浏览器页面。完成后测试框架停止临时服务并清理数据。

## 验收场景

两个 CLI 分别在自己的 Session 创建交易。A 调用钱包后重启服务，恢复时 A 显示「结果未确定」，B 保留原审批期限。用户从 B 页打开占用钱包的 A，在恢复页手动解除占用，再在 B 页切链。此时 A 的原页面补回哈希，B 继续持有钱包，A 的发送次数仍为一次。用户拒绝 B，关闭 A 的 Session，并从活动列表移除 A 后，CLI 仍能分别查回 A 的哈希与 B 的拒绝结果。[组合用例](../../../web/tests/e2e/collaboration-recovery.spec.ts)

| 验收项 | 关联证据 |
|---|---|
| A-04、17 多调用方与授权 | [请求排序、后到先处理](../../../web/tests/e2e/pending-requests.spec.ts) · [浏览器授权](../../../web/tests/e2e/authorization.spec.ts) |
| A-10～12 幂等、原链接与期限 | [活动记录重试、到期及访问权限](../../../internal/api/browser_activity_test.go) · [Session 生命周期](../../../internal/store/session_lifecycle_test.go) |
| A-14～17 中断、迟到结果及历史 | [本轮组合用例](../../../web/tests/e2e/collaboration-recovery.spec.ts) · [活动页查询与移除](../../../web/tests/e2e/activity.spec.ts) · [重新授权后补交](../../../web/tests/e2e/transaction-send.spec.ts) |

真实钱包广播与链上回执沿用[迭代 3 验收记录](../03-single-transaction/acceptance.md#真实钱包与链上结果)。

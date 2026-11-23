# 迭代 3 PR 计划

当前步骤为 PR 3-2，交付 SQLite 创建事务及按原 ID 重试。[需求](spec.md) · [设计](design.md)

| 步骤 | 单一交付 | 主要检查 |
|---|---|---|
| 3-0 | 需求、设计草案和 PR 计划 | [#36](https://github.com/Conflux-Chain/wallet-bridge/pull/36) 合并为 `30efa4b`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45128899817) 通过 |
| 3-1 | Go 交易输入及校验 | [#37](https://github.com/Conflux-Chain/wallet-bridge/pull/37) 合并为 `6d17b49`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45134102824) 通过 |
| 3-2 | 请求持久化与幂等创建 | 有效 Session、旧库迁移、重试／冲突、原期限及事务回滚 |
| 3-3 | 交接前的取消、到期与 Session 生命周期 | 关闭及空闲回收与并发创建，重启后无过期重置 |
| 3-4 | 连接与交易共用钱包执行槽 | 同请求多页、跨请求竞争、旧连接恢复回归 |
| 3-5 | 交易发送许可与 Session 关闭竞争 | 点击重放、交接响应丢失、取消和关闭的先后顺序 |
| 3-6 | 钱包结果、活动持久化及恢复 | 拒绝、hash、未确定、迟到回报、提交失败和重启 |
| 3-7 | Client 侧交易 HTTP 接口及 Swagger | 创建、读取、取消、服务凭据及错误映射 |
| 3-8 | 浏览器侧交易 HTTP 接口及 Swagger | 授权页面读取、执行归属、交接及结果回传 |
| 3-9 | 只读交易审阅页面 | 原始参数、费用来源、完整复制、原链接重开 |
| 3-10 | 页面核对固定钱包与发起发送 | 账户／网络修复、多页面、拒绝、丢包与模拟钱包调用次数 |
| 3-11 | 共享 Go client 的交易操作 | ID 重试、能力检查、可查询的发送结果 |
| 3-12 | CLI 创建、等待及按 ID 管理 | 独立程序与模拟钱包贯通，阻塞／异步、错误和退出码 |
| 3-13 | 组合验收与指南 | A-07～16、三平台 CI、MetaMask／Fluent EVM 测试网及 client 查链分别留证 |

首个实现 PR 只交付交易输入。真实钱包发送按用户授权执行，逐 PR 在 README 已链接的指南补充可复现步骤。完整交易队列和日志清理按[路线图](../../roadmap.md)在迭代 4 细化。

## PR 3-1 验证

Windows amd64 的交易输入专项、默认及 `no_web` 完整 Go 测试和两种 vet 通过。本机缺少创建符号链接的权限，平台／SQLite 的相关用例跳过。交易校验使用内存中的 Session，钱包调用在后续页面 PR 验证。[本地复现](design.md#本地验证)

## PR 3-2 验证

Windows 临时 SQLite 实测通过 v4 升级及迁移回滚、重开后幂等重试、内容冲突、双连接争用、提交失败整体回滚与 Session 失效后读回。默认及 `no_web` 完整 Go 测试与两种 vet 通过，本机的符号链接权限缺项同上。[本地复现](design.md#创建与重试测试)

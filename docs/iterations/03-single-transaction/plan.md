# 迭代 3 PR 计划

当前步骤为 PR 3-4c，交付交易发送前的执行归属、准备调用和中断恢复。[需求](spec.md) · [设计](design.md)

| 步骤 | 单一交付 | 主要检查 |
|---|---|---|
| 3-0 | 需求、设计草案和 PR 计划 | [#36](https://github.com/Conflux-Chain/wallet-bridge/pull/36) 合并为 `30efa4b`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45128899817) 通过 |
| 3-1 | Go 交易输入及校验 | [#37](https://github.com/Conflux-Chain/wallet-bridge/pull/37) 合并为 `6d17b49`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45134102824) 通过 |
| 3-2 | 请求持久化与幂等创建 | [#38](https://github.com/Conflux-Chain/wallet-bridge/pull/38) 合并为 `c6804cb`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45138626143) 通过 |
| 3-3 | 交接前的取消、到期与 Session 生命周期 | [#39](https://github.com/Conflux-Chain/wallet-bridge/pull/39) 合并为 `9a0e9fb`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45144595938) 通过 |
| 3-4a | SQLite 共享执行表与全局唯一占用约束 | [#40](https://github.com/Conflux-Chain/wallet-bridge/pull/40) 合并为 `d1d5662`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45148199287) 通过 |
| 3-4b | 占用状态按请求类型定位 | [#41](https://github.com/Conflux-Chain/wallet-bridge/pull/41) 合并为 `9dc8ba4`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45154618967) 通过 |
| 3-4c | 交易发送前的执行归属与中断恢复 | [#42](https://github.com/Conflux-Chain/wallet-bridge/pull/42) 待评审，已验证同请求多页、双向竞争与重启恢复，发送许可见 3-5 |
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

## PR 3-3 验证

Windows 临时 SQLite 的五组生命周期测试通过，覆盖到期前一毫秒、恰好到期、重开后时钟回拨，以及关闭 Session 时同批请求分别到期和取消。双连接并发创建与关闭重复 30 次通过。[本地复现](design.md#取消与到期测试)

## PR 3-4a 验证

Windows 临时 SQLite 验证了五种旧连接执行状态的升级与失败回滚、请求外键及双向占用。两类请求使用相同 UUID，双数据库连接并发争用重复 30 次通过。默认及 `no_web` Go 测试和 vet 通过，本机跳过需符号链接权限的用例。交易执行由测试写入，浏览器领取和真实钱包发送在后续 PR 验收。[本地复现](design.md#共享执行槽测试)

## PR 3-4b 验证

Windows 上的 SQLite／HTTP 测试验证了同 ID 的交易能阻止连接领取与恢复，服务凭据查询省略钱包占用信息。Chrome 的四项占用提示和连接恢复用例通过，包含旧响应格式。默认及 `no_web` 完整 Go 测试、两种 vet 和独立程序构建通过。本机跳过需符号链接权限的用例，真实交易发送待后续验收。[本地复现](design.md#钱包占用提示测试)

## PR 3-4c 验证

Windows 临时 SQLite 验证四组审阅归属与恢复测试，同 ID 的连接和交易并发领取重复 30 次通过。数据库提交失败时没有返回准备调用许可，关闭 Session 失败时待审批交易和审阅占用一同回滚。默认及 `no_web` Go 测试与 vet 通过。本机符号链接权限缺项同上。本次验证在存储层模拟准备调用，页面和真实钱包验证随接入交付。[本地复现](design.md#交易审阅归属测试)

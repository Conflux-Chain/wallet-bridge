# 迭代 3 PR 计划

当前步骤为 PR 3-8b，提供浏览器审阅归属与准备调用接口。[需求](spec.md) · [设计](design.md)

| 步骤 | 单一交付 | 主要检查 |
|---|---|---|
| 3-0 | 需求、设计草案和 PR 计划 | [#36](https://github.com/Conflux-Chain/wallet-bridge/pull/36) 合并为 `30efa4b`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45128899817) 通过 |
| 3-1 | Go 交易输入及校验 | [#37](https://github.com/Conflux-Chain/wallet-bridge/pull/37) 合并为 `6d17b49`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45134102824) 通过 |
| 3-2 | 请求持久化与幂等创建 | [#38](https://github.com/Conflux-Chain/wallet-bridge/pull/38) 合并为 `c6804cb`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45138626143) 通过 |
| 3-3 | 交接前的取消、到期与 Session 生命周期 | [#39](https://github.com/Conflux-Chain/wallet-bridge/pull/39) 合并为 `9a0e9fb`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45144595938) 通过 |
| 3-4a | SQLite 共享执行表与全局唯一占用约束 | [#40](https://github.com/Conflux-Chain/wallet-bridge/pull/40) 合并为 `d1d5662`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45148199287) 通过 |
| 3-4b | 占用状态按请求类型定位 | [#41](https://github.com/Conflux-Chain/wallet-bridge/pull/41) 合并为 `9dc8ba4`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45154618967) 通过 |
| 3-4c | 交易发送前的执行归属与中断恢复 | [#42](https://github.com/Conflux-Chain/wallet-bridge/pull/42) 合并为 `ccd052c`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45157567046) 通过 |
| 3-5 | 交易发送许可与 Session 关闭竞争 | [#43](https://github.com/Conflux-Chain/wallet-bridge/pull/43) 合并为 `5c1db8f`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45161624223) 通过 |
| 3-6a | 钱包回报及活动记录 | [#44](https://github.com/Conflux-Chain/wallet-bridge/pull/44) 合并为 `8a3eeb5`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45312089699) 通过 |
| 3-6b | 交接后中断与手动放行 | [#45](https://github.com/Conflux-Chain/wallet-bridge/pull/45) 合并为 `af6071b`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45314396864) 通过 |
| 3-7 | Client 侧交易 HTTP 接口及 Swagger | [#46](https://github.com/Conflux-Chain/wallet-bridge/pull/46) 合并为 `fb15269`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45317183499) 通过 |
| 3-8a | 浏览器读取交易及固定钱包，更新 Swagger | [#47](https://github.com/Conflux-Chain/wallet-bridge/pull/47) 合并为 `1905dcc`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45319487050) 通过 |
| 3-8b | 浏览器审阅归属与准备调用接口 | [#48](https://github.com/Conflux-Chain/wallet-bridge/pull/48) 待评审，覆盖多页面、一次性准备调用和中断恢复 |
| 3-8c | 浏览器发送决定及结果接口 | 拒绝、一次性发送许可、回报、重新授权及手动放行 |
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

## PR 3-5 验证

Windows 临时 SQLite 的六组发送许可测试通过，双连接竞争重试、取消及关闭 Session 各跑 30 次。默认及 `no_web` 的完整 Go 测试与 vet 通过，本机跳过需符号链接权限的用例。浏览器调用次数与真实钱包发送随页面接入验收。[本地复现](design.md#一次性发送许可测试)

## PR 3-6a 验证

Windows 的临时 SQLite 实测通过五组结果回报测试，双连接并发写入不同 hash 重复 30 次通过。默认及 `no_web` 完整 Go 测试和两种 vet 通过，本机跳过需符号链接权限的用例。页面保留回报、提示重新授权以及真实钱包操作随浏览器接入验收。[本地复现](design.md#钱包结果测试)

## PR 3-6b 验证

Windows 临时 SQLite 的五组发送中断测试通过，放行与 hash 回报并发重复 30 次。默认及 `no_web` 完整 Go 测试与两种 vet 通过，本机跳过需符号链接权限的用例。浏览器按钮和真实钱包弹窗随页面接入验收。[本地复现](design.md#发送中断恢复测试)

## PR 3-7 验证

Windows 的三组 HTTP 测试通过，覆盖省略意图、幂等重试、Session 关闭后查询、认证、字段与请求大小校验，以及模拟交接后取消和迟到 hash。默认及 `no_web` 完整 Go 测试与两种 vet 通过，本机跳过需符号链接权限的用例。钱包审阅页面和真实测试网发送待后续验收。[本地复现](design.md#交易-http-测试)

## PR 3-8a 验证

Windows 上三组浏览器交易读取 HTTP 测试通过，验证管理页能读到与 client 一致的交易、固定钱包及已关闭 Session 的历史。重复查询保留原期限、空闲时间与执行心跳。默认及 `no_web` 完整 Go 测试和两种 vet 通过，本机因权限跳过符号链接用例。[本地复现](design.md#浏览器交易读取测试)

## PR 3-8b 验证

Windows 上三组 HTTP 协调测试通过。双连接竞争及写锁内到期检查各重复 30 次通过。默认及 `no_web` 完整 Go 测试与两种 vet 通过，本机因权限跳过符号链接用例。实际扩展钱包的弹窗行为随页面接入验收。[本地复现](design.md#浏览器交易审阅协调测试)

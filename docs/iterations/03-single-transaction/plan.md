# 迭代 3 PR 计划

当前步骤为 PR 3-11，向共享 Go client 接入交易创建、查询和取消。[需求](spec.md) · [设计](design.md)

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
| 3-8b | 浏览器审阅归属与准备调用接口 | [#48](https://github.com/Conflux-Chain/wallet-bridge/pull/48) 合并为 `1f4d735`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45321790744) 通过 |
| 3-8c | 浏览器发送决定及结果接口 | [#49](https://github.com/Conflux-Chain/wallet-bridge/pull/49) 合并为 `b2a02ce`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45323830297) 通过 |
| 3-9 | 只读交易审阅页面 | [#50](https://github.com/Conflux-Chain/wallet-bridge/pull/50) 合并为 `32a14fe`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45335710178) 通过 |
| 3-10a | 在审阅页拒绝待审批交易 | [#51](https://github.com/Conflux-Chain/wallet-bridge/pull/51) 合并为 `48bb46d`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45339205191) 通过 |
| 3-10b | 识别 Session 固定钱包，核对实时账户与网络 | [#52](https://github.com/Conflux-Chain/wallet-bridge/pull/52) 合并为 `afdea78`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45341816653) 通过 |
| 3-10c | 领取审阅并修复钱包授权与网络 | [#53](https://github.com/Conflux-Chain/wallet-bridge/pull/53) 合并为 `c5d3d5f`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45344335614) 通过 |
| 3-10d | 发起发送并回报结果 | [#54](https://github.com/Conflux-Chain/wallet-bridge/pull/54) 合并为 `d29adb0`，[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45347739647) 通过 |
| 3-11 | 共享 Go client 的交易操作 | [#55](https://github.com/Conflux-Chain/wallet-bridge/pull/55) 待评审 |
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

## PR 3-8c 验证

Windows 上四组 HTTP 决定测试通过。两个 SQLite 连接同时拒绝和申请许可，重复 30 次通过。默认及 `no_web` 完整 Go 测试和两种 vet 通过，本机因权限跳过符号链接用例。本次通过接口模拟钱包回报，真实钱包发送待页面接入验收。[本地复现](design.md#浏览器交易决定测试)

## PR 3-9 验证

Windows amd64 构建通过。本机 Chrome 的六项交易审阅与占用测试通过，全量浏览器回归 24 项通过。用合成交易验证了金额精度、未知网络、费用来源、复制时传入的完整内容、撤销授权，以及原页显示迟到的 hash。重新授权用例等待页面确认授权后再跳转，本机重复 8 次通过。体验命令实测可打开示例、展开 data、在新标签页查看、显示五分钟到期状态并清理临时服务。真实钱包发送随 3-10 接入。[人工验证指南](../../transaction-review.md)

## PR 3-10a 验证

Windows amd64 构建及本机 Chrome 全部 27 项端到端测试通过。拒绝排队交易时，发起方查到拒绝结果，另一笔交易继续占用钱包。拒绝响应丢失时页面按原 ID 查询，发送许可先提交时显示「等待钱包」。用体验命令启动的合成交易也已实测拒绝、刷新与清理。[人工验证指南](../../transaction-review.md)

## PR 3-10b 验证

Windows amd64 构建通过。本机 Chrome 的 30 项端到端测试与 9 项钱包发现测试通过。模拟 Provider 覆盖第二个授权账户、钱包事件、迟到读数、冲突公告和旧式注入替换。人工体验命令已用 Chrome 和模拟钱包走通连接、创建零金额示例、核对账户与网络变化、拒绝及清理。MetaMask 和 Fluent 的本轮真实扩展核对待人工验收。[人工验证指南](../../transaction-review.md)

## PR 3-10c 验证

Windows amd64 构建通过，本机 Chrome 的 33 项端到端及 9 项钱包发现测试通过。两个标签页同时查看时未领取执行权；切链拒绝后可在原请求重试，关闭原页后可在另一页继续并授权账户。模拟丢失许可响应时钱包调用为零，丢失归还响应时钱包只调用一次。弹窗期间取消后可手动放行，迟到返回保留「已取消」。真实 MetaMask／Fluent 弹窗待人工验收。[本地复现](../../transaction-review.md)

## PR 3-10d 验证

Windows amd64 构建、本机 Chrome 的 42 项端到端和 9 项钱包发现测试通过。模拟钱包实测重复点击仅发送一次，许可丢失或绑定变化时发送零次。钱包拒绝、断连及无效 hash 均显示对应状态。原页刷新或重新授权后能补交已返回的 hash，另一配置手动放行后仍可收到迟到结果。MetaMask／Fluent 的真实测试网广播和回执待人工验收。[本地复现](../../transaction-review.md)

## PR 3-11 验证

Windows 上四组共享 client 测试通过。SQLite 提交后断开响应，按原 ID 重试取回原记录和期限；关闭 Session 后仍能读取原结果。模拟钱包交接后取消会返回「等待钱包」，断连后可按原 ID 取得迟到 hash 和网络。缺少能力的服务未收到 Bearer。真实钱包发送仍待组合验收。[本地复现](design.md#go-client-交易测试)

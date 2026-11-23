# 迭代 2 验收记录

2026-11-23。Windows 已完成 CLI → 本机服务 → Chrome → Session 的组合验证。[首次连接指南](../../first-connection.md)提供自动化与真实钱包两种复现方式。

| [验收项](../../acceptance.md) | 本轮已验证的行为 | 证据 |
|---|---|---|
| A-04 访问控制 | 本机 client 持服务凭据按 ID 访问，普通链接在未授权的浏览器中无法读取或审批请求 | [HTTP 权限测试](../../connection-http.md)、[浏览器原生请求](../../../web/tests/e2e/connection-reads.spec.ts) |
| A-05 本次同意 | 已授权账户仍须选择并同意。CLI 收到的 Session 对应所选钱包、账户与目标 chainId。目标网络名称依次取调用方名称、内置名称，未知网络显示 chainId | [CLI 与连接页面](../../../web/tests/e2e/connection-page.spec.ts) |
| A-06 异常反馈 | 无钱包有提示。钱包拒绝授权、无法自动切链或同意前账户／网络悄然变化，均保留 pending 与原期限。允许手动恢复，页面明确拒绝时 CLI 收到终态 | [发现测试](../../../web/tests/wallet/discovery.spec.ts)、[连接页面](../../../web/tests/e2e/connection-page.spec.ts) |
| A-07 固定绑定 | 错链不能完成，完成后不同绑定重放产生冲突，关闭后仍可查原钱包、账户和网络 | [连接核心与存储](../../connection-storage.md)、[CLI 查询与关闭](../../first-connection.md#模拟钱包回归) |
| A-11 原链接与关闭 | 关闭页面后可找回原请求，手动继续。显式关闭 Session 后可查关闭状态及原连接结果 | [跨标签页找回](../../first-connection.md#跨标签页找回请求)、[Session 历史](../../../internal/store/session_lifecycle_test.go) |
| A-12 期限 | 请求从创建起五分钟到期。有效 Session 经 client 访问续期，读原请求不续期，空闲到期后查询不会复活 | [期限与重启测试](../../../internal/store/session_lifecycle_test.go)、[HTTP 生命周期](../../../internal/api/browser_connections_test.go) |
| A-16 重启恢复 | 服务重启恢复原期限、绑定及结果。中断的连接需要手动继续，迟到的钱包回报不能建立 Session | [服务恢复](../../../internal/service/connection_coordination_test.go)、[存储与回报](../../connection-storage.md)、[CLI 重启查询](../../../web/tests/e2e/connection-page.spec.ts) |
| A-17 单次钱包操作 | 两页同时打开时只有一个执行者。取消后尚未返回的钱包调用仍占用执行槽，原调用返回或用户手动解除等待后才释放 | [双标签页流程](../../../web/tests/e2e/connection-recovery.spec.ts)、[调用交接](../../../internal/store/connection_handoff_test.go) |

## 环境与范围

- Windows amd64 10.0.26300、Chrome 157.0.8103.79、Go 1.27.4、Node 24.19.0。独立程序构建通过。默认与 `no_web` 的完整 Go 测试和 vet 通过，本机因权限跳过平台及 SQLite 的符号链接用例。[复现命令](../../connection-http.md)
- 独立程序与临时服务的 Chrome 测试通过 18 项，钱包发现通过 9 项，钱包行为由模拟 Provider 提供。三平台原生 CI 使用 Chromium，结果见本 PR 的 checks。[页面回归](../../first-connection.md#模拟钱包回归) · [发现回归](../../wallet-discovery.md)
- 用户已在 PR #30 验证 MetaMask 和 Fluent EVM 各一次真实连接。当时由 HTTP 发起，本轮正式 CLI 的端到端证据来自模拟钱包。钱包版本、真实钱包异常分支及逐平台人工结果待补。[人工记录](plan.md#pr-2-6a)
- A-07 的发送前校验、A-11 的关闭后待发交易、A-12 的待交易期间回收、A-16 的不确定交易结果及 A-17 的交易排队，随迭代 3／4 验收。最低系统及干净系统覆盖沿用[迭代 1 的待验项](../01-local-service/acceptance.md#尚待验收)。

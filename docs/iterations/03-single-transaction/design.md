# 单笔交易设计草案

本轮沿用 Go 服务、SQLite、共享 HTTP client 和 React 页面。下表是按现有实现拟定的事务边界，具体接口、状态枚举及迁移随对应 PR 细化。[架构概览](../../architecture.md) · [产品范围](spec.md)

| 时点 | 拟议实现 | 需要证明的性质 |
|---|---|---|
| 创建 | 服务在 SQLite 事务中核对 Session 的有效状态与固定绑定，保存不可变交易和原期限。CLI 与后续 MCP 复用同一 client | 创建和关闭并发时，关闭后不会留下可继续发送的请求。创建响应丢失后可以找回原记录 |
| 领取与核对 | 将现有连接操作的单一执行槽扩展到交易。页面用 Session 的钱包描述重新识别 Provider，候选有歧义时阻止发送并引导用户核对。用户操作前重读账户和网络 | 同一时刻连接与交易只占用一个钱包操作。重载后的 EIP-6963 UUID 不能用作持久身份。仅恢复数据库或打开页面不会弹出钱包 |
| 交给钱包 | 服务事务核对授权、执行归属、期限与 Session，将交接记录落盘。仅首次获准的页面调用一次 `eth_sendTransaction`，使用冻结的字段与固定的 `from`、`chainId` | 取消与交接只有一方先提交。许可响应丢失、页面崩溃或用户重复点击时，重试不能取得第二次发送许可 |
| 回报与重启 | 按原请求和那次发送的标识幂等写入钱包结果及活动记录。已交接而无法确认结果的请求可接受迟到 hash | 即使 Session 已关闭仍可写回原结果。重复回报不重复记账，冲突的回报不能覆盖已保存的 hash |

连接的「手动继续」适用于重新核对钱包。交易已获发送许可后只可恢复结果处理，手动解除占用的条件见[重启规则](spec.md#钱包弹窗与重启)。[连接执行设计](../02-connection-session/design.md)

Session 回收同时涉及显式访问、关闭、启动恢复和周期维护。接入交易时应从这些入口共同验证未完成请求保护、交接前取消和交接后的结果保留。[现有 Session 存储](../../../internal/store/session_lifecycle_test.go)

## PR 3-1 交易输入

Go 应用层接收具名字符串字段和 Session 绑定，产生可按值比较的交易。地址按 20 字节核对，data 按完整字节处理并保留前导零；整数接受十进制或 `0x` 十六进制，输出规范十六进制。金额及费用按最小单位精确处理，显式零与省略费用分别保留。未知字段及显式空字符串返回字段错误。Session 的活跃状态由 PR 3-2 在创建事务内检查。

目标地址保留调用方提供的大小写。value、gas 和费率接受无符号 256 位整数，chainId 沿用连接层的大整数范围。

### 本地验证

在仓库根目录使用 [go.mod](../../../go.mod) 指定的 Go 运行：

```sh
go test -tags no_web ./internal/app -run '^TestTransactionInput' -count=1 -v
```

预期 `TestTransactionInput` 和 `TestTransactionInputErrors` 及其子用例全部通过。测试在内存中校验已有 Session 的绑定与交易字段，命令退出即结束，无需启动服务、安装钱包或清理数据。

## PR 3-2 创建事务

schema v5 保存请求 ID、Session ID、自报来源与意图、规范化交易、创建和到期时间。在 SQLite 写事务内先按请求 ID 查原记录。重试以地址值、规范化数量及字节比较，仍返回首次保存的地址拼写。只有新请求才校验有效 Session，将续期与插入一起提交。空闲时间到达边界时按原期限关闭 Session。创建记录用于重取原始内容，后续的取消、到期和可执行状态在 PR 3-3 接入。

### 创建与重试测试

在 Windows 或 macOS 的仓库根目录，用 [go.mod](../../../go.mod) 指定的 Go 运行：

```sh
go test -tags no_web ./internal/store -run '^TestTransactionRequest' -count=1 -v
```

预期迁移与重试、提交回滚与空闲到期、双连接并发创建三组均通过。命令使用临时 SQLite 数据库，结束后自动清理，无需启动本机服务。

## PR 3-3 交接前的生命周期

schema v6 保存 `pending`、`canceled`、`expired` 和完成时间。v5 已关闭 Session 的交易按关闭时间与原期限回填。启动和周期维护先处理到期交易，再回收空闲 Session。SQLite 在 Session 关闭的同一事务内逐笔结束仍待处理的交易，任何一步失败都会整体回滚。

查询在观察到期限时进入写事务，再读一次最新决定。创建重试、取消和查询返回相同终态，即使服务重启或时钟回拨也保留完成时间。

### 取消与到期测试

在 Windows 或 macOS 的仓库根目录，用 [go.mod](../../../go.mod) 指定的 Go 运行：

```sh
go test -tags no_web ./internal/store -run '^TestTransactionLifecycle' -count=1 -v
```

预期五组测试通过，覆盖期限边界、关闭回滚、并发创建与关闭、未完成请求保护 Session 及 v5 数据恢复。命令使用临时 SQLite，退出时自动清理，无需启动服务。

## PR 3-4a 共享执行表

schema v7 将 v6 的连接执行记录迁入 `wallet_executions`，保留执行 ID、浏览器授权、阶段、心跳和钱包调用 ID。每行只引用一个连接请求或交易请求，各自的外键允许两类请求使用相同 UUID。两类记录共用一个部分唯一索引，`ready`、`calling`、`interrupted_calling` 合计最多一行。现有连接维护只更新连接执行记录。

### 共享执行槽测试

在 Windows 或 macOS 的仓库根目录，使用 [go.mod](../../../go.mod) 指定的 Go 运行：

```sh
go test -tags no_web ./internal/store -run '^TestWalletExecution' -count=1 -v
```

预期迁移、共享占用和请求外键三组测试通过。测试用临时 SQLite 构造五种旧连接状态，并让两个数据库连接竞争同一执行槽。命令退出即自动清理，无需启动服务或打开钱包。

## PR 3-4b 按请求类型展示钱包占用

授权浏览器读取 `GET /api/v1/status` 时，API 1.7 用可选的 `activeWalletRequest {kind, id, phase}` 标识连接或交易。连接占用时另带 1.6 的 `walletOperation`，供已打开的旧页面读取。空闲和服务 Bearer 响应省略两者。查询仅读取共享表。

连接页按类型和 ID 识别自己的请求。交易占用时，管理页与等待页展示交易请求 ID 及阶段，交易详情链接随 3-9 的页面交付。如果占用发生在页面查询之后，领取或恢复连接返回 `409 wallet_busy`，页面重新读取占用状态。

### 钱包占用提示测试

在 Windows 或 macOS 的仓库根目录，按 [README](../../../README.md#开发命令) 安装工具链和前端依赖。浏览器测试需要本机 Chrome 和空闲端口 18765。

```sh
go test -tags no_web ./internal/api -run '^TestBrowserWalletRequestStatus$' -count=1 -v
npm --prefix web run bundle
npm --prefix web run test:e2e:chrome -- wallet-occupancy.spec.ts connection-recovery.spec.ts
```

Go 测试在临时 SQLite 构造交易占用，经 HTTP 验证状态及领取／恢复冲突。浏览器测试自动访问本机服务，预期 `4 passed`。交易占用由模拟状态响应提供，Chrome 管理页显示交易 ID，同 ID 的连接页等待，解除占用后恢复操作。连接找回使用模拟钱包和实际服务。测试结束后自动停止临时服务并清理数据。

## PR 3-4c 交易审阅归属

页面以授权浏览器身份和执行 ID 领取仍有效的交易。领取、换页恢复和连接领取在 SQLite 写事务中竞争同一执行位。账户授权或切链使用单独的调用 ID，重复请求许可只能读回状态。准备调用正常返回后仍由原页面持有执行位，遵循 D-048。

页面离开、心跳超时或服务重启会中断原执行。有未返回的弹窗时保留占用，原调用返回或用户在原请求上手动处理后才放行。恢复有效请求须换执行 ID。取消、到期及 Session 关闭时，审阅状态和交易终态在同一事务内更新。发送许可和发送结果另见 3-5、3-6。

### 交易审阅归属测试

在 Windows 或 macOS 的仓库根目录，使用 [go.mod](../../../go.mod) 指定的 Go 运行：

```sh
go test -tags no_web ./internal/store -run '^TestTransactionReview' -count=1 -v
```

预期四组测试通过，覆盖准备调用只授权一次、切链后保留占用、页面中断与服务重启、同 ID 的两类请求争用，以及提交失败回滚。测试使用临时 SQLite 数据库模拟页面回报，退出后自动清理，无需运行服务或钱包。

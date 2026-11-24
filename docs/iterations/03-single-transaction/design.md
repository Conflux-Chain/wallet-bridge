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

## PR 3-5 一次性发送许可

页面在用户点击发送时提供本次调用 ID 和刚核对的钱包、账户、chainId。服务在同一个 SQLite 写事务中检查浏览器授权、当前审阅归属、待审批状态、原期限和 Session 绑定，随后把请求记为 `awaiting_wallet`，记录交接时间，并把执行位置为 `calling`。只有首次成功提交才返回库中冻结的交易供调用。原调用的重试只读回已交接的事实。

取消和关闭 Session 与发放许可共用数据库写锁。先取消或到期会阻止交接；先取得许可则保留原发送记录和钱包占用。等待钱包的请求保护 Session 免于空闲回收。重启会中断执行并保留占用；交接后的结果、手动放行和迟到 hash 由 3-6 处理。

### 一次性发送许可测试

在 Windows 或 macOS 的仓库根目录，使用 [go.mod](../../../go.mod) 指定的 Go 运行：

```sh
go test -tags no_web ./internal/store -run '^TestTransactionSend' -count=1 -v
go test -tags no_web ./internal/store -run '^TestTransactionSendConcurrent$' -count=30
```

第一条命令预期六组测试通过，包含 v8 数据库升级、重启、交接提交失败和审批到期的最后一毫秒。第二条用两个数据库连接分别竞争重试、取消和关闭 Session，预期全部通过。测试在临时 SQLite 中读取发送许可，命令退出时自动清理。无需启动服务或连接钱包。

## PR 3-6a 钱包回报

授权页面用原请求、执行和发送调用 ID 回报钱包结果。服务按 D-049 分类，在同一事务中保存结果并释放该调用的占用。Session 关闭后仍可回报。钱包返回的 hash 按 32 字节比较。

每个请求最多有一条发送结果记录，与冻结交易关联，作为本地交易活动的数据来源。重复回报保留原时间。迟到的确定结果可更新「结果未确定」，已经记录的 hash 或明确拒绝保留原值。3-6b 单独处理尚未回报时的中断和手动放行。

### 钱包结果测试

在 Windows 或 macOS 的仓库根目录，用 [go.mod](../../../go.mod) 指定的 Go 运行：

```sh
go test -tags no_web ./internal/store -run '^TestTransactionResult' -count=1 -v
go test -tags no_web ./internal/store -run '^TestTransactionResultConcurrentHashes$' -count=30
```

第一条预期五组测试通过，覆盖拒绝、迟到 hash、重新授权补交及失败回滚。第二条用两个 SQLite 连接同时写入不同 hash，预期每次仅保留一个。命令使用临时数据库，退出时自动清理，无需运行服务或打开钱包。

## PR 3-6b 发送调用中断

页面主动中断、心跳超时、授权失效及服务重启共用恢复规则。SQLite 在中断执行的同一事务内为已交接的发送记录「结果未确定」。原调用继续占用钱包，也继续保护 Session 免于空闲回收。恢复时补记旧版本已中断且缺少结果的发送。

管理页凭当前授权和交易请求 ID 确认已处理弹窗。只有已中断的发送可由此释放。放行保留执行和调用 ID，重复确认返回原交易当前状态。原钱包回报与放行串行提交，迟到的回报仍可更新同一条活动记录。

### 发送中断恢复测试

在 Windows 或 macOS 的仓库根目录，使用 [go.mod](../../../go.mod) 指定的 Go 运行：

```sh
go test -tags no_web ./internal/store -run '^TestTransactionInterruption' -count=1 -v
go test -tags no_web ./internal/store -run '^TestTransactionInterruptionConcurrentHash$' -count=30
```

第一条预期五组测试通过，覆盖中断恢复、跨配置放行、准备调用隔离、写入回滚和迟到 hash。第二条用两个 SQLite 连接同时确认放行和提交 hash，每次均应保存 hash 并释放占用。测试退出时自动清理临时数据库，无需启动服务或打开钱包。

## PR 3-7 Client 交易 HTTP 接口

API 1.8 通过 `transactions.lifecycle` 声明能力。使用本机服务 Bearer 在 `/api/v1/transactions` 创建请求，按 ID 查询或取消。创建和幂等重试均返回 200，同 ID 内容冲突返回 409。取消返回已生效的状态，交接后的客户端据此继续查询结果。

创建请求体上限选用 1 MiB，以容纳合约调用的十六进制 data。原始交易放在 `transaction` 对象内，数量使用字符串。响应包含规范化交易、原期限和已记录的结果，发送回报中取得的 hash 位于 `txHash`。

### 交易 HTTP 测试

在 Windows 或 macOS 的仓库根目录，使用 [go.mod](../../../go.mod) 指定的 Go 执行以下命令。测试直接访问 HTTP handler，使用临时 SQLite，无需启动服务或钱包。

```sh
go test -tags no_web ./internal/api -run '^TestTransactionHTTP' -count=1 -v
```

预期三组测试均显示 `PASS`。Lifecycle 验证省略意图、按 ID 重试和取消。Boundary 验证服务凭据、原始字段及请求大小。WalletResult 模拟钱包交接和迟到的 hash，验证 client 能查到最终结果。测试结束时自动清理临时数据库。[接口定义](../../swagger/swagger.yaml)

## PR 3-8a 浏览器读取交易

已授权的管理页通过 `GET /api/v1/browser/transactions/{id}` 读取原交易及其 Session 固定的钱包。响应沿用 client 的交易字段，增加 `wallet`。页面可用 `transaction.from` 和 `transaction.chainId` 核对账户和网络。钱包描述在 Session 建立时固定，关闭后仍可读取。

普通页面链接只包含请求 ID。接口沿用管理 Cookie 及同源检查，每次读取按原期限计算交易状态。查询不领取钱包、不刷新执行心跳或 Session 空闲计时。执行协调和发送结果回报分别由 3-8b 和 3-8c 交付。

### 浏览器交易读取测试

在 Windows 或 macOS 的仓库根目录，使用 [go.mod](../../../go.mod) 指定的 Go 运行以下命令。测试通过 HTTP handler 读取临时 SQLite 中的交易，无需启动服务或安装钱包。

```sh
go test -tags no_web ./internal/api -run '^TestBrowserTransactionHTTP' -count=1 -v
```

预期 Observation、Deadline 和 Access 三组测试均显示 `PASS`，分别覆盖重复打开与关闭后的历史、原审批期限、管理 Cookie 和同源访问。退出时自动清理测试数据。

## PR 3-8b 浏览器审阅协调

管理页在 `/api/v1/browser/transactions/{id}/execution` 用 GET 观察进度，用 POST 执行操作。GET 返回阶段，尚未领取为 `unclaimed`。仅在发送前已中断时返回 `recoverFromId`，供用户手动继续。页面自行保留当前执行及钱包调用 ID。

POST 要求管理 Cookie 及本机 Origin，请求体含 `action` 和页面生成的执行 `id`。各操作复用 3-4c 的 SQLite 事务。

| action | 其他字段及含义 |
|---|---|
| `claim`、`heartbeat`、`interrupt` | 领取、续执行心跳或中断本页 |
| `recover` | 用新执行 ID 和 `previousId = recoverFromId` 确认继续。存在未结束的弹窗时，先由用户处理 |
| `begin_preparation` | 新的 `callId`，第二次及后续调用另带上一次的 `previousId`。仅本次响应含 `invoke: true` 时调用钱包授权或切链 |
| `return_preparation` | 用原 `callId` 确认弹窗已结束，之后原页面可继续核对交易 |

提交发生通信错误时使用原 ID 和内容重试。执行已变更返回 `409 execution_conflict`，其他请求占用钱包返回 `409 wallet_busy`。发送许可、交接后的结果及手动放行见 3-8c。

### 浏览器交易审阅协调测试

在 Windows 或 macOS 的仓库根目录使用 [go.mod](../../../go.mod) 指定的 Go 执行以下命令。测试直接访问 HTTP handler 和临时 SQLite，无需启动服务或安装钱包。

```sh
go test -tags no_web ./internal/api -run '^TestBrowserReviewHTTP' -count=1 -v
go test -tags no_web ./internal/store -run '^(TestTransactionReviewDeadlineUnderWriteLock|TestTransactionReviewConcurrentClaim)$' -count=30
```

第一条预期三组测试通过，覆盖多页面、准备调用重试、中断恢复、授权撤销和发送后的观察。第二条验证连接与交易只有一方领取成功，以及服务取得数据库写锁后按实际时间检查期限。退出时自动清理测试数据。

## PR 3-8c 浏览器发送决定与结果

在 `/api/v1/browser/transactions/{id}` 下使用四个 POST 操作，均要求有效管理 Cookie 和本机 Origin。

| 操作 | 页面提交 | 返回 |
|---|---|---|
| `reject` | 空请求体，可从排队页操作 | 先于交接时记录明确拒绝；重复或已过期、已交接时返回已生效状态 |
| `send` | 原执行 ID、新调用 ID、上一次准备调用 ID（如有），以及刚读到的钱包、账户和网络 | 首次提交成功返回一次许可和冻结交易。原调用重试返回 `invoke: false` |
| `result` | 原执行 ID、原发送调用 ID 和钱包返回的 hash 或错误码 | 记录本次发送结果，支持重新授权后的补交和迟到 hash |
| `acknowledge` | 用户确认已处理原弹窗后提交空请求体 | 放行已中断的发送，保留原结果与后续回报能力 |

审阅页拒绝时，在同一 SQLite 事务内关闭待审批请求并写入 `rejected` 结果。已有的 `transaction_status` 视图向各入口提供同一结果。通过 `handedOffAt` 可区分页面拒绝和钱包发送弹窗拒绝。

### 浏览器交易决定测试

在 Windows 或 macOS 的仓库根目录，使用 [go.mod](../../../go.mod) 指定的 Go 执行以下命令。测试通过 HTTP handler 和临时 SQLite 验证行为，无需启动服务或安装钱包。

```sh
go test -tags no_web ./internal/api -run '^TestTransactionDecisionHTTP' -count=1 -v
go test -tags no_web ./internal/api -run '^TestTransactionDecisionConcurrent$' -count=30
```

第一条预期四组测试通过，覆盖排队拒绝、结果写入失败回滚、许可响应重试、跨浏览器放行、重新授权后补交 hash、钱包拒绝及到期。第二条用两个数据库连接同时拒绝和申请发送许可，预期每次只生效一个决定。命令退出时清理临时数据库。

## PR 3-9 只读交易审阅页

普通链接采用 `/?transaction=<请求 ID>`。复用管理页授权流程，授权后从 3-8a 的 GET 接口读取内容。原链接可刷新或重新打开；等待审批、等待钱包和结果未确定时持续读取状态。钱包占用提示链接到对应交易。

页面分开展示调用方自报的名称和意图、Session 固定的钱包与网络、冻结的交易参数及结果。地址、hash 和完整 data 可复制，长 data 按需展开。数量以整数格式化，费用标明调用方提供或由钱包估算。页面读取无需领取执行或调用 Provider。钱包实时核对、切链、拒绝和发送在 3-10 接入。

## PR 3-10b 钱包状态核对

待审批页面用发现方式、名称和 rdns 匹配标准钱包。唯一匹配时只读账户与网络，旧式入口按 D-057 显式确认。比对固定地址是否在当前授权账户列表内，同时比对 chainId。钱包事件、页面重新聚焦或手动核对会更新读数。候选冲突、断连或读取失败时清除旧读数。弹窗操作与执行归属由 3-10c 接入。

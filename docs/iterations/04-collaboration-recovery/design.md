# 协作与恢复设计

## 读取待审批请求

`GET /api/v1/browser/pending-requests` 沿用浏览器管理 cookie、固定 Host／Origin 和只读请求约束。返回 `requests` 数组，每项含 `kind`、`id`、`clientName`、`chainId`、`createdAt` 和 `expiresAt`。详情沿用原审阅链接。前端通过能力 `browser.pending-requests` 判断服务是否提供列表。

SQLite 用一条查询读取两类仍为 pending 且未到期的请求，共用一次读取时间。同毫秒依次按种类与 ID 排序，两种请求可使用同一 ID。复用原请求表和执行槽，无需迁移。执行权仍由用户操作时的领取事务决定。

## 活动记录查询

管理页通过 `browser.activity` 能力发现 `GET /api/v1/browser/activity`，使用浏览器管理 cookie。每条摘要包含请求类型、ID、调用方、网络、创建时间、当前状态，以及已有的 Session ID 和交易哈希。详情沿用原请求入口。

同一次 SQLite 读取合并连接请求和交易状态视图，按创建时间、ID、类型依次倒序。待审批请求越过期限即显示已到期。使用两个创建时间索引和 `limit`／`after` 游标向前翻阅历史，每页默认及最多 50 条。读取沿用原始记录，旧数据和后续回报均可查询。

## 本地验证

在装有项目指定 Go 版本的 Windows 或 macOS 仓库根目录执行以下命令。

```sh
go test -tags no_web ./internal/api -run '^TestBrowserPendingRequests' -count=1 -v
```

预期两项测试通过。用例通过 HTTP handler 和临时 SQLite 建立先到连接、后到交易及同 ID 的两类请求，核对顺序、读取权限、期限和交接后移出列表。测试自动清理，无需启动服务或钱包。

### 验证活动记录接口

在 Windows 或 macOS 仓库根目录，使用 [go.mod](../../../go.mod) 指定的 Go 版本执行：

```sh
go test -tags no_web ./internal/api -run '^TestBrowserActivity' -count=1 -v
```

预期三个用例通过。测试经 HTTP 接口读取临时 SQLite，验证同毫秒同 ID 的跨页边界、翻页时插入新请求、重试去重、到期显示、读取权限，以及关闭 Session 后补回哈希。测试自动清理，无需占用本机服务端口。

## 验证管理首页

在 Windows 或 macOS 仓库根目录，按 [README 环境准备](../../../README.md#环境准备)安装依赖。首页集成测试需要空闲的本机端口 18765。

```sh
npm --prefix web run bundle
npm --prefix web run browser:install
npm --prefix web run test:e2e -- pending-requests.spec.ts
```

测试自动打开临时服务的已授权首页，预期 `3 passed`。同 ID 的连接和交易各有独立入口，后到交易可以先取得钱包并在模拟钱包中切链。回到首页可看到仍待审批的连接，调用方取消后点击「重新检查」即可看到它移出列表。测试结束时会停止临时服务并清理数据。

手动体验时，用包含此变更的独立程序运行 `wallet-bridge ui`。保持管理首页打开，再运行以下两条命令。使用自定义数据目录时，所有命令都附上相同的 `--data-dir`。

```sh
wallet-bridge connect --chain-id 1 --client-name Codex --no-open --no-wait
wallet-bridge connect --chain-id 71 --client-name WorkBuddy --no-open --no-wait
```

切回首页可见 Codex 在前、WorkBuddy 在后，两项均可打开。点击第二项，在原审阅页选择「拒绝本次连接」，回到首页点击「重新检查」，第一项仍可打开。用同样方式拒绝第一项。若此次启动了专用测试服务，运行 `wallet-bridge service stop` 收尾。

## 验证活动记录页面

沿用上一节的环境和两个 CLI 请求，在已授权首页点击「查看活动记录」。最近创建的请求排在前面。点击名称可打开原请求，拒绝后返回活动页点击「刷新记录」，应看到同一条记录更新为「用户已拒绝」。另一个请求也可按上述步骤完成和清理。每页最多 20 条，有更早的记录时可翻页，刷新会更新当前页。交易收到钱包回传的哈希后显示「已发送」及完整哈希。

在仓库根目录、端口 18765 空闲时运行：

```sh
npm --prefix web run bundle
npm --prefix web run browser:install
npm --prefix web run test:e2e -- activity.spec.ts
```

预期三个用例通过。首项使用临时服务创建同 ID 的连接和交易，关闭 Session 后核对状态及原请求链接。其余用例模拟分页、迟到哈希、服务错误和浏览器授权失效。测试自动停止服务并清理临时数据。

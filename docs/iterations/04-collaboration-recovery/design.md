# 协作与恢复设计

## 读取待审批请求

`GET /api/v1/browser/pending-requests` 沿用浏览器管理 cookie、固定 Host／Origin 和只读请求约束。返回 `requests` 数组，每项含 `kind`、`id`、`clientName`、`chainId`、`createdAt` 和 `expiresAt`。详情沿用原审阅链接。前端通过能力 `browser.pending-requests` 判断服务是否提供列表。

SQLite 用一条查询读取两类仍为 pending 且未到期的请求，共用一次读取时间。同毫秒依次按种类与 ID 排序，两种请求可使用同一 ID。复用原请求表和执行槽，无需迁移。执行权仍由用户操作时的领取事务决定。

## 本地验证

在装有项目指定 Go 版本的 Windows 或 macOS 仓库根目录执行以下命令。

```sh
go test -tags no_web ./internal/api -run '^TestBrowserPendingRequests' -count=1 -v
```

预期两项测试通过。用例通过 HTTP handler 和临时 SQLite 建立先到连接、后到交易及同 ID 的两类请求，核对顺序、读取权限、期限和交接后移出列表。测试自动清理，无需启动服务或钱包。

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

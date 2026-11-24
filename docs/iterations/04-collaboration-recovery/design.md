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

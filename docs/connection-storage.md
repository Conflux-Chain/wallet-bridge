# 连接存储与事务测试

PR 2-3a2b 将连接／Session 存储升级到 schema v3，按 ID 查询、访问及关闭，移除独立凭据。此前的连接原子完成和生命周期规则继续保留。本指南验证存储层，尚无 HTTP／CLI／页面连接入口，无需新增人工浏览器操作。

## 本地验证

前置条件：Windows 或 macOS，安装 [go.mod](../go.mod) 指定的 Go。以下命令在仓库根目录运行，不需要 Node.js、钱包、资金或已启动的 Bridge 服务：

```sh
go test -tags no_web ./internal/store -run '^Test(Connection|Session)' -count=1 -v
```

预期 27 组测试（含子用例）通过、退出码为 0，覆盖：

- 从 v1 授权库及有记录的 v2 库迁移，成功后原授权、全部连接终态、活跃／关闭 Session 保留；复制或表重建后的失败整体回滚。
- 请求与 Session 持久化、大 chainId、毫秒时间及原始期限保留；读取不续期或刷新访问时间。
- 按 ID 查询／访问选定记录、重复与并发创建保护，关闭 Session 后原连接结果仍可读取；不再测试 client 间凭据隔离。
- 数据约束与缺表／损坏列拒绝；取消 Go context 时不写入记录。
- 连接成功与 Session 同时提交；插入／提交失败或 ID 冲突时整体回滚。
- 取消／拒绝后迟到成功不建 Session；恰好到期也不建 Session，期限不重置。
- 相同回传保留原结果，冲突回传不能改写绑定；两个数据库连接并发完成／取消／到期只留下一个终态。
- 仅有效 Session 的 client 访问续期，原始查询不续期；关闭／回收后历史可查、重启或时钟回退也不复活。
- Session 访问／关闭及批量维护的更新／提交失败整体回滚；跨数据库连接并发访问、关闭与到期不会改写固定绑定。

仅验证本次资源凭据移除的 3 组 schema／迁移测试：

```sh
go test -tags no_web ./internal/store -run '^TestConnectionResource' -count=1 -v
```

预期新库无资源摘要列；旧库升级及重开保留原结果和时间，成功回传可不带令牌重试；失败回滚后原表、字段及记录仍完整，修复测试注入故障后可重试升级。索引、外键及原有数据约束继续有效，旧版迁移器拒绝 v3。

仅运行 8 组连接事务测试：

```sh
go test -tags no_web ./internal/store -run '^TestConnection(Completion|Finish|Transitions)' -count=1 -v
```

仅运行 8 组生命周期测试：

```sh
go test -tags no_web ./internal/store -run '^(TestSessionLifecycle|TestConnectionSessionExpiryMaintenance)' -count=1 -v
```

测试使用临时私有数据库，命令完成即结束并自动清理，无需额外停止；不占用固定服务端口，也不迁移正式用户数据库。部分读取／约束测试用 SQL 准备数据，事务测试调用实际存储方法；均不代表真实钱包连接已验收。

完整后端回归：

```sh
go test -tags no_web ./... -count=1
go vet -tags no_web ./...
```

## 数据与交付边界

数据库位置及保护方式见[本地服务指南](local-service.md#sqlite-存储测试与数据)。新程序启动时会追加 schema v3；仅支持 v1／v2 的程序会拒绝该版本。测试和构建不更新正在运行的旧服务，正式升级按用户授权执行。

连接／Session 不保存访问令牌或摘要，浏览器授权的 cookie 摘要独立保留；本机接入仍使用[统一服务凭据](local-credentials.md)。`closed_at` 表示显式关闭或空闲回收后的失效时间，历史读取不等于允许钱包操作。维护方法尚未接入启动／周期调用，应用核心、浏览器权限和外部接入由后续 PR 交付。状态与验证证据统一见[迭代 plan](iterations/02-connection-session/plan.md)。

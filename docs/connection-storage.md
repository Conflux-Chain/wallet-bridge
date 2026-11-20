# 连接存储与事务测试

PR 2-2a／2-2b 提供 schema v2、凭据查询和连接原子完成；PR 2-3a1 增加 Session 访问／关闭和批量到期存储。本指南验证存储层，尚无 HTTP／CLI／页面连接入口，无需新增人工浏览器操作。

D-034 已确认资源只按 ID 定位；本 PR 先统一服务凭据，schema v2 暂留旧摘要列／接口。下一个独立 PR 负责迁移及更新本指南，不将当前存储测试视为新访问模型已全部实现。

## 本地验证

前置条件：Windows 或 macOS，安装 [go.mod](../go.mod) 指定的 Go。以下命令在仓库根目录运行，不需要 Node.js、钱包、资金或已启动的 Bridge 服务：

```sh
go test -tags no_web ./internal/store -run '^Test(Connection|Session)' -count=1 -v
```

预期 24 组测试（含子用例）通过、退出码为 0，覆盖：

- 从 v1 授权库迁移，升级失败整体回滚，成功后原授权保持不变。
- 请求与 Session 持久化、大 chainId、毫秒时间及原始期限保留；读取不续期或刷新访问时间。
- 不同 ID／凭据隔离、重复与并发创建保护，关闭 Session 后原连接结果仍可读取。
- 数据约束与缺表／损坏列拒绝；取消 Go context 时不写入记录。
- 连接成功与 Session 同时提交；插入／提交失败或 ID／摘要冲突时整体回滚。
- 取消／拒绝后迟到成功不建 Session；恰好到期也不建 Session，期限不重置。
- 相同回传保留原结果，冲突回传不能改写绑定；两个数据库连接并发完成／取消／到期只留下一个终态。
- 仅有效 Session 的凭据访问续期，原始查询不续期；关闭／回收后历史可查、重启或时钟回退也不复活。
- Session 访问／关闭及批量维护的更新／提交失败整体回滚；跨数据库连接并发访问、关闭与到期不会改写固定绑定。

仅运行 PR 2-2b 新增的 8 组事务测试：

```sh
go test -tags no_web ./internal/store -run '^TestConnection(Completion|Finish|Transitions)' -count=1 -v
```

仅运行 PR 2-3a1 新增的 8 组生命周期测试：

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

数据库位置及保护方式见[本地服务指南](local-service.md#sqlite-存储测试与数据)。新程序启动时会追加 schema v2；v1 程序会拒绝打开较新 schema。测试和构建不更新正在运行的旧服务，正式升级按用户授权执行。

记录只保存凭据摘要；返回模型不包含摘要或原始凭据。访问／关闭方法在事务内验证 Session 凭据；`closed_at` 表示显式关闭或空闲回收后的失效时间，历史读取不等于允许钱包操作。维护方法尚未接入启动／周期调用，应用核心、浏览器权限和外部接入由后续 PR 交付。状态与验证证据统一见[迭代 plan](iterations/02-connection-session/plan.md)。

# 连接记录与迁移测试

PR 2-2a 提供连接／Session 数据模型、SQLite schema v2、创建待处理连接，以及按 ID／凭据摘要读取记录。Session 的创建与连接完成事务、取消／到期、核心流程和 HTTP／CLI／页面接入由后续 PR 交付；本次没有新增人工浏览器操作。

## 本地验证

前置条件：Windows 或 macOS，安装 [go.mod](../go.mod) 指定的 Go。以下命令在仓库根目录运行，不需要 Node.js、钱包、资金或已启动的 Bridge 服务：

```sh
go test -tags no_web ./internal/store -run '^Test(Connection|Session)' -count=1 -v
```

预期 8 组测试（含子用例）通过、退出码为 0，覆盖：

- 从 v1 授权库迁移，升级失败整体回滚，成功后原授权保持不变。
- 请求与 Session 持久化、大 chainId、毫秒时间及原始期限保留；读取不续期或刷新访问时间。
- 不同 ID／凭据隔离、重复与并发创建保护，关闭 Session 后原连接结果仍可读取。
- 数据约束与缺表／损坏列拒绝；取消 Go context 时不写入记录。

测试使用临时私有数据库，完成后自动清理，不占用固定服务端口，也不迁移正式用户数据库。Session 读取场景通过测试 SQL 准备数据，不代表已提供 Session 创建接口。

完整后端回归：

```sh
go test -tags no_web ./... -count=1
go vet -tags no_web ./...
```

## 数据与交付边界

数据库位置及保护方式见[本地服务指南](local-service.md#sqlite-存储测试与数据)。新程序启动时会追加 schema v2；v1 程序会拒绝打开较新 schema。测试和构建不更新正在运行的旧服务，正式升级按用户授权执行。

记录只保存凭据摘要；返回模型不包含摘要或原始凭据。此层的历史记录查询不等于授权执行钱包操作，活跃 Session 检查、访问时间更新与回收由后续核心实现。状态与验证证据统一见[迭代 plan](iterations/02-connection-session/plan.md)。

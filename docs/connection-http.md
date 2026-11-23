# 连接与 Session HTTP 测试

已实现 client 侧创建／查询／取消连接及访问／关闭 Session，API 1.3 公告 `connections.lifecycle`。浏览器执行 HTTP、共用 Go client、CLI 连接命令及连接页面尚未接入；本单元没有真实钱包操作或可用的连接页面链接。

## 本地验证

前置条件：Windows 或 macOS，安装 [go.mod](../go.mod) 指定的 Go。在仓库根目录运行；无需 Node.js、服务、浏览器、钱包或资金：

```sh
go test -tags no_web ./internal/api -run '^TestConnectionHTTP' -count=1 -v
```

预期 7 组测试（含并发子用例）通过、退出码为 0，验证：

- 超过 1 KiB 的连接说明可提交；十进制／十六进制的大 chainId 精确等值，相同 ID 重试与数据库重开保留原状态和期限，不同内容报 409。
- 五个入口只接受服务 Bearer；无凭据、cookie、错误／重复凭据及错误 Host／Origin 被拒绝，不创建连接、取消请求或续期／关闭 Session。
- 参数、未知字段、尾随 JSON、媒体类型、body 大小、方法及资源错误按接口契约返回；客户端不能设置状态、期限或直接建立 Session。
- 查询原连接不续期 Session；有效 Session 访问才续期，关闭／空闲回收后仍返回固定绑定和原时间，重开与重试不复活。
- 取消保持幂等，未结束的钱包调用仍占用等待；到期返回原期限，迟到取消报冲突。
- 存储不可用时返回稳定的 `storage_error`，不误报成功或泄露数据库细节；重开后可查询原记录并重试。
- 使用随机端口真实 HTTP 并发提交：相同内容均返回同一记录，不同内容仅一个成功，其余报冲突。

测试使用私有临时 SQLite 和系统分配的端口，命令结束自动关闭服务并清理，无需停止正式 Bridge。历史 Session 和钱包调用阶段由内部方法准备，不表示真实钱包授权或浏览器审批已验收。

完整后端回归：

```sh
go test -tags no_web ./... -count=1
go vet -tags no_web ./...
```

## 接口与交付边界

路径、输入、响应及权限由 [handler 注释](../internal/api/connections.go)维护，生成文档见 [Swagger JSON](swagger/swagger.json)／[YAML](swagger/swagger.yaml)；生成步骤见[本地服务指南](local-service.md#接口文档与后端检查)。本单元的范围与取舍见[当轮 design](iterations/02-connection-session/design.md#pr-2-4aclient-http-接入)。

正式服务仍使用固定 `127.0.0.1:18765`；测试和构建不重启或升级已运行的程序。独立程序的固定端口组合验证在空闲的 CI 环境执行，实际检查及 PR 进度见[当轮 plan](iterations/02-connection-session/plan.md)。

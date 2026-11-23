# 连接与 Session HTTP 测试

API 1.6 支持 client 侧连接／Session，以及浏览器执行协调、钱包交接与连接决定。浏览器仍需页面调用 Provider，管理 cookie 读取服务状态可获取占用钱包的请求 ID 与阶段，空闲时省略。浏览器复现步骤见[首次连接指南](first-connection.md)。

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

## 浏览器执行协调 HTTP 验证

前置条件及工作目录同上，无需服务、浏览器或钱包：

```sh
go test -tags no_web ./internal/api -run '^TestBrowserConnectionHTTP' -count=1 -v
```

预期 9 组测试（含子用例）通过、退出码为 0，验证：

- 六个入口都要求管理 cookie。GET 可以省略 Origin，POST 要求精确 Origin；拒绝过期／撤销 cookie、重复 cookie／Origin、伪造归属字段、错误方法和超限 body。
- 读取不领取、不续心跳或 Session；领取／恢复重放保留心跳，不返回浏览器授权 ID、cookie 或摘要。
- 其他页面可读进度，写入须匹配当前主体和执行；中断后显式恢复，旧 ID 不能再操作，新请求不绕过全局执行槽。
- 心跳失联不能续期复活；连接到期保留原期限。取消／到期／重启不证明钱包调用已结束，未知调用仍占用等待；终态恢复只解除等待，不创建新执行或 Session。
- 在认证与写事务之间撤销授权，四类写入均被拒绝；更新／插入及提交失败不误报成功、原执行不变，移除测试故障后可重试。
- 随机端口真实 HTTP 验证两个浏览器竞争同一请求或不同请求，只有一个领取成功。

测试使用自动清理的私有临时 SQLite 与随机端口；命令结束即停止并清理。历史授权／期限由测试数据准备，调用阶段由内部方法模拟；重启场景重开数据库并调用真实恢复核心，未调用钱包或渲染连接页面。

### 浏览器钱包交接与决定

在仓库根目录运行 `go test -tags no_web ./internal/api -run '^TestBrowserDecisionHTTP' -count=1 -v`。两组测试使用临时 SQLite 并自动清理，检查调用许可重放、前驱调用 ID、最终同意／拒绝、错误网络及恢复后旧执行失效。

### 在 Chrome 中验证读取

安装 Chrome 和 [README](../README.md#环境准备) 中的工具，在仓库根目录执行以下命令。测试需要本机 18765 端口空闲。

```sh
npm ci --prefix web
npm --prefix web run bundle
npm --prefix web run test:e2e:chrome -- connection-reads.spec.ts
```

预期为 `1 passed`。测试创建临时服务和独立浏览器上下文，用页面原生 fetch 读取连接与执行，核对授权前的 401、授权后的结果、钱包交接和同意，最后从 client 接口核对测试账户与网络。测试结束自动停止临时服务并清理数据。

## 接口与交付边界

路径、输入、响应及权限由 [client handler](../internal/api/connections.go) 和[浏览器 handler](../internal/api/browser_connections.go)注释维护，生成文档见 [Swagger JSON](swagger/swagger.json)／[YAML](swagger/swagger.yaml)；生成步骤见[本地服务指南](local-service.md#接口文档与后端检查)。范围与取舍见[当轮 design](iterations/02-connection-session/design.md)。

正式服务仍使用固定 `127.0.0.1:18765`；测试和构建不启动或升级用户服务。独立程序的固定端口组合验证在空闲的 CI 环境执行，实际检查及 PR 进度见[当轮 plan](iterations/02-connection-session/plan.md)。认证期间的存储失败沿用 `authorization_error`，认证后核心操作的存储失败为 `storage_error`；两者都不返回内部细节。

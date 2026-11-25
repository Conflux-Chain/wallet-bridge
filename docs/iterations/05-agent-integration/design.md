# MCP 适配设计

以 `wallet-bridge mcp` 启动 STDIO 进程，使用官方 Go MCP SDK。在 `internal/mcp` 注册工具，复用 `internal/client` 接入后台。共享服务持有请求和 Session。[模块设计](../00-foundation/design.md#3-模块职责)

| 操作 | 调用与结果 |
|---|---|
| 初始化、列出工具 | 本地完成协议握手／工具发现。标准输出仅传输 MCP 消息，诊断写标准错误。共享服务在首个钱包工具调用时按需启动或复用。 |
| 创建连接或交易 | 通过 HTTP client 提交，返回请求类型、ID、原链接、状态和期限。在工具结果中同时提供 agent 可读取的结构化字段和可展示的链接。 |
| 查询与取消 | 按请求类型和 ID 调用对应 client 方法。创建和查询分开，断开 STDIO 只结束本地调用。查询原请求可跨 MCP 进程恢复，显式取消以服务记录为准。 |
| 查询或关闭 Session | 复用 Session 接口。连接成功后按返回的 Session ID 读取钱包、账户和网络。显式查询有效 Session 会续期。 |

连接等待由仍在执行任务的 agent 发起。先展示链接，再间隔查询原请求，收到终态后结束等待。请求 ID 可放入对话上下文，服务端记录是恢复依据。

### 首次连接工具

`create_connection` 接受 `requestId`、`chainId` 和可选的 `chainName`、`intent`。ID 沿用 HTTP 的 32 位小写十六进制格式，网络接受十进制或十六进制字符串。`get_connection` 只接受原请求 ID，可跨 MCP 进程查询。两者返回结构化记录及原链接，连接成功时带回 Session ID。

`cancel_connection` 取消仍待审批的连接。若服务报告终态冲突，工具查询原请求并返回已生效的结果。用户结束本轮等待后仍可稍后继续。

### Session 工具

连接成功后，agent 用返回的 `sessionId` 调用一次 `get_session`，向用户展示固定的钱包、账户和网络。显式查询有效 Session 会刷新五分钟空闲计时。轮询连接使用 `get_connection`，保留原空闲期限。`close_session` 返回关闭状态，重复关闭和后续查询保留原绑定与关闭时间。[Session 规则](../02-connection-session/spec.md)

### 创建单笔交易

`create_transaction` 接受 `requestId`、`sessionId`、可选 `intent` 和原始 `transaction` 对象。工具列出支持的交易字段，金额与费用使用最小单位字符串。调用方名称取自 MCP 握手。服务核对 Session 绑定、冻结交易，返回规范化字段、状态、期限和原审阅链接。重试携带原 ID 和原内容，每笔新交易分配新 ID。[输入及幂等规则](../03-single-transaction/spec.md#创建请求)

## 验证 STDIO 接入

在 Windows 或 macOS 按 [README 环境准备](../../../README.md#环境准备)安装依赖，从仓库根目录运行：

```sh
npm --prefix web run bundle
npm --prefix web run smoke
```

预期输出 `MCP discovery passed` 和 `Smoke passed`。测试通过管道启动构建出的程序，完成握手、列出工具，再关闭标准输入并检查进程正常退出。工具发现阶段的临时用户目录应保持为空。测试会自动清理该目录，运行期间可保留本机已有的 HTTP 服务。

## 验证 MCP 连接

在 Windows 或 macOS 仓库根目录按上一节构建程序，确认 18765 端口空闲后执行：

```sh
npm --prefix web run browser:install
npm --prefix web run test:e2e -- mcp-connection.spec.ts
```

预期 `2 passed`。第一项使用临时服务和模拟钱包核对重试、用户同意、Session 绑定、重复关闭和历史查询。第二项验证服务重启后取消待审批连接，重复取消仍返回同一结果。测试结束会停止临时服务并清理数据。真实 Codex 验收见[迭代计划](plan.md)。

## 验证 MCP 交易创建

沿用上一节的构建、浏览器安装及空闲端口条件，在仓库根目录运行 `npm --prefix web run test:e2e -- mcp-transaction.spec.ts`。预期 `1 passed`。用例从 STDIO 提交含 calldata 的请求，在模拟钱包页面核对原数据与缺省意图，验证账户／网络不符、重复创建、内容冲突及关闭 Session 后取回原结果。测试自动停止临时服务并清理数据。

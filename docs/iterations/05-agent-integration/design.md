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

`create_connection` 接受 `requestId`、`chainId` 和可选的 `chainName`、`intent`。ID 沿用 HTTP 的 32 位小写十六进制格式，网络接受十进制或十六进制字符串。`get_connection` 只接受原请求 ID，可跨 MCP 进程查询。两者返回结构化记录及原链接，连接成功时带回 Session ID。读取绑定账户使用后续 5-3 的显式 Session 查询，轮询原连接不会给 Session 续期。

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

预期 `1 passed`。测试用 STDIO 客户端启动 MCP 子进程，用临时服务和模拟钱包核对原链接、重试冲突、浏览器同意和 Session 关闭后的历史结果。测试结束会停止临时服务并清理数据。真实 Codex 验收见[迭代计划](plan.md)。

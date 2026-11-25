# MCP 适配设计

以 `wallet-bridge mcp` 启动 STDIO 进程，使用官方 Go MCP SDK。5-1 已实现协议握手与工具发现，当前工具列表为空。后续在 `internal/mcp` 注册钱包工具，复用 `internal/client` 接入后台。共享服务持有请求和 Session。[模块设计](../00-foundation/design.md#3-模块职责)

| 操作 | 调用与结果 |
|---|---|
| 初始化、列出工具 | 本地完成协议握手／工具发现。标准输出仅传输 MCP 消息，诊断写标准错误。共享服务在首个钱包工具调用时按需启动或复用。 |
| 创建连接或交易 | 通过 HTTP client 提交，返回请求类型、ID、原链接、状态和期限。在工具结果中同时提供 agent 可读取的结构化字段和可展示的链接。 |
| 查询与取消 | 按请求类型和 ID 调用对应 client 方法。创建和查询分开，断开 STDIO 只结束本地调用。查询原请求可跨 MCP 进程恢复，显式取消以服务记录为准。 |
| 查询或关闭 Session | 复用 Session 接口。连接成功后按返回的 Session ID 读取钱包、账户和网络。显式查询有效 Session 会续期。 |

连接等待由仍在执行任务的 agent 发起。先展示链接，再间隔查询原请求，收到终态后结束等待。请求 ID 可放入对话上下文，服务端记录是恢复依据。具体工具名、输入 schema 与重试示例随对应功能 PR 补齐。

## 验证 STDIO 接入

在 Windows 或 macOS 按 [README 环境准备](../../../README.md#环境准备)安装依赖，从仓库根目录运行：

```sh
npm --prefix web run bundle
npm --prefix web run smoke
```

预期输出 `MCP discovery passed` 和 `Smoke passed`。测试通过管道启动构建出的程序，完成握手、列出工具，再关闭标准输入并检查进程正常退出。工具发现阶段的临时用户目录应保持为空。测试会自动清理该目录，运行期间可保留本机已有的 HTTP 服务。

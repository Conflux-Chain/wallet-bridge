# 验证首次钱包连接

在仓库根目录执行。需要 [开发环境](../README.md#环境准备)、空闲的本机端口 18765，以及装有 MetaMask 和 Fluent（EVM 模式）的 Chrome 配置。测试目标是 Conflux eSpace Testnet，chainId 为 `0x47`（71）。选用已配置该网络的测试钱包，账户可以没有余额。

```sh
npm ci --prefix web
npm --prefix web run try:connection
```

1. 将终端给出的一次性浏览器授权链接在测试 Chrome 配置中打开。这个链接只在本机使用。使用独立的 Chrome 测试配置可保留原配置对其他本机服务的授权。
2. 在同一配置打开第一个 `?connection=…` 链接。核对调用方「Wallet Bridge 验收」、用途与目标网络，选择 MetaMask，点击「读取账户和网络」。
3. 如需授权，点击「授权访问账户」。可以在钱包中拒绝一次，再在原页面重试。网络不同则点击「切换到 0x47」；钱包不支持自动切换时在钱包中手动切换并重新读取。
4. 选择一个账户，核对钱包、地址和网络，再点击「同意连接」。页面应显示「已同意连接」，终端应输出相同的钱包名称、账户地址和 chainId。
5. 在同一配置打开终端随后给出的第二个请求链接，选择 Fluent 的 EVM 账户，重复上述核对。每个请求有效五分钟。

命令构建独立程序并使用临时数据目录。两次测试结束后自动停止测试服务并清理，提前停止按 Ctrl+C。若端口已占用，先确认占用服务，再按[本地服务指南](local-service.md)处理。需要反馈时提供钱包与 Chrome 版本、操作步骤和核对结果，授权链接留在本机。

## 用 CLI 发起连接

使用包含页面的本机程序。下例用 `wallet-bridge` 代表构建后的可执行文件。首次使用时运行 `wallet-bridge ui`，在装有钱包的浏览器配置中完成 Bridge 管理页授权。

```sh
wallet-bridge connect --chain-id 71 --client-name "Example CLI" --intent "核对本次使用的账户"
```

命令会确保本地服务就绪，打印普通请求链接并请求打开默认浏览器。在页面核对并同意后，终端输出钱包、账户、chainId 和 Session ID。按 Ctrl+C 可结束本地等待（退出码 130），原链接在原五分钟期限内仍可使用。打开浏览器失败时可手动打开输出的链接，按原 ID 查询。

上例的「目标网络」应显示 `Conflux eSpace Testnet · 0x47`。传入 `--chain-name` 时优先显示调用方给出的名称和 chainId。省略名称且网络未知时，页面只显示 chainId。

脚本可以组合两个选项，自行打开链接和查询结果：

```sh
wallet-bridge connect --chain-id 0x47 --no-open --no-wait --json
```

stdout 返回一个 JSON 对象，其中 `id`、`url`、`state`、`expiresAt` 可直接使用。等待模式先将请求信息写入 stderr，成功时 stdout 的 JSON 还含 `session`。用户拒绝、显式取消或到期时退出码为 1，stderr 的最后一条 JSON 给出错误码。省略 `--client-name` 时页面显示「Wallet Bridge CLI」。用完可通过下方管理命令关闭 Session，停止服务使用 `wallet-bridge service stop`。

## 跨标签页找回请求

沿用上述环境，在仓库根目录运行：

```sh
npm --prefix web run bundle
npm --prefix web run test:e2e:chrome -- connection-recovery.spec.ts
```

预期输出 `2 passed`。同一 Chrome 配置的等待页与管理首页可以在新标签打开占用钱包的请求。原页面仍在调用钱包时只能查看进度。用户关闭原页并处理完弹窗后，可以在找回的页面继续连接。原请求已取消时，点击「解除等待」即可回到其他请求。测试使用模拟钱包和临时服务，结束后自动清理。

## 模拟钱包回归

在仓库根目录安装上述依赖后，使用空闲的 18765 端口和本机 Chrome：

```sh
npm --prefix web run bundle
npm --prefix web run test:e2e:chrome -- connection-page.spec.ts
```

预期输出 `5 passed`。测试覆盖网络名称的显示顺序、拒绝后重试、切链不支持时手动恢复、同意前账户或网络变化，以及显式拒绝和服务重启。独立 CLI 在用户确认后返回最终绑定。测试使用模拟钱包，结束时停止隔离服务并清理临时目录。

## 用 CLI 管理已有连接

构建后使用 `dist/` 下的本机程序。以下用 `wallet-bridge` 代表该可执行文件。将 `<request-id>` 换成普通连接链接中 `?connection=` 后的 ID。若发起方使用了自定义数据目录，各命令附上相同的 `--data-dir <绝对路径>`。

```text
wallet-bridge connection get <request-id> --json
wallet-bridge connection cancel <request-id> --json
wallet-bridge session get <session-id> --json
wallet-bridge session close <session-id> --json
```

查询连接可见状态和原期限。待处理时可用 `cancel` 取消；成功后使用结果中的 `sessionId` 查询钱包、账户和网络，或用 `close` 关闭。关闭后再次查询返回 `closed` 和原绑定。查询有效 Session 会刷新五分钟空闲计时。

服务停止时命令会自动启动并恢复数据。省略 `--json` 可查看终端文本。自动化脚本可按退出码区分成功（0）、服务或资源错误（1）、用法错误（2）。要手动停止服务，运行 `wallet-bridge service stop` 并沿用同一数据目录。

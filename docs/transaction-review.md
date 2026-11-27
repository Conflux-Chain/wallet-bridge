# 体验交易审阅页

在 Windows 或 macOS 的仓库根目录执行。需要 [Go、Node.js 与 npm](../README.md#环境准备)、Chrome 和空闲的本机端口 18765。示例使用临时数据库、合成账户与交易，不需要安装钱包或准备测试币。

```sh
npm ci --prefix web
npm --prefix web run try:transaction
```

1. 在 Chrome 无痕窗口中打开终端打印的一次性链接。授权完成后，地址栏保留 `/?transaction=<请求 ID>`，可刷新或在同一无痕窗口的新标签页重新打开。
2. 核对来源「示例 CLI」、调用方意图、目标网络 Conflux eSpace Testnet 和固定钱包「测试钱包」。原生币金额应为 **1.250000000000000001 CFX**，下方精确值为 **1250000000000000001 最小单位**。
3. 复制发送地址和目标地址，展开「原始 data」并复制，预期为 `0x00` 后接 128 个 `f0` 字节。Gas 上限为 75000，最高费率为 40、最高优先费为 0，费率单位均为最小单位 / gas。
4. 在五分钟内点击「拒绝这笔交易」。页面显示「用户已拒绝」，刷新仍可查看该结果。如需单独体验到期，重新运行示例并等待五分钟。
5. 结束体验时回到终端按 **Enter**，看到「测试服务已停止，临时数据已清理」后关闭无痕窗口。

## 用 CLI 发起交易

运行上面的 `try:transaction` 会通过 `transaction send --no-open --no-wait --json` 创建一笔示例。要验证默认等待行为，在终端保留示例服务，先完成无痕窗口授权。另开终端进入仓库根目录，把下例中的 Session ID 和临时目录换成示例终端打印的值，在五分钟内执行。Windows PowerShell 使用以下命令。macOS Intel 或 Apple Silicon 将程序路径换成 `./dist/wallet-bridge-darwin-amd64` 或 `./dist/wallet-bridge-darwin-arm64`。

```powershell
./dist/wallet-bridge-windows-amd64.exe transaction send --session-id "<Session ID>" --chain-id 71 --from 0xabababababababababababababababababababab --to 0xcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd --value 13 --client-name "Example CLI" --intent "核对 13 个最小单位" --data-dir "<临时数据目录>" --no-open --json
```

把新命令打印的 `url` 放入刚授权的无痕窗口，核对来源、意图和金额，点击「拒绝这笔交易」。终端应输出同一请求 ID、`state: rejected` 并退出 1。加上 `--no-wait` 可立即取得 ID 和链接，退出 0。省略 `--no-open` 时默认打开浏览器。体验结束回到示例终端按 Enter 清理。

金额和费用接受十进制或 `0x` 十六进制最小单位。合约调用用 `--data` 传完整字节，gas 上限用 `--gas`。费用可由钱包估算，或传 `--gas-price`，或成对传 `--max-fee-per-gas` 和 `--max-priority-fee-per-gas`。

| 等待发送的结果 | 退出码 | 后续操作 |
|---|---|---|
| `sent` | 0 | 用返回的网络和 `txHash` 查询链上回执 |
| `rejected`、`canceled`、`expired` | 1 | 查看输出的请求状态 |
| `uncertain` | 3 | 运行 `checkCommand` 或按原 ID 查询迟到的 hash |
| Ctrl+C | 130 | 本地等待结束，可继续查询或显式取消原请求 |

`--json` 等待模式把初始 ID 和 URL 写入 stderr，最终交易记录写入 stdout。参数用法错误退出 2，服务访问失败退出 1。

### 检查 Session ID 报错

实际发送时，运行 `wallet-bridge connect --chain-id 71` 并在浏览器同意连接。将终端最终打印的 `Session` 行中的 ID 填入 `--session-id`。使用 JSON 时取 `session.id`。

开发者可在仓库根目录运行以下回归。只需 Go，测试自动退出，无需启动服务或清理数据。

```sh
go test -tags no_web ./internal/cli -run '^TestSendSessionIDDiagnostic$' -count=1 -v
```

用本次构建的程序将 `--session-id` 设为 `aaa` 或省略该项，预期 stderr 指出格式错误或缺参，说明取得 ID 的方法，退出码为 2。添加 `--json` 时，stderr 返回 `code: bad_request`。

## 用 CLI 查询和取消示例交易

在仓库根目录运行上面的 `npm --prefix web run try:transaction`，保留该终端。另开一个终端，进入同一仓库，将以下两个占位符替换为示例终端打印的交易请求 ID 和临时数据目录。此流程只需合成交易，无需打开浏览器。

```sh
go run ./cmd/wallet-bridge transaction get "<请求ID>" --data-dir "<临时数据目录>" --json
go run ./cmd/wallet-bridge transaction cancel "<请求ID>" --data-dir "<临时数据目录>" --json
go run ./cmd/wallet-bridge transaction get "<请求ID>" --data-dir "<临时数据目录>"
```

在五分钟内执行，首次查询应返回 `pending`，取消后应看到同一 ID 和 `canceled`。已到期的请求显示 `expired`。钱包已接手时，取消命令显示当前状态，后续可按原 ID 查询 hash。结束后回到示例终端按 **Enter** 停止并清理临时服务。

## 核对插件钱包并体验发送

在装有 MetaMask 或 Fluent EVM 的 Chrome 测试配置中操作。先按上文安装依赖并释放端口，然后在仓库根目录运行以下命令。

```sh
npm --prefix web run try:transaction -- --wallet
```

1. 打开终端打印的连接链接，选择钱包，读取账户和网络。在 Conflux eSpace Testnet（chainId 71）选择测试账户，点击「同意连接」。
2. 回到终端按 Enter，在同一 Chrome 配置打开交易链接。示例金额为零，收发地址均为所选账户。「钱包状态核对」应显示「网络匹配」和「固定发送账户可用」。
3. 在第二个标签页打开同一交易链接，在钱包内换到另一网络。在第一个标签页点击「切换到 Conflux eSpace Testnet」。若在钱包弹窗中拒绝，原交易仍可重试。允许切链后，页面应显示「网络匹配」，第二个标签页提示在原页操作。
4. 关闭第一个标签页，在第二个标签页点击「继续审阅」。若钱包未向站点提供账户，可点击「授权访问账户」。如钱包已授权其他账户，请在钱包中切换或授权固定发送地址。
5. 在五分钟内核对实际交易和费用，点击「在钱包中确认并发送」。只体验流程时，在钱包弹窗内拒绝，预期原交易显示「用户已拒绝」。若选择在钱包内最终确认，钱包会广播测试网交易，账户需有测试 CFX 支付 gas。钱包返回后，原页显示「已发送」和可复制的 hash，可用该 hash 查询测试网回执。
6. 等待钱包时保留原标签页。若浏览器授权失效，在同一配置的新标签页重新授权，再回到原页点击「重新检查」。已返回的结果可从原标签页补交。结束后回到终端按 Enter 停止并清理临时服务，每次体验重新创建示例。

### 验证测试网回执

MetaMask 和 Fluent（EVM 模式）各运行一次上面的 `--wallet` 示例。验收发送时可从步骤 2 直接到步骤 5。使用有少量测试 CFX 的账户，在钱包确认目标网络为 71、收发地址均为本人的测试账户、金额为 0，以及钱包显示的 gas 费用。

看到「已发送」后，先保留示例终端。在另一个终端进入仓库根目录，替换请求 ID 和目录，取得 CLI 保存的 `txHash`。核对它与页面相同，再独立查询回执。

```sh
go run ./cmd/wallet-bridge transaction get "<请求ID>" --data-dir "<临时数据目录>" --json
node scripts/check-testnet-receipt.mjs "<txHash>"
```

脚本向 Conflux eSpace Testnet 公共 RPC 查询一次。核对 `chainId=0x47`、`transactionHash`、收发地址和区块号。`status=0x1` 表示链上执行成功，`0x0` 表示已打包但执行失败。暂时没有回执时稍后用同一个 hash 再查。记录钱包名称、版本、操作系统、Chrome 版本和 hash，回到示例终端按 Enter 清理，再换另一个钱包。[本轮验收记录](iterations/03-single-transaction/acceptance.md)

## 浏览器回归

同样在仓库根目录运行，端口 18765 需空闲。

```sh
npm --prefix web run bundle
npm --prefix web run test:e2e:chrome -- transaction-flow.spec.ts transaction-review.spec.ts transaction-wallet.spec.ts transaction-send.spec.ts wallet-occupancy.spec.ts
```

测试启动独立程序，使用合成 Session、交易和模拟钱包，覆盖参数展示、钱包选择、切链重试、重复点击、许可与回报响应丢失、授权撤销和迟到 hash。测试退出时自动停止服务并清理数据。[当轮验收进度](iterations/03-single-transaction/plan.md)

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

## 核对插件钱包的账户与网络

在装有 MetaMask 或 Fluent EVM 的 Chrome 测试配置中操作。先按上文安装依赖并释放端口，然后在仓库根目录运行以下命令。

```sh
npm --prefix web run try:transaction -- --wallet
```

1. 打开终端打印的连接链接，选择钱包，读取账户和网络。在 Conflux eSpace Testnet（chainId 71）选择测试账户，点击「同意连接」。
2. 回到终端按 Enter，在同一 Chrome 配置打开交易链接。示例金额为零，收发地址均为所选账户。「钱包状态核对」应显示「网络匹配」和「固定发送账户可用」。
3. 在钱包内更改网络，原页应显示「网络不匹配」。若从站点授权账户中移除固定地址，页面应提示切换或授权该地址。钱包可同时授权多个地址，固定地址仍在列表中时仍显示可用。
4. 结束时可在页面拒绝示例交易，回到终端按 Enter 停止并清理临时服务。分别验证两个钱包时重新运行命令。

## 浏览器回归

同样在仓库根目录运行，端口 18765 需空闲。

```sh
npm --prefix web run bundle
npm --prefix web run test:e2e:chrome -- transaction-review.spec.ts transaction-wallet.spec.ts wallet-occupancy.spec.ts
```

预期十二项通过。测试启动独立程序，使用合成 Session、交易和模拟钱包，覆盖交易展示、拒绝、发送竞争、迟到 hash、候选冲突、旧式钱包确认及账户／网络变化。测试退出时自动停止服务并清理数据。[当轮验收进度](iterations/03-single-transaction/plan.md)

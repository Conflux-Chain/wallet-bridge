# 体验交易审阅页

在 Windows 或 macOS 的仓库根目录执行。需要 [Go、Node.js 与 npm](../README.md#环境准备)、Chrome 和空闲的本机端口 18765。示例使用临时数据库、合成账户与交易，不需要安装钱包或准备测试币。

```sh
npm ci --prefix web
npm --prefix web run try:transaction
```

1. 在 Chrome 无痕窗口中打开终端打印的一次性链接。授权完成后，地址栏保留 `/?transaction=<请求 ID>`，可刷新或在同一无痕窗口的新标签页重新打开。
2. 核对来源「示例 CLI」、调用方意图、目标网络 Conflux eSpace Testnet 和固定钱包「测试钱包」。原生币金额应为 **1.250000000000000001 CFX**，下方精确值为 **1250000000000000001 最小单位**。
3. 复制发送地址和目标地址，展开「原始 data」并复制，预期为 `0x00` 后接 128 个 `f0` 字节。Gas 上限为 75000，最高费率为 40、最高优先费为 0，费率单位均为最小单位 / gas。
4. 服务持续运行时，五分钟后原请求显示「已到期」。结束体验时回到终端按 **Enter**，看到「测试服务已停止，临时数据已清理」后关闭无痕窗口。

## 浏览器回归

同样在仓库根目录运行，端口 18765 需空闲。

```sh
npm --prefix web run bundle
npm --prefix web run test:e2e:chrome -- transaction-review.spec.ts wallet-occupancy.spec.ts
```

预期六项通过。测试启动独立程序，用合成 Session 创建实际 HTTP 请求，并覆盖授权、精确金额、复制内容、未知网络、费用缺省、取消、迟到 hash 和占用链接。测试退出时自动停止服务并清理数据。钱包状态核对和实际发送按[当轮计划](iterations/03-single-transaction/plan.md)验收。

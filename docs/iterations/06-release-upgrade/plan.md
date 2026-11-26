# 迭代 6 PR 计划

6-0 已合并为 `bbfcc7b`，三平台 CI 通过。[PR #72](https://github.com/Conflux-Chain/wallet-bridge/pull/72) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45569170021)

6-1 已合并为 `4951d74`，三平台 CI 通过本机程序与三份文件摘要检查。[PR #73](https://github.com/Conflux-Chain/wallet-bridge/pull/73) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45573020381)

6-2 已合并为 `a387bb5`。三平台 CI 通过候选文件核对与模拟草稿用例。实际草稿核对及公开发布留待用户整体验收通过后执行。[PR #74](https://github.com/Conflux-Chain/wallet-bridge/pull/74) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45575259197) · [演练与发布步骤](release.md)

6-3 已合并为 `4ba6e7a`，三平台 CI 通过。Windows 真实查询本仓库返回 `no_release`，版本选择通过模拟分页响应验证。[PR #75](https://github.com/Conflux-Chain/wallet-bridge/pull/75) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45578324116)

6-4a 已合并为 `2a32d0d`。三平台 CI 通过，Apple Silicon 首轮在既有浏览器授权测试读取地址栏时失败，同一提交重跑通过。[PR #76](https://github.com/Conflux-Chain/wallet-bridge/pull/76) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45582151454) · [字节校验测试](design.md#下载字节校验)

6-4b 已合并为 `49c38de`，三平台 CI 通过。Windows 真实查询不存在的发行标签返回 404。真实附件下载成功路径待首次公开发行后验证。[PR #77](https://github.com/Conflux-Chain/wallet-bridge/pull/77) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45587482667) · [复现步骤](design.md#下载候选版本)

6-5a 已合并为 `c74c4fc`。三平台验证删除下载副本后仍可从固定路径运行 CLI 和 MCP。[PR #78](https://github.com/Conflux-Chain/wallet-bridge/pull/78) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45591481978) · [复现步骤](design.md#本机安装)

6-5b 已合并为 `156e9cf`，三平台双版本启动通过。macOS Intel 的浏览器授权地址栏检查首轮失败，同一提交重跑通过。[PR #79](https://github.com/Conflux-Chain/wallet-bridge/pull/79) · [CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/45598446163) · [复现步骤](design.md#启动已启用的服务)

6-6a 待评审。Windows 本机通过服务端占用、主动重试、晚到哈希及并发检查，定向用例重复 20 次通过。[复现步骤](design.md#升级停机检查)

| 步骤 | 单一交付 | 验收场景 |
|---|---|---|
| 6-1 | 本地生成发行产物与校验清单 | 同一版本有三个目标文件，逐项核对版本、平台、摘要及内嵌页面，当前平台能直接运行 |
| 6-2 | GitHub Releases 发行流程 | 用候选产物演练清单、平台文件与稳定／预览标记，公开发布安排在用户整体验收之后 |
| 6-3 | 查询可升级版本 | 固定发行源，覆盖稳定版、仅预览版、显式选择、无更新和查询失败 |
| 6-4a | 从固定发行源下载并核对字节 | 核对指定标签、清单与平台附件，遇到截断、内容被改、摘要冲突时返回错误 |
| 6-4b | CLI 下载与候选目录写入 | 读取程序平台，校验通过后原子保存；覆盖失败清理、重复下载与并发 |
| 6-5a | 可选安装与固定程序路径 | 移除下载副本后可从固定路径运行 CLI 和 MCP，并发安装或重试可复用相同程序 |
| 6-5b | 启动时定位本机启用版本 | 固定入口和旧 client 启动服务时遵循启用版本，覆盖直接运行、服务复用及 MCP 接入 |
| 6-6a | 服务端升级停机检查 | 占用时返回请求，空闲时暂停接单并停止服务，覆盖与钱包领取并发 |
| 6-6b | 升级客户端处理停机结果 | 区分占用、已接受、旧服务缺少能力，核实旧服务退出后方可继续 |
| 6-7 | 升级期间的数据备份与恢复 | 在隔离数据目录恢复旧版可读状态，核对凭据、Session、请求和已保存的哈希 |
| 6-8 | 用户主动切换、就绪确认和失败回退 | 正常切换、启动失败、进程中断及并发重试均可回到可用版本 |
| 6-9 | 跨版本重连与发行验收 | 用两个版本演练 CLI、MCP 与页面恢复，核对原 ID、期限与迟到结果，验证钱包发送次数 |

每个实现 PR 提供自己的复现步骤。首版总验收分别列出三平台构建、隔离升级演练、真实钱包与 agent 结果，以及仍需验证的组合。

6-9 完成后交付候选程序和用户体验指南，记录用户实际测试的版本、反馈及复测结论。[首版发布前的用户验收](spec.md#首版发布前的用户验收)

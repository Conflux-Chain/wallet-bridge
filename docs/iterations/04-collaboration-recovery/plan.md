# 迭代 4 PR 计划

| 步骤 | 单一交付 | 验证 |
|---|---|---|
| 4-1 | 已授权浏览器读取跨 client 的待审批列表 | 同 ID 两类请求、创建顺序、权限、到期与状态转换 |
| 4-2 | 管理首页显示列表并打开原请求 | 空列表、旧服务、撤销授权、钱包空闲时先处理后到请求 |
| 4-3 | 查询本机活动记录 | Session 关闭后可查、重试及迟到 hash 反映在原记录 |
| 4-4 | 管理页查看活动 | 从记录核对请求与结果 |
| 4-5 | 按用户选择清理活动记录 | 规则确认后验证清理范围、重复清理和中断恢复 |
| 4-6 | 多 client／多页面组合验收 | 交接、服务重启、原页恢复和手动放行 |

4-1 已合并为 `6efdb1a`。HTTP 与 SQLite 测试覆盖权限、排序、同 ID、无续期及后到交易先取得发送许可。Windows amd64、macOS amd64／arm64 CI 通过，Windows 在同一提交上重跑通过。[PR #59](https://github.com/Conflux-Chain/wallet-bridge/pull/59)

4-2 已实现首页列表与原请求导航，待评审。Windows 独立程序构建通过，本机 Chrome 在模拟列表响应下验证了同 ID 两种链接、窄屏、空态、读取失败、授权失效和旧服务。三平台 CI 运行带真实本地服务与模拟钱包的集成用例。[4-2 PR](https://github.com/Conflux-Chain/wallet-bridge/pulls?q=is%3Apr+head%3Acodex%2Fhome-pending-requests) · [复现步骤](design.md#验证管理首页)

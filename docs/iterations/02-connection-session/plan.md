# 迭代 2 PR 计划草案

状态：2026-11-20 PR 2-0～2-3a1 已合并；PR 2-3a2a 已实现待评审，按 D-034 分步简化后继续核心。沿用 [spec](spec.md)、[design](design.md)。

## 分步交付

| 步骤 | 单一目标与边界 | 验证重点 | 状态 |
|---|---|---|---|
| PR 2-0：需求与方案 | 本轮 spec、设计边界、PR 拆分；区分已确认与后续检查点 | 文档一致、问题来源明确、链接有效 | [#15](https://github.com/Conflux-Chain/wallet-bridge/pull/15) 已合并 |
| PR 2-1：钱包发现 | 标准 Provider 发现、去重、旧式入口及钱包选择；不建立 Session | 多钱包／迟到／冲突公告、StrictMode、不自动弹窗及实际发现 | [#16](https://github.com/Conflux-Chain/wallet-bridge/pull/16) 已合并，用户已确认双钱包发现 |
| PR 2-2a：连接记录与迁移 | 数据模型、追加迁移、创建连接与按凭据查询；不创建 Session | v1 升级、摘要隔离、持久化与 schema 约束 | [#17](https://github.com/Conflux-Chain/wallet-bridge/pull/17) 已合并 |
| PR 2-2b：连接原子写入 | 完成连接并建立唯一 Session、取消／拒绝／到期及回传幂等 | 事务回滚、终态竞争、迟到成功不建 Session | [#18](https://github.com/Conflux-Chain/wallet-bridge/pull/18) 已合并 |
| PR 2-3a1：生命周期存储 | 有效 Session 访问／关闭、批量到期的事务存储 | 凭据隔离、终止后不续期、维护回滚及并发竞争 | [#19](https://github.com/Conflux-Chain/wallet-bridge/pull/19) 已合并 |
| PR 2-3a2a：单一本机凭据 | 合并服务普通／控制角色，自动复用一份密钥；不改连接存储 | 单文件初始化、旧目录复用、共享管理、浏览器边界和旧服务拒绝 | [#20](https://github.com/Conflux-Chain/wallet-bridge/pull/20) 待评审 |
| PR 2-3a2b：资源凭据移除 | 追加迁移并调整存储接口，以 ID 定位连接／Session | 升级保留记录、原子回滚、期限与历史不变 | 待开发 |
| PR 2-3a2c：连接与 Session 核心 | 创建、完成、拒绝、取消、查询、关闭／回收与基础重启恢复 | 绑定固定、期限、并发竞争和结果保留 | 待细化 |
| PR 2-3b：执行协调 | 多页领取、钱包调用标记、互斥与显式恢复；不增加交易流程 | 重复领取、页面丢失、迟到回传、不自动重入钱包 | 待开发 |
| PR 2-4：HTTP 接入 | 核心／执行接口、共享 client 与 Swagger 同步 | client／浏览器权限分离、参数、状态和回传幂等 | 待开发 |
| PR 2-5：CLI | 连接、请求查询／取消、Session 查询／关闭及 ID 使用 | 实际程序、等待／取消／输出边界、敏感信息保护 | 待开发 |
| PR 2-6：连接页面 | 集成钱包授权、切链、单账户选择、本次同意与结果 | 模拟 Provider 的真实页面流程、状态变化、多页竞争 | 待开发 |
| PR 2-7：组合验收 | 三平台组合与 MetaMask／Fluent 连接验收、交接资料 | A-04～07、11～12、16～17；明确模拟、实际环境及用户验收 | 待开发 |

PR 2-1 后依次接入存储、核心、HTTP、CLI 与页面；必要测试和文档随对应 PR 交付，不留到最后补齐。每个实现单元从合并后的 main 创建分支；本轮只连接钱包，不请求签名或发送交易。

## 当前步骤与检查点

- PR #19 已按用户“请继续”合并为 `a28d9f6`；核对分支内容与 main 一致后清理本地／远端分支。本单元基于该 main，分支为 `feat/shared-service-credential`。
- 用户已确认首次连接在最终账户／网络就绪后同意，取消／到期后即使钱包返回成功也不再建 Session；记录为 D-030／031。
- 用户确认同用户本地 client 可信、单服务凭据、资源 UUID 定位；D-034 替代 D-019。先完成两次独立简化，再组合核心；不实现此前未确认的客户端预生成资源凭据方案。
- 用户已确认失效后保留历史查询，仅有效 Session 的 client 访问续期；记录为 D-032／033。
- 用户已确认固定钱包及[请求审阅交互](../../approval-flow.md)。交易实现仍属迭代 3，ABI 解码沿用后续扩展范围。
- 核心流程、HTTP／CLI、页面集成的产品检查点按 spec 逐组补充；影响对应 PR 的问题处理后才编码。
- 迭代 1 的环境补验和用户验收继续保留，不将转入下一轮视为已通过。

## 验证

PR 2-0：23 份 Markdown 的 170 个本地链接、53 个标题锚点及 Git 空白检查通过；最终 head `e921fa0` 的[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44681421214) 通过。后续真实钱包证据见对应 PR，迭代 1 缺失环境尚未验收。

### PR 2-1

- 已实现标准发现、按实例去重、UUID 冲突提示、旧式升级及显式选择，接入已授权页和 Vite 预览；没有钱包 RPC、Session 或后端改动。
- Windows 本地类型检查、生产页面／独立程序构建、`go test ./web ./internal/api` 和空 PATH smoke 通过；Chrome 的 9 项模拟 Provider 浏览器测试通过。三平台完整 CI 随 PR 记录。
- 最终 head `34ad43f` 的[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44685168549) 全部通过；每平台完成 10 项授权页面测试、9 项发现测试及构建、Go 测试、vet 和三组 smoke。
- 使用独立 Chrome 测试上下文，检查了无钱包、模拟双钱包选择的桌面及 360px 窄屏截图。此前自动检查用户 Chrome 因工具无法核实当前 URL 而停止，没有操作钱包或更改扩展设置。
- 2026-11-20 用户人工确认打开页面同时看到 MetaMask 和 Fluent：双钱包同时发现通过。未提供 Chrome／钱包版本及来源字段；单独启用、选择／重新检测／刷新、无弹窗和账户授权未由这条反馈覆盖，不标为人工通过；相关模拟覆盖及后续实测分别保留。
- 本地 Playwright Chromium 下载较慢，已停止下载并使用已安装 Chrome 完成测试；CI 使用 Chromium。原正式服务保持运行，未升级或重启；预览、自动测试与停止步骤见[测试指南](../../wallet-discovery.md)。
- 24 份 Markdown 的本地链接、标题锚点及 Git 空白检查通过。

### PR 2-2a

- 已完成记录／接口、schema v2、创建连接及按 ID／凭据查询；只提供存储基础，尚无 Session 创建、连接终态转换或外部接入。
- Windows 上完整 Go 测试、`no_web` 测试和两种 vet 通过；新增 8 组存储测试（含子用例），覆盖 v1 升级保留授权、迁移失败整体回滚、重开后绑定／期限保留、凭据隔离、重复／并发创建、大 chainId、schema 约束及损坏拒绝。原有 Windows 符号链接测试因系统权限跳过，未计作通过。
- Windows 页面／独立程序构建及空 PATH smoke 通过；25 份 Markdown 的 185 个本地链接、61 个标题锚点及 Git 空白检查通过。
- 验证使用临时私有数据库；原正式服务及数据库未升级或重启。复现步骤见[连接记录与迁移测试](../../connection-storage.md)，三平台完整 CI 随 PR 记录；无新增人工页面验收项目。
- 最终 head `68b0ba6` 的[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44688805543) 全部通过（Windows amd64、macOS Intel／Apple Silicon）。

### PR 2-2b

- 已实现连接完成／终止的事务写入、到期判定和相同回传幂等；Session 绑定与摘要冲突不覆盖历史结果。仍未接入核心鉴权、执行协调或 HTTP／CLI／页面。
- Windows 新增 8 组 SQLite 事务测试通过，覆盖插入及提交失败整体回滚、Session ID／摘要冲突、重开后幂等、关闭后重放、到期边界、取消后迟到成功及跨数据库连接并发终态竞争。
- Windows 完整／`no_web` Go 测试、两种 vet、页面／独立程序构建与空 PATH smoke 通过；指南中的 16 组存储测试通过。25 份 Markdown 的 186 个本地链接、61 个标题锚点及 Git 空白检查通过。
- 复现见[连接存储与事务测试](../../connection-storage.md)；三平台 CI 随 PR 记录。本次无新增人工页面验收，正式服务及数据库未升级或重启。
- 最终 head `5e15f7c` 的[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44691060510) 全部通过（Windows amd64、macOS Intel／Apple Silicon）。

### PR 2-3a1

- 已实现 Session 凭据访问／关闭和批量到期三个存储方法，复用 schema v2，保留绑定、摘要及原连接结果；尚未接入核心流程、启动／周期维护或外部接口。
- Windows 新增 8 组真实 SQLite 生命周期测试通过，覆盖有效访问续期、原始查询不续期、到期边界、关闭幂等、凭据隔离、时钟回退／重开不复活，以及跨数据库连接并发竞争。更新／提交失败时，批量连接及 Session 维护整体回滚。
- Windows 完整／`no_web` Go 测试、两种 vet、页面／独立程序构建及空 PATH smoke 通过；指南中的 24 组存储测试通过。复现见[连接存储与事务测试](../../connection-storage.md)，三平台 CI 随 PR 记录。本次无新增人工页面验收，正式服务及数据库未升级或重启。
- 25 份 Markdown 的 189 个本地链接、63 个标题锚点及 Git 空白检查通过。
- 最终 head `16a145c` 的[三平台 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44692949282) 全部通过（Windows amd64、macOS Intel／Apple Silicon）。

### PR 2-3a2a

- 已移除本机普通／控制角色，复用 `client.key`；新装单凭据，旧控制文件保留但不能鉴权。API 1.2 公告共享认证能力，新 client 在发送 Bearer 前拒绝旧服务。schema v2 和内部资源摘要留给下一 PR。
- Windows 完整／`no_web` Go 测试、两种 vet、页面／独立程序构建和空 PATH smoke 通过；Swagger 从注释重新生成。新增／调整的真实文件、六进程初始化、临时 HTTP 服务及旧服务模拟检查通过，覆盖旧目录身份保留、统一管理、旧控制密钥拒绝和浏览器边界。
- Markdown 本地链接、标题锚点和 Git 空白检查通过；Windows 原有符号链接测试因系统权限跳过，未计作通过。
- 复现见[服务接入兼容性测试](../../local-service.md#服务接入兼容性测试)。正式服务仍在运行，本地未执行占用固定端口的 service／background／浏览器 smoke，交由三平台 CI 验证；不重启或迁移正式服务。无新增人工钱包验收项目。
- 最终 head 的三平台 CI 结果记录在 PR #20 checks／说明中。

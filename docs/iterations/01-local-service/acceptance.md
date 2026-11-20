# 迭代 1 验收记录

状态：2026-11-20 全部 PR 已合并，本地与[三平台原生 CI](https://github.com/Conflux-Chain/wallet-bridge/actions/runs/44663676405)通过（最终 head `205c196`）；环境补验与用户验收尚未完成。范围以 [spec](spec.md) 为准，逐 PR 检查见 [plan](plan.md)。

## 执行方式

1. 复用当前 Go、三组 smoke 和浏览器测试，将浏览器流程改为运行仓库外的单一程序、空 PATH 和独立数据目录。
2. 本地用已安装 Chrome 的独立上下文，三个原生 CI 用 Chromium；实际默认浏览器与缺失环境分别验收。
3. 按下表汇总证据、提供本地复现入口，保留无法执行的验收项。

## A-01～04 汇总

| 场景 | 已有证据 | 组合结果／限制 |
|---|---|---|
| A-01 独立程序 | 三目标内嵌构建；[隔离运行](../../../web/tests/e2e/setup.ts)与 10 项浏览器流程 | 本地 Windows 通过；三平台最终 CI 见本 PR checks。干净系统与最低版本待验收 |
| A-02 单实例与常驻 | [background smoke](../../../scripts/background-smoke.mjs) 六 CLI 并发 ensure、复用、调用者退出后查询；真实进程锁测试 | 本地 Windows 通过；实际终端关闭待单独验收 |
| A-03 身份／版本／就绪 | client 测试覆盖身份、能力、不兼容、超时；[service smoke](../../../scripts/service-smoke.mjs)／background smoke 验证假服务、占用、重启和停止 | 本地 Go／smoke 通过；三平台最终 CI 见本 PR checks |
| A-04 本轮访问控制 | HTTP／存储／权限测试及[浏览器](../../../web/tests/e2e/authorization.spec.ts)授权、重启、撤销流程 | 本地 Windows 通过，符号链接权限不足项由已有原生 CI 补验；其他普通用户身份实测待验收。后续 client 信任模型已由 [D-034](../../decisions.md)简化 |

## 本地执行结果

- Windows 11 amd64 10.0.26300，Chrome 157.0.8103.79；`bundle:all`、完整／no_web Go 测试、go vet 和三组 smoke 通过。此次 Go 结果复用了未变更后端的测试缓存；各平台 CI 另执行完整检查。
- `test:e2e:chrome` 的 10 项测试通过（16.5 秒）；CLI 从仓库外的 `Wallet Bridge 独立程序` 目录运行，PATH 为空，程序目录仅一个可执行文件。真实交换、重启、退出和撤销均使用临时数据。
- 响应丢失、cookie 保存失败和浏览器 cookie 到期沿用注入／模拟；服务端到期另由 Go 测试验证。截图呈现证据沿用 PR 1-6d，此次未重复视觉检查。
- 测试结束已停止自身服务并清理本次临时目录；原正式服务使用原程序与原数据恢复，未安装升级。

## 本地复现

- [服务指南](../../local-service.md)：构建、三组 smoke、前台／后台、查询与停止。
- [浏览器指南](../../browser-testing.md)：实际页面授权、退出、重启复用及 Playwright。固定端口须空闲，测试使用独立临时数据。
- [凭据说明](../../local-credentials.md)：运行数据及权限边界。无需手动准备私钥或凭据文件。

## 尚待验收

- Windows 10、macOS 13 最低系统；Windows amd64 与两种 macOS 架构上未安装 Go／Node 的干净系统。
- 其他普通操作系统用户的实际访问隔离、实际终端关闭后的后台常驻。
- 系统默认浏览器真实打开；平台及结果随本 PR 更新。
- 用户本轮使用反馈。真实钱包和 agent 按后续迭代验收，本轮不调用钱包。

# 浏览器授权页面测试

本指南覆盖授权、服务状态及退出；钱包连接与交易尚未开放。Vite 地址用于开发预览及[钱包发现测试](wallet-discovery.md)，真实授权使用内嵌页面 `http://127.0.0.1:18765/`。

## 前置条件

在仓库根目录操作，先按 [README](../README.md#环境准备)安装工具并执行 `npm ci --prefix web`；手动验证使用 Chrome。构建命令：

```sh
npm --prefix web run bundle
```

固定端口 18765 须空闲。若旧版本服务正在运行，先用原版本程序、原数据目录的 `service status --json` 确认，再正常 `service stop --json`；前台服务可在原终端按 Ctrl+C。不要按端口或旧 PID 强杀进程。测试新构建不代表已安装服务升级；需保留旧程序时，应在构建前复制到独立位置。

## 手动验证授权页面

以下为 Windows PowerShell 命令。macOS 将程序路径替换为 `./dist/wallet-bridge-darwin-arm64`（Intel 为 `darwin-amd64`）；其余参数相同。默认使用正式用户数据，可追加同一绝对 `--data-dir` 隔离数据，目录规则见[服务指南](local-service.md#手动启动与访问)。

```powershell
.\dist\wallet-bridge-windows-amd64.exe service ensure --json
```

1. 在 Chrome 打开 `http://127.0.0.1:18765/`。未授权配置应显示「需要浏览器授权」，不显示服务版本或授权 ID。已有有效 cookie 时直接进入已授权状态。
2. 运行下列命令，复制输出链接到目标 Chrome 配置。链接两分钟有效且只能使用一次；不要粘贴到聊天或日志。

   ```powershell
   .\dist\wallet-bridge-windows-amd64.exe ui --print-url
   ```

   预期地址栏的 `#authorize=…` 消失，页面显示「浏览器已授权」、服务版本／启动时间、当前授权 ID 和约 30 天后的有效期。刷新、同配置新开标签仍授权，有效期不变。重复打开已使用链接应显示失效，可点「重新检查」查询现有授权。
3. 依次运行以下命令，再在原页面点「重新检查」，应恢复已授权状态且有效期不变。服务停止期间检查应显示无法确认服务状态，并隐藏旧管理信息。

   ```powershell
   .\dist\wallet-bridge-windows-amd64.exe service stop --json
   .\dist\wallet-bridge-windows-amd64.exe service ensure --json
   ```

4. 点击「退出当前浏览器」，应显示已退出；刷新后需重新授权。其他独立浏览器配置的授权不受影响。重新授权后，可通过 [CLI 撤销](local-service.md#浏览器授权-cli)验证失效反馈：页面保持前台时约 10 秒内更新，重新聚焦也会检查。
5. 结束测试时运行下列命令。数据与 cookie 保留；隔离目录只在确认停止后按需删除。若先前暂停了旧服务，可用保留的旧程序与原目录重新 `service ensure`。

   ```powershell
   .\dist\wallet-bridge-windows-amd64.exe service stop --json
   ```

默认浏览器启动需另验：`ui` 不带 `--print-url` 时应打开系统默认浏览器并完成同一流程；若默认浏览器不是 Chrome，可继续使用上述显式链接方式。

## 自动浏览器测试

同样要求固定端口空闲且已完成 `bundle`。首次安装与运行：

```sh
npm --prefix web run browser:install
npm --prefix web run test:e2e
```

预期 Chromium 的 10 个测试通过，退出码为 0。测试以真实独立程序、内嵌页面和临时数据库验证授权／重放、fragment 清理、HttpOnly cookie、刷新／重启复用、配置隔离、退出及 CLI 撤销。响应丢失和禁止保存 cookie 的失败分支使用注入；浏览器到期通过设置已过期 cookie 模拟，服务端 30 天到期另由 [Go 测试](local-service.md#浏览器授权-http-测试)验证。

已安装 Google Chrome 时也可执行：

```sh
npm --prefix web run test:e2e:chrome
```

测试将程序单独复制到仓库外、含空格和中文的临时目录，从该目录以空 PATH 运行 CLI；内嵌页面不依赖仓库资源。预览 origin 用例单独读取前端构建产物，不计入独立程序证据。这种隔离不代替未安装工具链的干净系统验收。

测试使用独立浏览器上下文，不读取用户 Chrome 配置或正式凭据。结束后确认自身服务停止并清理临时目录。启动／停止结果无法确认时，保留路径并报错，不停止其他服务。故障中断后，先用同一个构建程序和报错目录查询／正常停止，再清理该临时目录。

为避免授权链接和 cookie 进入诊断产物，默认关闭 trace、截图与视频；不要对授权链接开启含敏感参数的调试日志。类型检查／生产构建、自动浏览器测试、实际视觉检查及真实钱包／用户验收分别记入[迭代 plan](iterations/01-local-service/plan.md#pr-1-6d)。

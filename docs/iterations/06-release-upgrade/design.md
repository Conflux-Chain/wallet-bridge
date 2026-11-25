# 发布与升级设计草案

## 已有实现

现有构建脚本支持三种原生目标，嵌入页面及版本信息。`version --json` 可读取平台、发行版本、源码修订和页面打包状态。每个平台已有独立程序 smoke，可用于检验发行产物。[构建脚本](../../../scripts/build.mjs) · [版本信息](../../../internal/buildinfo/info.go)

HTTP client 已验证服务身份、API 兼容性和所需能力。服务停止时，`Ensure` 会启动当前 client 的配套程序。SQLite 在服务就绪前执行迁移，并拒绝过新的 schema。升级需补充启用版本的选择及跨版本恢复。[服务启动](../../../internal/client/lifecycle.go) · [数据库打开与迁移](../../../internal/store/store.go)

## 本地发行构建

在 Windows 或 macOS 的仓库根目录按 [README 环境准备](../../../README.md#环境准备)安装依赖，然后运行：

```sh
npm --prefix web run release:build -- v0.0.0-local.1
```

版本参数使用带 `v` 前缀的语义化版本。命令在 `dist/releases/v0.0.0-local.1/` 生成三个独立程序和 `SHA256SUMS`。清单每行包含十六进制 SHA-256、两个空格及文件名。构建完成后整组移入版本目录，已有同名目录会提示冲突。

在 Windows PowerShell 中从仓库根目录核对：

```powershell
Push-Location dist/releases/v0.0.0-local.1
try {
  foreach ($line in Get-Content SHA256SUMS) {
    $digest, $name = $line -split '  ', 2
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $name).Hash -ne $digest) {
      throw "Checksum mismatch: $name"
    }
  }
  .\wallet-bridge-windows-amd64.exe version --json
} finally { Pop-Location }
```

在 macOS 的仓库根目录运行下列命令。Intel 机器将 `arm64` 换为 `amd64`：

```sh
(cd dist/releases/v0.0.0-local.1 && shasum -a 256 -c SHA256SUMS && ./wallet-bridge-darwin-arm64 version --json)
```

预期三个文件摘要都匹配，本机程序输出 `version: v0.0.0-local.1`、对应 `platform` 和 `uiEmbedded: true`。命令退出即完成测试。清理时删除此次生成的版本目录即可。发行者签名及其验证方式留到后续 PR 确定。

## 拟实施的切换顺序

1. **准备版本。** 根据发行渠道与本机平台选择目标，校验后置于独立版本目录。确认启用前保留当前版本。校验材料、来源和首次安装方式随对应 PR 设计。
2. **取得切换权。** 服务端在同一协调边界内检查钱包占用并暂停新工作，防止检查后又领取请求。保留已交给钱包的回报入口。两个升级者并发或旧 CLI 同时启动时，只允许一次服务切换。
3. **切换与恢复。** 旧服务停止写入后形成可还原的数据状态。新版完成迁移及就绪检查，再开放业务并记录启用版本。开放前失败可恢复旧程序和配套数据。开放后已有新请求或迟到哈希时，恢复不能用旧快照覆盖这些记录。具体持久化顺序和断电恢复点在切换 PR 前评审。
4. **重连。** CLI／MCP 从启用版本启动服务，在原地址重新校验身份与能力。浏览器重读原请求及钱包调用状态，已有发送许可的请求禁止再次调用钱包。固定的 Codex 命令路径应在升级后仍能接入。

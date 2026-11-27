# 从候选程序到 GitHub Release

## 本地演练

在 Windows 或 macOS 的仓库根目录，按[构建步骤](design.md#本地发行构建)生成示例版本后执行：

```sh
npm --prefix web run release:review -- v0.0.0-local.1
node --test scripts/release-review.test.mjs
```

第一条命令离线复算三份程序的摘要、核对清单并读取本机程序的内嵌版本。预期输出含 `channel: prerelease`、源码修订、清单摘要 `manifestSha256` 及四项附件。第二条命令用模拟草稿验证文件替换、缺件、标签和渠道错误。命令退出即完成演练，清理方法见构建步骤。

## 交给用户体验

开发完成后，用干净的提交构建候选版本。维护者保存核对输出，确认 `modified` 为 `false`，将程序、清单和用户指南交给用户。版本号和清单摘要标识这次验收的文件。在[本地体验索引](../../local-trials/README.md)登记当轮指南与反馈，修复后交付下一轮候选包并记录复测结果。[用户验收约定](spec.md#首版发布前的用户验收)

## 准备升级演练目录

维护者在体验者的本机账户下操作。按[本地构建](design.md#本地发行构建)准备本机工具和内嵌页面，在仓库根目录对已核对的候选运行：

```sh
go test -tags integration ./internal/update -run '^TestReleaseTrials$' -count=1 -v -args -trial-version v0.1.0 -keep-trials
```

输出 `dist/upgrade-trials-v0.1.0/resume` 和 `rollback`。两个目录分别生成本用户的私有凭据、空库及真实备份，第二个故意移除缓存中的目标程序。准备过程离线且无需占用服务端口。已有输出目录时命令会报错，复测前先停止演练服务并移走旧目录。给体验者提供[用户操作步骤](../../local-trials/v0.1.0/round-01/guide.md#5-体验升级中断恢复)。自动验证时省略 `-keep-trials` 并释放 18765 端口，命令实际执行两次恢复，检查版本与退出结果后清理临时目录。

## 用户确认可以发布之后

1. 在本仓库为验收记录中的 `revision` 创建同名版本标签。已有标签时核对它指向该提交。
2. 在 GitHub Releases 为该标签新建草稿。上传原候选目录中的三份程序和 `SHA256SUMS`，按记录的 `channel` 设置预览标记。保存为草稿。
3. 维护者使用有权限读取草稿的 GitHub CLI，在本机原候选目录仍可用时，从仓库根目录运行下列命令。两个占位符分别替换为已验收版本和**验收时保存**的清单摘要。

   ```sh
   npm --prefix web run release:review -- <版本号> --draft <已验收的清单摘要>
   ```

   命令只读核对远端标签提交、草稿状态、渠道、附件名和 GitHub 返回的四份 SHA-256。成功时退出码为 0。失败时按报错修正草稿，重新核对。
4. 核对通过后在同一草稿中点击 **Publish release**。草稿内容若有变更，先重新执行第 3 步。记录发布链接与版本号。

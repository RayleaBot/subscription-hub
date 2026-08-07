# RayleaBot Subscription Hub Plugin

`raylea.subscription-hub` 提供第三方内容订阅、定时检查、推送渲染和 Vue 管理页面。插件后端、管理页面、模板与静态资源共同进入独立 artifact。

## 目录结构

- `cmd/subscription-hub/`：只负责启动进程。
- `internal/plugin/`：订阅命令、平台处理、调度和测试。
- `internal/assets/`：由 Go 嵌入的默认配置；构建时映射为 artifact 根目录的 `default_config.json`。
- `ui/`：Vue 管理页面。
- `templates/`：推送渲染模板与静态资源。
- `tools/build/`：统一组装后端、UI、默认配置与模板。

## 本地联调

将本仓库路径写入 RayleaBot 根目录下被忽略的 `plugin-workspace.local.json`，并运行：

```powershell
$env:RAYLEA_PLUGIN_DEV = 'watch'
$env:RAYLEA_SERVER_RELOAD = 'watch'
node scripts/start-dev.mjs
```

启动脚本会把主仓库的 Go 与 Vue SDK 映射到 `.rayleabot/`，构建当前平台 artifact，再通过离线 `plugin dev-sync` 原子同步到 `plugins/installed/`。构建或同步失败时继续使用上一个已安装产物。

## 发布

推送 `v*` 标签后，工作流使用固定 SDK 引用测试 Go 与 Vue 代码，构建 Windows x64、Linux x64 和 macOS arm64 ZIP，并创建 GitHub Release。插件目录仓库随后记录产物摘要并发布签名目录。

License: MIT

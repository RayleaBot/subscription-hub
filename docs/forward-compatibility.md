# 合并转发兼容说明

解析与订阅推送均调用宿主 `message.forward.send`，由宿主按目标映射为 `send_group_forward_msg` 或 `send_private_forward_msg`。每个媒体使用一个 `node`；节点 `data.uin` 是分享者或订阅人的 QQ 号，`data.name` 是群名片或昵称，`data.content` 包含原图或视频。外层 `source` 是内容作者昵称。

| OneBot 实现 | 原图、视频与发送人 | 顶部作者昵称 |
| --- | --- | --- |
| NapCat | 支持节点 `uin` / `name` 和图片、视频消息段 | Packet 转发路径支持 `source`；普通转发路径不保证自定义展示 |
| LLBot 8.2.1 原版 | 支持群聊、私聊合并转发及节点发送人；单个视频上限为 1 GiB | 存在两处实现缺陷，忽略自定义标题 |
| LLBot 8.2.1 应用下述修复后 | 保持原有媒体和发送人处理 | 将 `source` 传入卡片标题，保留默认标题行为 |

LLBot 8.2.1 的 `SendForwardMsg` 声明 `source/news/summary/prompt`，但没有将它们传给底层转发元素；`MessageBuilding` 中的标题表达式还缺少括号，使非空标题被替换为默认文字。插件侧增加其他字段不能修复这两处问题。

依据：[LLBot 8.2.1 OneBot 转发接口](https://github.com/LLOneBot/LuckyLilliaBot/blob/v8.2.1/src/onebot11/action/go-cqhttp/SendForwardMsg.ts)、[节点及媒体转换](https://github.com/LLOneBot/LuckyLilliaBot/blob/v8.2.1/src/onebot11/transform/message/outgoing.ts)、[转发卡片生成](https://github.com/LLOneBot/LuckyLilliaBot/blob/v8.2.1/src/ntqqapi/helper/messageBuilding.ts)、[视频大小限制](https://github.com/LLOneBot/LuckyLilliaBot/blob/v8.2.1/src/ntqqapi/entities.ts)、[NapCat 转发实现](https://github.com/NapNeko/NapCatQQ/blob/main/packages/napcat-onebot/action/msg/SendMsg.ts)。

## LLBot 8.2.1 修复

在本插件源码目录执行，路径替换为包含 `package.json` 和 `llbot.js` 的 LLBot 程序目录：

```powershell
node scripts/patch-llbot-forward.mjs 'C:/path/to/llbot' --check
node scripts/verify-llbot-forward.mjs 'C:/path/to/llbot'
node scripts/patch-llbot-forward.mjs 'C:/path/to/llbot' --apply
```

修复脚本只接受已核对的 8.2.1 发行版和两处精确代码匹配。写入前检查 JavaScript 语法，并把原文件保存为同目录的 `llbot.js.before-forward-metadata.bak`；重复执行不会重复修改。修复保留代码行数，避免改变原发行版 source map 的后续行号。完成后重启 LLBot，使运行进程加载修复。

LLBot 更新后重新核对版本与实现。脚本对未知版本、代码已变化或修复不完整的文件拒绝写入，不能直接用于其他版本。

需要撤销时执行以下命令，再重启 LLBot。只有当前代码仍与这次修复一致时才会恢复，避免覆盖后续更新。

```powershell
node scripts/patch-llbot-forward.mjs 'C:/path/to/llbot' --restore
```

## 验证边界

`verify-llbot-forward.mjs` 从指定发行版提取实际 OneBot 转发处理器、媒体节点转换和转发卡片生成代码，在隔离上下文执行；上传与 QQ 发送使用本地替身。它验证群聊和私聊、原图与视频节点、发送人 QQ 号和昵称、自定义与默认标题、预览与摘要，以及修复幂等性和代码漂移拒绝。原版输入还会先复现标题丢失。

该验证不连接 QQ、不上传媒体、不发测试消息。真实账号的媒体上传、QQ 客户端展示与适配器运行状态需另行实测。

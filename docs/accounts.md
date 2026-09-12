# 账号管理

订阅与解析插件提供账号保存、扫码、检查和头像展示。账号元数据保存在插件 KV，Cookie 加密保存在插件密钥存储。

## 页面与操作

插件管理页的 **账号管理** 页负责：

- 按平台分组展示账号：平台、账号 ID、备注、昵称、UID、头像、启用状态与 CK 检查状态。
- 添加、编辑和删除账号。编辑时 CK 留空表示保留当前值。
- 扫码获取 CK；Bilibili、微博、抖音和网易云音乐均提供扫码入口。
- 手动检查 CK，检查结果写入账号资料与凭据状态。

聊天命令不会修改账号；账号变更全部通过插件页面或后续插件管理动作完成。

## 凭据状态

| 状态 | 含义 |
| --- | --- |
| `unknown` | 尚未完成校验，或网络、限流、风控等无法证明凭据失效的检查结果 |
| `valid` | CK 已校验可用 |
| `invalid` | CK 已失效，需要重新保存或扫码 |

- 明确的未登录响应进入 `invalid`；网络、限流、风控和 HTTP 432 等无法证明凭据失效的结果保持 `unknown`。
- `invalid` 账号不会提供给订阅检查；重新保存、扫码或手动检查可更新状态。
- 订阅检查遇到明确的认证拒绝时记录观察结果并触发插件内状态更新。

## 扫码登录状态

扫码登录采用以下状态：

```plain
[*] -> pending_scan
pending_scan -> pending_confirm / verification_required / succeeded
pending_confirm -> verification_required / succeeded
verification_required -> pending_confirm / succeeded
pending_scan / pending_confirm / verification_required -> expired / failed
```

| 状态 | 类型 | 含义 |
| --- | --- | --- |
| `pending_scan` | 瞬态 | 等待用户扫描二维码 |
| `pending_confirm` | 瞬态 | 已扫码，等待用户在平台端确认 |
| `verification_required` | 瞬态 | 需要在登录浏览器完成短信、滑块或验证码等交互校验 |
| `expired` | 终态 | 二维码或登录会话已过期 |
| `failed` | 终态 | 平台拒绝、浏览器关闭或会话发生不可恢复错误 |
| `succeeded` | 终态 | 凭据已取得并保存为账号 |

轮询遇到未知状态时按失败处理。同一页面最多保持一个扫码会话；取消、重新扫码或离开页面时发送取消请求，旧响应不会覆盖新会话。网络断开时由会话期限兜底回收，插件进程退出时宿主回收其所属浏览器。

## 浏览器会话

抖音扫码登录与登录态用户解析通过宿主的通用浏览器动作执行：

- `browser.launch` 启动宿主托管的浏览器会话并返回 CDP 调试端点；插件在浏览器内完成页面交互与网络读取。
- 插件使用固定的持久化 profile（`douyin-login`），扫码登录与用户解析共享，同一 profile 同时只允许一个会话。
- `browser.close` 关闭会话并释放 profile；宿主在会话生命周期到期时也会自动回收。
- 浏览器模式与远程调试地址属于插件配置：`account_browser_mode`（`auto`、`visible`、`headless`、`remote_cdp`）与 `account_browser_remote_debugging_url`。
- `remote_cdp` 只接受无凭据的本机回环 HTTP(S) 或 WS(S) 端点；本地浏览器使用插件隔离的 profile，远程调试地址应指向专用浏览器。

## 配置

| 配置 | 作用 |
| --- | --- |
| `account_check_interval_minutes` | CK 自动检查间隔，默认 360 分钟；`0` 关闭自动检查，页面手动检查仍可用。非零值范围为 15 到 10080 |
| `account_browser_mode` | 扫码登录浏览器模式，默认 `auto` |
| `account_browser_remote_debugging_url` | `remote_cdp` 模式使用的浏览器调试地址 |

自动检查由插件定时任务 `subscription-hub-accounts` 驱动，任务在管理面 **任务调度** 中以「账号检查」展示。

## 存储与权限

- 账号元数据保存在插件 KV 的 `account:<platform>:<account_id>`。
- CK 保存在插件密钥存储的 `account.<platform>.<account_id>.cookie`，仅在插件进程内按需读取，响应与日志不回显明文。
- 每次账号写入生成新的 revision；检查结果只提交到未被编辑或删除的同一版本。网络、风控等检查失败保留上次成功取得的资料。
- 相关 manifest 权限：`secret.read`、`secret.write`、`secret.delete`、`browser.launch`、`browser.close`。
- 平台相关 HTTPS 请求通过 `http.request` 动作执行，复用宿主的 DNS、重定向复查、SSRF、私网拦截与响应大小限制。

## 查询与设置

账号页支持搜索平台、账号 ID、备注、昵称和 UID，并分页显示账号与最近检查时间。自动检查间隔、登录浏览器模式和本机调试地址在“账号设置”内保存。

管理动作 `account.list` 接受可选 `platform`、`query`、`offset` 和 `limit`。`offset` 从 0 开始，`limit` 默认 20、上限 100；结果按平台和账号 ID 升序，返回 `accounts`、`platforms`、`total`、`offset`、`limit`，空列表为 `[]`。`account.upsert`、`account.delete`、`account.validate` 管理单个账号；`account.qr_create`、`account.qr_poll` 和 `account.qr_cancel` 管理扫码会话。

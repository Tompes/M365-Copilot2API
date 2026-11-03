# 个人 Microsoft 账号（实验性适配）

此分支保留组织账号的 PKCE 登录，并新增个人账号凭证导入与独立的消费者 ChatHub 协议。
个人账号不能通过把组织授权地址改成 `/consumers` 来登录。也不要在个人账号入口粘贴 Microsoft 原生客户端回调网址。

## 网页手动导入（推荐）

不需要安装 Python 或启动新浏览器。

1. 登录本网关管理员控制台，进入「账号管理 → 微软个人账号」，点击「复制 Console 脚本」。脚本源码也可展开查看，对应仓库的 `scripts/personal-console.js`。
2. 在你常用浏览器打开 https://m365.cloud.microsoft/chat 并登录个人 Microsoft 账号，打开开发者工具的 Console，运行脚本。
3. **不要刷新页面**。新建聊天并发送一条简短消息，让网页建立新的聊天连接。脚本最多等待 5 分钟；成功后会输出 JSON，在 DevTools 支持时自动复制该 JSON，并恢复它临时监听的浏览器接口。
4. 回到本网关，将 JSON 粘贴到凭证输入框，点击「验证并添加个人账号」。也可把 JSON 存成文件后导入。
5. 网关会先刷新凭证（如有刷新令牌），再发送一条 `Reply only OK.` 验证消息，只有得到预期回复才保存。导入会消耗少量账号调用额度。

脚本不向任何网关上传数据，不读取本地浏览器存储，也不额外发送网络请求，只观察后续的聊天 WebSocket 及对应 OAuth 响应。仅在两个指定微软官方域名运行，且只导出个人账号路由。刷新令牌只有在它与实际聊天访问令牌匹配时才会导出，避免混用其他微软资源的令牌。

浏览器可能直接使用缓存令牌，所以不一定能采集到刷新令牌。此时导出的是短期访问凭证，页面会提示到期后重新采集；不能保证一直自动续期。脚本使用保守的短期过期时间，真正有效性以微软接口为准。不要在运行脚本后刷新页面，因为刷新会清除临时监听。

JSON 中包含套餐时保留其 `Free` / `Premium` 值；下拉框仅用于未包含套餐的手动文件。只有 `access_token` 不能确定账号路由，因此输入必须包含 `oid`、`expires_at` 等必要字段；脚本会生成完整格式。

凭证仅保存在当前页面内存，提交时才发送到你自己的网关，不写入 localStorage。请只导入到你信任且控制的网关，远程网关应使用 HTTPS。不要把令牌、完整聊天连接 URL、JSON 或抓包文件发送到聊天或提交代码。

## 可选：本地登录助手

仍可使用原有助手；浏览器仅在自己的电脑运行，云端服务不需要 Python、Playwright 或浏览器。

```sh
python3 -m venv .venv-personal
.venv-personal/bin/python -m pip install playwright
.venv-personal/bin/python -m playwright install chromium
.venv-personal/bin/python scripts/personal_login.py
```

在弹出的专用浏览器中登录个人 Microsoft 账号，打开聊天并发送测试消息。文件保存在 `secrets/personal.json`（0600），配置保存在 `secrets/personal-browser`。默认等待 10 分钟，不覆盖已有导出；重新采集可用 `--output secrets/personal-new.json`。在控制台直接导入文件即可。Windows-Copilot-API 的有效 `session/token.json` 也兼容。

服务端使用原有账号存储机制，建议配置私密的 `M365_MASTER_KEY` 并持久化数据目录。免费实例没有额外配置付费磁盘；容器替换可能丢失已保存数据。

## 范围与验证

- 个人账号与组织账号可以共存；个人账号使用独立的 OAuth 租户、聊天路由租户、套餐场景及聊天请求字段。
- 支持个人账号文本聊天及 OpenAI 兼容接口的普通响应和 SSE 流式响应。账号是否能使用某个模型由 Microsoft 的套餐和限额决定。
- 个人账号的图片/文件输入尚未适配，会明确返回错误。图片生成、组织云端历史管理、工具能力及其他扩展功能未做个人账号端到端验证。
- 个人账号使用独立连接，不参与组织版 WebSocket 预热/复用，也不初始化组织资源的云端历史客户端。
- 2026-11-02：真实个人 **Premium** 账号验证通过：原生 Go 刷新令牌、凭证导入及聊天探测、`/v1/chat/completions` 非流式和流式，均收到 `OK`。**Free 的协议分支有自动测试，但尚无真实免费个人账号验证。**
- Go 全量测试、个人账号相关测试及前端 TypeScript / Vite 构建通过；本机诊断凭证和原始抓包没有纳入源码。

后续复测时，微软曾对当前网络返回 `Copilot isn’t available in your region.`，即使响应标记成功也无法实际聊天。此分支会识别该地区限制，并拒绝把它当成成功回复或验证通过。此前真实聊天成功不代表所有地区/网络都可用；当前受到地区限制时不能报告服务已就绪。

这属于网页协议适配，并非 Microsoft 官方 API。Microsoft 改动网页协议或账号策略后可能需要更新。

## 本地开发

后端：`go test ./...`、`go build ./cmd/server`（Go 1.23 或更新版本）。
前端：`cd webapp && npm ci && npm run build`。构建产物位于 Go 的嵌入目录，发布二进制前应先构建前端。

源码分支：`feat/personal-accounts`。发布使用自己账号下的 fork，避免修改上游仓库。
后续若部署，可沿用原服务单副本 **0.2 vCPU / 512 MB**，不需要额外浏览器服务或数据库；免费配额资格需在部署时重新确认，持久化磁盘等单独计费资源需要另行批准。

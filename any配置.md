# any 配置

本文记录 Claude Code 接入 Any/New API 类第三方 Anthropic 兼容服务的最小可用配置流程。目标是让人和 AI 都能按步骤复现，先跑通一个稳定入口，再按需扩展 profile。

> 安全规则：不要把真实 API Key、token、Cookie 写进项目仓库、聊天记录或公开文档。示例里统一使用 `<YOUR_TOKEN>` 占位。

## 1. 配置目标

本流程解决四件事：

1. 跳过 Claude Code 首次安装后的官方登录引导。
2. 建立全局基础开关，减少非必要流量和安装检查干扰。
3. 配置 Any/New API 供应商的 `ANTHROPIC_BASE_URL` 和 `ANTHROPIC_AUTH_TOKEN`。
4. 避免常见误判：代理端口填错、Base URL 多写 `/v1`、短时间 403/5xx 被误认为本地配置错误、模型别名和供应商可用模型不匹配。

## 2. 文件位置

Windows 下 `~` 通常等于 `C:\Users\<用户名>`。本文以当前用户 `admin` 为例时，路径形如：

```text
C:\Users\admin\.claude.json
C:\Users\admin\.claude\settings.json
C:\Users\admin\.claude\settings.anyrouter.json
```

通用写法：

```text
~\.claude.json
~\.claude\settings.json
~\.claude\settings.anyrouter.json
```

## 3. 第一步：处理首次登录引导

新安装 Claude Code 时，可能没有 `~/.claude/settings.json`，并且会被登录引导拦住。

登录引导状态由 `~/.claude.json` 顶层字段控制：

```json
{
  "hasCompletedOnboarding": true
}
```

执行规则：

1. 打开 `~/.claude.json`。
2. 搜索 `hasCompletedOnboarding`。
3. 如果存在，把值改成 `true`。
4. 如果不存在，在 JSON 顶层添加 `"hasCompletedOnboarding": true`。

这一步完成后，Claude Code 应能进入 Prompt 输入界面。

## 4. 第二步：写全局基础配置

创建或修改：

```text
~\.claude\settings.json
```

推荐最小全局配置：

```json
{
  "env": {
    "HTTPS_PROXY": "http://127.0.0.1:7890",
    "HTTP_PROXY": "http://127.0.0.1:7890",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    "CLAUDE_CODE_ATTRIBUTION_HEADER": "0",
    "DISABLE_INSTALLATION_CHECKS": "1"
  },
  "cleanupPeriodDays": 720
}
```

关键点：

- `env` 里的值必须全部写成字符串，不要写布尔值或数字。
- `HTTP_PROXY` / `HTTPS_PROXY` 必须换成你本机代理软件的实际端口。
- 如果不用代理，或代理软件已经开了系统 TUN，可以先不写代理项。
- `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1"` 会禁用遥测、错误报告、自动更新等非必要流量。
- `CLAUDE_CODE_ATTRIBUTION_HEADER: "0"` 用于关闭额外客户端归因 header。
- `DISABLE_INSTALLATION_CHECKS: "1"` 用于减少安装方式检查提示。
- `ENABLE_TOOL_SEARCH` 不建议默认写入。它依赖模型能力和上游支持，不稳定时反而容易增加变量。

代理端口确认方法：

1. 打开 Clash Verge 或同类代理软件。
2. 找到 HTTP/Mixed Port。
3. 如果显示 `7897`，就写 `http://127.0.0.1:7897`。
4. 不要死抄 `7890`，端口必须以你本机实际软件为准。

## 5. 第三步：写 Any/New API 供应商配置

推荐用 profile 文件管理供应商：

```text
~\.claude\settings.anyrouter.json
```

示例：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://anyrouter.top",
    "ANTHROPIC_AUTH_TOKEN": "<YOUR_TOKEN>"
  },
  "model": "opus[1m]"
}
```

如果你的 New API 地址不是 `https://anyrouter.top`，就替换成你的实际地址，例如：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://muyuan.do",
    "ANTHROPIC_AUTH_TOKEN": "<YOUR_TOKEN>"
  },
  "model": "claude-sonnet-4-6"
}
```

Base URL 规则：

- Claude Code 会自己拼接 `/v1/messages`。
- `ANTHROPIC_BASE_URL` 只写根地址。
- 不要写成 `https://example.com/v1`。
- 末尾有没有 `/` 通常都应避免，推荐不带尾斜杠。

Token 规则：

- Any/New API 这类 Anthropic 兼容路由通常使用 `ANTHROPIC_AUTH_TOKEN`。
- 不要和 Codex、OpenCode 的 API Key 配置混用。
- 不要把 token 写到项目文件里。

## 6. 第四步：启动 Claude Code

推荐命令：

```powershell
claude --settings C:\Users\admin\.claude\settings.anyrouter.json
```

如果要继续上次会话：

```powershell
claude --settings C:\Users\admin\.claude\settings.anyrouter.json -c
```

如果首次安装时 profile 没生效，`/status` 里看到 Auth Token 为空，可以先把 profile 里的供应商 env 复制到主配置 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://anyrouter.top",
    "ANTHROPIC_AUTH_TOKEN": "<YOUR_TOKEN>",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    "CLAUDE_CODE_ATTRIBUTION_HEADER": "0",
    "DISABLE_INSTALLATION_CHECKS": "1"
  },
  "cleanupPeriodDays": 720,
  "model": "opus[1m]"
}
```

跑通后，再决定是否恢复 profile 分离管理。

## 7. 模型选择规则

优先原则：

1. 先用供应商明确支持的模型。
2. 如果 Any 路由当前推荐 Opus 1M，就用 `opus[1m]`。
3. 如果你的 New API 支持 Sonnet，日常编码优先用 `claude-sonnet-4-6`。
4. 如果 4xx 无 body 或疑似某个模型不可用，先切到供应商推荐模型，不要在本地配置上反复乱改。

注意：

- Anyrouter 一类服务可能提供和官方一致的模型 ID，因此通常不需要设置 `ANTHROPIC_DEFAULT_HAIKU_MODEL` 等默认路由。
- 阿里云百炼这类非官方模型映射才需要配置默认 Haiku/Sonnet/Opus 映射。

阿里云映射示例：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://dashscope.aliyuncs.com/apps/anthropic",
    "ANTHROPIC_API_KEY": "<YOUR_TOKEN>",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "qwen3.5-flash",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "qwen3.5-flash",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "qwen3.5-plus"
  }
}
```

## 8. 验证供应商模型列表

通常供应商会提供 `/v1/models` 端点。可以用 token 查询模型列表。

PowerShell 示例：

```powershell
$headers = @{ Authorization = "Bearer <YOUR_TOKEN>" }
Invoke-RestMethod -Headers $headers -Uri "https://anyrouter.top/v1/models"
```

看点：

- 响应里有哪些模型 ID。
- 是否包含你在 `model` 字段里配置的模型。
- 如果模型列表能返回，但 Claude Code 仍失败，问题更可能在模型可用性、消息接口兼容、代理或供应商波动。

## 9. 常见故障判断

### 9.1 `/status` 里 Auth Token 为空

原因：

- `--settings` profile 没被首次启动流程正确加载。
- env 写在了错误文件。
- JSON 格式错误。

处理：

1. 把 `ANTHROPIC_BASE_URL` 和 `ANTHROPIC_AUTH_TOKEN` 临时写入主 `~/.claude/settings.json`。
2. 重启 Claude Code。
3. `/status` 确认 Auth Token 不为空。

### 9.2 3 秒内报 403

判断：

- 如果配置刚刚可用过，New API 后台也有 token 消耗，3 秒内 403 很可能是供应商或上游波动。
- 不要立刻认定是本地配置错。

处理：

1. 等一会儿再试。
2. 切到供应商推荐模型，例如 `opus[1m]`。
3. 查看供应商后台是否有请求和 token 消耗。
4. 若有消耗但 Claude Code 报错，优先按供应商返回异常处理。

### 9.3 `API Error: Unable to connect to API (ECONNRESET)`

判断：

- 网络到 Base URL 不通。
- 代理端口不对。
- 代理软件没开，或模式不对。

处理：

1. 检查 `ANTHROPIC_BASE_URL` 能否在浏览器或 curl 访问。
2. 检查 Clash Verge 的实际 HTTP/Mixed 端口。
3. 把 `HTTP_PROXY` / `HTTPS_PROXY` 改成实际端口。
4. 如果用了 TUN，可以临时移除代理配置测试。

### 9.4 4xx 无 body

可能原因：

- 当前模型没额度。
- 当前模型被供应商临时下线。
- Haiku 或辅助模型欠费，但主模型可用。

处理：

1. 切到 Opus 1M 或供应商推荐模型。
2. 如果继续会话 `-c` 可用，但新任务失败，可能是某个辅助模型调用失败。
3. 用 POST 或供应商日志查看真正的错误 message。

### 9.5 New API 后台显示成功、有 token 消耗，但 Claude Code 仍报错

判断：

- 请求确实到达供应商。
- 本地 token/base_url 大概率不是完全错误。
- 问题可能在响应格式、模型兼容、上游中断、Cloudflare/网关超时。

处理：

1. 等待波动恢复。
2. 换推荐模型。
3. 检查是否配置了错误的 Base URL 后缀。
4. 不要泄露 token 给他人排查。

## 10. AI 执行清单

AI 帮用户配置 Claude Code 时，按这个顺序执行：

1. 确认目标工具是 Claude Code，不是 Codex 或 OpenCode。
2. 不在项目文件里保存真实 token。
3. 检查 `~/.claude.json`，确保 `hasCompletedOnboarding: true`。
4. 检查或创建 `~/.claude/settings.json`。
5. 读取用户代理软件实际端口，不死抄 `7890`。
6. 写入基础 env，所有值必须是字符串。
7. 创建或更新 `~/.claude/settings.anyrouter.json`。
8. `ANTHROPIC_BASE_URL` 不加 `/v1`。
9. token 使用 `ANTHROPIC_AUTH_TOKEN`，除非目标供应商明确要求 `ANTHROPIC_API_KEY`。
10. 启动命令使用 `claude --settings <profile>`。
11. 用 `/status` 检查 Auth Token 是否为空。
12. 如 Auth Token 为空，临时把供应商 env 合并到主 `settings.json`。
13. 3 秒内 403 且后台有消耗时，先等，不要马上乱改配置。
14. ECONNRESET 优先查网络和代理端口。
15. 4xx/5xx 优先查供应商状态、模型可用性和真实 error message。

## 11. 最终推荐形态

如果已经跑通，推荐保留两层配置：

全局基础配置：`~/.claude/settings.json`

```json
{
  "env": {
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    "CLAUDE_CODE_ATTRIBUTION_HEADER": "0",
    "DISABLE_INSTALLATION_CHECKS": "1"
  },
  "cleanupPeriodDays": 720
}
```

供应商 profile：`~/.claude/settings.anyrouter.json`

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://anyrouter.top",
    "ANTHROPIC_AUTH_TOKEN": "<YOUR_TOKEN>"
  },
  "model": "opus[1m]"
}
```

启动：

```powershell
claude --settings C:\Users\admin\.claude\settings.anyrouter.json
```

如果用户当前实际使用的是 `https://muyuan.do`，则把 profile 的 `ANTHROPIC_BASE_URL` 改为：

```json
"ANTHROPIC_BASE_URL": "https://muyuan.do"
```

并使用用户级 secret 保存 token。

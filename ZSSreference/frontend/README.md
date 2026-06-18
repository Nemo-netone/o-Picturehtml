# SimulSpeak Frontend

SimulSpeak 前端是一个 TypeScript + React + Vite 单页应用，负责浏览器音频采集、WebSocket 控制信令、WebRTC 媒体链路、双语字幕渲染，以及翻译策略和配音控制。

## 阅读顺序

前端开发前先阅读以下文档：

1. [`../docs/frontend/mvp.md`](../docs/frontend/mvp.md) - 前端唯一权威 MVP 规划，包含范围、里程碑、目录结构与验收标准。
2. [`../docs/03-frontend-spec.md`](../docs/03-frontend-spec.md) - 页面布局、组件职责、字幕状态模型与渲染规则。
3. [`../docs/05-interfaces.md`](../docs/05-interfaces.md) - 前端与服务端 WebSocket / WebRTC / HTTP 接口契约。
4. [`../docs/frontend/conventions.md`](../docs/frontend/conventions.md) - 命名、分层、样式 token、测试、mock、错误处理规范。
5. [`../docs/frontend/todolist.md`](../docs/frontend/todolist.md) - 细粒度任务清单。

## 启动命令

在 `frontend/` 目录执行：

```bash
pnpm install
pnpm dev
```

常用命令：

```bash
pnpm dev          # 启动 Vite 开发服务器
pnpm build        # TypeScript 构建检查 + Vite 生产构建
pnpm lint         # ESLint 检查
pnpm format:check # Prettier 格式检查
pnpm preview      # 预览生产构建产物
```

## 环境变量

前端环境变量使用 Vite 的 `VITE_` 前缀。建议在 `frontend/.env.local` 中配置，本地私有配置不要提交。

```ini
VITE_WS_URL=ws://localhost:8080/ws
VITE_TENANT_ID=tenant-a
VITE_CLIENT_ID=simulspeak-web
```

约定：

- `VITE_WS_URL`：WebSocket 控制通道地址，默认指向本地服务端 `/ws`。
- `VITE_TENANT_ID` / `VITE_CLIENT_ID`：用于组装 `?token={tenantId}:{clientId}`，拼接逻辑应收敛在 `src/ws/WsClient.ts`。
- 翻译、ASR、TTS provider 密钥不得进入浏览器环境变量，应由服务端环境变量或配置中心管理。
- 后续应提交 `frontend/.env.example`，只包含可公开的示例值。

## 目标目录结构

前端代码按 MVP 规划落到以下结构：

```text
frontend/
├─ src/
│  ├─ types/protocol.ts        # WS 消息类型、类型守卫、字段 getter
│  ├─ ws/WsClient.ts           # WS 连接、心跳、握手、消息分发
│  ├─ rtc/RtcClient.ts         # 音频采集、PeerConnection、SDP/ICE、下行音频
│  ├─ state/subtitles.ts       # 字幕 store 与渲染规则
│  ├─ session/useSession.ts    # 编排 WS + RTC 生命周期
│  ├─ styles/tokens.ts         # 字幕颜色、动画、阈值等设计 token
│  ├─ components/
│  │  ├─ TopBar.tsx
│  │  ├─ ControlBar.tsx
│  │  └─ SubtitlePanel.tsx
│  ├─ config.ts                # 读取 Vite env，导出结构化配置
│  └─ App.tsx
├─ mock/
│  └─ fixtures/*.json          # 标准 WS 回放序列
├─ .env.example
└─ package.json
```

依赖方向遵循：

```text
config -> types -> ws / rtc -> state -> session -> components -> App
```

组件不得直接 import `ws/` 或 `rtc/`，WS/RTC 事件统一由 `session/` 编排后写入状态。

## Mock 联调方式

服务端未就绪时，M1-M3 以前端 mock WS 回放推进。

mock 约定：

- mock 服务端放在 `frontend/mock/`。
- 回放数据放在 `frontend/mock/fixtures/*.json`。
- fixture 必须逐字遵循 [`../docs/05-interfaces.md`](../docs/05-interfaces.md) A 节协议。
- 标准回放序列至少覆盖：
  - `asr_result` partial 多帧到 final。
  - `translation_result` partial(tmt) 多帧到 final(deepseek)。
  - 至少一句 `revised=true`，用于验证“已纠正”高亮。
  - 英文或中文缺失场景，用于验证优雅降级。

当前仓库尚未实现 mock server 和 fixture。实现后应在 `package.json` 中增加对应脚本，并把启动方式补到本 README。

## 验收命令

每次提交前至少运行：

```bash
pnpm build
pnpm lint
pnpm format:check
```

涉及字幕状态或协议类型时，还应补充 Vitest 单元测试，并以 mock fixture 作为统一输入基准。测试脚本落地后应统一加入 `package.json`，例如：

```bash
pnpm test
```

MVP 手动验收以 [`../docs/frontend/mvp.md`](../docs/frontend/mvp.md) 的 M0-M5 里程碑为准。每个里程碑完成后，对照对应“验收”项逐条核验，通过后再进入下一个里程碑。

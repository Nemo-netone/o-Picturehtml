# SimulSpeak 前端开发规范（conventions）

> 依据 [`03-frontend-spec.md`](../03-frontend-spec.md)、[`05-interfaces.md`](../05-interfaces.md)、[`mvp.md`](./mvp.md)、[`todolist.md`](./todolist.md) 制定。
> 本文是**约束性规范**：多人/多 PR 协作时统一命名、分层、样式、测试、联调与错误处理。与 mvp/todolist 的「做什么」互补，本文回答「按什么标准做」。
> 项目级 PR 流程见仓库根 [`CLAUDE.md`](../../CLAUDE.md)，本文不重复，仅在涉及处引用。

---

## 1. 代码规范

### 1.1 命名

| 类别 | 规则 | 示例 |
|------|------|------|
| 组件文件/组件 | PascalCase，文件名 = 组件名 | `SubtitlePanel.tsx` → `SubtitlePanel` |
| Hook | `useXxx` 驼峰 | `useSession.ts` → `useSession` |
| Store | `useXxxStore` | `useSubtitleStore` |
| 类型 / 接口 | PascalCase；WS 消息类型以语义命名 | `AsrResultMessage`、`SubtitleLine`、`Strategy` |
| 普通变量/函数 | camelCase | `parseFrame`、`utteranceId` |
| 常量 | UPPER_SNAKE_CASE（见 §3） | `THROTTLE_MS`、`MAX_SUBTITLE_LINES` |
| WS 事件字符串 | 与 05-interfaces 协议字段**逐字一致**，集中为常量对象 | `WS_TYPE.asr_result` |

- 禁止拼音命名；禁止无意义缩写（`tmp`、`data2`）。
- 布尔变量用 `is/has/should` 前缀：`isConnected`、`enFinal`、`hasTranslation`。

### 1.2 目录分层与依赖方向

目录结构以 [`mvp.md` §3](./mvp.md) 为准。分层依赖**单向**，禁止反向/横向穿透：

```
config ──▶ types ──▶ ws / rtc ──▶ state ──▶ session ──▶ components ──▶ App
```

硬约束：
- `components/` **只**从 `state/` 与 `session/` 取数据，**禁止**直接 import `ws/`、`rtc/`。
- `ws/`、`rtc/` 不得 import `components/` 或 `state/`（它们只产出事件/回调，由 `session/` 编排写入 store）。
- 数据单向流：WS/RTC 事件 → `session` 编排 → `state` store → 组件订阅渲染。组件不得直接改 WS/RTC。

### 1.3 类型与字段访问

- **所有 WS 字段访问必须经 `types/protocol.ts` 的 getter / 类型守卫**，禁止在组件或 store 里裸取 `msg.metadata.xxx`。
  - 典型：`utteranceId` 在 asr 帧位于 `metadata.utteranceId`，在 translation 帧位于顶层 `utteranceId` —— 统一封装 `getUtteranceId(msg)`，调用方不感知差异（对应 mvp 第 145 行风险项）。
- 收到的 WS 帧先用类型守卫（`isAsrResult(msg)` 等）窄化，再分发，禁止 `as any` 强转。
- `Strategy`、`SubtitleStatus` 等使用字面量联合类型，不用裸 `string`。

---

## 2. 样式 / Design Tokens 规范

所有字幕状态相关的颜色、动画、阈值集中到 `src/styles/tokens.ts`，**组件内禁止写死十六进制颜色或魔法时长**。

```ts
// src/styles/tokens.ts（示例，最终值以 03-frontend-spec 为准）
export const COLOR = {
  subtitlePending: '#9aa0a6', // 灰：partial / 未锁定
  subtitleFinal:   '#202124', // 黑：final / 锁定
  reviseHighlight: '#fff3b0', // 黄底：纠错高亮
} as const

export const MOTION = {
  reviseHighlightMs: 600,     // ✦已纠正 高亮淡入淡出时长
} as const
```

- 字幕状态 → 样式必须是**单一映射函数**（如 `subtitleColor(line)`），组件不自行 if/else 拼颜色。
- 锁定标记 🔒、「✦已纠正」等文案/图标集中常量化，便于后续 i18n。

---

## 3. 具名常量规范（消除魔法数字）

散落在 mvp/todolist 的数值统一收敛到 `src/config.ts` 或 `tokens.ts`，具名导出：

| 常量 | 含义 | 建议值 | 出处 |
|------|------|--------|------|
| `THROTTLE_MS` | 中文 partial 节流刷新间隔 | 150–200 | mvp §5 |
| `MAX_SUBTITLE_LINES` | 字幕列表保留条数 | 50 | todolist 阶段4 |
| `HEARTBEAT_INTERVAL_MS` | `ping` 发送间隔 | 待定 | todolist 阶段2 |
| `HEARTBEAT_TIMEOUT_MS` | `pong` 超时判定 | 待定 | todolist 阶段2 |
| `RECONNECT_BACKOFF_MS` | 重连退避（加分项） | 待定 | todolist 阶段6 |

代码中禁止出现裸字面量 `50` / `200`，一律引用常量。

---

## 4. 测试规范

| 模块 | 是否要求自动化测试 | 说明 |
|------|------------------|------|
| `state/subtitles.ts` 渲染规则 | **必须**（单元测试） | 纯函数/纯 reducer，输入帧序列断言输出 SubtitleLine 状态（灰/黑/revised/锁定） |
| `types/protocol.ts` getter / 类型守卫 | **必须**（单元测试） | 覆盖 asr 与 translation 两种 `utteranceId` 取值路径 |
| `ws/WsClient` 编解码/分发 | 建议 | 可用 mock socket 注入帧 |
| `rtc/RtcClient`、UI 组件 | 手动验收为主 | 依赖浏览器媒体能力，按 mvp 里程碑「验收」逐条核验 |

约定：
- 测试框架统一 **Vitest**（与 Vite 同生态）。
- 渲染规则测试以 §5 的「标准回放序列」为唯一基准输入。
- 里程碑「验收通过」的硬性门禁可叠加 settings.json Stop hook 跑 `npm run build && npm run lint`（见 mvp 第 120 行注）。

---

## 5. Mock / 联调数据规范

mock WS 服务端不是「可选风险对策」，而是 **M1–M3 的关键依赖**，需固化：

- 位置：`frontend/mock/`，回放数据 `frontend/mock/fixtures/*.json`。
- 标准回放序列（一段英文一句）必须覆盖完整状态机：
  1. `asr_result` partial（多帧，灰）→ `asr_result` final（黑锁定）
  2. `translation_result` partial(tmt)（灰，多帧用于验证节流）
  3. `translation_result` final(deepseek)（黑）
  4. 其中至少一句 `revised=true`（验证「✦已纠正」高亮）
- 帧格式**逐字遵循** [`05-interfaces.md`](../05-interfaces.md) A 节，时序间隔写入 fixture（partial→final 间隔），保证回放可复现。
- 该序列同时作为 §4 渲染规则单元测试与 M3 手动验收的**唯一基准数据**，避免两套口径。

---

## 6. 配置与环境变量规范

| 项 | 规范 |
|----|------|
| `VITE_WS_URL` | 必填，`.env.example` 给默认 `ws://localhost:8080/ws` |
| `tenantId` / `clientId` | 来源在 `config.ts` 收敛；MVP 阶段可由 env 注入，**禁止散落硬编码在组件里** |
| token 拼接 | `?token={tenantId}:{clientId}` 统一在 `WsClient` 内组装，调用方传结构化字段，不手拼字符串 |
| 安全 | token / 凭据不写入仓库、不打日志（见 §7）；`.env` 入 `.gitignore`，只提交 `.env.example` |

---

## 7. 日志规范

- 统一前缀，便于过滤：`[ws]`、`[rtc]`、`[state]`、`[session]`。
- 级别：`console.debug` 高频帧（partial）、`console.info` 生命周期（握手三段、连接状态变化）、`console.warn` 可恢复异常、`console.error` 信令级 error 帧。
- **禁止**打印 token、完整凭据、整段音频数据。
- 高频日志（每帧 partial）默认走 debug，可由开关关闭，避免刷屏。

---

## 8. 浏览器兼容性基线

- 目标浏览器：**Chrome / Edge 最新两个大版本**（依赖 WebRTC、`getDisplayMedia`、autoplay 解锁）。
- `getDisplayMedia({audio:true})` 需安全上下文：`localhost` 视为安全；生产需 HTTPS。
- 下行配音 `<audio autoplay>` 须由用户手势（「开始」按钮）解锁，禁止在无手势时尝试自动播放。
- 不承诺 Safari / 移动端兼容（MVP 范围外）。

---

## 9. 错误处理对照表

区分「信令级错误」与「翻译类静默错误」是核心约束（对应 mvp 第 98、144 行）。统一按下表处理：

| 错误类型 | 触发 | 是否打断 UI | 处理 / 文案 |
|----------|------|------------|-------------|
| 信令 `error` 帧 | 服务端返回 `type:"error"` | 是（顶栏/Toast 提示） | 顶栏标红 + 「连接异常：{message}」 |
| WS 断开 | `onclose`/心跳超时 | 是（顶栏状态） | 顶栏「已断开」；MVP 用「停止再开始」，加分项做自动重连 |
| 翻译类错误（无 key / DeepSeek 失败 / 迟到） | 中文缺失或 final 不来 | **否（静默降级）** | 中文留空或保留 TMT 快翻灰字，**不弹错误、不中断** |
| translation final 乱序/迟到 | 锁定行被旧 partial 覆盖风险 | 否 | 锁定行（`zhFinal`/`enFinal`）只接受 revise，不被 partial 回退 |
| 媒体采集失败 | `getUserMedia`/`getDisplayMedia` 拒绝 | 是 | 控制栏提示「无法获取音频源，请检查权限」 |
| autoplay 被拦 | 下行音轨无手势播放 | 否 | 提示用户点击页面以启用配音；字幕不受影响 |

原则：**字幕主链路对翻译类异常永不崩溃**，只做优雅降级；仅信令/媒体/连接级错误允许打断 UI。

---

## 10. 与 PR 规范的关系

- 全部任务以 PR 形式提交，单 PR 单功能，套用四段式模板，主分支始终可运行 —— 见 [`CLAUDE.md`](../../CLAUDE.md)。
- 任务到 PR 的具体拆分映射见 [`mvp.md` §4.1](./mvp.md)。
- 本规范的修改本身也走 PR。

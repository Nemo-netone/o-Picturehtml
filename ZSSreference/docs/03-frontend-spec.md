# 03 · 前端页面功能规格

前端是一个独立 Web 单页应用（SPA），自带 WebRTC 客户端与 WebSocket 控制客户端，完成音频采集、信令握手、双语字幕渲染与纠错/配音控制。技术栈：TypeScript + React（或等价框架）。

前端只维护一条到 `api-server` 的业务 WebSocket，不直接连接 PBX 控制通道。`webrtc_offer` / `ice` 发给 `api-server` 后由主服务转发给选定 `pbx-node`；浏览器与该 `pbx-node` 直接建立 WebRTC 媒体连接。

## 1. 页面目标

一个单页面：选择音频源与纠错策略 → 一键开始同传 → 实时看「英文原文 + 中文译文」双语字幕（PBX TMT 灰字 → DeepSeek Flash 黑字纠错高亮）→ 中文配音同步 → 结束后可导出/总结。

## 2. 页面布局

```
┌──────────────────────────────────────────────────────────────────────┐
│ SimulSpeak 同声传译           ● 已连接  延迟: 1.2s  会话:U-07          │ 顶栏(状态)
├──────────────────────────────────────────────────────────────────────┤
│ 音频源:[标签页/系统 ▾][麦克风]  策略:[混合▾]  配音:[开]  [▶ 开始][■ 停止]│ 控制栏
├──────────────────────────────────────────────────────────────────────┤
│  双语字幕（自动滚动，按句分块）                                          │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ EN  We use a transformer architecture.            🔒            │  │ 已锁定
│  │ 中  我们采用一种 Transformer 架构。      ✦已纠正 🔒              │  │ (黑,纠错高亮)
│  ├────────────────────────────────────────────────────────────────┤  │
│  │ EN  the attention mechanism allows…                             │  │ 进行中
│  │ 中  注意力机制允许……（待定）                                    │  │ (灰)
│  └────────────────────────────────────────────────────────────────┘  │
├──────────────────────────────────────────────────────────────────────┤
│ [导出 SRT] [导出 TXT] [生成要点总结]            术语表: transformer→… │ 底栏(加分)
└──────────────────────────────────────────────────────────────────────┘
```

## 3. 组件清单与职责

| 组件 | 职责 |
|------|------|
| 顶栏状态 | 连接状态、端到端延迟估计、当前会话/utterance |
| 音频源选择 | 标签页/系统音频（`getDisplayMedia({audio:true})`）或麦克风（`getUserMedia`），取音频 track 加入 PeerConnection |
| 策略选择 | 下拉：混合(默认)/仅快翻/仅纠错；写入 client_hello 或运行时 `set_strategy`，控制主服务 DeepSeek Flash 纠错策略，不表示前端直接选择 PBX 内部实现 |
| 配音开关 | 是否对 final 中文触发 TTS 下行配音 |
| 开始/停止 | 建立/关闭 WebRTC + WS；驱动信令握手 |
| WebRTC 客户端 | 创建 RTCPeerConnection、采集音频 track、createOffer、处理 answer/ICE、接收下行配音音频 |
| WS 控制客户端 | 只连接 `api-server` 的 `/ws`，发 client_hello/offer/ice/set_strategy，收 asr_result、PBX TMT translation_result、DeepSeek Flash translation_result/事件 |
| **双语字幕面板** | 核心组件：按 utteranceID 分块渲染「EN 行 + 中文行」，处理灰/黑/纠错高亮、自动滚动 |
| 术语表展示 | 实时显示会话术语表（可选，体现术语一致性） |
| 导出/总结 | 导出 srt/txt；请求要点总结（加分） |

## 4. 前端状态模型

字幕以 utteranceID 为主键聚合（`Map<string, SubtitleLine>` + 有序列表）：

```ts
type SubtitleStatus = "pending" | "locked" | "revised";

interface SubtitleLine {
  utteranceId: string;
  en: string;             // 英文原文（来自 asr_result）
  enFinal: boolean;       // 英文是否已 final
  zh: string;             // 中文译文（来自 translation_result）
  zhFinal: boolean;       // 中文是否已 final（纠错/锁定完成）
  engine?: string;        // "tmt" | "deepseek-flash"
  revised: boolean;       // DeepSeek Flash 是否与 PBX TMT 快翻不同（触发高亮）
  status: SubtitleStatus;
  updatedAt: number;
}
```

## 5. 渲染规则（commit/revise 可视化）

| 来源消息 | 行为 |
|----------|------|
| `asr_result`(partial) | upsert 行，更新 `en`，英文样式=灰；不存在则新建追加到列表底部 |
| `asr_result`(final) | 更新 `en`，`enFinal=true`，英文样式=黑(锁定) |
| `translation_result`(engine=tmt) | 来自 PBX TMT 快翻，更新 `zh`，中文样式=灰(待定)；同一 `utteranceId` 就地刷新，避免抖动 |
| `translation_result`(final,engine=deepseek-flash) | 来自主服务 DeepSeek Flash 纠错，覆盖同一 `utteranceId` 的 TMT 灰字，`zhFinal=true`，中文样式=黑(锁定)；若 `revised=true` 附「✦已纠正」并播放一次高亮动画(0.6s) |

样式约定：灰 `#9aa0a6`，黑 `#202124`，纠错高亮黄底淡入淡出。锁定行不再变更。列表保留最近 N 条（如 50），超出截断；新句到达自动滚动到底（用户上滚查看历史时暂停自动滚动）。

## 6. 交互流程

### 6.1 开始同传
1. 读取音频源、策略、配音开关。
2. 获取音频 MediaStream（标签页/系统或麦克风）。
3. 建立到 `api-server` 的 WebSocket → 收 `connected` → 发 `client_hello`（含纠错策略、配音开关）。
4. 创建 RTCPeerConnection，加音频 track，`createOffer` → 发 `webrtc_offer` 给 `api-server` → `api-server` 转发给选定 `pbx-node` → 收 `webrtc_answer` → 交换 `ice`。
5. 媒体连接建立后，浏览器音频直接上行到 `pbx-node`；前端通过主服务 WebSocket 接收 `asr_result`、PBX TMT `translation_result` 和 DeepSeek Flash `translation_result` 并渲染；下行音频轨自动播放配音。

### 6.2 停止/重连
- 停止：关闭 PeerConnection 与 WS，保留已显示字幕。
- 断线重连：WS 重连 + 重新 offer；重建后以新 utteranceID 继续（历史字幕保留展示）。

### 6.3 策略切换
- 运行时切换：发送 `set_strategy`（见接口），控制后续句子的主服务 DeepSeek Flash 纠错行为；已锁定句不变，PBX TMT 实时灰字通道仍作为基础链路。

## 7. 边界与体验
- 英文/中文任一缺失时只渲染当前可用的一行（如 DeepSeek Flash 纠错未回前先显示 PBX TMT 灰字）。
- 长句换行、专有名词保留原文（提示词约束）。
- 延迟估计：用「音频帧时间 vs 字幕到达时间」粗估，顶栏展示。
- 无障碍：字幕区可调字号；高对比配色。

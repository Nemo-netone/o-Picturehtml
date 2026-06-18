# SimulSpeak 前端 MVP 规划

> 依据 [`03-frontend-spec.md`](../03-frontend-spec.md)、[`05-interfaces.md`](../05-interfaces.md)、[`todolist.md`](./todolist.md) 制定。
> 编码标准（命名/分层/样式 token/测试/mock/错误表）见 [`conventions.md`](./conventions.md)；PR 流程见仓库根 [`CLAUDE.md`](../../CLAUDE.md)。
> 目标：以最小代价打通**端到端可演示**的实时同传主链路，加分项后置。

---

## 0. 进度看板（随时勾选观察进度）

> 用法：完成一项就把 `[ ]` 改成 `[x]`；里程碑下所有子项 + 验收都勾上，才勾该里程碑。整体进度看本节顶部计数。

**整体进度：2 / 6 里程碑 · 16 / 19 PR**（完成后手动更新此行）

| 里程碑 | 状态 | PR 进度 |
|--------|------|--------|
| M0 脚手架 | ✅ 已验收 | 2 / 2 |
| M1 协议 + WS 握手 | ✅ 已验收 | 5 / 5 |
| M2 字幕面板 | 🟡 进行中 | 3 / 3 |
| M3 WebRTC | 🟡 进行中 | 3 / 3 |
| M4 编排 + 控制栏 | 🟡 进行中 | 3 / 4 |
| M5 边界 + 顶栏 | ⬜ 未开始 | 0 / 2 |

> 状态图例：⬜ 未开始 ｜ 🟡 进行中 ｜ ✅ 已验收。每完成一个里程碑，同步更新本表状态与「整体进度」计数。

---

## 1. MVP 目标（一句话）

> 打开页面 → 选「麦克风/标签页音频 + 混合策略」→ 点「开始」→ 实时看到**英文原文（灰→黑）+ 中文译文（灰快翻→黑重译，纠错高亮）** 双语字幕，并听到中文配音 → 点「停止」保留字幕。

成功标准 = 一次完整演示中，一段英文语音能稳定产出双语字幕且字幕随 partial/final 正确变色与就地纠正。

---

## 2. MVP 范围（In / Out）

### ✅ In（必须做，构成可演示闭环）
| 模块 | 内容 | 对应 todolist |
|------|------|--------------|
| 工程脚手架 | Vite + React + TS，环境变量 `VITE_WS_URL` | 阶段 0 |
| 协议类型 | WS 全量消息 TS 类型（至少主链路用到的） | 阶段 1 |
| WS 控制客户端 | 连接 / JSON 编解码 / requestId / 心跳 / 握手 / 消息分发 / error 帧 | 阶段 2 |
| WebRTC 客户端 | 音频采集（麦克风 + 标签页二选一）/ offer-answer / ice / 下行配音播放；信令层可 mock 验收，媒体层依赖真实服务端 | 阶段 4 |
| 字幕状态模型 | `Map<utteranceId, SubtitleLine>` + 渲染规则（灰/黑/纠错）+ 自动滚动；基于 mock 回放优先形成可视化 demo | 阶段 3 |
| UI 三栏 | 顶栏状态 / 控制栏 / 双语字幕面板 | 阶段 5（底栏后置） |
| 交互编排 | 开始 / 停止 / 运行时策略切换 / 麦克风输入可视化（Web Audio API 音量波浪条） | 阶段 6（重连后置） |
| 优雅降级 | 单语缺失只渲染已有行；翻译错误不中断 | 阶段 7（部分） |

### ⛔ Out（MVP 后，加分/健壮性）
- 断线自动重连（阶段 6）
- 导出 SRT / TXT、要点总结、术语表展示（阶段 8 / 底栏）
- 延迟精确估计（先用占位/粗估）
- 无障碍字号调节、高对比主题切换
- 显式 `tts_command` 手动配音（依赖服务端 dubbing=1 自动触发即可）

---

## 3. 目录结构

```
frontend/
├─ src/
│  ├─ types/protocol.ts        # 阶段1：WS 消息 + SubtitleLine 类型
│  ├─ ws/WsClient.ts           # 阶段2：连接/心跳/握手/分发
│  ├─ rtc/RtcClient.ts         # 阶段4：采集/PeerConnection/ice/下行音频
│  ├─ audio/useAudioVisualizer.ts # 阶段6：Web Audio API 输入音量分析（复用 MediaStream）
│  ├─ state/subtitles.ts       # 阶段3：zustand store + 渲染规则
│  ├─ session/useSession.ts    # 阶段6：编排 WS+RTC 生命周期
│  ├─ components/
│  │  ├─ TopBar.tsx            # 顶栏状态
│  │  ├─ ControlBar.tsx        # 音频源/策略/配音/开始停止
│  │  ├─ AudioVisualizer.tsx   # 输入音量波浪条（Web Audio API 数据驱动）
│  │  └─ SubtitlePanel.tsx     # 核心双语字幕面板
│  ├─ config.ts                # env: VITE_WS_URL / tenantId / clientId
│  └─ App.tsx
├─ .env.example                # VITE_WS_URL=ws://localhost:8080/ws
└─ package.json
```

---

## 4. 里程碑（按依赖顺序，每个都可独立验证）

### ✅ M0 · 脚手架可跑（0.5d）
- [x] `npm create vite` + ESLint/Prettier；`config.ts` 读取 env。
- [x] **验收**：`npm run dev` 出空白三栏布局（静态占位）。

### ✅ M1 · 协议类型 + WS 握手（1d）
- [x] `protocol.ts`：定义 `connected/client_hello/_ack/webrtc_offer/answer/ice/asr_result/translation_result/set_strategy/_ack/tts_result/ping/pong/error` + `SubtitleLine`。
- [x] `WsClient`：连 `ws://{host}/ws?token={tenantId}:{clientId}`，自动 JSON 编解码、`requestId` 生成、`ping/pong` 心跳、事件订阅分发。
- [x] 握手：收 `connected` → 发 `client_hello`（以前端 ↔ 主服务协议权威 [`05-interfaces.md`](../05-interfaces.md) A.2 为准：`responseMode:"compact"`、`metadata.translateStrategy/dubbing`；ASR `engine_model_type=16k_en`、`forward_partial=1` 与 TTS provider 配置由服务端保证，不下发浏览器）→ 等 `client_hello_ack`。
- [x] **验收**：控制台打印握手三段；mock 服务端回 `asr_result` 能被分发回调收到。

### 🟡 M2 · 字幕状态 + 双语面板（核心，2d）
- [x] `subtitles.ts`（zustand）实现渲染规则：
  - [x] `asr_result(partial)` → upsert `en`，灰；不存在则新建追加。
  - [x] `asr_result(final)` → `enFinal=true`，黑锁定。
  - [x] `translation_result(partial,tmt)` → `zh` 灰，**节流刷新**防抖。
  - [x] `translation_result(isFinal=true)` → `zhFinal=true` 黑色锁定；`engine=deepseek-flash && revised=true` 时加「✦已纠正」+0.6s 高亮动画；`deepseek-flash` final 可覆盖 `tmt` partial/final，且不再被后续 `tmt` 覆盖。
- [x] 保留最近 50 条。
- [x] 自动滚到底，用户上滚则暂停自动滚动。
- [x] `SubtitlePanel`：按 `utteranceId` 分块渲「EN 行 + 中文行」，🔒 锁定标记，灰/黑/高亮样式（灰 `#9aa0a6`、黑 `#202124`、黄底淡入淡出）。
- [ ] **验收**：回放一段 mock 消息序列，字幕按 partial→final 正确变色、就地纠正、自动滚动。

### 🟡 M3 · WebRTC 上行 + 下行配音（1.5d）
- [x] 采集：`getUserMedia`（麦克风）/ `getDisplayMedia({audio:true})`（标签页/系统）二选一取 track。
- [x] `RTCPeerConnection` 加 track → `createOffer` → 发 `webrtc_offer` → 收 `webrtc_answer`（setRemoteDescription）→ 双向 `ice` 交换。
- [x] `ontrack` → `<audio autoplay>` 播放中文配音。
- [x] 监听 `connectionState/iceConnectionState` 上报顶栏。
- [x] **信令层验收（mock 可关闭）**：能创建 offer、通过 WS 发 `webrtc_offer`、接收 mock/测试替身 `webrtc_answer` 并 setRemoteDescription，ICE candidate 能双向分发，状态变化可上报。当前前端 RTC 层已实现并通过本地 lint/build/test；真实链路仍待 api-server/pbx-node 联调确认。
- [ ] **媒体层验收（依赖真实服务端）**：与服务端建立 PeerConnection（connected 状态），说话后服务端能收到音频；下行配音音轨能出声。若 pbx-node/媒体服务未就绪，此项标记为外部阻塞，不阻塞 M2 字幕面板与 mock demo 推进。

### 🟡 M4 · 编排串联 + 控制栏（1d）
- [x] `ControlBar`：音频源选择、策略下拉（混合/纯TMT/纯DeepSeek）、配音开关、开始/停止。
- [x] `useSession` 串联「开始」：读取音频源/策略/配音 → 取 MediaStream → WS 握手 → RTC offer/answer/ice → 渲染回推。
- [ ] 输入音量可视化：复用当前会话采集到的 `MediaStream`，用 Web Audio API（`AudioContext` + `AnalyserNode`）计算音量/频谱，`AudioVisualizer` 在控制栏展示实时波浪条；停止会话时取消动画循环并释放分析节点。
- [x] 「停止」：关 PeerConnection + WS，保留字幕。
- [x] 运行时策略切换：发 `set_strategy` → 等 `set_strategy_ack`，对后续句生效。
- [ ] **验收**：真实点「开始」跑通完整链路；说话时控制栏波浪条随输入音量变化，停止后静止/隐藏；中途切策略对新句生效。当前已完成前端编排实现并通过本地 lint/build/test，完整真实链路仍待 api-server/pbx-node 联调确认。

### ⬜ M5 · 边界与降级 + 顶栏打磨（0.5d）
- [ ] 英/中任一缺失只渲已有行；翻译类错误不弹中断（中文留空/保留快翻）。
- [ ] 无翻译 key 时仅英文字幕不崩。
- [ ] 顶栏：连接状态、字幕到达/体感延迟粗估（以 `~0.8s` 这类约值展示，避免精确毫秒口径）、会话/utteranceId。
- [ ] **验收**：人为制造中文延迟/缺失，前端不崩、字幕优雅降级。

**预计总工期 ≈ 6.5 人日**（单人；含联调缓冲）。

### 执行约定 · 每做完一部分就判断状态

每个里程碑（M0–M5）作为一个独立任务推进，按下列闭环执行——**做完一部分立即判断状态，验收通过才进入下一个**：

1. **开始前**：从主分支切功能分支，把该任务标记为 `in_progress`。
2. **完成后核验**：对照该里程碑的「验收」逐条核验（能跑就跑、mock 回放、手动验证）。
3. **状态判断报告**：输出一段结论——✅ 通过 / ⚠️ 有遗留 / ❌ 未达标 + 原因。
4. **门禁推进**：验收通过才以 **PR** 形式提交（套用 CLAUDE.md 四段式模板），合并后主分支须保持可运行，再标 `completed` 并开始下一个；未通过则保持 `in_progress` 并说明阻塞，不跨越。

> 粒度提示：里程碑只是开发节奏，**不等于 PR 粒度**。一个里程碑通常拆成多个「单功能 PR」，拆分映射见 §4.1。

依赖顺序（单人串行，逐个推进）：

```
M0 脚手架 ──▶ M1 协议+WS握手 ──▶ M2 字幕面板(mock回放可验收) ──▶ M3 WebRTC(信令mock / 媒体真服) ──▶ M4 编排+控制栏 ──▶ M5 边界+顶栏
```

> 注：「判断状态」依赖对验收标准的推理判断，由执行者完成，而非固定 shell 钩子。如需机械化硬性门禁（如每次停止自动 `npm run build && lint`），可另加 settings.json Stop hook，与本约定叠加。

### 4.1 里程碑 → PR 拆分映射（逐个勾选）

遵循 CLAUDE.md「单 PR 单功能、主分支始终可运行」。每个 PR 自身可运行、可独立验证；标题统一前缀 `feat(fe):` / `chore(fe):`。**合并一个就勾一个**，并回到 §0 看板更新计数。

**M0 · 脚手架（2 / 2）**
- [x] PR-1 `chore(fe): 初始化 Vite+React+TS 脚手架与 ESLint/Prettier` — 工程脚手架 + 空三栏占位
- [x] PR-2 `chore(fe): config.ts 读取 env 与 .env.example` — 环境变量收敛

**M1 · 协议 + WS 握手（5 / 5）**
- [x] PR-3 `feat(fe): 定义 WS 协议类型与 SubtitleLine` — `types/protocol.ts` + getter/类型守卫
- [x] PR-4 `feat(fe): WsClient 连接/JSON 编解码/requestId` — WS 基础连接
- [x] PR-5 `feat(fe): WS 心跳 ping/pong 与超时检测` — 心跳
- [x] PR-6 `feat(fe): WS 握手与消息分发/error 帧处理` — 握手 + 分发
- [x] PR-7 `chore(fe): mock WS 服务端与标准回放 fixtures` — 联调基座（见 conventions §5）

**M2 · 字幕面板（3 / 3）**
- [x] PR-8 `feat(fe): 字幕 store 与渲染规则（含单测）` — `state/subtitles.ts` + Vitest
- [x] PR-9 `feat(fe): SubtitlePanel 双语面板与样式 token` — 面板渲染 + `tokens.ts`
- [x] PR-10 `feat(fe): 自动滚动与上滚暂停` — 滚动行为

**M3 · WebRTC（3 / 3；媒体层真实服务端待联调验收）**
- [x] PR-11 `feat(fe): 音频采集（麦克风/标签页二选一）` — 采集
- [x] PR-12 `feat(fe): RtcClient offer/answer/ice 信令` — 上行 PeerConnection，mock 可验收
- [x] PR-13 `feat(fe): 下行配音 ontrack 自动播放` — 下行音频，真实出声依赖服务端

**M4 · 编排 + 控制栏（3 / 4；真实链路待联调验收）**
- [x] PR-14 `feat(fe): ControlBar 控制栏（音频源/策略/配音/起停）` — 控制栏
- [x] PR-15 `feat(fe): useSession 编排开始/停止生命周期` — 编排串联
- [x] PR-16 `feat(fe): 运行时策略切换 set_strategy` — 策略切换
- [ ] PR-17 `feat(fe): Web Audio API 输入音量波浪条` — 复用会话 MediaStream，实时展示说话音量

**M5 · 边界 + 顶栏（0 / 2）**
- [ ] PR-18 `feat(fe): 单语缺失降级与翻译错误不中断` — 边界降级（conventions §9）
- [ ] PR-19 `feat(fe): 顶栏连接状态与延迟粗估` — 顶栏打磨

> 上列 PR 划分为建议基线；若某项实际很小可合并、很大需再拆，原则始终是「一个 PR 只做一件事且可运行」。

---

## 5. 关键技术决策

| 决策点 | MVP 选择 | 理由 |
|--------|---------|------|
| 状态管理 | zustand | 轻量，字幕 store 单一来源，组件订阅简单 |
| 字幕主键 | `utteranceId`（来自 metadata/字段） | partial/final 同句就地更新的唯一依据 |
| TMT/LLM 分栏 | 同一 `utteranceId` 下分别展示 | TMT final 是中文草稿，LLM final 是校准结果 |
| 配音 | 仅依赖服务端 `dubbing=1` 自动下行 | 不做前端 `tts_command`，减负 |
| 延迟估计 | 字幕到达/体感延迟粗估（`~0.8s` 这类约值），不精确 | 演示够用，避免呈现为精确性能指标，精确化后置 |
| 输入音量可视化 | Web Audio API（`AudioContext` + `AnalyserNode`）驱动控制栏波浪条，复用当前会话 `MediaStream` | 不引入重型可视化库；实时反馈说话状态，且避免重复申请麦克风权限 |
| 重连 | MVP 不做，「停止再开始」替代 | 闭环优先 |

---

## 6. 风险与对策

| 风险 | 影响 | 对策 |
|------|------|------|
| 服务端未就绪，前端无法联调 | 阻塞真实 WebRTC 媒体层与完整端到端联调 | 先写 **mock WS 服务端**（按 05-A 回放 asr/translation 帧），前端独立推进并优先完成 M2 字幕面板；M3 WebRTC 拆为 mock 可验收的信令层与依赖真实服务端的媒体层 |
| `getDisplayMedia` 浏览器/HTTPS 限制 | 标签页音频取不到 | MVP 默认麦克风优先；标签页作为可选项，localhost 视为安全上下文 |
| WebRTC 下行音频自动播放被拦 | 配音不出声 | 「开始」按钮即用户手势，借此解锁 `<audio>` autoplay |
| Web Audio API 生命周期泄漏 | 停止后仍占用动画循环或音频分析节点，影响性能/麦克风状态判断 | `useAudioVisualizer` 只接收已存在的会话 `MediaStream`；停止时 `cancelAnimationFrame`、断开 `MediaStreamAudioSourceNode/AnalyserNode`，必要时 `close/suspend AudioContext`，不额外调用 `getUserMedia` |
| translation final 乱序 / 迟到 | 锁定行被覆盖 | 锁定行（`zhFinal`/`enFinal`）只接受 revise，不被 partial 回退 |
| `utteranceId` 字段位置不一致 | 字幕错位 | 类型层统一从 `metadata.utteranceId`(asr) 与顶层 `utteranceId`(translation) 取，封装 getter |

---

## 7. 验收（MVP Demo 清单）

1. 点「开始」→ 顶栏显示「已连接」。
2. 播放/朗读英文 → 英文字幕灰字出现 → final 变黑加 🔒。
3. 中文先出灰字（TMT 快翻）→ DeepSeek 重译变黑；不同则「✦已纠正」高亮 0.6s。
3.5. 控制栏输入音量波浪条随说话音量变化；停止后可视化静止/隐藏且不再占用分析循环。
4. 配音开关开启时，能听到中文配音下行播放。
5. 运行时把策略切到「纯TMT」→ 后续句不再出现重译纠正。
6. 点「停止」→ 连接关闭，已显示字幕保留。
7. 制造翻译缺失 → 前端不崩，仅显示英文。

全部通过即 MVP 达成；其后进入 todolist 阶段 6（重连）/8（导出总结）等加分项。

# 02 · 系统架构与业务逻辑

## 1. 项目结构

SimulSpeak 是单一 Go 项目，内部按「同传业务主服务」和「PBX 媒体节点」拆分边界。代码在一个仓库内，但运行时可以启动多个 `pbx-node` 横向承载 WebRTC 音频、ASR、TMT 快翻与 TTS；`api-server` 负责同传会话、节点选择、信令转发、字幕转发、DeepSeek Flash 纠错、前端 API 和 PBX 节点调度。

```
SimulSpeak1/
├── cmd/
│   ├── api-server/              # 同传业务主服务：会话、字幕、DeepSeek Flash 纠错、前端 API
│   ├── pbx-node/                # PBX 媒体节点：webrtc / asr / tmt / tts / 控制通道
│   ├── worker/                  # 可选：总结、导出、异步任务
│   └── seed-demo/               # Demo 初始化
│
├── internal/
│   ├── app/                     # 各 cmd 的启动装配
│   │   ├── api/
│   │   └── pbx/
│   │
│   ├── api-server/              # api-server 独享：前端 HTTP/WS、会话编排、PBX bridge
│   │   ├── httpapi/
│   │   └── router/
│   │
│   ├── pbx/                     # PBX 独享：媒体链路、控制通道、话单/录音等
│   │   ├── webrtc/              # Pion/WebRTC manager
│   │   ├── httpapi/             # pbx-node 独享 HTTP/WS API
│   │   ├── control/             # /pbx/ws 控制通道
│   │   ├── media/
│   │   ├── cdr/
│   │   ├── recording/
│   │   └── sdk/
│   │
│   ├── ai/                      # 公共 AI provider：pbx 用 vad/asr/tmt/tts，api-server 用 llm
│   │   ├── vad/
│   │   ├── asr/
│   │   ├── tmt/
│   │   ├── tts/
│   │   └── llm/
│   │
│   ├── protocol/pbx/            # api-server ↔ pbx-node 内部控制协议
│   ├── config/                  # 公共配置
│   ├── logging/                 # 公共日志
│   ├── model/                   # 公共模型
│   ├── eventbus/                # 公共事件总线
│   ├── etcdutil/                # etcd helper
│   ├── errors/                  # 公共错误类型
│   ├── interpreter/             # 同传业务核心抽象
│   ├── subtitle/                # 字幕流、SRT/Markdown 导出
│   ├── summary/                 # 会后总结
│   ├── session/                 # 同传会话，不要和 pbx/session 混
│   ├── store/                   # sqlite
│   └── transport/               # HTTP/WebSocket transport helper
│
├── pkg/
│   └── client/                  # 如果以后给第三方调用 SimulSpeak API，再放这里
└── third_party/
    ├── onnxruntime-linux-x64-1.26.0/ # Silero VAD 使用的 ONNX Runtime Linux x64 运行库
    └── silero-vad/                   # silero_vad.onnx 模型
```

## 2. 四层架构

```
┌─────────────────────────────── 浏览器（前端 SPA）──────────────────────────────┐
│ 音频源(标签页/系统/麦克风) · 纠错策略(DeepSeek Flash/仅快翻) · 双语字幕面板 · 配音开关 │
└───────────────┬──────────────────────────────────────────────▲─────────────────┘
        WebSocket 控制 / 字幕流 / 前端 API                    WebRTC 音频上行/配音下行
                │                                                │
┌───────────────▼───────────────┐             ┌──────────────────▼──────────────────┐
│ api-server                     │  信令转发    │ pbx-node-1 / pbx-node-N              │
│ - 同传会话与用户状态             │────────────►│ - WebRTC PeerConnection             │
│ - PBX 节点选择与会话映射          │◄────────────│ - Opus→PCM16/16k、VAD、ASR(16k_en)  │
│ - asr_result / tmt字幕转发        │ asr+tmt结果 │ - TMT 快翻 translation_result        │
│ - DeepSeek Flash final纠错        │ tts_command │ - TTS 合成与 PCMU 下行               │
│ - subtitle revision/summary/export│             │ - PBX 节点注册、负载与健康检查         │
└────────────────────────────────┘             └─────────────────────────────────────┘
        外部：腾讯 ASR(wss) · 腾讯 TMT · DeepSeek Flash · 腾讯 TTS · etcd
```

### 2.1 业务流程与职责边界

1. 前端只与 `api-server` 建立一条业务 WebSocket，所有 `client_hello`、`webrtc_offer`、`ice`、`set_strategy`、字幕事件都走这条连接。
2. `api-server` 收到 SDP/ICE 后选择一个可用 `pbx-node`，把 WebRTC 信令转发给该节点；浏览器与该 `pbx-node` 直接建立 WebRTC 媒体链路。
3. `pbx-node` 作为媒体处理核心，接收浏览器音频流，完成 Opus 解码、VAD、英文 ASR、ASR final 后 TMT 快翻，并把 `asr_result` 与 `translation_result(engine=tmt,isFinal=true)` 通过 PBX 控制通道回传给 `api-server`。
4. `api-server` 作为业务编排核心，收到 PBX 结果后原样转发英文 ASR 与 TMT 中文草稿给前端；当 ASR final 到达时，调用 DeepSeek Flash 做上下文/术语表纠错，生成 `translation_result(engine=deepseek-flash,isFinal=true,revised=...)` 覆盖同一 `utteranceId` 的 LLM 结果。
5. 配音由 `api-server` 主动触发：final/revised 中文产生后，`api-server` 向承载该会话的 `pbx-node` 发送 `tts_command`，`pbx-node` 合成中文 TTS 并通过当前 WebRTC 下行轨播放给浏览器。
6. PBX 不持有同传业务状态；`api-server` 不直接处理媒体帧。PBX 负责媒体与实时语音能力，主服务负责节点调度、业务逻辑、字幕状态和前端交互。

## 3. 媒体面设计（WebRTC）

### 3.1 信令握手
1. 浏览器经 WebSocket 连接 `api-server`，收到 `connected`。
2. 浏览器发 `client_hello`（含翻译策略、配音开关、音频参数），`api-server` 创建同传会话并选择一个可用 `pbx-node`。
3. 浏览器 `createOffer` 后发 `webrtc_offer` 给 `api-server`，`api-server` 转发给目标 `pbx-node`。
4. `pbx-node` 创建 `PeerConnection`、生成 answer，经 `api-server` 回 `webrtc_answer`。
5. ICE candidate 仍经 `api-server` 转发；媒体真实链路在浏览器和选定 `pbx-node` 之间建立。

### 3.2 上行音频管线
- 服务端 `OnTrack` 接收远端音频轨，后台 goroutine 持续 `ReadRTP` 取 Opus 包。
- Opus 包经解码器（48k 单声道）解为 PCM，再重采样到 **PCM16LE / 16kHz / 单声道**（腾讯 ASR 标准）。
- PCM 帧先过 VAD 门控（见第 4 节），通过的语音帧写入 ASR 流。

### 3.3 下行配音管线
- 为 `PeerConnection` 添加一条 **PCMU/8000** 本地音频轨。
- 中文配音文本经 TTS 合成 → WAV/PCM → 重采样 + G.711 μ-law 编码为 PCMU、切为 20ms 帧 → 入播放队列。
- 单独 goroutine 按帧时长节拍 `WriteSample` 实时播出，实现自然配音。

## 4. VAD 与切句设计

- VAD provider 支持两种模式：`simple` 使用 PCM16 RMS 能量门控；`silero` 使用 `third_party/silero-vad/silero_vad.onnx` + ONNX Runtime 推理。
- Silero 接入配置由 `SIMULSPEAK_VAD_PROVIDER=silero`、`SIMULSPEAK_VAD_MODEL_PATH`、`SIMULSPEAK_VAD_RUNTIME_LIBRARY_PATH` 控制；仓库已包含默认 Linux x64 运行库和模型，缺失或替换版本时可执行 `make install-third-party`。
- WebRTC 常见 Opus 包解码后是 20ms PCM，也就是 16k 下 320 samples；Silero 16k 推理窗口是 512 samples。PBX 会按会话缓存真实 PCM samples，累计到 512 后再推理，未凑满窗口的帧只进入 pre-roll，不会被当成静音丢弃。
- VAD 维护**预滚 buffer**，语音真正开始时把预滚帧一并放行（避免吞首字）。
- 连续语音帧达到 `startFrames` 判定语音开始；连续静音帧达到 `endSilenceFrames` 判定语音结束。
- **语音结束即切句**：关闭当前 ASR 流，触发该句 final；下一段语音懒加载新流。一次「语音开始→结束」对应一句话（一个 utteranceID）。
- 参数（阈值、起始帧、结束静音帧、预滚帧）可由会话配置覆盖。

## 5. 流式 ASR 设计

- 采用腾讯云实时 ASR，引擎 `16k_en`（英文）。每句话懒加载建立一条 WebSocket 长连接（`OpenStream`）。
- 持续写入 PCM16/16k 帧；后台消费 `Results()`，按 `slice_type` 产出 **partial / final**。
- partial 用于灰色实时字幕，final 为该句定稿。会话内做文本去重，避免重放重复回调。
- 句末（VAD 结束）关闭该句流；流错误时禁用并按需重连，不阻塞 RTP 读取。

## 6. 实时快翻与纠错编排

翻译拆成两段：**TMT 快翻在 `pbx-node` 侧执行**，用于 ASR final 后的快速中文草稿；**DeepSeek Flash 纠错在 `api-server` 侧执行**，用于 final/revised 黑字。前端看到的所有字幕仍统一由 `api-server` 的业务 WebSocket 推送。

| 策略 | PBX 实时结果 | 主服务 final 处理 |
|------|--------------|----------------|
| 混合（默认）| ASR final 后转发 `engine=tmt` 中文草稿 | DeepSeek Flash 纠错并锁定 |
| 仅快翻 | ASR final 后转发 `engine=tmt` 并锁定 | 不调用 DeepSeek，锁定 TMT |
| 仅纠错 | 可转发 TMT 作为占位/兜底 | DeepSeek Flash final 覆盖同句 |

- PBX TMT 路径只由 ASR final 触发，`api-server` 收到后直接转发给前端，作为该句中文草稿。
- DeepSeek Flash 路径要求**高质量+一致性**，`api-server` 注入「最近已锁定中文 + 会话术语表」，出黑色锁定中文，并据与 PBX TMT 快翻差异给出 `revised`。
- 策略只控制主服务是否执行/如何执行 DeepSeek Flash 纠错，不表示前端直接选择 PBX 内部实现。

## 7. commit/revise 与术语表

### 7.1 稳定 utteranceID（关键）
`pbx-node` 的 VAD 每句关流/重开流，因此一句话对应一个 ASR 流生命周期。PBX 媒体 `Session` 维护**按句自增的稳定 id**：开流时生成 `utteranceID = callID + "-utt-" + seq`，该句所有 ASR partial/final 与 TMT 快翻共用同一 id；关流后下一句自增。`api-server` 的同传会话使用同一 id 生成 DeepSeek Flash 纠错结果，前端据此**就地更新同一行**。

### 7.2 状态机（每行字幕）
```
            partial到达                 final到达
  (空) ───────────────► pending(灰) ───────────────► locked(黑)
                          ▲   │ 新partial(节流)         │ 纠错≠快翻
                          └───┘ 就地刷新                ▼
                                                   revised(黑+高亮一次)
```
锁定/纠错为终态，不再变动；pending 允许被后续 partial 覆盖。

### 7.3 术语表与上下文
- **术语表 glossary**：会话级 `map[string]string`（源词→译法）。final 纠错时注入提示词，模型输出新术语并回写，保证后续句子术语一致。
- **上下文 context**：保留最近 N 句（默认 2–3）已锁定中文，纠错时作为参考（不改写既有字幕），提升连贯性，也是「纠错」的判据来源。

## 8. 中文配音（TTS 下行）设计

- 会话开启配音时，`api-server` 在 final 中文译文产生后向承载该会话的 `pbx-node` 发送 `tts_command`。`pbx-node` 执行中文 TTS 合成（中文音色、`zh-CN`），音频经 PCMU 下行轨实时播出（见 3.3）。
- 配音与字幕解耦：配音失败仅影响声音，不影响字幕。可降级为「仅锁定句配音」。

## 9. 并发、生命周期、容错

### 9.1 并发与生命周期
- TMT 快翻运行在 `pbx-node`，与 ASR 同属媒体节点实时能力；失败时不阻塞 ASR。
- DeepSeek Flash 是网络调用，运行在 `api-server` 的同传会话中，**不阻塞** `pbx-node` 的 ASR/TMT 消费循环。每个同传 `Session` 启动一个**纠错编排 goroutine（pump）**，经缓冲 channel 接收 final 纠错任务，**串行**处理以保证顺序与术语表一致。
- PBX TMT final 结果必须入库并按竞态规则转发；DeepSeek Flash final 任务必须保留。
- 生命周期绑定 WebSocket 连接 ctx（连接关闭即取消 pump）；翻译外呼用 `context.WithTimeout`（默认 8s）。

### 9.2 容错降级矩阵
| 故障 | 影响 | 降级 |
|------|------|------|
| PBX TMT 失败/超时 | TMT 中文草稿缺失 | 继续转发 ASR，直接等 DeepSeek final 或仅显示英文 |
| DeepSeek Flash 失败/超时 | final 纠错缺失 | **锁定最新 PBX TMT 快翻**，revised=false |
| TMT+DeepSeek 都失败 | 中文缺失 | 仅显示英文 final，记日志 |
| TTS 失败 | 无配音 | 仅字幕 |
| 翻译队列积压 | 延迟升高 | 丢弃 partial，仅保 final |
| ASR 流错误 | 识别中断 | 禁用并按需重连 |

原则：**字幕主链路（ASR→显示）永不被翻译/配音故障中断。**

## 10. 横向扩展（集群面）

### 10.1 扩展单元与无状态化
- `pbx-node` 是主要横向扩展单元，可独立启动多个，分别承载 WebRTC、VAD、ASR、TTS 与媒体播放。
- `api-server` 是同传业务入口，可先单实例参赛；当前会话存储使用 SQLite，后续扩展时通过入口层做 WebSocket 粘性或再引入专用共享状态存储。
- **会话粘性**：一次同传会话会绑定一个 `api-server` 会话和一个 `pbx-node` 媒体会话；媒体侧不做跨节点热切换，断线则重连重建。
- **共享状态下沉**：PBX 节点列表、负载、路由配置存 etcd；翻译密钥走服务端环境变量/配置中心，不下发浏览器。

### 10.2 流量分发
- `pbx-node` 启动后向 `internal/registry`(etcd) 注册能力与负载（current/max calls）。
- `api-server` 通过 `pkg/client` 或 `internal/registry` 查询选择节点，按 `round_robin` / `least_connections` / `zone_affinity` 为新会话分配 `pbx-node`。
- 浏览器的业务 WebSocket 保持连接到 `api-server`；WebRTC 媒体链路由 `api-server` 转发信令后落到选定 `pbx-node`。

### 10.3 扩展性注意点
- **外部 API 限频**：ASR/TMT/TTS 限流在 `pbx-node`，DeepSeek Flash 限流在 `api-server`；分别维护连接池与限流。
- **可观测**：`api-server` 统计字幕延迟、修正次数、DeepSeek 失败率；`pbx-node` 统计 WebRTC、ASR、TMT、TTS 与媒体播放指标。
- **Demo 取舍**：现场可用 1 个 `api-server` + 1 个 `pbx-node`；多实例作为能力点说明 + 最小验证（启动 2 个 `pbx-node` 并观察入口分流）。

## 11. 端到端数据流（时序）

```
用户说: "We use a transformer architecture."
  t0  前端 WebSocket 连接 api-server，SDP/ICE 经 api-server 转发给选定 pbx-node
  t1  浏览器与 pbx-node 建立 WebRTC，音频上行 → Opus 解码 PCM16/16k
  t2  pbx-node VAD 语音开始 → 开腾讯 ASR 流，分配 utteranceID=U
  t3  pbx-node ASR partial "we use a"
        → asr_result(U,en,partial) → api-server → 前端英文行(灰)
  t4  pbx-node ASR partial 增长
        → api-server 原样转发，同一 utteranceId 就地刷新英文
  t5  pbx-node VAD 语音结束 → ASR final "We use a transformer architecture."
        → asr_result(U,en,final) → api-server → 前端英文行(黑锁定)
        → pbx-node TMT 快翻 "我们使用变压器架构。"
        → translation_result(U,zh,final,engine=tmt) → api-server → 前端 TMT 中文草稿
        → api-server 调 DeepSeek Flash(上下文+术语)
        → "我们采用一种 Transformer 架构。"
        → translation_result(U,zh,final,engine=deepseek-flash,revised=true)
        → 前端中文行(黑锁定)+高亮；术语表/上下文在 api-server 更新
  t6  api-server 发送 tts_command(final中文) → pbx-node TTS → PCMU 下行 → 浏览器播放中文配音
```

## 12. 延迟预算（NFR 对齐）

| 阶段 | 目标耗时 |
|------|----------|
| WebRTC 上行 + Opus 解码 | < 100ms |
| ASR 首个 partial | 0.5–1s |
| PBX TMT 快翻草稿 | ASR final 后 +0.2–0.5s |
| DeepSeek Flash 纠错（黑字锁定/纠错）| +0.5–1.5s（累计 ≤ 3s）|
| 中文配音起播 | final 后 ≤ 1s |

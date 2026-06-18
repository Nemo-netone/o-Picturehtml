# 04 · 后端架构与业务逻辑

## 1. 模块划分

| 模块 | 路径 | 职责 |
|------|------|------|
| PBX 媒体面 | `internal/pbx/webrtc` | PeerConnection 管理；Opus→PCM16/16k 解码与重采样；上行音频管线；PCMU 配音下行轨；维护 PBX 媒体会话和稳定 utteranceID |
| PBX HTTP/WS 接入 | `internal/pbx/httpapi` | pbx-node 独享 HTTP 路由；挂载 `/pbx/ws`、`/pbx/health` |
| PBX 控制通道 | `internal/pbx/control` + `internal/protocol/pbx` | PBX 内部 WebSocket 控制协议；SDP/ICE 交换；asr_result / translation_result(tmt) / tts_result |
| PBX AI | `internal/ai/{vad,asr,tmt,tts}` | VAD、腾讯流式 ASR（引擎 `16k_en`）、腾讯 TMT 快翻、腾讯 TTS |
| 同传编排 | `internal/api-server/httpapi` | 消费 ASR + PBX TMT 结果、维护字幕状态、调用 DeepSeek Flash 纠错、触发 TTS、SQLite 入库 |
| 翻译/纠错 | `internal/ai/llm` | DeepSeek Flash / OpenAI-compatible provider、术语表注入、final/revision 纠错 |
| 字幕/总结 | `internal/subtitle` / `internal/summary` | 字幕流、SRT/Markdown 导出、会后总结 |
| API 传输 | `internal/api-server/httpapi` | 前端 API、业务 WebSocket、translation_result 回推 |
| 集群面 | `internal/registry` / `internal/api-server/router` | PBX 节点注册、api-server 会话路由、健康检查 |

## 2. DeepSeek Flash 纠错抽象

```go
// internal/ai/llm
type Provider interface {
    Name() string
    Translate(ctx context.Context, req Request) (Result, error)
}
type Request struct {
    CallID, UtteranceID    string
    Text                   string            // 源文本(英文)
    SourceLang, TargetLang string            // en / zh
    Quality                bool              // true=final纠错；保留字段便于后续扩展
    Context                []string          // 最近已锁定中文
    Glossary               map[string]string // 会话术语表
    FastTranslation        string            // PBX TMT 快翻结果，供纠错对照/兜底
}
type Result struct {
    Text  string
    Terms map[string]string // 纠错产生的术语，回写会话术语表
}
```
内置 provider：`deepseek-flash`（OpenAI 兼容）和 `mock`（测试/无 key）。TMT 快翻不在 `api-server` 内调用，由 `pbx-node` 负责。详见 [05-interfaces](./05-interfaces.md)。

## 3. 纠错策略编排

```go
type Strategy string
const (
    StrategyHybrid   Strategy = "hybrid"   // 默认：PBX TMT实时灰字 + DeepSeek Flash final纠错
    StrategyTMT      Strategy = "tmt"      // 仅使用PBX TMT，final时锁定最新快翻
    StrategyDeepSeek Strategy = "deepseek" // PBX TMT可作占位/兜底，final由DeepSeek Flash覆盖
)
```

| 策略 | PBX 实时字幕 | api-server final 行为 |
|------|--------------|----------------------|
| hybrid | 转发 `engine=tmt` 灰字 | 调 DeepSeek Flash，成功则覆盖为 revised/locked |
| tmt | 转发 `engine=tmt` 灰字 | 不调 DeepSeek，锁定最新 TMT |
| deepseek | 可转发 `engine=tmt` 占位 | 调 DeepSeek Flash，TMT 仅作兜底 |

## 4. 媒体面与 AI 接线（Session 内）

一次同传会话对应一个业务 `Session` 和一个 PBX 媒体 `Session`，串起整条管线：

1. **上行音频**：浏览器 → `pbx-node`，`OnTrack` → 读 RTP Opus → 解码重采样为 PCM16/16k。
2. **VAD 切句**：`pbx-node` PCM 帧过能量门控；语音开始懒加载 ASR 流并分配稳定 utteranceID，语音结束关流定稿。
3. **ASR + TMT**：`pbx-node` 写 PCM 到腾讯流式 ASR，消费 partial/final；ASR partial 只回调英文 `asr_result`，ASR final 触发一次 TMT 快翻，并把 `translation_result(engine=tmt,isFinal=true)` 回传给 `api-server`。
4. **字幕转发 + 纠错**：`api-server` 先把 PBX ASR/TMT 结果转发给前端；ASR final 到达后，`internal/interpreter` 按策略调用 DeepSeek Flash 产出 `translation_result(engine=deepseek-flash,isFinal=true,revised=...)`。
5. **配音**：final/revised 中文（配音开启时）由 `api-server` 发送 `tts_command` 给 `pbx-node`，PBX TTS → PCMU 下行轨播放。

## 5. 核心业务逻辑

### 5.1 稳定 utteranceID
- PBX 媒体 `Session` 维护 `utteranceSeq int` 与 `curUtteranceID string`（`asrMu` 保护）。
- 开 ASR 流成功时 `seq++`，`curUtteranceID = callID+"-utt-"+seq`，并重置该句去重集合。
- ASR 与 PBX TMT 结果回调统一使用 `curUtteranceID`，英文/中文共享，前端就地更新。

### 5.2 字幕转发与纠错 pump
- 同传业务 `Session` 持有：`translator *translate.Client`、`strategy`、`correctionJobs chan correctionJob`，以及 `transMu` 保护的 `glossary map[string]string`、`context []string`、`lastTMTZH map[string]string`。
- 会话建立时（translator 非 nil）启动 `runCorrectionPump(ctx)`，ctx=连接级。
- PBX TMT 结果到达后：保存 `lastTMTZH[utteranceID]` 并立即转发给前端，显示为灰色待定中文。
- ASR final 到达后按策略入队纠错任务：`hybrid/deepseek` 必入队；`tmt` 直接锁定 `lastTMTZH`，不调用 DeepSeek Flash。
- pump 串行处理 job：
  1. 读取 ASR final、`lastTMTZH[utteranceID]`、`context`、`glossary`。
  2. 调 `translator.Translate`（DeepSeek Flash），传入 `FastTranslation` 供模型纠错与失败兜底。
  3. 失败则锁定 `lastTMTZH`；若无 TMT 则仅锁定英文并留空中文。
  4. `Revised = zh != lastTMTZH[utt]`。
  5. 回调 `onTranslation` 推 `engine=deepseek-flash` final/revised。
  6. final：`glossary` 合并 `Result.Terms`；`context` 追加该中文（capped）；清理 `lastTMTZH`。

### 5.3 中文配音触发
- final/revised 的 `translation_result` 后，若会话开启配音：`api-server` 构造中文 TTS 请求（中文音色、`zh-CN`）发给承载会话的 `pbx-node` → 合成 → PCMU 下行播放。配音失败不影响字幕。

## 6. 并发、生命周期、顺序保证
- 纠错 pump 每会话单 goroutine，**串行**消费 → 顺序与术语表一致；不阻塞 PBX ASR/TMT 消费循环。
- PBX TMT 灰字转发对主链路**非阻塞**；DeepSeek Flash final 纠错任务必须保留。
- 外呼超时：`context.WithTimeout(connCtx, 8s)`。
- 生命周期：pump、外呼挂连接级 ctx，连接关闭即取消。

## 7. 错误处理矩阵

| 故障 | 处理 |
|------|------|
| PBX TMT 失败/超时 | 不影响 ASR；前端仅显示英文，等待 DeepSeek Flash final |
| DeepSeek Flash 失败/超时 | 用最新 PBX TMT 快翻结果作为锁定值，revised=false |
| TMT+DeepSeek 都失败 | 仅显示英文 final，中文留空并记日志 |
| TTS 失败 | 仅字幕 |
| 翻译队列积压 | 丢 partial 保 final |
| ASR 流错误 | 禁用并按需重连 |

原则：**字幕主链路（ASR→显示）永不被翻译/配音故障中断。**

## 8. 配置（环境变量，服务端，不下发浏览器）

| 变量 | 默认 | 说明 |
|------|------|------|
| `SIMULSPEAK_ASR_PROVIDER` / `SIMULSPEAK_TMT_PROVIDER` / `SIMULSPEAK_TTS_PROVIDER` | mock | ASR/TMT/TTS provider，如 `tencent-asr`、`tencent-tmt`、`tencent-tts` |
| `SIMULSPEAK_TRANSLATE_PROVIDER` | - | TMT provider 兼容别名；未设置 `SIMULSPEAK_TMT_PROVIDER` 时生效 |
| `SIMULSPEAK_TENCENT_ASR_ENDPOINT` | `wss://asr.cloud.tencent.com/asr/v2` | 腾讯 ASR base URL |
| `SIMULSPEAK_TENCENT_ASR_APPID` | - | 腾讯 ASR AppID |
| `SIMULSPEAK_TENCENT_ASR_SECRETID` / `SIMULSPEAK_TENCENT_ASR_SECRETKEY` | - | 腾讯 ASR 密钥 |
| `SIMULSPEAK_TENCENT_TMT_ENDPOINT` | `https://tmt.tencentcloudapi.com` | 腾讯 TMT endpoint |
| `SIMULSPEAK_TENCENT_TMT_REGION` | `ap-guangzhou` | 腾讯 TMT 地域 |
| `SIMULSPEAK_TENCENT_TMT_PROJECT_ID` | `0` | 腾讯 TMT ProjectId |
| `SIMULSPEAK_TENCENT_TMT_SECRETID` / `SIMULSPEAK_TENCENT_TMT_SECRETKEY` | - | 腾讯 TMT 密钥；不需要 AppID |
| `SIMULSPEAK_TENCENT_TTS_ENDPOINT` | `https://tts.tencentcloudapi.com` | 腾讯 TTS base URL |
| `SIMULSPEAK_TENCENT_TTS_APPID` | - | 腾讯 TTS AppID |
| `SIMULSPEAK_TENCENT_TTS_SECRETID` / `SIMULSPEAK_TENCENT_TTS_SECRETKEY` | - | 腾讯 TTS 密钥 |
| `SIMULSPEAK_LLM_PROVIDER` | `openai-compatible` | AI final 纠错 provider，兼容 OpenAI Chat Completions |
| `SIMULSPEAK_LLM_ENDPOINT` / `SIMULSPEAK_LLM_MODEL` | - | LLM Chat Completions endpoint 与模型 |
| `SIMULSPEAK_LLM_API_KEY` | - | LLM API key；只在服务端读取 |
| `SIMULSPEAK_LLM_LIVE_TEST` | `0` | 设为 `1` 时允许 live test 外呼 LLM |
| `DEEPSEEK_API_KEY` | - | `api-server` DeepSeek Flash key（sk-...） |
| `SIMULSPEAK_VAD_PROVIDER` | `silero` | VAD provider：`simple` 为 RMS 能量法，`silero` 为 ONNX 推理 |
| `SIMULSPEAK_VAD_MODEL_PATH` | `./third_party/silero-vad/silero_vad.onnx` | Silero ONNX 模型路径；仓库已内置默认模型 |
| `SIMULSPEAK_VAD_RUNTIME_LIBRARY_PATH` / `SIMULSPEAK_ONNX_RUNTIME_LIBRARY_PATH` | `./third_party/onnxruntime-linux-x64-1.26.0/lib/libonnxruntime.so.1.26.0` | ONNX Runtime 动态库路径；两个变量名等价，任填其一 |
| `SIMULSPEAK_VAD_SAMPLE_RATE` | `16000` | Silero / ASR 输入采样率；WebRTC Opus 解码后会重采样为 PCM16/16k |
| `DEEPSEEK_ENDPOINT` | https://api.deepseek.com/v1/chat/completions | |
| `DEEPSEEK_MODEL` | deepseek-flash | |
| `TRANSLATE_STRATEGY` | hybrid | 默认纠错策略，可被会话覆盖 |
| `TRANSLATE_TIMEOUT_MS` | 8000 | DeepSeek Flash 外呼超时 |

DeepSeek Flash translator 在 `api-server` 启动时按 env 构造一次，各同传 Session 共享 client、各自持有会话级术语表/上下文。无 key 时 translator=nil，主服务仅转发 PBX ASR/TMT，字幕主链路不崩。

## 9. 测试策略
- `translate` 包单测：mock DeepSeek Flash provider、JSON 纠错解析、术语合并、降级路径。
- 集成：mock PBX ASR + mock PBX TMT + mock DeepSeek Flash 走通 `asr_result`/`translation_result` 帧。
- 门槛：`go test ./... -count=1`、`-race`、`go vet ./...`、`make build` 全过。

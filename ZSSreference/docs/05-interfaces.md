# 05 · 接口定义（全部）

本章定义 SimulSpeak 的**全部接口契约**，开工即按此实现。分五层：
A. 前端 ↔ 主服务 WebSocket 控制协议  B. 内部 Go 接口与数据模型  C. 外部服务 API（ASR / TMT / DeepSeek Flash / TTS）  D. 环境变量  E. HTTP 接口与错误约定。

---

## A. WebSocket 控制协议（前端 ↔ api-server）

入口：`ws://{host}/ws?token={tenantId}:{clientId}`。文本帧 JSON。前端只连接 `api-server`，不直接连接 PBX 控制通道；SDP/ICE 由 `api-server` 转发给选定 `pbx-node`，终端用户音频经 WebRTC 直接上行到该 `pbx-node`，字幕/事件再由 `api-server` 统一回推给前端。

### A.0 消息总表

| 方向 | type | 作用 |
|------|------|------|
| 服务端→前端 | `connected` | 连接建立，下发 connectionId |
| 前端→服务端 | `client_hello` | 初始化：回包模式、纠错策略、配音开关 |
| 服务端→前端 | `client_hello_ack` | 确认初始化 |
| 前端→服务端 | `webrtc_offer` | 上行 SDP offer |
| 服务端→前端 | `webrtc_answer` | 下行 SDP answer |
| 双向 | `ice` | 交换 ICE candidate |
| 服务端→前端 | `asr_result` | 英文识别结果（partial/final）|
| 服务端→前端 | `translation_result` | 中文翻译结果：PBX TMT 灰字或主服务 DeepSeek Flash final/revised |
| 前端→服务端 | `set_strategy` | 运行时切换翻译策略 |
| 服务端→前端 | `set_strategy_ack` | 策略切换确认 |
| 前端→服务端 | `tts_command` | （可选调试）显式请求 TTS 配音；正式链路由主服务主动触发 |
| 服务端→前端 | `tts_result` | TTS 进入下行队列确认 |
| 双向 | `ping` / `pong` | 应用层心跳 |
| 服务端→前端 | `error` | 错误回包 |

公共字段：`type`、`requestId`、`connectionId`、`callId`、`userId`、`metadata`。

### A.1 connected（服务端→前端）
```json
{ "type": "connected", "connectionId": "conn-1", "tenantId": "tenant-a", "clientId": "simulspeak-web" }
```

### A.2 client_hello（前端→api-server）
声明纠错策略与配音开关。ASR/TMT/TTS 密钥和 provider 配置由服务端环境变量或配置中心提供，不下发浏览器。
```json
{
  "type": "client_hello", "requestId": "hello-1",
  "tenantId": "tenant-a", "clientId": "simulspeak-web", "responseMode": "compact",
  "metadata": { "translateStrategy": "hybrid", "dubbing": "1" }
}
```
要点：`metadata.translateStrategy ∈ {hybrid,tmt,deepseek}`；`metadata.dubbing ∈ {0,1}`。PBX 侧固定使用英文 ASR `engine_model_type=16k_en`，ASR partial 只回传英文，ASR final 触发一次 TMT 快翻。回包 `client_hello_ack`：`{ "type":"client_hello_ack", "requestId":"hello-1", "connectionId":"conn-1", "responseMode":"compact" }`。

### A.3 webrtc_offer / webrtc_answer / ice
前端仍只发给 `api-server`；`api-server` 选择 `pbx-node` 后转发 offer/ICE，并把 PBX answer/ICE 回给前端。
```json
{ "type": "webrtc_offer", "requestId": "offer-1", "callId": "call-1", "userId": "user-1", "sdp": "v=0\r\n..." }
{ "type": "webrtc_answer", "requestId": "offer-1", "callId": "call-1", "userId": "user-1", "sdp": "v=0\r\n..." }
{ "type": "ice", "callId": "call-1", "userId": "user-1", "candidate": "{\"candidate\":\"candidate:...\",\"sdpMid\":\"0\",\"sdpMLineIndex\":0}" }
```

### A.4 asr_result（服务端→前端，英文原文）
英文原文展示依赖此帧。同句 partial/final 共享 `metadata.utteranceId`（稳定 id）。
```json
{
  "type": "asr_result", "callId": "call-1", "userId": "user-1",
  "text": "We use a transformer architecture.",
  "isFinal": true, "confidence": 0.95, "language": "en",
  "metadata": { "utteranceId": "call-1-utt-7" }
}
```

### A.5 translation_result（服务端→前端，中文译文）
`translation_result` 统一从 `api-server` 发给前端，但有两类来源：

- `engine=tmt`：来自 PBX TMT 快翻，由 ASR final 触发，通常 `isFinal=true`，前端可作为 TMT 译文单独显示。
- `engine=deepseek-flash`：来自主服务 DeepSeek Flash 纠错，通常 `isFinal=true`，前端显示为黑色锁定字幕；`revised=true` 时高亮。

PBX TMT 示例：
```json
{
  "type": "translation_result", "callId": "call-1", "userId": "user-1",
  "utteranceId": "call-1-utt-7",
  "sourceText": "We use a transformer architecture.",
  "text": "我们使用变压器架构。",
  "isFinal": true, "engine": "tmt", "revised": false, "language": "zh"
}
```

DeepSeek Flash 纠错示例：
```json
{
  "type": "translation_result", "callId": "call-1", "userId": "user-1",
  "utteranceId": "call-1-utt-7",
  "sourceText": "We use a transformer architecture.",
  "text": "我们采用一种 Transformer 架构。",
  "isFinal": true, "engine": "deepseek-flash", "revised": true, "language": "zh"
}
```
| 字段 | 类型 | 说明 |
|------|------|------|
| utteranceId | string | 与对应 asr_result 同值，前端就地更新同一行 |
| sourceText | string | 对应英文原文 |
| text | string | 中文译文 |
| isFinal | bool | true=该 engine 对当前 utterance 的最终结果；TMT 与 DeepSeek Flash 可分别显示 |
| engine | string | `"tmt"` / `"deepseek-flash"` |
| revised | bool | true=DeepSeek Flash 纠错与 PBX TMT 快翻不同，前端高亮「已纠正」 |

### A.6 set_strategy（前端→服务端）
```json
{ "type": "set_strategy", "requestId": "s-1", "metadata": { "translateStrategy": "tmt" } }
```
回包：`{ "type": "set_strategy_ack", "requestId": "s-1", "metadata": { "translateStrategy": "tmt" } }`。

### A.7 tts_command / tts_result
配音默认由 `api-server` 在 final/revised `translation_result` 后主动发往承载会话的 `pbx-node`（dubbing=1）。前端显式 `tts_command` 只作为调试能力，`api-server` 收到后仍会转发给 PBX：
```json
{ "type": "tts_command", "requestId": "tts-1", "callId": "call-1", "userId": "user-1", "text": "我们采用一种 Transformer 架构。", "voice": "101001", "language": "zh-CN" }
{ "type": "tts_result", "requestId": "tts-1", "callId": "call-1", "format": "pcmu", "sampleRate": 8000, "isLast": true, "metadata": { "audioTransport": "webrtc" } }
```
TTS 音频本体经 WebRTC 下行播放，不经 WebSocket 传输。

### A.8 ping / pong / error
```json
{ "type": "ping", "requestId": "p-1" }
{ "type": "pong", "requestId": "p-1", "connectionId": "conn-1" }
{ "type": "error", "requestId": "x", "callId": "call-1", "error": "..." }
```

### A.9 典型流程
```
前端 connected → client_hello → api-server 选择 pbx-node
 → 前端 webrtc_offer/ice → api-server 转发 PBX → webrtc_answer/ice 回前端
 → 浏览器与 pbx-node 建 WebRTC 媒体链路
 → pbx-node 音频处理 → asr_result(en) + translation_result(engine=tmt) 回 api-server
 → api-server 转发 ASR/TMT 到前端
 → ASR final 后 api-server 调 DeepSeek Flash
 → translation_result(engine=deepseek-flash,revised?) 回前端
 → api-server tts_command → pbx-node TTS → WebRTC 中文配音下行
```

---

## B. 内部 Go 接口与数据模型

### B.1 数据模型
```go
type ASRResult struct {
    CallID, UtteranceID string
    Text                string
    IsFinal             bool
    Confidence          float64
    Language            string
}
type TranslationResult struct {
    CallID, UtteranceID string
    SourceText          string
    Text                string
    IsFinal             bool
    Engine              string // "tmt" or "deepseek-flash"
    Revised             bool
}
```

### B.2 DeepSeek Flash 纠错抽象 `internal/ai/llm`
```go
type Provider interface {
    Name() string
    Translate(ctx context.Context, req Request) (Result, error)
}
type ProviderFactory func(config Config) Provider

type Config struct {
    Provider, APIKey, Endpoint, Model string
    Params  map[string]string
    Secrets map[string]string
    Region  string
    Timeout time.Duration
}
type Request struct {
    CallID, UtteranceID    string
    Text                   string
    SourceLang, TargetLang string
    Quality                bool
    Context                []string
    Glossary               map[string]string
    FastTranslation        string
}
type Result struct {
    Text  string
    Terms map[string]string
}

func NewClient(config Config) *Client
func RegisterProvider(name string, factory ProviderFactory)
func (c *Client) Translate(ctx context.Context, req Request) (Result, error)

// 内置 provider 名：deepseek-flash / mock
```

### B.3 策略编排
```go
type Strategy string
const (
    StrategyHybrid   Strategy = "hybrid"   // PBX TMT灰字 + DeepSeek Flash final纠错
    StrategyTMT      Strategy = "tmt"      // 仅锁定PBX TMT
    StrategyDeepSeek Strategy = "deepseek" // DeepSeek Flash final覆盖，TMT可占位/兜底
)
```

### B.4 PBX 媒体面接线 `internal/pbx/webrtc`
```go
type ASRResultCallback func(result model.ASRResult)
type TranslationResultCallback func(result model.TranslationResult)
type OfferRequest struct {
    ConnectionID, CallID, UserID, SDP string
    ProviderConfigs map[model.CapabilityType]model.ProviderConfig
    OnICE               ICECallback
    OnASRResult         ASRResultCallback
    OnTranslationResult TranslationResultCallback // PBX TMT 快翻结果
}

// Session 关键字段：
//   onASRResult ASRResultCallback
//   onTranslationResult TranslationResultCallback
//   curUtteranceID string; utteranceSeq int
//
// 关键方法：
//   beginUtterance()        开流时分配稳定 utteranceID
//   forwardASRResult()          回调英文给 api-server
//   forwardTranslationResult()  回调PBX TMT快翻给 api-server
```

### B.5 业务信令面接线 `internal/transport/websocket`
```go
// wsMessage 字段含：Type/RequestID/ConnectionID/CallID/UserID/SDP/Candidate/Text/
//   IsFinal/Confidence/Language/Voice/Metadata/ProviderConfigs/Error
//   + SourceText、Engine string、Revised bool（translation_result 用）
// 处理 webrtc_offer 时转发给选定 pbx-node
// 收到 pbx-node asr_result 后转发前端并交给 internal/interpreter
// 收到 pbx-node translation_result(engine=tmt) 后保存为快翻、转发前端
// internal/interpreter 产出 translation_result(engine=deepseek-flash) 后写回前端
// 处理 set_strategy → 切换同传 Session 策略
```

---

## C. 外部服务 API（出站）

### C.1 腾讯云实时 ASR（英文识别）
| 项 | 值 |
|----|----|
| 协议 | WebSocket 长连接 |
| 端点 | `wss://asr.cloud.tencent.com/asr/v2/{appId}?...`（签名 URL）|
| 引擎 | `engine_model_type=16k_en`，`voice_format=opus/pcm`，`needvad=1` |
| 鉴权 | SecretId/SecretKey 签名 |
| 输入 | 二进制音频帧（PCM16/16k）|
| 输出 | JSON：`code`、`result.slice_type`（0=partial,2=final）、`result.voice_text_str` |

### C.2 腾讯机器翻译 TMT — TextTranslate（PBX 快翻）
| 项 | 值 |
|----|----|
| 域名 | `tmt.tencentcloudapi.com` |
| Action / Version | `TextTranslate` / `2018-03-21` |
| Region | `ap-guangzhou`（可配）|
| 鉴权 | TC3-HMAC-SHA256；头 `Authorization`、`X-TC-Action`、`X-TC-Timestamp`、`X-TC-Version`、`X-TC-Region` |
| 限制 | 默认 QPS≈5；单次文本约 2000 字符 / 6000 UTF-8 字节，超限需限流/分句 |

请求 / 响应：
```json
{ "SourceText": "We use a transformer architecture.", "Source": "en", "Target": "zh", "ProjectId": 0 }
{ "Response": { "TargetText": "我们使用变压器架构。", "Source": "en", "Target": "zh", "RequestId": "xxxx" } }
```
由 `pbx-node` 调用；映射：ASR final 文本 → `SourceText`；`Response.TargetText` → `translation_result(engine=tmt,isFinal=true)`。
> ⚠️ TMT 逐步下线，provider 可插拔，后续可换混元 `ChatTranslations`（`hunyuan.tencentcloudapi.com`，原生支持 GlossaryIDs/References），仅改 provider。

### C.3 DeepSeek Flash — Chat Completions（主服务纠错）
| 项 | 值 |
|----|----|
| 端点 | `https://api.deepseek.com/v1/chat/completions` |
| 鉴权 | `Authorization: Bearer ${DEEPSEEK_API_KEY}` |
| 模型 | `deepseek-flash` |
| 流式 | 支持（本期非流式即可）|

纠错请求（Quality=true，注入上下文+术语和 PBX TMT 快翻，要求 JSON）：
```json
{
  "model": "deepseek-flash", "temperature": 0, "stream": false,
  "response_format": {"type": "json_object"},
  "messages": [
    {"role": "system", "content": "You are a simultaneous interpreter (English→Chinese). Improve the PBX TMT draft using the glossary and context.\nGlossary:\n- transformer => Transformer 架构\nPreceding Chinese subtitles (context only):\n- ……\nReturn strict JSON: {\"translation\":\"<zh>\",\"terms\":{\"<en>\":\"<zh>\"}}. Output JSON only."},
    {"role": "user", "content": "NEW English sentence: We use a transformer architecture.\nPBX TMT draft: 我们使用变压器架构。"}
  ]
}
```
响应：
```json
{ "choices": [ { "message": { "role": "assistant",
  "content": "{\"translation\":\"我们采用一种 Transformer 架构。\",\"terms\":{\"transformer\":\"Transformer 架构\"}}" } } ] }
```
由 `api-server` 调用；映射：`Result.Text←content.translation`（解析失败则整体作译文）；`Result.Terms←content.terms`。失败时锁定 PBX TMT draft。

### C.4 腾讯云 TTS（中文配音）
| 项 | 值 |
|----|----|
| 域名 | `tts.tencentcloudapi.com` |
| 鉴权 | TC3-HMAC-SHA256 |
| 参数 | `Text`(中文)、`VoiceType`、`Codec=wav/pcm`、`SampleRate`、语言 `zh-CN` |
| 输出 | 合成音频 → 服务端编码为 PCMU/8000 → WebRTC 下行 |

---

## D. 环境变量

| 变量 | 默认 | 用途 |
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
| `DEEPSEEK_API_KEY` | - | `api-server` DeepSeek Flash 密钥 |
| `DEEPSEEK_ENDPOINT` | https://api.deepseek.com/v1/chat/completions | DeepSeek Flash 端点 |
| `DEEPSEEK_MODEL` | deepseek-flash | DeepSeek Flash 模型 |
| `TRANSLATE_STRATEGY` | hybrid | 默认纠错策略 |
| `TRANSLATE_TIMEOUT_MS` | 8000 | DeepSeek Flash 外呼超时 |

无 DeepSeek key 时主服务只转发 PBX ASR/TMT，主链路不崩。

---

## E. HTTP 接口（加分）与错误约定

| 方法 | 路径 | 入参 | 出参 |
|------|------|------|------|
| GET | `/api/sessions/{callId}/subtitles.srt` | callId | 双语 srt 文本 |
| POST | `/api/sessions/{callId}/summary` | `{ "lang":"zh" }` | `{ "summary": "中文要点..." }`（内部调 DeepSeek 总结全场字幕）|

错误约定：
- `error` 帧用于信令/协议错误（非法 JSON、provider 配置缺失、WebRTC 失败等）。
- **翻译/纠错类错误不发 `error` 帧中断前端**，改为该句中文留空或保留 PBX TMT 快翻、记服务端日志。
- 降级矩阵见 [04-backend-spec](./04-backend-spec.md) 第 7 节：字幕主链路（ASR→显示）永不被翻译/配音故障中断。

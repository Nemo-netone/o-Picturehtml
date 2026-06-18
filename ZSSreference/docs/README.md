# SimulSpeak1 —— AI 同声传译助手 · 设计文档

本目录是 SimulSpeak1 的完整设计文档（需求 → 架构 → 前后端 → 接口）。

---

## 系统能力与技术栈

| 模块 | 技术 | 职责 |
|------|------|------|
| 媒体面（PBX） | Go + Pion WebRTC + Opus(gopus) | 浏览器音频上行、Opus 解码为 PCM、中文配音音频下行 |
| 信令面（api-server） | WebSocket（自定义控制协议）| 前端只连 api-server；转发 SDP/ICE 给 PBX，回推字幕/事件 |
| VAD 语音检测 | Simple RMS / Silero ONNX + 预滚 + 切句 | 过滤静音、按句切分，驱动一句话生命周期 |
| 流式 ASR | 腾讯云流式 ASR，引擎 `16k_en` | 英文 partial/final 识别 |
| 快翻草稿（PBX） | 腾讯机器翻译 TMT `TextTranslate` | ASR final 后出中文草稿（灰字） |
| 纠错 final（api-server） | DeepSeek Flash | 带上下文+术语表，出黑色锁定并纠错 |
| 中文配音 TTS（PBX） | 腾讯云 TTS + PCMU 下行 | api-server 发 `tts_command`，PBX WebRTC 下行播放 |
| 集群面 | etcd 注册中心 + 路由策略 | PBX 节点注册、负载上报、最少负载/轮询路由 |
| 前端 | React SPA（WebRTC + WebSocket）| 音频采集、双语字幕渲染、策略/配音控制 |

## 四层架构

- **信令面**：api-server 承载前端 WebSocket、SDP/ICE 转发、字幕回推、LLM 纠错编排。
- **媒体面**：pbx-node 承载 WebRTC、Opus↔PCM 编解码、VAD 切句、流式 ASR、TMT 快翻、TTS 配音。
- **AI 面**：VAD/ASR/TMT/TTS/LLM 全部接口化为可插拔 provider，环境变量一键切换 mock/真实实现。
- **集群面**：PBX 节点注册到 etcd，api-server 按策略为新会话选择承载节点，支持横向扩展。

职责边界：**PBX 不持有同传业务状态；api-server 不直接处理媒体帧。**

## 架构与流程图片

- [项目架构图 SVG](./images/architecture.svg) / [PNG](./images/architecture.png)
- [端到端业务流程图 SVG](./images/business-flow.svg) / [PNG](./images/business-flow.png)
- [Mermaid 与 DOT 源文件索引](./diagrams.md)

## 文档索引（建议阅读顺序）

1. [01-requirements.md](./01-requirements.md) —— 需求规格（FR1–FR12、NFR、MoSCoW 优先级、验收标准）
2. [02-architecture.md](./02-architecture.md) —— **系统架构与业务逻辑**：项目结构、四层设计、WebRTC 管线、VAD/ASR/翻译/TTS、commit/revise 状态机、容错、横向扩展、时序、延迟预算
3. [03-frontend-spec.md](./03-frontend-spec.md) —— 前端页面功能与渲染规则
4. [04-backend-spec.md](./04-backend-spec.md) —— 后端模块与业务逻辑
5. [05-interfaces.md](./05-interfaces.md) —— **全部接口定义**：WebSocket 协议（前端↔api-server、api-server↔pbx-node）、HTTP API、Go 内部接口、外部服务 API、环境变量
6. [diagrams.md](./diagrams.md) —— 架构图、业务流程图、状态机、容错、扩展拓扑（Mermaid 源码）

前端补充文档：
- [frontend/mvp.md](./frontend/mvp.md) —— 前端 MVP 范围、里程碑 M0–M5、目录结构
- [frontend/conventions.md](./frontend/conventions.md) —— 前端编码规约
- [frontend/frontend-structure.drawio](./frontend/frontend-structure.drawio) —— 前端结构图

## 关键术语

| 术语 | 含义 |
|------|------|
| partial | ASR 中间识别结果，可能被后续修正 |
| final | ASR 最终识别结果，一句话定稿 |
| utteranceID | 一句话的稳定标识；同句 partial→final 共享，前端据此就地更新 |
| commit/revise | 字幕「锁定/可改」机制：未定稿灰色可改，定稿黑色锁定 |
| 快翻 | PBX 在 ASR final 后触发的机器翻译草稿（TMT） |
| 纠错 | api-server final 阶段带上下文+术语表的 DeepSeek Flash 翻译，可产生 revised |
| 术语表 glossary | 会话级 源词→译法 映射，保证专业术语全程一致 |

## 本地 Silero VAD 接入

仓库已内置 Linux x64 的 ONNX Runtime 1.26.0 与 `silero_vad.onnx`：

- `third_party/onnxruntime-linux-x64-1.26.0/lib/libonnxruntime.so.1.26.0`
- `third_party/silero-vad/silero_vad.onnx`

`.env.example` 默认引用这些路径。若切换到 `SIMULSPEAK_VAD_PROVIDER=silero`，直接复制 `.env.example` 后填云服务密钥即可启动。WebRTC 20ms 音频帧会在 PBX 内按会话累计到 Silero 需要的 512 samples 后再推理，不需要前端改变 RTP/Opus 发包大小。
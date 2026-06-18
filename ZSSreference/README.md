# SimulSpeak1

> 🎬 **Demo 视频**：[Bilibili BV1ebEb6DEux](https://www.bilibili.com/video/BV1ebEb6DEux/)

SimulSpeak1 是一个 Go 实现的 **AI 同声传译助手**（英文 → 中文）：浏览器采集英文音频，经 WebRTC 上行到 PBX 媒体节点，PBX 完成 VAD 切句 → 流式 ASR → TMT 快翻 → 中文 TTS 配音；主服务 `api-server` 负责节点调度、信令转发、字幕状态机、DeepSeek Flash 纠错覆盖，最终以前端**双语字幕（英文原文 + 中文译文）+ 中文配音**实时呈现。

> 职责边界：**PBX 是媒体处理核心**，负责音频接收、ASR/TMT 实时结果和下行配音；**api-server 是业务编排核心**，负责前端 WebSocket、PBX 节点选择、字幕转发和 DeepSeek Flash final/revision。

---

## 系统总览

```
                    ┌── WebSocket ──▶  api-server  ──PBX Control WS──┐
                    │  (字幕 / 控制)    ▲                             │
                    │                  │  asr_result                 │
浏览器 (SPA)         │                  │  translation_result          ▼
                    │                  │                  ┌── pbx-node ──┐
                    │                  └──────────────────│  VAD → ASR   │
                    └──────── WebRTC ────────────────────│  TMT → TTS   │
                       (Opus 上行 / PCMU 下行)            │  Opus ↔ PCM  │
                                                         └──────────────┘
```

### 数据流（端到端）

1. 浏览器打开 SPA，WebSocket 连接 `api-server`，发送 `client_hello`
2. `api-server` 创建会话，从 etcd/memory registry 选一个 `pbx-node`
3. `api-server` 转发 SDP Offer / ICE Candidate 完成 WebRTC 握手
4. `pbx-node` 接收 Opus 音频 → 解码为 PCM16/16kHz → VAD 切句
5. VAD 触发一句话开始 → 流式 ASR 产出 partial/final（英文原文，白色字幕）
6. ASR final 后 → TMT 快翻（中文草稿，灰色字幕）
7. `api-server` 调用 DeepSeek Flash + 术语表上下文 → polished 译文（黑色锁定），若与草稿不同则高亮修正
8. `api-server` 发 `tts_command` → `pbx-node` TTS 合成 → PCMU 音频下行

### 四层架构

| 层 | 组件 | 职责 |
|----|------|------|
| **信令面** | `api-server` | 前端 WebSocket / HTTP，会话管理，SDP/ICE 转发，字幕状态机，DeepSeek Flash 纠错 |
| **媒体面** | `pbx-node` | WebRTC PeerConnection（Pion），Opus 编解码，VAD 切句，流式 ASR，TMT 快翻，TTS 配音 |
| **AI 面** | `internal/ai/*` | VAD / ASR / TMT / TTS / LLM provider 抽象，支持 mock / tencent / openai-compatible |
| **集群面** | etcd / memory registry | PBX 节点注册与发现，按策略（最少负载 / 轮询）为会话选择节点 |

---

## 项目结构

```
SimulSpeak1/
├── cmd/
│   ├── api-server/     # 同传业务主服务
│   ├── pbx-node/       # PBX 媒体节点
│   ├── worker/         # 异步 worker（词汇本、总结）
│   └── seed-demo/      # Demo 数据初始化
├── internal/
│   ├── ai/             # AI provider 层（vad, asr, tmt, tts, llm）
│   ├── api-server/     # api-server 专用逻辑
│   ├── pbx/            # PBX 媒体处理核心
│   ├── protocol/pbx/   # api-server ↔ pbx-node 控制协议
│   ├── config/         # 统一配置（YAML + env + CLI）
│   ├── bootstrap/      # 应用生命周期管理
│   ├── subtitle/       # 字幕流与 SRT/Markdown 导出
│   ├── summary/        # 会后总结
│   ├── session/        # 会话管理
│   ├── store/sqlite/   # SQLite 持久化（GORM）
│   ├── registry/       # etcd 节点注册中心
│   ├── eventbus/       # 内部事件总线
│   └── model/          # 共享数据模型
├── pkg/client/          # SDK：节点池、负载均衡、中继、WebSocket
├── frontend/            # React + Vite SPA（pnpm）
├── web/pbx-probe/       # PBX 调试探针（Web 界面）
├── docs/                # 完整设计文档（需求 → 架构 → 接口）
├── third_party/         # ONNX Runtime 1.26.0 + silero_vad.onnx
└── deployments/         # Docker Compose 配置
```

---

## 原创与依赖声明

### 原创代码量

| 层 | 代码行数 | 说明 |
|----|----------|------|
| `internal/pbx/` | 7,593 | PBX 媒体核心：WebRTC 管理、媒体管线、控制通道、录音、话单 |
| `internal/ai/` | 5,776 | AI provider 层：VAD（simple / Silero ONNX）/ ASR / TMT / TTS / LLM 全抽象实现 |
| `internal/api-server/` | 5,518 | api-server 业务逻辑：HTTP/WS 接入、PBX 桥接、路由 |
| `internal/store/` | 2,284 | SQLite 持久化 + 会话 / CDR / 字幕存储 |
| `internal/model/` | 1,434 | 共享数据模型定义 |
| `internal/config/` | 1,252 | 四级优先级配置体系（默认→YAML→env→CLI） |
| `internal/etcdutil/` | 994 | etcd 连接管理与操作封装 |
| `cmd/*` | 865 | 4 个可执行入口（api-server / pbx-node / worker / seed-demo） |
| 其余 internal 包 | ~2,500 | registry / eventbus / session / protocol / worker / gateway / security 等 |
| `frontend/` | 9,605 | React + Vite + TypeScript：双语字幕、WebRTC 采集、策略控制、配音开关 |
| `docs/` | — | 12 份完整设计文档（需求 → 架构 → 接口） |
| **合计** | **~43,000** | Go 33,000 + 前端 9,600（不含 vendor / third_party） |

### Go 依赖库（go.mod）

| 依赖 | 用途 | 在项目中的角色 |
|------|------|---------------|
| `github.com/pion/webrtc/v4` | WebRTC 协议栈（PeerConnection / SDP / ICE / SRTP） | PBX 媒体链路核心 |
| `github.com/pion/rtp` | RTP 包编解码 | 音频帧收发 |
| `github.com/pion/sdp/v3` | SDP 解析 | WebRTC 信令协商 |
| `layeh.com/gopus` | Opus 音频编解码 | Opus ↔ PCM 转换 |
| `github.com/go-chi/chi/v5` | HTTP 路由 | api-server / pbx-node 的 HTTP API |
| `gorm.io/gorm` | ORM 框架 | 数据库抽象层 |
| `github.com/glebarez/sqlite` | 纯 Go SQLite 驱动 | 会话 / 字幕 / CDR 本地持久化 |
| `go.etcd.io/etcd/client/v3` | etcd 客户端 | PBX 节点注册与服务发现 |
| `github.com/yalue/onnxruntime_go` | ONNX Runtime Go binding | Silero VAD 模型推理 |
| `gopkg.in/yaml.v3` | YAML 解析 | 配置文件加载 |
| `golang.org/x/net` | WebSocket 支持 | 前端信令连接 + PBX 控制通道 |

> 以上为直接依赖（`require` 块），间接依赖（`indirect`）由上述库传递引入，详见 [go.mod](go.mod)。

### 内置第三方资产（third_party/）

| 文件 | 说明 | 许可 |
|------|------|------|
| `third_party/onnxruntime-linux-x64-1.26.0/` | Microsoft ONNX Runtime 1.26.0 Linux x64 动态库 | MIT |
| `third_party/silero-vad/silero_vad.onnx` | Silero VAD ONNX 模型（语音活动检测） | [Silero License](https://github.com/snakers4/silero-vad) |

> 这两个文件已随仓库提交，无需额外下载即可使用 Silero VAD 模式。仅在文件缺失或需替换版本时运行 `make install-third-party`。

### 前端依赖（frontend/）

| 依赖 | 用途 |
|------|------|
| `react` + `react-dom` (19) | UI 框架 |
| `zustand` | 轻量状态管理 |
| `vite` (8) | 构建工具 |
| `typescript` (6) | 类型系统 |
| `ws` | WebSocket 客户端（mock server） |

### 运行时外部云服务

以下服务为**可选依赖**——切换到 `mock` provider 后可在完全离线环境运行：

| 服务 | 用途 | 接口 | Provider 名 |
|------|------|------|-------------|
| 腾讯云 ASR | 流式英文语音识别（`16k_en`） | WebSocket | `tencent-asr` |
| 腾讯云 TMT | 文本机器翻译快翻 | HTTP API | `tencent-tmt` |
| 腾讯云 TTS | 中文语音合成 | HTTP API | `tencent-tts` |
| DeepSeek Flash | LLM 翻译纠错（OpenAI-compatible） | Chat Completions | `openai-compatible` |
| etcd | 分布式 PBX 节点注册发现 | gRPC | `etcd` |

### 原创力一览

以下是本项目从零实现的核心能力（非依赖库直接提供）：

| 能力 | 说明 |
|------|------|
| **四层架构设计** | 信令面 / 媒体面 / AI 面 / 集群面的边界划分与编排 |
| **WebRTC 信令中继** | api-server 转发 SDP / ICE，控制面与媒体面彻底解耦 |
| **VAD 切句管线** | Silero ONNX 推理 + 帧缓存（320→512 samples）+ pre-roll + 静音门控，驱动一句话生命周期 |
| **流式 ASR 管理** | 按句懒加载/关闭腾讯 ASR WebSocket，partial/final 回调 + utteranceID 稳定标识 |
| **两段翻译流水线** | PBX 侧 TMT 快翻（灰色草稿）→ api-server 侧 DeepSeek Flash 纠错（黑色锁定），延迟与质量分级保障 |
| **commit / revise 状态机** | 每行字幕 pending→locked→revised 三终态，pending 串行刷新、locked 不再变动、revised 高亮可见 |
| **术语表自积累** | 会话级源词→译法映射，模型输出时回写，保证后续句子术语一致 |
| **TTS 下行配音** | api-server 触发 → pbx-node TTS 合成 → PCMU 编码 → 20ms 帧节拍播放，与字幕解耦 |
| **多级容错降级** | TMT 失败等 DeepSeek，DeepSeek 失败锁 TMT，全部失败仅显英文——字幕主链永不中断 |
| **可插拔 provider 体系** | VAD / ASR / TMT / TTS / LLM 五大能力全部接口化，环境变量一键切换 mock/真实实现 |
| **四级配置体系** | 内置默认 → YAML → `.env`/env → CLI 参数，`.env` 自动加载 |
| **etcd 集群注册** | PBX 节点注册 / 心跳 / 负载上报 + api-server 最少负载 / 轮询 / 亲和路由 |
| **PBX 调试探针** | 独立 Web 界面，可观察 WebSocket 信令 / SDP / ICE / RTP 统计全过程 |
| **React 双语字幕 SPA** | WebRTC 音频采集 + WebSocket 字幕/控制 + 灰→黑→高亮渲染 + 策略切换 + 配音开关 |

---

## 快速开始

### 环境要求

| 依赖 | 版本 | 说明 |
|------|------|------|
| Go | ≥ 1.25 | 后端编译与运行 |
| Node.js | ≥ 20 | 前端构建 |
| pnpm | 最新 | 前端包管理 |
| ONNX Runtime | 1.26.0 (Linux x64) | Silero VAD 推理（已内置在 `third_party/`） |
| etcd | 可选 | 多节点部署；单进程开发用 `memory` 模式 |

### Docker Compose 一键部署

仓库内置 Demo 完整栈编排，默认使用 `mock` ASR/TMT/TTS/LLM 和 `simple` VAD，不需要外部云服务密钥即可启动：

```bash
make docker-demo
# 等价于：
docker compose -f deployments/docker-compose.demo.yml up --build
```

启动后访问：

| 服务 | 地址 | 说明 |
|------|------|------|
| 前端 SPA | http://localhost:5173 | React 页面，nginx 代理 `/api` 和 `/ws` |
| api-server | http://localhost:8080 | HTTP API / WebSocket 入口 |
| pbx-node | http://localhost:8081/pbx/health | PBX 控制面健康检查 |
| etcd | http://localhost:2379 | 节点注册中心 |

Compose 会启动 `etcd`、`api-server`、`pbx-node`、`worker` 和 `frontend`，并使用共享 volume 保存 SQLite 会话库和录音数据。常用端口可通过环境变量覆盖，例如：

```bash
FRONTEND_PORT=8088 API_SERVER_PORT=18080 make docker-demo
```

停止服务：

```bash
docker compose -f deployments/docker-compose.demo.yml down
# 如需清空 demo 数据：
docker compose -f deployments/docker-compose.demo.yml down -v
```

如需接入真实腾讯云/LLM provider，先导出对应 `SIMULSPEAK_*` 环境变量，再执行 `make docker-demo`；默认编排会把这些变量传入容器。

### 启动后端

```bash
cp .env.example .env     # 按需修改配置；.env 已被 .gitignore 忽略
make dev                  # 同时启动 pbx-node + api-server + worker
# 或分别启动：
make pbx                  # 启动 pbx-node（媒体节点）
make api                  # 启动 api-server（业务主服务）
make worker               # 启动异步 worker
```

### 启动前端

```bash
cd frontend
pnpm install
cp .env.example .env      # 按需修改 VITE_WS_URL
pnpm dev                  # Vite 开发服务器，默认 http://localhost:5173
```

### PBX 调试探针

```bash
make probe-install
make probe                # 启动 PBX 调试探针
```

### Demo 初始化

```bash
make seed                 # 运行 seed-demo，填充 memory registry 和配置
```

---

## 构建

```bash
make build                # 输出到 bin/：api-server, pbx-node, worker, seed-demo
```

---

## 配置

配置优先级（后者覆盖前者）：**内置默认值 → YAML (`--config`) → `.env` / 环境变量 (`SIMULSPEAK_*`) → 命令行参数**。

启动时自动加载工作目录下的 `.env`（可用 `SIMULSPEAK_ENV_FILE` 覆盖路径）。全部可配置项见 [.env.example](.env.example)。

### 关键配置项

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `SIMULSPEAK_ASR_PROVIDER` | ASR 引擎：`mock` / `tencent-asr` | `mock` |
| `SIMULSPEAK_TMT_PROVIDER` | 快翻引擎：`mock` / `tencent-tmt` | `mock` |
| `SIMULSPEAK_TTS_PROVIDER` | TTS 引擎：`mock` / `tencent-tts` | `mock` |
| `SIMULSPEAK_VAD_PROVIDER` | VAD 模式：`simple`（RMS 能量）/ `silero`（ONNX） | `silero` |
| `SIMULSPEAK_LLM_PROVIDER` | LLM 纠错：`openai-compatible` / `mock` | `openai-compatible` |
| `SIMULSPEAK_ETCD_MODE` | 注册中心：`memory`（单进程）/ `etcd`（分布式） | `memory` |

### Silero VAD

仓库已内置 Linux x64 的 ONNX Runtime 1.26.0 与 `silero_vad.onnx`，默认 `.env.example` 直接引用。仅当文件缺失或需替换版本时才运行：

```bash
make install-third-party
```

---

## 翻译策略

| 策略 | PBX 实时结果 | api-server final 行为 | 纠错能力 | 延迟 |
|------|-------------|----------------------|----------|------|
| **混合（默认）** | TMT 快翻草稿 | DeepSeek Flash 纠错覆盖 | 强 | 低/中 |
| 仅快翻 | TMT 快翻（锁定） | 不再纠错 | 弱 | 最低 |
| 仅纠错 | TMT 作兜底 | DeepSeek Flash final 覆盖 | 中 | 中 |

---

## Make 命令参考

| 命令 | 说明 |
|------|------|
| `make help` | 列出所有可用目标 |
| `make dev` | 同时启动 pbx-node + api-server + worker |
| `make api` | 启动 api-server |
| `make pbx` | 启动 pbx-node |
| `make worker` | 启动异步 worker |
| `make seed` | 运行 Demo 数据初始化 |
| `make build` | 构建所有二进制到 `bin/` |
| `make test` | 运行全部 Go 测试 |
| `make test-pbx` | 仅运行 PBX 相关测试 |
| `make test-silero` | 运行 Silero ONNX 真实推理测试 |
| `make fmt` | 格式化 Go 代码 |
| `make vet` | 运行 go vet |
| `make probe` | 启动 PBX 调试探针 |
| `make install-third-party` | 安装 ONNX Runtime + Silero 模型 |
| `make docker-dev` | 启动开发依赖（etcd 等） |
| `make docker-demo` | 启动 Demo 完整栈 |

---

## 测试

```bash
make test                 # 全量测试
make test-pbx             # PBX 媒体面测试
make test-silero          # Silero ONNX 真实推理测试（需 VAD=Silero）
go vet ./...              # 静态检查
```

---

## 文档

按推荐阅读顺序排列：

1. [需求规格](docs/01-requirements.md) —— 功能需求 FR1–FR12、非功能需求、验收标准
2. [系统架构](docs/02-architecture.md) —— 四层设计、WebRTC 管线、VAD/ASR/TMT/LLM 链路、commit/revise 状态机、容错、延迟预算
3. [前端规格](docs/03-frontend-spec.md) —— 页面功能与渲染规则
4. [后端规格](docs/04-backend-spec.md) —— 后端模块与业务逻辑
5. [接口定义](docs/05-interfaces.md) —— 全部 WebSocket 协议、HTTP API、Go 内部接口、外部服务 API、环境变量

补充文档：[设计文档索引](docs/README.md) · [前端 MVP](docs/frontend/mvp.md) · [前端规约](docs/frontend/conventions.md) · [流程图](docs/diagrams.md) · [项目规范](CLAUDE.md)

---

## 贡献

- 每个 PR 只做一件事，提交粒度尽可能小
- PR 描述需包含：标题、功能描述、实现思路、测试方式
- 主分支始终可运行
- 前端改动前必读 [docs/frontend/mvp.md](docs/frontend/mvp.md)

详见 [CLAUDE.md](CLAUDE.md)。

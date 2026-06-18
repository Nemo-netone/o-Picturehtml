# SimulSpeak1 流程图

> 以下流程图使用 Mermaid 语法，GitHub / VS Code / 大部分 Markdown 渲染器均可直接展示。

## 已生成图片

| 图片 | SVG | PNG | 源文件 |
|------|-----|-----|--------|
| 项目架构图 | [architecture.svg](images/architecture.svg) | [architecture.png](images/architecture.png) | [architecture.dot](images/architecture.dot) |
| 端到端业务流程图 | [business-flow.svg](images/business-flow.svg) | [business-flow.png](images/business-flow.png) | [business-flow.dot](images/business-flow.dot) |

### 项目架构图

![SimulSpeak1 项目架构图](images/architecture.svg)

### 端到端业务流程图

![SimulSpeak1 端到端业务流程图](images/business-flow.svg)

---

## 1. 四层架构总览

```mermaid
graph TB
    subgraph Browser["🖥 浏览器 (SPA)"]
        AudioSrc["音频源<br/>标签页 / 系统 / 麦克风"]
        Subtitles["双语字幕面板<br/>白=英文原文 灰=草稿 黑=锁定"]
        Strategy["控制面板<br/>翻译策略 / 配音开关"]
    end

    subgraph Signaling["📡 信令面 (api-server)"]
        WsFront["前端 WebSocket<br/>信令 · 字幕 · 控制"]
        SessMgr["会话管理<br/>Session · 状态 · 术语表"]
        PBXSelect["PBX 节点选择<br/>最少负载 / 轮询 / 亲和"]
        DeepSeek["DeepSeek Flash 纠错<br/>上下文 + 术语表 → revised"]
        SubFSM["字幕状态机<br/>pending → locked → revised"]
        Sqlite[("SQLite<br/>持久化")]
    end

    subgraph Media["🎙 媒体面 (pbx-node)"]
        WebRTC["Pion WebRTC<br/>PeerConnection · SDP/ICE"]
        OpusDec["Opus 解码<br/>48kHz → PCM16/16k"]
        VAD["VAD 语音检测<br/>simple / Silero ONNX"]
        ASR["流式 ASR<br/>腾讯云 16k_en · WS"]
        TMT["TMT 快翻<br/>腾讯云 TextTranslate"]
        TTS["TTS 中文配音<br/>腾讯云 → PCMU 下行"]
    end

    subgraph Cluster["🌐 集群面"]
        etcd[("etcd 注册中心<br/>节点注册 · 负载上报 · 发现")]
    end

    subgraph External["☁️ 外部服务"]
        TencentASR[("腾讯云 ASR")]
        TencentTMT[("腾讯云 TMT")]
        TencentTTS[("腾讯云 TTS")]
        DeepSeekAPI[("DeepSeek API")]
    end

    Browser -->|"WebSocket (控制+字幕)"| Signaling
    Browser -->|"WebRTC (Opus↑/PCMU↓)"| Media
    Signaling -->|"PBX 控制 WS<br/>SDP/ICE 转发<br/>tts_command"| Media
    Media -->|"asr_result<br/>translation_result"| Signaling
    Media -->|"注册 · 心跳 · 负载"| Cluster
    Signaling -->|"节点发现 · 选择"| Cluster

    ASR -.->|"流式 WS"| TencentASR
    TMT -.->|"HTTP API"| TencentTMT
    TTS -.->|"HTTP API"| TencentTTS
    DeepSeek -.->|"Chat Completions"| DeepSeekAPI

    style Browser fill:#e1f5fe,stroke:#0288d1
    style Signaling fill:#fce4ec,stroke:#d81b60
    style Media fill:#e8f5e9,stroke:#388e3c
    style Cluster fill:#fff3e0,stroke:#f57c00
    style External fill:#f3e5f5,stroke:#7b1fa2
```

---

## 2. 端到端数据流（时序图）

```mermaid
sequenceDiagram
    participant B as 🖥 浏览器
    participant API as 📡 api-server
    participant ETCD as 🗄 etcd
    participant PBX as 🎙 pbx-node
    participant TX as ☁️ 腾讯云
    participant DS as 🧠 DeepSeek

    Note over B,DS: === 信令握手阶段 ===

    B->>API: ① WebSocket 连接
    API-->>B: connected
    B->>API: ② client_hello (策略、配音开关、音频参数)

    API->>ETCD: ③ 查询可用节点 (least_load)
    ETCD-->>API: pbx-node-1 (负载 2/100)

    API->>PBX: ④ 创建媒体会话
    PBX-->>API: session_created

    B->>API: ⑤ webrtc_offer (SDP)
    API->>PBX: ⑥ 转发 SDP offer
    PBX->>PBX: 创建 PeerConnection
    PBX-->>API: ⑦ webrtc_answer (SDP)
    API-->>B: ⑧ 转发 answer

    loop ICE 交换
        B->>API: ice_candidate
        API->>PBX: 转发 ICE
        PBX->>API: ice_candidate
        API->>B: 转发 ICE
    end

    Note over B,DS: === 媒体传输阶段 (WebRTC 直连已建立) ===

    B->>PBX: ⑨ Opus 音频上行 (20ms/帧)
    PBX->>PBX: Opus 解码 → PCM16/16kHz

    Note over B,DS: === VAD 切句 + ASR ===

    PBX->>PBX: VAD(Silero ONNX) 检测语音开始
    PBX->>TX: ⑩ 打开流式 ASR WebSocket
    PBX->>TX: 持续写入 PCM 帧

    TX-->>PBX: ⑪ partial: "we use a"
    PBX-->>API: asr_result (utteranceId, partial)
    API-->>B: asr_result → 英文行灰字

    TX-->>PBX: ⑫ partial: "we use a transformer"
    PBX-->>API: asr_result (同 utteranceId, partial)
    API-->>B: asr_result → 就地刷新英文

    PBX->>PBX: VAD 检测静音结束
    PBX->>TX: ⑬ 关闭 ASR 流
    TX-->>PBX: ⑭ final: "We use a transformer architecture."

    Note over B,DS: === TMT 快翻 + DeepSeek 纠错 ===

    PBX->>TX: ⑮ TMT TextTranslate
    TX-->>PBX: "我们使用变压器架构。"
    PBX-->>API: translation_result (tmt, 灰色草稿)
    API-->>B: TMT 草稿 → 中文灰字

    API->>API: 构建上下文 (最近3句+术语表)
    API->>DS: ⑯ DeepSeek Flash 纠错
    DS-->>API: "我们采用一种 Transformer 架构。"
    API->>API: 比较: revised=true (≠TMT)
    API-->>B: translation_result (deepseek-flash, 黑字+高亮)
    API->>API: 更新术语表

    Note over B,DS: === TTS 配音 ===

    API->>PBX: ⑰ tts_command (polished 中文)
    PBX->>TX: ⑱ TTS 合成
    TX-->>PBX: PCM 音频
    PBX->>PBX: PCM → PCMU 编码 → 20ms 帧
    PBX->>B: ⑲ PCMU 音频下行 (WebRTC)

    Note over B: 用户看到：<br/>We use a transformer architecture.<br/>我们采用一种 Transformer 架构。
```

---

## 3. 字幕状态机（commit / revise）

```mermaid
stateDiagram-v2
    [*] --> 空: 等待语音开始

    空 --> pending: ASR partial 到达<br/>灰度渲染英文原文
    pending --> pending: 新 partial（同 utteranceId）<br/>就地刷新英文

    pending --> locked: ASR final + TMT 快翻<br/>英文黑字锁定<br/>中文灰字(TMT草稿)

    state 中文处理 <<fork>>
    locked --> 中文处理: api-server 调用 DeepSeek Flash

    中文处理 --> locked: 纠错 == TMT<br/>中文变黑锁定<br/>revised=false
    中文处理 --> revised: 纠错 ≠ TMT<br/>中文变黑锁定 + 高亮闪动<br/>revised=true

    revised --> [*]: 终态，不再变动
    locked --> [*]: 终态，不再变动

    note right of pending
        pending 阶段是唯一的可变阶段
        同一 utterance 的 partial 会覆盖
    end note

    note right of revised
        locked 和 revised 都是终态
        已锁定的字幕永不跳变
    end note
```

---

## 4. VAD 切句 + ASR 流管理

```mermaid
flowchart TD
    Start(["WebRTC 音频帧到达<br/>Opus 20ms → PCM16/16k"]) --> Queue["帧缓存<br/>累积 320 samples → 512 samples<br/>(匹配 Silero 推理窗口)"]

    Queue --> Silero{{"Silero ONNX 推理<br/>speech_prob > threshold ?"}}

    Silero -->|"否 (静音)"| SilenceCheck{"连续静音帧<br/>≥ endSilenceFrames ?"}
    SilenceCheck -->|否| NextFrame["等待下一帧"]
    NextFrame --> Start
    SilenceCheck -->|是| SpeechEnd["语音结束"]

    Silero -->|"是 (语音)"| PreRoll["pre-roll buffer<br/>补发句首被缓存的帧"]
    PreRoll --> SpeechStart{"是否已打开 ASR 流？"}

    SpeechStart -->|否| OpenStream["懒加载打开 ASR WebSocket<br/>生成新 utteranceID"]
    OpenStream --> WritePCM["写入 PCM 帧到 ASR 流"]
    SpeechStart -->|是| WritePCM

    WritePCM --> ASRCallback["ASR 回调<br/>partial → 灰字<br/>final 到达时标记"]
    ASRCallback --> Start

    SpeechEnd --> CloseStream["关闭当前 ASR 流"]
    CloseStream --> TriggerFinal["触发该句 ASR final"]
    TriggerFinal -->|"下一段语音"| Start

    TriggerFinal --> TMT["PBX 调 TMT 快翻"]
    TMT --> DS["api-server 调 DeepSeek 纠错"]
    DS --> TTS["api-server → pbx-node TTS 配音"]

    style Silero fill:#fff3e0,stroke:#f57c00
    style OpenStream fill:#e8f5e9,stroke:#388e3c
    style CloseStream fill:#fce4ec,stroke:#d81b60
```

---

## 5. 翻译策略决策流

```mermaid
flowchart LR
    ASRFinal["ASR final 到达"] --> PBXTMT["PBX 执行 TMT 快翻<br/>→ 灰色草稿字幕"]
    PBXTMT --> Strategy{{"当前策略？"}}

    Strategy -->|"仅快翻"| LockTMT["锁定 TMT 草稿<br/>→ 黑色字幕"]
    Strategy -->|"仅纠错"| DSOnly["DeepSeek Flash 纠错<br/>覆盖同 utteranceId"]
    Strategy -->|"混合(默认)"| DSMixed["DeepSeek Flash 纠错<br/>带上下文 + 术语表"]

    DSOnly --> Compare1{"纠错结果<br/>vs TMT 草稿"}
    DSMixed --> Compare{"纠错结果<br/>vs TMT 草稿"}

    Compare -->|相同| LockSame["黑色锁定<br/>revised=false"]
    Compare -->|不同| Revise["黑色锁定 + 高亮<br/>revised=true"]

    Compare1 -->|相同| LockSame
    Compare1 -->|不同| Revise

    LockTMT --> TTS{"配音开启？"}
    LockSame --> TTS
    Revise --> TTS

    TTS -->|是| PBXTTS["pbx-node TTS 合成<br/>→ PCMU 下行播放"]
    TTS -->|否| Done["完成"]

    style Strategy fill:#e1f5fe,stroke:#0288d1
    style Revise fill:#fce4ec,stroke:#d81b60
```

---

## 6. 容错降级矩阵

```mermaid
flowchart TD
    ASR(["ASR 流"]) --> ASROK{"ASR 正常？"}
    ASROK -->|是| TMT(["TMT 快翻"])
    ASROK -->|否| ASRFallback["禁用当前流<br/>按需重连<br/>不阻塞后续句子"]

    TMT --> TMTOK{"TMT 正常？"}
    TMTOK -->|是| DS(["DeepSeek 纠错"])
    TMTOK -->|否| DS

    DS --> DSOK{"DeepSeek 正常？"}
    DSOK -->|是| Final["✅ 黑色锁定<br/>纠错 ≤ revised"]
    DSOK -->|否| TMTLock{"有 TMT 草稿？"}

    TMTLock -->|是| LockFallback["✅ 锁定 TMT 草稿<br/>revised=false"]
    TMTLock -->|否| EnOnly["⚠️ 仅显示英文 final<br/>中文缺失记日志"]

    style ASR fill:#e8f5e9,stroke:#388e3c
    style TMT fill:#e8f5e9,stroke:#388e3c
    style DS fill:#e8f5e9,stroke:#388e3c
    style Final fill:#c8e6c9,stroke:#2e7d32
    style LockFallback fill:#fff9c4,stroke:#f9a825
    style EnOnly fill:#ffcdd2,stroke:#c62828
```

---

## 7. 横向扩展拓扑

```mermaid
graph TB
    subgraph Users["👥 用户"]
        U1["用户 A"]
        U2["用户 B"]
        U3["用户 C"]
    end

    LB["api-server<br/>WebSocket 粘性会话"]

    subgraph PBXCluster["🎙 PBX 节点集群"]
        direction TB
        P1["pbx-node-1<br/>calls: 2/100<br/>zone: default"]
        P2["pbx-node-2<br/>calls: 1/100<br/>zone: default"]
        P3["pbx-node-N<br/>calls: 0/100<br/>zone: backup"]
    end

    ETCD2[("etcd<br/>节点注册 · 负载上报")]

    U1 -->|"WS + WebRTC"| LB
    U2 -->|"WS + WebRTC"| LB
    U3 -->|"WS + WebRTC"| LB

    LB -->|"select: least_load"| P1
    LB -->|"select: least_load"| P2
    LB -->|"select: zone_affinity"| P3

    P1 -->|"注册 / 心跳 / 负载"| ETCD2
    P2 -->|"注册 / 心跳 / 负载"| ETCD2
    P3 -->|"注册 / 心跳 / 负载"| ETCD2

    LB -->|"发现 / 查询"| ETCD2

    style Users fill:#e1f5fe,stroke:#0288d1
    style LB fill:#fce4ec,stroke:#d81b60
    style PBXCluster fill:#e8f5e9,stroke:#388e3c
    style ETCD2 fill:#fff3e0,stroke:#f57c00
```

---

## 使用说明

这些图在以下环境中均可直接渲染：

- **GitHub / GitLab**：直接查看 Markdown 文件即可
- **VS Code**：安装 [Markdown Preview Mermaid Support](https://marketplace.visualstudio.com/items?itemName=bierner.markdown-mermaid) 插件
- **Typora / Obsidian**：内置支持
- **在线**：复制到 [Mermaid Live Editor](https://mermaid.live/) 查看和导出

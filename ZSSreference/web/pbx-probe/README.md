# API Server Probe

基于 React/Vite 的 SimulSpeak 调试探针，用于连接运行中的 `api-server` 前端 WebSocket，观察信令、媒体链路和 AI 字幕链路。

## 启动

```bash
cd web/pbx-probe
npm install
npm run dev
```

另起后端：

```bash
go run ./cmd/api-server        # 默认监听 0.0.0.0:8080
go run ./cmd/pbx-node
```

打开 `http://127.0.0.1:5173`，WebSocket 地址填写 api-server 的 `/ws`：

```text
ws://127.0.0.1:8080/ws?token=tenant-a:react-probe
```

探针不直接连接 `pbx-node` 的 `/pbx/ws`。PBX 控制通道由 `api-server` 内部连接和转发。

探针在浏览器控制台输出 WebSocket 消息、SDP/ICE 事件、WebRTC 状态、上行 RTP 统计、音频电平、ASR 事件、翻译事件、TTS 命令回执与播放状态。

## Provider 配置

探针不再在 `client_hello` 中发送 provider 配置或云服务密钥。ASR/TMT/TTS/LLM 的 provider、endpoint、AppID、SecretID、SecretKey 等由后端 `.env` 提供，前端只发送 `responseMode`、`translateStrategy`、`dubbing` 和 WebRTC 信令。

## 音频源

- `浏览器生成音`：用 Web Audio 生成正弦音并作为 RTP 发送，无需麦克风权限。
- `麦克风`：发送所选输入设备音频并显示本地电平。

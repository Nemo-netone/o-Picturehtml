# Mock WS 服务端（联调基座）

不依赖真实后端,按标准回放序列把 `asr_result` / `translation_result` 帧按时序
回推给前端,用于 M1–M3 独立推进(conventions §5)。帧格式逐字遵循
[`docs/05-interfaces.md`](../../docs/05-interfaces.md) A 节。

## 运行

```bash
pnpm mock           # ws://localhost:8080/ws
pnpm mock 8099      # 自定义端口
```

把前端 `.env` 的 `VITE_WS_URL` 指向该地址即可联调。

## 行为

- 连接(`/ws?token=tenantId:clientId`)即回 `connected`。
- 收到 `client_hello` → 回 `client_hello_ack`,并开始回放默认 fixture。
- `ping` → `pong`;`set_strategy` → `set_strategy_ack`;`webrtc_offer` → 占位
  `webrtc_answer`(PR-7 不接真实媒体)。

## Fixtures

`fixtures/one-utterance.json` 是**唯一基准**回放序列(一段英文一句),覆盖完整
状态机:asr partial→final、tmt 快翻(多帧,验证节流)→deepseek final,且含一句
`revised=true`(验证「✦已纠正」高亮)。每帧带 `delayMs`(相对上一帧间隔)以复现
时序。该序列同时作为 `state/subtitles` 渲染规则单测与 M3 手动验收的基准,避免两套
口径。`mock/fixtures.test.ts` 校验其形状与协议一致。

# 05 · 接口与数据契约

## 1. 外部模型列表接口

```http
GET {baseUrl}/v1/models
Authorization: Bearer {apiKey}
```

兼容响应：

```json
{
  "data": [
    { "id": "model-name" }
  ]
}
```

也兼容数组形式：

```json
[
  { "id": "model-name" },
  "another-model"
]
```

## 2. 外部图片生成接口

```http
POST {baseUrl}/v1/responses
Authorization: Bearer {apiKey}
Content-Type: application/json
Accept: text/event-stream
```

文生图 payload：

```json
{
  "model": "model-name",
  "input": [
    { "role": "system", "content": "..." },
    { "role": "user", "content": "请生成以下描述的图片：..." }
  ],
  "tools": [
    {
      "type": "image_generation",
      "output_format": "png",
      "size": "1024x1024",
      "quality": "standard",
      "style": "natural"
    }
  ],
  "tool_choice": { "type": "image_generation" },
  "stream": true
}
```

图生图 payload 的 user content 包含：

```json
[
  { "type": "input_image", "image_url": "data:image/jpeg;base64,..." },
  { "type": "input_text", "text": "请根据以下要求..." }
]
```

## 3. 响应解析

生成请求必须固定使用用户当前选择的 `model`。失败重试只允许重试同一个模型，不得自动切换到模型列表里的其它模型，避免实际请求模型和页面选择不一致。

生成请求显式携带 `tool_choice: { "type": "image_generation" }`，要求 Responses API 调用图片工具。如果流式响应只出现 `response.output_text.*` 而没有图片数据，前端按“当前模型没有返回图片”处理，并提示用户确认该模型是否支持 `image_generation`。

`app.js` 会递归查找：

- `data:image/*;base64,...`
- `b64_json`
- `image_base64`
- `base64`
- `result`
- `output`
- `content`

找到图片后统一转成 `data:image/png;base64,...`。

流式响应逐行解析 `event:` 和 `data:`；如果响应最后一段没有换行，尾部 buffer 也会进入同一解析路径，避免图片数据被留在未处理缓冲区。

## 4. localStorage

| Key | 数据 | 说明 |
|-----|------|------|
| `img_gen_api_configs` | `ApiConfig[]` | API 配置列表 |
| `img_gen_active_api` | `string` | 当前配置 ID |
| `img_gen_prompt_history` | `string[]` | 提示词历史 |
| `img_gen_image_params` | `ImageParams` | 尺寸、质量、风格 |
| `img_gen_auto_download` | `"true" / "false"` | 自动下载开关 |
| `img_gen_guide_shown` | `"true"` | 是否关闭过引导 |

## 5. IndexedDB

数据库：`img-gen-gallery`
版本：`1`
对象仓库：`records`
主键：`id`

记录结构：

```json
{
  "id": 1780000000000,
  "dataUrl": "data:image/png;base64,...",
  "prompt": "用户提示词或增强提示词",
  "mode": 1,
  "refDataUrl": null,
  "createdAt": "2026-06-19T09:00:00.000Z",
  "time": "2026/6/19 09:00:00",
  "params": {
    "size": "1024x1024",
    "quality": "standard",
    "style": "natural"
  }
}
```

`mode = 1` 表示文生图，`mode = 2` 表示图生图。

## 6. 导入导出 JSON

```json
{
  "version": "1.0",
  "exportDate": "2026-06-19T09:00:00.000Z",
  "gallery": [],
  "apiConfigs": [],
  "activeApiId": "default-openai",
  "promptHistory": [],
  "imageParams": {},
  "autoDownload": false
}
```

导入会覆盖当前本地数据；导入前必须确认。

导入后会校验 `activeApiId` 是否存在于导入后的 `apiConfigs`。如果不存在，自动回退到第一项配置，避免当前配置显示和生成表单失配。

## 7. ZIP 批量下载

批量下载不依赖第三方 CDN。`app.js` 在浏览器内生成 ZIP：

- 图片保存到 `ai-generated-images/*.png`。
- 每张图附带同名 `.json` 元数据文件。
- ZIP 条目使用 UTF-8 文件名和 store 模式，不做压缩。

这样可以避免 CloudBase 静态托管或国内网络下外部 JSZip 脚本加载失败。

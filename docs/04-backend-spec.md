# 04 · 后端/集成边界说明

本项目没有自有长驻后端。这里记录浏览器与外部服务、浏览器本地能力、Cloudflare Pages Functions 代理之间的“后端边界”，避免后续 AI 误以为需要新增服务器。

## 1. 模块划分

| 模块 | 位置 | 职责 |
|------|------|------|
| 外部图片生成服务 | 用户配置的 Base URL | 提供 `/v1/models` 和 `/v1/responses` |
| Cloudflare API 代理 | `functions/v1/[[path]].js` | 部署后把同源 `/v1/*` 转发到用户配置的上游 Base URL，降低 CORS 风险 |
| Cloudflare 媒体代理 | `functions/__picture_media.js` | 部署后代理远程图片下载，便于保存到浏览器图库 |
| 浏览器本地 KV | localStorage | 保存 API 配置、当前配置、提示词历史、图片参数 |
| 浏览器对象库 | IndexedDB | 保存图片记录和 Base64 图片 |
| 文件系统出口 | 下载链接 | 导出 JSON、单图下载、ZIP 下载 |
| 图片预处理 | Canvas + FileReader | 参考图压缩为 data URL |

## 2. 服务编排

```text
生成请求
  -> normalizeBaseUrl()
  -> buildImagePayload()
  -> createApiRequest()
  -> fetch(部署环境同源 /v1/responses 或本地 baseUrl + /v1/responses)
  -> readImageResponse()
  -> extractImageDataUrl()
  -> saveGalleryRecord()
```

## 3. 错误矩阵

| 故障 | 处理 |
|------|------|
| Base URL 缺失或格式错误 | 阻止生成并提示 |
| API Key 缺失 | 阻止生成并提示 |
| `/v1/models` CORS/网络失败 | 显示诊断建议，不影响手动输入模型 |
| `/v1/responses` HTTP 错误 | 记录事件，批量生成继续下一张 |
| Pages Functions 未部署 | `/v1/*` 或 `/__picture_media` 预检不是 204；需要确认根目录 `functions/` 已参与部署 |
| 流结束但无图片 | 按失败计数处理 |
| IndexedDB 写入失败 | 显示错误，不阻断当前结果显示 |
| SSE 最后一行无换行 | 尾部 buffer 继续走统一流式解析，避免漏读图片 |
| ZIP 下载 | 使用浏览器内置 ZIP 生成逻辑，不加载 JSZip CDN |

## 4. 配置

没有项目级业务环境变量。所有 API 配置来自页面表单并保存在浏览器本地。Cloudflare 部署认证只允许存在于本机命令环境或 Cloudflare 控制台，不写入仓库。

敏感项：API Key 只保存在用户浏览器 localStorage，不进入仓库、不进入文档示例。

## 5. 测试策略

- 用 `node --check assets/js/app.js` 做语法检查。
- 用 `node --check cloudbase-app/assets/js/app.js` 检查 CloudBase 发布副本。
- 用 `node --check functions/v1/[[path]].js` 和 `node --check functions/__picture_media.js` 检查 Cloudflare Functions。
- 用浏览器打开 `index.html` 检查初始化、Tab、数据管理和配置管理。
- 外部 API 功能需要用户提供真实 Base URL、API Key、Model 后人工验收。

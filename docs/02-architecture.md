# 02 · 架构说明

## 1. 分层结构

| 层 | 文件/区域 | 职责 |
|----|-----------|------|
| 文档层 | `README.md`、`docs/`、`CLAUDE.md` | 记录事实源、接手规则、验收 |
| 页面结构层 | `index.html` | 声明页面区域、控件 ID、无业务逻辑 |
| 样式层 | `assets/css/app.css` | 视觉、布局、响应式、状态样式 |
| 应用逻辑层 | `assets/js/app.js` | 状态、事件、API、本地存储、渲染 |
| Cloudflare Functions 层 | `functions/` | Pages 部署时提供 `/v1/*` 和 `/__picture_media` 同源代理 |
| 本地运行层 | `scripts/start-local.ps1` | Windows 本地启动静态服务、选择可用端口、打开浏览器 |
| 发布副本层 | `cloudbase-app/` | 静态托管发布副本，包含入口、样式、脚本和函数副本 |
| 浏览器能力层 | localStorage、IndexedDB、Clipboard、FileReader、Canvas | 本地保存、图片压缩、复制、下载 |
| 外部服务层 | OpenAI-compatible API | 模型列表和图片生成 |

## 2. 运行链路

```text
页面加载
  -> 缓存 DOM
  -> 加载 localStorage 配置/历史/参数
  -> 打开 IndexedDB 读取展馆
  -> 绑定事件
  -> 渲染 API 配置、展馆、统计、网络状态

点击开始生成
  -> 校验 active API 配置、Base URL、API Key、Model、提示词
  -> 按有无参考图选择文生图/图生图
  -> 构造 /v1/responses 请求
  -> 部署环境下经 Cloudflare Pages Functions 同源代理到用户配置 Base URL
  -> 读取 SSE 或 JSON 响应
  -> 提取 data:image/png;base64
  -> 渲染结果卡片
  -> 写入 IndexedDB
  -> 更新展馆和数据统计
```

## 3. 数据流

| 数据 | 来源 | 存储 | 消费方 |
|------|------|------|--------|
| API 配置 | 用户表单 | localStorage `img_gen_api_configs` | 生成、模型获取、网络诊断 |
| 当前配置 ID | 用户点击启用 | localStorage `img_gen_active_api` | 当前配置展示、隐藏生成表单 |
| 提示词历史 | 成功生成 | localStorage `img_gen_prompt_history` | 历史面板 |
| 图片参数 | 尺寸/质量/风格控件 | localStorage `img_gen_image_params` | 请求 payload |
| 图片记录 | 生成成功 | IndexedDB `img-gen-gallery.records` | 展馆、预览、导出、下载 |
| 参考图片 | 文件选择 | 内存 `state.refImages` | 图生图请求 |
| 本地服务端口 | `scripts/start-local.ps1` 参数或自动探测 | PowerShell 进程内 | 浏览器访问静态页面 |
| 发布副本 | 根目录静态源文件和函数副本 | `cloudbase-app/` | CloudBase/Cloudflare 静态托管 |

## 4. 关键边界

- `index.html` 只放结构，不写业务脚本。
- `app.css` 不依赖 JavaScript 变量；状态通过 class 和属性表达。
- `app.js` 不把 API Key 打到控制台，不写入项目文件。
- `scripts/start-local.ps1` 只负责本地静态服务，不读取或写入 API Key。
- `cloudbase-app/` 是发布副本，不是新的事实源；源文件修改后必须同步复制。
- `functions/` 是 Cloudflare Pages Functions 的部署入口；`cloudbase-app/functions/` 是发布副本，二者需同步。
- 外部 API 地址必须经过 `normalizeBaseUrl()` 处理，避免重复 `/v1`。
- IndexedDB 写入失败不能阻断页面其它区域渲染，但要显示状态。

## 5. 已知历史 bug 根因

| 根因 | 旧表现 | 新边界 |
|------|--------|--------|
| 缺少统一状态提示函数 | 多处 `showStatus()` 调用时报 `ReferenceError` | `showStatus()` 是公共 UI 工具 |
| 函数名漂移 | `renderApiConfigList()` 不存在 | 统一 API 配置渲染函数名 |
| 直接覆盖按钮 `textContent` | 内部 span 被删除，后续查询失败 | `setGenerateButtonState()` 只改 `.gen-btn-text` |
| 外部资源缺失 | `card-back.png` 404 | 卡背由 CSS 生成 |
| 流式响应尾行未解析 | SSE 最后一行无换行时图片数据可能留在 buffer | `processImageStreamLine()` 统一解析普通行和尾部 buffer |
| 批量下载依赖外部 CDN | CloudBase/国内网络下 JSZip 加载失败会让批量下载不可用 | 内置 ZIP 生成逻辑，不加载外部脚本 |
| 导入状态缺少一致性校验 | `activeApiId` 可能指向不存在配置 | 导入后校验 ID，失败则回退第一项配置 |
| 重试时自动切换候选模型 | 实际请求模型和用户选择不一致 | 生成链路只重试当前选择模型，不从模型列表自动降级 |
| 模型只返回文本 | 日志大量 `response.output_text.*`，最终没有图片 | 请求显式 `tool_choice: image_generation`；只回文字时提示模型不支持图片工具 |

## 6. 验证边界

- 语法门禁：`node --check assets/js/app.js`。
- 发布副本语法门禁：`node --check cloudbase-app/assets/js/app.js`。
- Cloudflare Functions 语法门禁：`node --check functions/v1/[[path]].js` 和 `node --check functions/__picture_media.js`。
- 手动门禁：`docs/frontend/mvp.md` 的验收清单。
- 浏览器门禁：加载页面无控制台初始化错误，Tab、配置、展馆、数据管理可点击。
- 本地运行门禁：`.\scripts\start-local.ps1 -NoBrowser` 能启动静态服务；端口冲突时自动后移。

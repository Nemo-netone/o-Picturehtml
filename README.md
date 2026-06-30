# o-Picturehtml

纯前端 AI 图片生成工具。项目从 `image-gen-refactor.html` 的单文件原型重构而来，保留原有“画图 + 图片展馆”双页体验，拆出可维护的 HTML、CSS、JavaScript 与接手文档。

## 系统总览

```text
用户
  -> index.html 页面
  -> assets/js/app.js 状态与业务逻辑
  -> 浏览器 localStorage / IndexedDB 保存配置、历史、图片记录
  -> 外部 OpenAI-compatible Responses API /v1/responses 生成图片
  -> 图片结果渲染到结果区，并保存到展馆
```

## 借鉴来源

本项目使用本仓库的 `Onezzr/ssr` 文档驱动方法重构，借鉴点如下：

| 借鉴来源 | 本项目落点 |
|----------|------------|
| `ZSSreference/README.md` 的项目门面结构 | 本 README 的总览、数据流、结构、运行、文档索引 |
| `ZSSreference/docs/01-requirements.md` | `docs/01-requirements.md` 的 FR/NFR、范围、验收 |
| `ZSSreference/docs/02-architecture.md` | `docs/02-architecture.md` 的分层、数据流、边界 |
| `ZSSreference/docs/03-frontend-spec.md` | `docs/03-frontend-spec.md` 的页面、组件、状态、交互说明 |
| `ZSSreference/docs/04-backend-spec.md` | `docs/04-backend-spec.md` 说明本项目无自有后端，只有浏览器集成边界 |
| `ZSSreference/docs/05-interfaces.md` | `docs/05-interfaces.md` 的外部 API、本地存储、导入导出契约 |
| `ZSSreference/docs/frontend/mvp.md` | `docs/frontend/mvp.md` 的里程碑、验收、修复记录 |
| `ZSSreference/docs/frontend/conventions.md` | `docs/frontend/conventions.md` 的可检查前端约束 |

## 项目结构

```text
.
├── index.html                     # 新版应用入口
├── image-gen-refactor.html         # 原单文件原型，保留作参考
├── assets/
│   ├── css/app.css                 # 视觉、布局、响应式、动效
│   └── js/app.js                   # API 配置、生成、展馆、导入导出、诊断
├── functions/                      # Cloudflare Pages Functions 部署入口
│   ├── __picture_media.js           # 图片资源代理
│   └── v1/[[path]].js               # OpenAI-compatible /v1/* 代理
├── cloudbase-app/                  # CloudBase 静态托管发布副本，不含密钥
│   ├── index.html
│   ├── functions/
│   └── assets/
│       ├── css/app.css
│       └── js/app.js
├── docs/
│   ├── README.md                   # 文档入口和阅读顺序
│   ├── 01-requirements.md          # 需求事实源
│   ├── 02-architecture.md          # 架构事实源
│   ├── 03-frontend-spec.md         # 前端规格
│   ├── 04-backend-spec.md          # 浏览器集成/后端边界说明
│   ├── 05-interfaces.md            # 接口和数据契约
│   └── frontend/
│       ├── mvp.md                  # 执行计划和验收
│       └── conventions.md          # 前端实现规范
└── CLAUDE.md                       # AI 接手规则
```

## 核心功能

- API 配置管理：新增、编辑、删除、启用、模型拉取。
- 文生图：调用 OpenAI-compatible `/v1/responses`，使用 `image_generation` 工具。
- 批量生成：支持 1/3/5/10/20/50 张，逐张生成、进度统计、取消；第 1 张使用原提示词，第 2 张起构造不同镜头、氛围、色彩和风格的多维增强提示词。
- 提示词历史：本地保存、复用、置顶、删除。
- 图片展馆：IndexedDB 保存生成记录，支持卡牌/普通模式、排序、分组、筛选、预览，提示词默认折叠。
- 背景展示：默认保留原背景图，支持顶部上传自定义背景；页面不再叠加全局暗遮罩，双击空白处可隐藏 UI 只看背景原图。
- 数据管理：导出/导入全部数据、批量下载图片、清除所有图片、清空数据、自动下载开关。
- 网络状态：底部轻量显示在线状态和延迟。

## 已修复的旧原型问题

| 旧问题 | 新实现 |
|--------|--------|
| 调用 `showStatus()` 但没有定义，导入/导出等操作会报错 | 新增统一 `showStatus(type, message)` |
| 调用不存在的 `renderApiConfigList()` | 统一使用 `renderApiConfigs()` |
| 图生图模式用 `genBtn.textContent` 覆盖按钮内部结构 | 使用 `setGenerateButtonState()` 只更新按钮文本节点 |
| 展馆卡背依赖缺失的 `card-back.png` | 改为 CSS 生成卡背视觉，不依赖外部图片 |
| 单文件 8000 多行，后续 AI 难接手 | 拆分入口、样式、逻辑和 docs 事实源 |
| SSE 最后一行没有换行时可能漏读图片数据 | 用 `processImageStreamLine()` 统一解析普通行和尾部 buffer |
| 批量下载依赖 CDN JSZip，CloudBase/国内网络下可能加载失败 | 改为内置 ZIP 生成，不再依赖外部脚本 |
| 导入数据的 `activeApiId` 可能指向不存在配置 | 导入后校验当前配置 ID，不存在则回退到第一项配置 |
| 生成图片时反复重建隐藏图库 DOM，图库较大时页面滚动卡顿 | 画图页只更新图库计数和统计；进入图库页才渲染网格，离开图库页卸载隐藏图片节点 |

## 快速开始

本项目是纯静态前端，直接打开 `index.html` 即可使用。为了避免部分浏览器限制本地文件能力，推荐启动本地静态服务：

Windows 推荐使用本仓库脚本，它默认从 `5188` 开始自动寻找可用端口并打开浏览器：

```powershell
.\scripts\start-local.ps1
```

如果不想自动打开浏览器：

```powershell
.\scripts\start-local.ps1 -NoBrowser
```

也可以手动启动静态服务：

```powershell
python -m http.server 5173
```

然后访问：

```text
http://127.0.0.1:5173/
```

如果 5173 被占用或浏览器无法打开，换一个端口即可，例如：

```powershell
python -m http.server 5188 --bind 127.0.0.1
```

## 配置

在页面的“API 配置管理”中填写：

| 字段 | 说明 |
|------|------|
| 配置名称 | 本地显示名 |
| Base URL | OpenAI-compatible 服务地址，例如 `https://api.openai.com` |
| API Key | 用户自己的密钥，只保存在浏览器本地 |
| Model | 支持 `image_generation` 工具的模型 |

安全边界：本项目是纯前端，API Key 会存入当前浏览器 localStorage。不要在公共电脑或不可信浏览器保存密钥。

## 常用检查

```powershell
node --check assets/js/app.js
node --check cloudbase-app/assets/js/app.js
```

当前已完成的自动检查记录见 `docs/frontend/mvp.md` 的“验证记录”。浏览器交互和真实 API 生成仍以 `docs/frontend/mvp.md` 的手动验收清单为准。

## CloudBase 发布准备

发布副本位于 `cloudbase-app/`，当前任务只准备目录，不执行发布。源文件更新后重新复制到发布副本：

```powershell
Copy-Item -Force -LiteralPath index.html -Destination cloudbase-app\index.html
Copy-Item -Force -LiteralPath assets\css\app.css -Destination cloudbase-app\assets\css\app.css
Copy-Item -Force -LiteralPath assets\js\app.js -Destination cloudbase-app\assets\js\app.js
```

发布命令示例：

```powershell
tcb hosting deploy .\cloudbase-app -e <你的环境ID>
```

安全边界：`cloudbase-app/` 也不得写入 API Key、token、Cookie 或其它密钥。

## Cloudflare Pages 发布

Cloudflare Pages 项目名固定为 `o-picturehtml`，生产分支固定为 `main`，稳定访问域名为：

```text
https://o-picturehtml.pages.dev
```

部署目录由 `wrangler.toml` 指定为 `cloudbase-app/`。根目录 `functions/` 是 Wrangler Pages 识别 Functions 的入口；如果更新代理函数，需要同步根目录 `functions/` 和 `cloudbase-app/functions/`。

```powershell
wrangler pages deploy cloudbase-app --project-name o-picturehtml --branch main
```

部署认证只允许通过 Cloudflare 控制台、Wrangler 登录态或当前 Shell 的 `CLOUDFLARE_API_TOKEN` 环境变量提供；不得把 Token、API Key、SecretId、SecretKey 写入仓库或文档。如果 Token 已出现在聊天记录、日志或截图中，部署完成后应在 Cloudflare 控制台轮换。

最近一次生产部署记录：

| 日期 | 分支 | 结果 | 验证 |
|------|------|------|------|
| 2026-06-30 | `main` | `https://ef935115.o-picturehtml.pages.dev` 部署完成，稳定域名 `https://o-picturehtml.pages.dev` 可访问 | 首页 `HEAD` 返回 200；`/v1/models` 与 `/__picture_media` 的 `OPTIONS` 返回 204 |

## PR 与贡献规则

- 采用 `Onezzr/ssr` 文档驱动流程，代码变化后同步更新对应事实源文档。
- 一个 PR 只做一个清晰目标，主分支始终保持可运行。
- PR 描述使用 [.github/pull_request_template.md](./.github/pull_request_template.md)。
- 真实外部 API、计费、限流、CORS 等必须在 PR 里标注是否已验证。

## 文档索引

建议后续 AI 按顺序阅读：

1. `CLAUDE.md`
2. `docs/README.md`
3. `docs/01-requirements.md`
4. `docs/02-architecture.md`
5. `docs/03-frontend-spec.md`
6. `docs/05-interfaces.md`
7. `docs/frontend/conventions.md`
8. `docs/frontend/mvp.md`

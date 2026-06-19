# 前端重构 MVP

## 0. 进度看板

**整体进度：6 / 6 里程碑**

| 里程碑 | 状态 | 验收 |
|--------|------|------|
| M0 文档事实源 | 已完成 | README、docs、CLAUDE 已建立 |
| M1 静态结构拆分 | 已完成 | `index.html`、`assets/css/app.css`、`assets/js/app.js` |
| M2 核心生成功能复刻 | 已完成 | API 配置、文生图、图生图、批量生成 |
| M3 展馆和数据管理复刻 | 已完成 | IndexedDB、筛选、导入导出、批量下载 |
| M4 旧 bug 修复 | 已完成 | `showStatus`、函数名、按钮结构、卡背资源 |
| M5 CloudBase 发布准备和二次审计 | 已完成 | `cloudbase-app/`、DOM 对照、SSE 尾行、ZIP 下载、导入状态 |

## 1. MVP 目标

用可维护静态前端复刻旧 HTML 的界面和功能，同时修复已知 bug 并补齐接手文档。

## 2. 里程碑到文件映射

| 里程碑 | 文件 |
|--------|------|
| M0 | `README.md`、`docs/*`、`CLAUDE.md` |
| M1 | `index.html`、`assets/css/app.css` |
| M2 | `assets/js/app.js` 的 API、生成、参考图、提示词模块 |
| M3 | `assets/js/app.js` 的 gallery、data manager、preview 模块 |
| M4 | `assets/js/app.js` 的公共 UI、渲染函数、按钮状态 |
| M5 | `cloudbase-app/*`、`assets/js/app.js` 的响应解析、ZIP 下载、导入状态校验 |

## 3. 任务/PR 粒度映射

本仓库使用 GitHub PR 流程。每个 PR 只做一个清晰目标，并保持合并后主分支可运行：

| 任务 | 范围 | 验证 |
|------|------|------|
| T1 文档事实源 | `README.md`、`docs/*`、`CLAUDE.md` | 文档层级完整、无 TODO 占位、无密钥 |
| T2 静态页面结构 | `index.html` | 关键 DOM ID 与 `app.js` 缓存一致 |
| T3 样式与响应式 | `assets/css/app.css` | 画图/展馆布局可用，按钮文字不溢出 |
| T4 API 配置与生成 | `assets/js/app.js` 的配置、模型、生成链路 | `node --check` + 配置/生成手动验收 |
| T5 展馆与数据管理 | `assets/js/app.js` 的 IndexedDB、导入导出、下载 | `node --check` + 展馆/数据手动验收 |
| T6 历史 bug 修复 | 公共 UI、渲染函数、按钮状态 | 对照“已修 bug 清单”逐项验证 |
| T7 发布准备和二次审计 | `cloudbase-app/`、`README.md`、`docs/*`、`assets/js/app.js` | 发布目录存在、语法检查通过、DOM ID 对照无缺失 |

每次只改一个任务范围；跨任务时先在本文件新增拆分说明。

## 3.1 PR 映射

| PR | 范围 | 状态 | 验证 |
|----|------|------|------|
| PR-1 `重构静态图片生成工具并准备 CloudBase 发布目录` | T1-T7 首次重构交付：拆分 HTML/CSS/JS、修复历史 bug、建立 SSR 文档栈和 `cloudbase-app/` | 待创建 | 见“验证记录” |

## 4. 已修 bug 清单

- [x] 补齐 `showStatus()`。
- [x] 把 `renderApiConfigList()` 改为真实存在的 `renderApiConfigs()`。
- [x] 图生图和重置状态不再用 `genBtn.textContent` 覆盖按钮内部结构。
- [x] 卡牌背面不再依赖不存在的 `card-back.png`。
- [x] 导入数据后重新渲染 API 配置、展馆、历史、统计。
- [x] 清空数据后重新初始化默认 API 配置。
- [x] SSE 最后一行没有换行时继续解析尾部 buffer，避免漏读图片。
- [x] 批量下载不再依赖 JSZip CDN，改为内置 ZIP 生成。
- [x] 导入数据后校验 `activeApiId` 是否存在，不存在则回退第一项配置。
- [x] 建立 `cloudbase-app/` 静态发布副本，暂不发布。

## 5. 手动验收清单

- [ ] 打开 `index.html` 无初始化报错。
- [ ] 点击“图片展馆”和“画图”能切换。
- [ ] 添加 API 配置后可以启用，当前配置显示正确。
- [ ] 模型列表获取失败时显示可理解错误。
- [ ] 快速示例能写入提示词。
- [ ] 上传参考图片能显示缩略图并可删除。
- [ ] 未填必要字段时点击生成会被阻止。
- [ ] 生成成功后图片进入结果区和展馆。
- [ ] 导出所有数据能下载 JSON。
- [ ] 清空数据后页面计数归零并恢复默认配置。

## 6. 验证记录

| 日期 | 类型 | 命令/方式 | 结果 | 备注 |
|------|------|-----------|------|------|
| 2026-06-19 | 自动语法检查 | `node --check assets/js/app.js` | 通过 | 只能证明 JS 语法有效，不等同于浏览器交互和真实 API 通过 |
| 2026-06-19 | 文档/skill 检查 | `Onezzr/ssr` 文档栈核对 | 通过基础结构 | README、docs/01-05、mvp、conventions、CLAUDE 均已建立 |
| 2026-06-19 | DOM/资源静态审计 | `node E:\tmp\audit-o-picturehtml.js E:\twentySixGitHub\ThreeStandard\o-Picturehtml` | 通过 | 97 个 JS 缓存 ID 均存在；无重复 ID；CSS/JS 引用存在；无旧函数名/卡背资源残留 |
| 2026-06-19 | 发布副本静态审计 | `node E:\tmp\audit-o-picturehtml.js E:\twentySixGitHub\ThreeStandard\o-Picturehtml\cloudbase-app` | 通过 | CloudBase 副本的 DOM ID 和静态资源路径一致 |
| 2026-06-19 | 发布副本语法检查 | `node --check cloudbase-app/assets/js/app.js` | 通过 | 发布目录 JS 与源目录同步后通过 |
| 2026-06-19 | 静态服务 HTTP 检查 | `python ...with_server.py --server "python -m http.server 5188 --bind 127.0.0.1" ... http-check-port.py` | 通过 | 根目录和 `cloudbase-app/` 均返回 200，关键文本存在；5173 在本机环境下被占用或拦截 |
| 2026-06-19 | Chrome headless file 检查 | `python E:\tmp\chrome-file-check.py` | 通过 | 根目录和 `cloudbase-app/` 的 `file://` 加载均渲染关键文本，并生成截图 |
| 2026-06-19 | 浏览器 Playwright 尝试 | `python ...with_server.py ... Playwright` | 未执行成功 | 本机缺少 Python `playwright` 包；未因此引入项目依赖 |

浏览器交互和真实外部 API 仍按上方手动验收清单执行；未提供真实 Base URL、API Key、Model 时，不勾选生成链路相关项。

## 7. 风险

| 风险 | 对策 |
|------|------|
| 不同 OpenAI-compatible 服务返回图片字段不同 | `extractImageDataUrl()` 递归兼容多种字段 |
| 浏览器 CORS 限制 | 网络诊断显示建议，必要时用户换支持 CORS 的服务 |
| API Key 存在 localStorage 风险 | README 和 docs 标注安全边界 |
| 无真实 API Key 无法全自动验收生成 | 保留语法检查和手动验收清单 |
| 本机缺少 Playwright Python 包 | 已做静态 DOM/资源审计、HTTP 检查和 Chrome headless file 检查；完整交互自动化仍需安装 Playwright 后补跑 |
| 5173 端口在本机环境异常关闭连接 | 验证时改用 5188 端口；README 仍保留 5173 示例，若本机冲突可换端口 |

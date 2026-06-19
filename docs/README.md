# o-Picturehtml · 设计文档

本目录是 o-Picturehtml 的文档事实源，使用 `Onezzr/ssr` 的文档驱动开发方式组织。后续 AI 接手时先读本文，再按任务类型进入对应文档。

## 阅读顺序

1. [01-requirements.md](./01-requirements.md)：项目范围、功能需求、非功能需求、验收标准。
2. [02-architecture.md](./02-architecture.md)：文件分层、运行链路、数据流、边界。
3. [03-frontend-spec.md](./03-frontend-spec.md)：页面结构、组件职责、状态模型、交互规则。
4. [04-backend-spec.md](./04-backend-spec.md)：本项目无自有后端，记录浏览器与外部服务的集成边界。
5. [05-interfaces.md](./05-interfaces.md)：外部 API、本地存储、IndexedDB、导入导出契约。
6. [frontend/mvp.md](./frontend/mvp.md)：重构里程碑、已修复 bug、验收清单。
7. [frontend/conventions.md](./frontend/conventions.md)：前端代码和样式规范。
8. [../cloudbase-app/README.md](../cloudbase-app/README.md)：CloudBase 静态托管副本说明。
9. [../.github/pull_request_template.md](../.github/pull_request_template.md)：PR 事实源和验证模板。

## 技术栈

| 模块 | 技术 | 职责 |
|------|------|------|
| 页面入口 | HTML5 | 提供语义化结构、控件、状态区域 |
| 样式 | CSS3 | 深色玻璃态界面、响应式布局、卡牌/预览/表单状态 |
| 交互逻辑 | 原生 JavaScript | 状态管理、API 调用、IndexedDB、localStorage、导入导出 |
| 图片生成 | OpenAI-compatible Responses API | 通过 `/v1/responses` 和 `image_generation` 工具生成图片 |
| 本地数据 | localStorage + IndexedDB | 保存 API 配置、提示词历史、图片记录 |

## 文档边界

- 需求事实只改 `01-requirements.md`。
- 架构、文件职责、数据流只改 `02-architecture.md`。
- 页面/组件/状态/交互只改 `03-frontend-spec.md`。
- API、本地存储、导入导出格式只改 `05-interfaces.md`。
- 执行进度和验收结果只改 `frontend/mvp.md`。
- 代码规范只改 `frontend/conventions.md`。

## 关键术语

| 术语 | 含义 |
|------|------|
| 文生图 | 用户只输入提示词生成图片 |
| 图生图 | 用户上传参考图后，按提示词编辑或生成新图 |
| API 配置 | Base URL、API Key、Model 的本地组合 |
| 展馆 | IndexedDB 保存的生成记录页面 |
| 卡牌模式 | 展馆中用 CSS 卡背和翻转效果展示图片 |
| 记录 | 一条生成结果，包含图片、提示词、模式、时间、参数 |
| 发布副本 | `cloudbase-app/` 中可交给 CloudBase 静态托管的文件副本 |
| SSR 事实源 | README、docs、CLAUDE 和 conventions 中记录的可验证项目事实 |

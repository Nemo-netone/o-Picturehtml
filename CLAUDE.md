# 项目工作规范

本项目采用 `Onezzr/ssr` 文档驱动开发方式。所有较大改动都先确认事实源，再改代码；代码变化后同步更新对应文档。

## 前置阅读规则

修改本项目之前，先按顺序阅读：

1. `README.md`
2. `docs/README.md`
3. 与任务相关的事实源：
   - 需求或范围：`docs/01-requirements.md`
   - 架构或文件职责：`docs/02-architecture.md`
   - 页面、组件、交互：`docs/03-frontend-spec.md`
   - API、本地存储、导入导出：`docs/05-interfaces.md`
   - 代码风格：`docs/frontend/conventions.md`
4. 涉及项目规范、README、docs、验收、PR 拆分、架构边界时，额外阅读：
   - `Onezzr/ssr/SKILL.md`
   - `Onezzr/ssr/references/zssreference-patterns.md`

## 修改规则

1. 保留 `image-gen-refactor.html` 作为旧原型参考，除非用户明确要求删除。
2. 新实现以 `index.html`、`assets/css/app.css`、`assets/js/app.js` 为准。
3. `cloudbase-app/` 是 CloudBase 静态托管副本，不是新的事实源；源文件变化后必须同步复制。
4. 不把 API Key、token、Cookie 或其它密钥写入仓库。
5. 不新增构建工具，除非用户明确要求迁移到 React/Vite。
6. 修 bug 先定位根因，再改最小负责面。
7. 每次改动后更新相关 docs 或 MVP 状态。
8. 需求与文档事实源冲突时，先指出冲突并确认，不擅自偏离。
9. 文档里的“必须、禁止、不得”要尽量绑定到检查命令、手动验收或明确的审查点。

## PR / 任务粒度

- 每次任务只改一个清晰目标，避免把重构、样式、功能和文档混成一包。
- 较大工作先在 `docs/frontend/mvp.md` 增加里程碑或验收项。
- 交付说明必须包含：改了哪些事实源、验证了什么、还有哪些边界未验证。
- PR 描述必须使用 `.github/pull_request_template.md`，并勾选已同步的 SSR 事实源。
- 提交和 PR 标题使用中文，说明改动目标，不写含糊的“update/fix”。

## 默认 PR 描述结构

1. 变更内容：说明用户可感知功能、bug 修复或文档事实源变化。
2. SSR 事实源：列出已同步的 README、docs、CLAUDE 或 conventions。
3. 验证：列出实际运行过的命令和结果。
4. 未验证边界：真实 API、CORS、计费/限流、发布动作等未覆盖项必须写明。

## 默认验证

```powershell
node --check assets/js/app.js
node --check cloudbase-app/assets/js/app.js
```

无法真实调用外部图片生成 API 时，说明未验证的边界：真实 Base URL、API Key、Model、CORS、计费/限流。

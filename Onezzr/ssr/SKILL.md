---
name: ssr
description: Create or audit ZSSreference-style document-driven project specifications and execution contracts. Use when Codex needs to build, update, or review README structure, docs/01-05 design docs, CLAUDE.md workflow rules, MVP milestone plans, conventions, interface specs, architecture diagrams, PR traceability, source-document borrowing maps, or validation gates so a project follows a document-first development process.
---

# SSR 文档驱动开发

## 核心契约

把文档当成事实源，把代码当成文档的实现。动手前先读权威文档；写完后用验收清单、测试、lint、PR 模板或架构守护检查，让文档里的约束变成会被验证的契约。

不要照搬 ZSSreference 的同传业务名词；复用它的组织方法：编号递进、单一事实源、活文档看板、里程碑到 PR 映射、架构边界测试、表格化规范和验收门禁。

## 先读什么

1. 先检查当前仓库是否已有 README、`docs/`、`CLAUDE.md`、执行计划、conventions 或 ADR。
2. 如果任务超过一次小改，读取 `references/zssreference-patterns.md`，特别是“借鉴来源地图”和“工作流使用场景矩阵”。
3. 如果本 skill 位于 `Onezzr/ssr`，且 `../文档驱动开发规范.md` 存在，读取它作为完整学习笔记。
4. 若用户给了具体项目或参考文档，以用户给出的文件为最高优先级；发现冲突时先指出冲突，不擅自改写事实源。

## 借鉴来源纪律

使用 `ssr` 时要说明“借鉴了什么”，不要只说“参考了 ZSSreference”：

- 借鉴 README 时，点名 `ZSSreference/README.md` 的结构：系统总览、数据流、四层架构、项目结构、依赖声明、快速开始、配置、命令、测试、文档、贡献。
- 借鉴文档索引时，点名 `ZSSreference/docs/README.md` 的结构：技术栈表、四层架构、图片索引、阅读顺序、术语表。
- 借鉴前端规格时，点名 `ZSSreference/docs/03-frontend-spec.md` 的结构：页面目标、ASCII 页面布局、组件职责表、状态模型、渲染规则、交互流程、边界体验。
- 借鉴后端规格时，点名 `ZSSreference/docs/04-backend-spec.md` 的结构：模块划分、核心抽象、策略编排、业务接线、核心逻辑、并发生命周期、错误矩阵、配置、测试策略。
- 借鉴图表时，点名 `ZSSreference/docs/diagrams.md`、`docs/images/architecture.dot`、`docs/images/business-flow.dot`，保留图源码并生成 SVG/PNG。
- 借鉴执行节奏时，点名 `docs/frontend/mvp.md` 的活看板、In/Out、里程碑、PR 映射、决策、风险、验收。
- 借鉴规范时，点名 `docs/frontend/conventions.md` 的命名、分层、token、测试、mock、日志、安全、错误表。
- 借鉴流程门禁时，点名 `CLAUDE.md` 的前置阅读、单 PR 单功能、PR 四段式、主分支可运行。
- 借鉴架构守护时，点名 `internal/architecture/architecture_test.go` 的 forbidden import 扫描。

## 工作流选择

### 使用场景嵌入工作流

| 工作阶段 | 触发信号 | 先读 | 产出 | 收尾门禁 |
|----------|----------|------|------|----------|
| 立项 / 新仓库初始化 | 写项目规范、README、设计文档 | README、`docs/README`、`01-requirements` | 文档栈骨架 | 项目定位、范围、验收清楚 |
| 架构设计 | 画图、拆模块、定边界 | `02-architecture`、`diagrams.md`、DOT 图 | 架构文档和图 | 边界能被测试或 lint 守护 |
| 前端规格 | 做页面、组件、状态、交互、体验边界 | `03-frontend-spec` | 页面规格、组件职责、状态模型、交互流程 | UI 行为能被 mock/test/手动验收 |
| 后端规格 | 拆服务、写业务编排、生命周期、错误处理 | `04-backend-spec` | 后端模块说明、核心逻辑、错误矩阵、测试策略 | 主链路、降级、并发边界有验证 |
| 接口联调 | 定协议、API、mock | `05-interfaces` | 接口总表、字段表、示例、错误约定 | 示例能驱动 mock/test |
| MVP 拆分 | 拆任务、排 PR、做看板 | `docs/frontend/mvp.md` | 活看板、里程碑、PR 映射 | 单 PR 单功能且可运行 |
| 规范统一 | 命名、目录、日志、错误、测试 | `conventions.md` | 可检查规范 | 不写抽象口号 |
| AI 协作 | 写工作规则、让 AI 按规范执行 | `CLAUDE.md` | 前置阅读规则和 PR 模板 | AI 知道动手前读什么 |
| 交付展示 | 包装项目、说明原创性、画流程图 | README、`diagrams.md`、DOT 图 | 项目门面和图表 | 新人 5 分钟能理解并运行 |
| Review / 技术债 | 查文档过期、规范缺失、架构越界 | 全部权威文档和架构测试 | 缺口/冲突/门禁清单 | 每个问题指向事实源 |

### 新项目或缺少文档

按依赖顺序创建最小文档栈：

1. `README.md`：项目定位、系统总览、数据流、结构、依赖、快速开始、配置、测试、文档索引。
2. `docs/README.md`：阅读顺序、技术栈表、术语表、图表索引。
3. `docs/01-requirements.md`：元信息、背景、FR/NFR、In/Out、MoSCoW、验收标准。
4. `docs/02-architecture.md`：模块分层、职责边界、数据流、生命周期、禁止依赖关系。
5. `docs/03-frontend-spec.md`：页面目标、布局、组件职责、状态模型、渲染规则、交互流程、体验边界。
6. `docs/04-backend-spec.md`：模块划分、业务编排、核心逻辑、并发/生命周期、错误矩阵、配置、测试策略。
7. `docs/05-interfaces.md`：协议、HTTP/API、内部接口、数据模型、环境变量、错误约定。
8. 执行计划文档：进度看板、里程碑、每个里程碑的验收、里程碑到 PR 映射、风险表、决策表。
9. `conventions.md`：命名、目录分层、依赖方向、测试要求、mock 数据、日志、错误处理、安全。
10. `CLAUDE.md` 或项目工作规范：前置阅读规则、单 PR 单功能、PR 描述模板、主分支可运行。

### 已有项目需要升级规范

先建立“权威地图”：

- 标出每类事实唯一归属：需求、架构、接口、计划、命名、测试、流程。
- 找出重复定义、过期段落、互相矛盾的描述和无验收标准的约束。
- 只在事实源位置修改事实；其他文档改成相对链接或章节引用。
- 将关键架构边界转换为自动检查，至少写出可执行测试/lint 的规则设计。

### 编码任务

1. 先读该领域权威文档，确认 In/Out、目录结构、接口契约和验收标准。
2. 将需求追溯到 FR、里程碑或 PR 编号；找不到编号时补文档或说明缺口。
3. 若任务包含多个功能点，先拆成多个单功能 PR 计划。
4. 实现后逐条对照验收；通过才更新看板或状态。
5. 输出状态判断：通过、遗留、未达标，以及已运行的验证命令。

### Review 或审计

优先找这些问题：

- README 是否能让新人 5 分钟知道项目是什么、怎么跑、核心边界是什么。
- 需求是否有 FR/NFR、范围边界、优先级和可验收标准。
- 架构是否明确“谁负责什么、谁不能依赖谁”，并有测试或 lint 守护。
- 前端规格是否说明页面目标、组件职责、状态模型、渲染规则和交互边界。
- 后端规格是否说明模块职责、业务编排、生命周期、错误矩阵和测试策略。
- 接口是否有消息总表、字段表、示例、错误约定和环境变量。
- 执行计划是否有活看板、里程碑、PR 映射和门禁。
- conventions 是否是可检查规则，而不是“保持整洁”这类空话。

## 输出标准

写文档时使用以下格式习惯：

- 顶部一句话定位，必要时加元信息表。
- 结构化事实用表格；流程用编号；任务进度用复选框。
- 文件间用相对链接和章节锚，不复制同一事实。
- 每个重要需求、接口、边界、验收项都能被编号引用。
- 每个“必须、禁止、不得”都要尽量落到测试、lint、CI、PR 模板或人工验收清单。
- 每次输出都说明改了哪些事实源、哪些文件只是引用调整、哪些验证已完成。

## 验证门禁

完成前检查：

1. `SKILL.md` 或项目文档没有未替换的占位内容。
2. README、docs 索引、需求、架构、接口、计划、规约之间没有互相矛盾。
3. 需求到里程碑、PR、验收能追溯。
4. 架构边界有自动守护方案或明确的手动检查点。
5. 命令、端口、环境变量、密钥策略来自仓库事实；不要凭空编造。
6. 不把 token、API key、私密凭据写入文档或日志示例。

## 参考材料

读取 `references/zssreference-patterns.md` 获取详细模板、文档角色表和检查清单。

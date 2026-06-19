# 03 · 前端页面规格

## 1. 页面目标

保持旧原型的深色 AI 创作平台体验：左侧完成配置、提示词、生成和结果展示；右侧显示实时事件流；第二个 Tab 管理历史图片展馆。

## 2. 页面布局

```text
顶部 Tab
  ├─ 画图
  │   ├─ 主卡片
  │   │   ├─ 标题 / 引导
  │   │   ├─ API 配置管理
  │   │   ├─ 当前配置 / 网络状态
  │   │   ├─ 参考图片 / 参数 / 提示词 / 历史
  │   │   ├─ 生成数量 / 开始生成 / 进度
  │   │   └─ 结果网格 / 状态条
  │   └─ 实时事件流
  └─ 图片展馆
      ├─ 数据管理中心
      ├─ 展示控制
      └─ 展馆网格
```

## 3. 组件职责

| 组件 | DOM | 职责 |
|------|-----|------|
| 顶部 Tab | `.top-tab` | 切换画图和展馆 |
| API 管理器 | `apiConfigList`、`apiManagerPanel` | 配置 CRUD、模型拉取、启用 |
| 当前配置 | `currentApiName` | 显示生成实际使用的配置 |
| 网络状态卡 | `networkStatusText` 等 | 显示在线、延迟、诊断结果 |
| 参考图选择器 | `thumbRow`、`imageFile` | 多图上传、压缩、删除 |
| 提示词区域 | `prompt`、`promptHistoryPanel` | 输入、示例、历史 |
| 生成控制 | `genBtn`、`genProgress` | 批量生成、进度、取消 |
| 结果区 | `resultGrid` | 当前批次结果 |
| 展馆 | `galleryGrid` | 历史记录、预览、删除 |
| 预览遮罩 | `previewOverlay` | 图片查看、缩放、拖拽、键盘切换 |
| 数据管理 | `exportAllDataBtn` 等 | 导出、导入、批量下载、清空 |

## 4. 状态模型

```js
state = {
  apiConfigs: [],
  activeApiId: null,
  fetchedModels: [],
  selectedGenCount: 1,
  refImages: [],
  promptHistory: [],
  gallery: [],
  imageParams: { size, quality, style },
  generation: { active, cancelRequested, total, done, success, failed },
  galleryView: { displayMode, sortMode, groupByMode, groupByContent, activeFilters },
  preview: { open, index, scale, panX, panY }
}
```

## 5. 渲染规则

- 没有 API 配置时创建默认示例配置，但 API Key 为空。
- 启用配置后，隐藏的 `baseUrl/apiKey/model` 表单同步更新，生成逻辑只读这里。
- 有参考图时强制图生图，只生成一张。
- 生成失败不清空已成功图片；批量生成继续处理下一张。
- 展馆为空时显示空态；有记录时显示计数和筛选结果。
- 预览传入 URL 时隐藏上一张/下一张；传入索引时启用键盘切换。

## 6. 交互流程

### 6.1 文生图

1. 用户选择 API 配置。
2. 输入提示词，选择尺寸、质量、风格、数量。
3. 点击开始生成。
4. 页面逐张调用外部 API。
5. 成功图片进入结果区和展馆。

### 6.2 图生图

1. 用户上传参考图片。
2. 前端压缩到最大 1024 像素边长。
3. 点击开始生成。
4. 请求内容包含 `input_image` 和 `input_text`。
5. 成功后保存生成图和参考图。

### 6.3 数据管理

1. 导出：把 gallery、apiConfigs、promptHistory、imageParams、autoDownload 打包为 JSON。
2. 导入：校验 JSON 后覆盖本地数据，并重新渲染页面。
3. 清空：二次确认后清空 IndexedDB 和 localStorage。

## 7. 边界体验

- 所有危险操作需要确认。
- 网络错误给出原因和建议。
- 移动端主区域单列显示。
- 按钮禁用时仍保持原有 DOM 结构，避免下一次生成失败。

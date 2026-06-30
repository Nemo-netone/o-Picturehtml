# 03 · 前端页面规格

## 1. 页面目标

保持图片背景上的轻量玻璃创作工具体验：左侧完成配置、提示词、生成和结果展示；右侧显示实时事件流；第二个 Tab 管理历史图片展馆。

## 2. 页面布局

```text
顶部 Tab
  ├─ 画图
  ├─ 图片展馆
  ├─ 更换背景
  │   ├─ 主卡片
  │   │   ├─ 标题
  │   │   ├─ API 配置管理
  │   │   ├─ 当前配置
  │   │   ├─ 参数 / 提示词 / 历史
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
| 背景图设置 | `backgroundFileInput`、`changeBackgroundBtn`、`resetBackgroundBtn` | 上传自定义背景图、保存到本地、恢复默认背景 |
| API 管理器 | `apiConfigList`、`apiManagerPanel` | 配置 CRUD、模型拉取、启用 |
| 当前配置 | `currentApiName` | 显示生成实际使用的配置 |
| 网络状态条 | `networkStatusValue`、`networkPing` | 底部轻量显示在线状态和延迟 |
| 提示词区域 | `prompt`、`promptHistoryPanel` | 输入、示例、历史 |
| 生成控制 | `genBtn`、`genProgress` | 批量生成、进度、取消 |
| 结果区 | `resultGrid` | 当前批次结果、提示词默认隐藏查看 |
| 展馆 | `galleryGrid` | 历史记录、预览、提示词折叠查看、删除 |
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
  backgroundImage: '',
  generation: { active, cancelRequested, total, done, success, failed },
  galleryView: { displayMode, sortMode, groupByMode, groupByContent, activeFilters },
  preview: { open, index, scale, panX, panY }
}
```

## 5. 渲染规则

- 没有 API 配置时创建默认示例配置，但 API Key 为空。
- 默认背景继续使用 `assets/images/japanese-garden-bg.png`；不叠加全局暗遮罩，尽量保留背景原图色彩。
- 自定义背景图保存在浏览器 localStorage，页面加载时应用；恢复默认会删除对应本地配置。
- 双击页面空白处进入纯背景展示模式，隐藏所有 UI；再次双击或按 ESC 恢复。
- 启用配置后，隐藏的 `baseUrl/apiKey/model` 表单同步更新，生成逻辑只读这里。
- 当前画图页不再显示参考图片上传入口；如历史内存中存在参考图，生成逻辑仍按图生图兼容处理。
- 批量生成第 1 张使用用户原始提示词；第 2 张起构造结构化增强提示词，变化镜头、景别、光线、氛围、色彩、场景调度、风格和细节要求。
- 画图区生成结果卡片默认隐藏提示词，只展示图片和操作；点击“查看提示词”后展开提示词内容。
- 生成失败不清空已成功图片；批量生成继续处理下一张。
- 展馆为空时显示空态；有记录时显示计数和筛选结果。
- 展馆卡片默认隐藏提示词，只展示图片、标签、时间和操作；点击“查看提示词”后才展开提示词内容。
- 预览传入 URL 时隐藏上一张/下一张；传入索引时启用键盘切换。

## 6. 交互流程

### 6.1 文生图

1. 用户选择 API 配置。
2. 输入提示词，选择尺寸、质量、风格、数量。
3. 点击开始生成。
4. 页面逐张调用外部 API；第 1 张直接使用原提示词，第 2 张起使用多维增强版提示词。
5. 成功图片进入结果区和展馆。

### 6.2 图生图

画图页已移除参考图片上传入口。旧记录和内部图生图代码保留兼容，但当前主流程以文生图和批量文生图为主。

### 6.3 数据管理

1. 导出：把 gallery、apiConfigs、promptHistory、imageParams、autoDownload 打包为 JSON。
2. 导入：校验 JSON 后覆盖本地数据，并重新渲染页面。
3. 清空：二次确认后清空 IndexedDB 和 localStorage。

### 6.4 更换背景

1. 用户点击顶部“更换背景”。
2. 选择本地图片。
3. 前端压缩图片并保存到 localStorage。
4. `body.custom-bg` 使用该图片作为页面背景。
5. 点击“恢复默认”删除自定义背景配置。
6. 双击空白区域可以只展示背景图；该模式不显示玻璃面板和底部网络状态。

## 7. 边界体验

- 所有危险操作需要确认。
- 网络错误给出原因和建议。
- 移动端主区域单列显示。
- 按钮禁用时仍保持原有 DOM 结构，避免下一次生成失败。

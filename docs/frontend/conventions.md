# 前端实现规范

## 1. 文件职责

| 文件 | 规则 |
|------|------|
| `index.html` | 只写页面结构和稳定 ID，不写业务逻辑 |
| `assets/css/app.css` | 只写样式、状态类、响应式，不写内联业务状态 |
| `assets/js/app.js` | 只写原生 JS 逻辑，不引入构建依赖 |
| `scripts/start-local.ps1` | 只写本地静态服务启动逻辑，不写业务逻辑和密钥 |

## 2. 命名

| 类型 | 规则 |
|------|------|
| DOM ID | 旧 HTML 已存在的 ID 尽量复用 |
| 状态变量 | 放入 `state` 对象 |
| 存储 key | 放入 `STORAGE_KEYS` |
| 渲染函数 | `renderXxx()` |
| 事件绑定 | `bindXxx()` 或集中在 `bindEvents()` |
| 用户提示 | 统一走 `showStatus(type, message)` |

## 3. 状态约束

- 不直接在多个地方写同一个状态事实。
- API 配置事实源是 `state.apiConfigs`。
- 当前配置事实源是 `state.activeApiId`。
- 展馆事实源是 `state.gallery`。
- 参考图片只存在内存，不写 localStorage。
- 自定义背景图事实源是 `state.backgroundImage`，只保存压缩后的 data URL。

## 4. UI 约束

- 不用 `button.textContent = ...` 覆盖包含图标和 span 的按钮。
- 修改生成按钮文案必须调用 `setGenerateButtonState()`。
- 危险操作必须 `confirm()`。
- 动态插入用户文本必须使用 `textContent`，不要拼 `innerHTML`。

## 5. API 约束

- Base URL 统一走 `normalizeBaseUrl()`。
- API Key 不写入 console。
- fetch 错误统一走 `formatFetchError()`。
- 响应图片提取统一走 `extractImageDataUrl()`。
- SSE 行解析统一走 `processImageStreamLine()`，尾部 buffer 不单独写第二套路。
- 批量下载不得依赖外部 CDN 脚本；ZIP 生成逻辑保持在本地静态代码中。

## 6. 验证

每次改 `assets/js/app.js` 后至少运行：

```powershell
node --check assets/js/app.js
```

准备 CloudBase 发布副本后同时运行：

```powershell
node --check cloudbase-app/assets/js/app.js
```

涉及本地运行入口时，至少执行：

```powershell
.\scripts\start-local.ps1 -NoBrowser
```

涉及页面交互时，按 `docs/frontend/mvp.md` 的手动验收清单检查。

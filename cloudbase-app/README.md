# CloudBase 静态托管目录

本目录是 `o-Picturehtml` 的 CloudBase 静态托管发布副本，当前只做发布准备，不执行发布。

## 内容

```text
cloudbase-app/
├── index.html
└── assets/
    ├── css/app.css
    └── js/app.js
```

根目录的 `index.html`、`assets/css/app.css`、`assets/js/app.js` 是开发事实源；本目录是可部署副本。修改源文件后需要重新复制到本目录。

## 发布命令示例

```powershell
tcb hosting deploy .\cloudbase-app -e <你的环境ID>
```

本次任务不发布，只保证目录可以作为静态托管入口使用。

## 安全边界

不要把 API Key、token、Cookie 或其它密钥写进本目录。API Key 只允许由用户在浏览器页面中填写，并保存到用户浏览器 localStorage。

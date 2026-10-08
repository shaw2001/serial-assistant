# 串口助手 · v0.1.0

本地运行的多设备串口调试助手。首发支持 **Windows 10 / 11 x64**，采用 Electron 与原生 serialport；运行时不需要 Node.js，不连接远程服务。

## 下载和运行

在本仓库 **Releases → v0.1.0** 下载：

- `SerialAssistant-Setup-0.1.0-win-x64.exe`：安装版，当前用户安装，可选择安装目录。
- `SerialAssistant-0.1.0-win-x64.zip`：便携版，完整解压后运行 `SerialAssistant.exe`，不要只取出一个 EXE。
- `SHA256SUMS.txt`：构建产物的 SHA-256 校验值。

适配器仍需要对应的 Windows 驱动。首次启动选择实际 COM 端口，设置波特率后连接；程序不会自动连接或发送。此版本没有代码签名证书。

## 已实现

| 功能 | v0.1.0 行为 |
| --- | --- |
| 多设备 | 最多四个独立会话，分别连接、收发、统计和记录；发送只针对当前标签 |
| 串口设置 | 自动枚举、刷新、手动端口；常用和自定义波特率；数据位、停止位、奇偶校验、流控 |
| 收发 | 文本 / HEX / 混合显示；UTF-8、ASCII、GBK；结尾符和最终字节预览 |
| 发送控制 | 单次发送、10 ms–1 小时周期；忙时跳过，断开时停止；发送队列有上限 |
| 日志 | 时间、方向、字节计数；暂停显示仍接收；搜索、高亮、过滤、清空、自动滚动 |
| 原始记录 | 保存原始字节和主机时间；100 MiB 文件分段；TXT / JSON / JSONL 导出 |
| 快捷指令 | 紧凑列表，分组、新增、编辑、复制、删除、填入、发送 |
| 配置 | 本机自动保存设备参数、草稿、发送历史、指令；配置方案 JSON 导入 / 导出 |
| 界面 | 固定浅色、明亮搜索高亮、OPPO Sans、窗口置顶、键盘快捷键 |

`Ctrl + Enter` 发送当前输入；`Ctrl + F` 搜索缓存。暂停期间锁定接收编码，恢复后可重新解码原始缓存。

**TX** 表示成功写入并提交给串口驱动，不代表设备已确认；设备应答查看 RX。接收批次不代表协议帧。

## 数据保存与边界

配置和日志位于 `%APPDATA%\serial-assistant`，界面可打开实际日志目录。安装版、便携版使用相同的当前用户配置位置。

显示缓存最多 10,000 条 / 16 MiB，超过后裁剪旧记录；落盘记录独立继续。默认显示最近 800 行，关闭自动滚动后向上滚动可加载更多。搜索范围是缓存，完整数据从“完整会话（已记录）”导出。HEX 每行展示最多 512 字节，原始 JSON/JSONL 保留完整内容。

“当前可见来源记录”按原始接收批次导出：一个批次中的其他文本行也可能包含在内。暂停、过滤、清空显示都不会修改已保存原始文件。关闭“记录会话”期间的数据不能从完整会话恢复；磁盘写入失败会显示记录缺失和错误。

v0.1.0 不包含协议解析器、波形绘制、脚本执行、网络串口、自动升级、软件流控以外的手动 RTS/DTR 控制。拔出后需要手动重新连接。

## 从源码构建

使用 Node.js 24 LTS 和 npm：

```sh
npm ci
npm run build
npm test
npm start
```

Windows x64 打包（原生 Windows 环境编辑 EXE 版本资源）：

```sh
npm run build
npx electron-builder --win nsis zip --x64 --publish never --config.win.signAndEditExecutable=true
npm run checksums
```

`release/` 保存安装包、便携包和校验文件。构建首次从 OPPO 官方站点下载未经修改的 OPPO Sans 字体并核对固定 SHA-256，之后所有运行资源都在本机。字体详情见 `THIRD_PARTY_NOTICES.md`。

Linux 原生串口集成测试：`npm run test:pty`。Windows 桌面启动检查：构建后运行 `node scripts/desktop-smoke.cjs`。单元测试包含 DOM 环境中的界面操作，不代替人工视觉检查。

上传 `package.json` 到 main 会自动触发 GitHub Actions 的 **Release v0.1.0**，也可使用 **Run workflow** 手动触发。流程在 Windows 构建、执行测试、生成校验值，再创建 tag 和 Release。已有发布版本不会被覆盖。

实体验收步骤与测试边界见 `docs/TESTING.md`。项目代码当前未授予开源许可；第三方组件各自的许可继续适用。

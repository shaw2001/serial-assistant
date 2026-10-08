# 更新记录

## v0.1.2 — 2026-10-08

- Enter 发送、Shift+Enter 换行，兼容 Ctrl+Enter；中文输入法确认、长按和忙时不重复提交。
- 文本模式 Tab 转发单行前缀及 0x09，不追加结尾符；连续补全和随后执行不重复发送前缀。
- 补全结果显示在接收区，发送框清空后可追加参数，Enter 使用所选结尾符执行。
- 补全状态按串口隔离；保留 Shift+Tab、HEX 与对话框焦点导航。
- 增加五项键盘/实际字节回归测试及真实内核 PTY 的 Tab/Enter 控制字节验证。

## v0.1.0 — 2026-10-08

首个可连接真实串口的 Windows x64 MVP。

- 四设备会话与原生串口收发，独立队列、周期发送和统计。
- 文本 / HEX、UTF-8 / ASCII / GBK、结尾符与实际字节预览。
- 原始 JSONL 会话记录、分段保存、日志导出和配置方案。
- 固定浅色、OPPO Sans、紧凑快捷指令、自定义波特率、搜索高亮。
- 后端、日志模型和界面测试，Linux 原生 PTY 集成测试，Windows 构建流水线。

## v0.1.1

- Replace bundled Electron runtime with Go/Win32 serial and shared WebView2; deliver one standalone Windows EXE with embedded UI/font.
- Monospaced receive text, selectable newline rules, font size, line wrapping and metadata columns.
- Receive area fills remaining window height; compact send/status area and floating notifications.
- Editable baud while connected and explicit apply/reconnect, preserving other sessions and raw logs.
- Native Go race tests, real two-PTY integration, EXE version/resource and Windows UI/font/layout validation.

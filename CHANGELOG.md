# 更新记录

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

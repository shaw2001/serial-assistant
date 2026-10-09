# v0.1.8

安全修复：配置校验与页面隔离、发布脚本停用、敏感输入默认不落盘、日志配额、补全超时锁定、更新链接限制、依赖安全升级和发布权限隔离。旧下载撤下。详见 docs/releases/v0.1.8.md。

# 更新记录

## v0.1.7 — 2026-10-09

- 新增 GitHub 在线更新检查：底部按钮手动检查，启动后 24 小时节流静默检查一次。
- 发现新版本时弹窗显示版本、发布时间与安装包大小，可打开下载页或忽略该版本。
- 修复版本号漂移：EXE 版本改由构建脚本从 package.json 注入，顶栏与窗口标题不再停留在旧版本。

## v0.1.6 — 2026-10-09

- 日志可视行数提升约 40%：连接控件并入会话标签行，三层工具栏合并为一行。
- 搜索改为聚焦展开（Ctrl+F）；接收换行 / 字号 / 折行 / 时间方向收进 ⚙ 弹层。
- 发送面板精简：最终字节预览并入底行，与历史、周期发送、发送按钮同行。
- 修复 Tab 补全偶发卡死：补全超时（3s）自动恢复本地输入，Esc 可手动取消。

## v0.1.5 — 2026-10-08

- 收发日志区重构为暗色终端：高对比等宽文字、RX/TX 彩色方向徽章、斑马行与悬停高亮。
- 错误 / 警告 / 成功关键字与搜索高亮适配暗色背景；终端滚动条定制。
- 三层日志工具栏整合为紧凑工作台条；胶囊搜索框；分段控件现代化。
- 连接栏分组布局、状态胶囊徽章；快捷指令卡片化；发送面板聚焦高亮。
- 深色浮层通知、圆角对话框；保留 OPPO Sans 内嵌字体与全部既有交互。

## v0.1.4 — 2026-10-08

- 普通发送成功清空发送框；失败和发送中的新编辑保留。
- 快捷指令保留原有草稿，空框回车不重复发送上一条命令。

## v0.1.3 — 2026-10-08

- 设备 RTT 补全同步发送框，同前缀候选去重并可选择。
- 编辑、Tab 与 Enter 同步设备行差异，避免重复命令。
- 分包、ANSI 与多设备补全隔离；未识别回应时暂缓发送。

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

# v0.1.0 当前验证记录

日期：2026-10-08。Linux x86_64，Node.js 24.19.0。

- 13 项自动测试全部通过，包含后端、日志模型、DOM 界面交互。
- 生产 serialport 原生库与两个 Linux 内核 PTY 集成测试通过：双向 00–FF 字节、UTF-8 中文拆包、端口隔离、周期发送、无损落盘与导出、十次重连及拔出隔离。
- 便携版 ZIP 的 CRC、包内最终源码、前端、原始字体及 Windows 原生串口库均已核对；Windows 文件版本为 0.1.0，产品版本资源为等价的 0.1.0.0。
- 当前环境无法运行安装程序生成阶段所需的 Wine，因此本次直接交付便携版；NSIS 安装版需由已准备的 Windows 构建流程生成。
- Windows x64 使用 Electron 44.7.0 和 serialport 13.0.0 的 Windows N-API 原生预编译库打包。
- OPPO 官方原始字体 SHA-256 已核对；所有运行时界面资源均本地加载。
- 没有执行 Windows 物理 USB 设备验收、8 小时负载或人工 DPI 视觉验收。
- GitHub 仓库已创建，但当前连接不具备新私有仓库访问权限，网页登录后也无法恢复会话。因此源码提交、Windows GitHub Actions 和 GitHub Release 发布尚未完成。

源码内已包含 CI 和 Release 流水线。恢复仓库权限后，上传最终代码并运行 Release 工作流；只有全部检查与编译成功后才发布 v0.1.0。

如在自己的电脑发布，可先安装官方 GitHub CLI、Node.js 24，执行 `gh auth login --scopes repo,workflow`，再在工程目录运行 `node scripts/publish-github.cjs`。该脚本使用 gh 保存的认证，上传源码、触发并等待 Windows Release；不会覆盖已有发布版本。

## v0.1.1

- 14 JavaScript tests pass, including receive newline rules at every CRLF split, encoding/raw-byte preservation and connected baud apply/reconnect.
- 5 Go service tests pass with `-race`: payload encoding/limits, four independent ports, duplicate/limit checks, raw capture/export, thirty reopen cycles, failed TX, closing a blocked send, periodic stopping, bounded queues and config protection.
- Real Go two-kernel-PTY integration passes: 00/FF, fragmented Chinese, isolated TX, raw disk export, ten reconnects, periodic sends and one endpoint unplugged while another stays connected.
- Linux-to-Windows single-file EXE cross compilation passes. Final Windows workflow checks resource version, maximum 10 MiB size, actual embedded WebView2 renderer boot, embedded OPPO Sans load, footer position and receive-area height. Startup timing is runner-specific.
- Physical USB adapters, all custom baud rates/flow-control wiring, sleeping/resuming and long full-rate operation remain hardware acceptance items.

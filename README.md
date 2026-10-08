# 串口助手--完全Ai编写-GPT6.1Sol

本地多串口调试工具，浅色界面、OPPO Sans、紧凑快捷指令。v0.1.2 提供约 4.4 MiB 的单文件 Windows x64 EXE，双击运行。

## 下载与使用

从 [GitHub Releases](https://github.com/shaw2001/serial-assistant/releases) 下载 `SerialAssistant-0.1.2-win-x64.exe`，无需安装或解压。选择串口与波特率后连接。最多四个设备独立收发，指令只发送到当前标签。

已连接时修改波特率后，点击“应用并重连”；周期发送会停止，日志和其他设备保留。USB 串口需要其适配器驱动。

## 发送快捷键与 RTT 补全

发送框中 **Enter 发送、Shift+Enter 换行**，兼容 Ctrl+Enter。中文输入法确认不会触发发送，发送过程中或按住按键不会重复提交。

文本模式下，Tab 将当前单行输入前缀和 `0x09` 发送到设备，不附加结尾符，焦点留在发送框。空输入 Tab 可查看 RTT 命令列表。补全由设备 Shell 提供，结果显示在接收区；已提交的输入会从发送框清空，随后输入作为参数或后缀追加，Enter 或“执行”完成设备中的指令。连续 Tab 和后续回车不会重复发送前缀。

例如输入 `he` 后 Tab 实际发送 `68 65 09`；再按 Tab 只发送 `09`；直接回车按当前“追加 CRLF”设置发送 `0D 0A`，执行设备补全出的指令。接收换行规则不影响这些发送字节。补全期间先完成当前输入，再使用快捷指令或周期发送。选择“无结尾符”时不会自行追加回车，需选择 CR、LF 或 CRLF 才能执行 RTT 指令。

Shift+Tab 可离开发送框；HEX、其他控件和对话框保留正常 Tab 导航。多行输入用 Enter 发送，Tab 不提交其中的换行。设备关闭或重连只清除本地补全状态，不会自动执行或取消设备指令。

## 接收显示

默认 14px 等宽文本，保留 RT-Thread `ps` 等输出中的空格和列对齐。默认关闭屏幕自动折行和时间/方向列，按设备 CR/LF/CRLF 分行。顶部可以切换接收换行规则、字号、自动折行和时间/方向。接收换行设置只影响显示，与发送的“追加 CRLF”独立；原始会话保存收到的实际字节。

接收区占据窗口剩余空间，发送区和底栏保持紧凑。显示缓存有上限，完整已记录会话可导出；清空显示不删除会话文件。

## 运行环境

Windows 10 / 11 x64，Microsoft WebView2 Runtime。Windows 11 和多数 Windows 10 已有所需运行库；旧版或精简系统缺少时按程序提示从微软官方下载 Evergreen Runtime。EXE 内嵌 HTML、CSS、JavaScript、OPPO Sans 和 WebView2Loader，不捆绑浏览器，不安装应用，不需 Node.js。运行时串口操作及界面不联网。

配置和原始会话存入 `%APPDATA%\SerialAssistant`，恢复已有兼容配置后需手动连接。UI 继续使用 OPPO Sans，串口数据使用 Consolas 等宽字体。

## 从源码构建

构建机需 Node.js 24、Go 1.26.1；首轮安装依赖和下载官方 OPPO Sans 需联网。字体下载有固定 SHA256 校验。

```shell
npm ci
npm test
npm run build:native
```

生成 `release/SerialAssistant-0.1.2-win-x64.exe`，可在 Linux 交叉编译；实际 Windows 启动验证由 GitHub Actions 执行。构建脚本生成版本资源、DPI 清单，将界面和字体嵌入单文件。

```shell
cd native
go test -race ./internal/core
cd ..
python3 scripts/native-pty.py
```

最后一条仅在 Linux 上运行，验证两个真实内核 PTY 的二进制和分包中文、独立收发、原始导出、十次重连、周期发送及单设备拔出。

## 结构

- `src/renderer`：共用界面、分包解码和接收显示模型。
- `native/internal/core`：新 Go 串口服务、编码、配置、分段日志、导出。
- `native/cmd/serial-assistant`：Win32 串口、WebView2 桌面窗口、文件对话框。
- `native/web`：构建时生成并嵌入界面资源。
- `src/main`、原有后端测试：v0.1.0 Node/Electron 行为参考；不进入 v0.1.2 成品。

发布流程先通过前端测试、Go 并发测试和内核串口集成，再在 Windows 编译并验证版本、成品体积、真实桌面启动、字体加载与布局后发布。v0.1.0 安装版和 ZIP 保留。

软件未使用签名证书。自动化检查不代替真实 USB 设备、长期满速、睡眠恢复与多种 DPI 的验收。TX 表示数据已提交驱动，不代表设备应答。第三方许可见 `THIRD_PARTY_NOTICES.md`。

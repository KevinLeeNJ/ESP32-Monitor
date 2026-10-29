ESP32 Monitor — Windows x64 绿色包
=================================

版本：见同目录 VERSION.txt
适用：Windows 10 / 11 x64

包含文件
--------
monitor-client.exe        图形界面（GUI），启动后由它在后台托管 Agent
monitor-agent.exe         采集/上传/ESP32 连接进程，GUI 会以管理员权限启动它
helpers\lhm-helper\       LibreHardwareMonitor .NET Framework 4.7.2 硬件传感器助手
app-icon.ico              应用图标
THIRD_PARTY_NOTICES.txt   第三方组件与许可证说明

系统要求
--------
1. Microsoft Edge WebView2 Runtime（Windows 11 通常已内置）。
   缺失时启动会弹出提示，请安装 Evergreen Runtime 后重试：
   https://developer.microsoft.com/microsoft-edge/webview2/
2. .NET Framework 4.7.2 或更高版本（读取 CPU/磁盘温度与 SMART 所需）。
   Windows 10 1809+ / Windows 11 通常已包含；缺失时 GUI 会提示安装。
3. 后台 Agent 需要管理员权限读取低层传感器。启动 GUI 时 Windows 会弹出一次 UAC，
   请选择“是”；GUI 本身仍以普通权限运行。

首次启动
--------
1. 双击 monitor-client.exe。若出现 SmartScreen 提示，选择“更多信息”→“仍要运行”
   （当前发行包未做代码签名）。
2. 在 UAC 提示中允许 monitor-agent.exe 运行。
3. 打开左侧「Server 连接」，填写 Server 地址与验证码完成设备注册（本地联调地址
   可用 http://127.0.0.1:18080）。
4. 在「指标采集」页选择要上传的指标，或直接在 Server 页开启“采集与上传”。

后台托盘与登录自启
------------------
* 「启动与后台」页可开启“关闭窗口后继续在后台运行”和“显示状态栏图标”。
* 开启状态栏图标后，左键单击或右键菜单“打开主窗口”可重新唤出；菜单“退出
  ESP32 Monitor”会先停止 Agent 再退出。
* 开启“登录时自启”会写入 HKCU\Software\Microsoft\Windows\CurrentVersion\Run，
  并创建一个名为“ESP32-Monitor Elevated Agent”的最高权限登录任务；关闭该开关
  会同时清理这两项。登录自启以 --login-item 后台启动，不显示主窗口。

命令行诊断
----------
在 PowerShell/终端中运行：

  .\monitor-agent.exe --collector-scan

可查看本机实时采集能力；若缺少 .NET Framework 4.7.2 或未获管理员权限，输出会明确
给出原因，不会伪造数值。

数据位置与卸载
--------------
设置、上传令牌与采集缓存位于：
  %APPDATA%\ESP32-Monitor

绿色包卸载：
1. 在应用中关闭“登录时自启”和“显示状态栏图标”，然后完全退出。
2. 删除本目录即可。
3. 如需清除个人数据，再删除 %APPDATA%\ESP32-Monitor。
若未先关闭登录自启，请手动删除注册表值
  HKCU\Software\Microsoft\Windows\CurrentVersion\Run\ESP32Monitor
以及计划任务“ESP32-Monitor Elevated Agent”。

常见问题
--------
* CPU 温度显示不可用：确认 UAC 已允许 Agent 提权，且系统已安装 .NET Framework
  4.7.2+；部分虚拟机/旧硬件本身不暴露该传感器。
* 托盘图标不显示：Windows 11 默认把新图标收进“^”溢出区，可从那里拖到任务栏。
* 上传失败：确认 Server 为 v2 协议；当前客户端不支持旧版 v1 摄取接口。

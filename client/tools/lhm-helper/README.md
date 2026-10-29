# Windows LibreHardwareMonitor helper

This is a framework-dependent Windows-only helper used by `monitor-agent`.
It targets **.NET Framework 4.7.2** (`net472`) and pins
`LibreHardwareMonitorLib` at **0.9.4**. Although 0.9.6 declares a `net472`
asset, its dependency closure fails with `BadImageFormatException` on a real
.NET Framework host. 0.9.4 is the selected formal `net472`-compatible release.

`client/scripts/build-windows-app.ps1` restores the lock file, builds this
project, and copies its executable plus DLL dependencies to
`helpers/lhm-helper/` in the Windows package. It never uses `dotnet publish`
or a self-contained runtime. The `dotnet` CLI is only a build tool; the
released executable requires the Windows .NET Framework 4.7.2 runtime (or a
newer in-place version such as 4.8/4.8.1).

If the target machine has no suitable .NET Framework, the Go Agent returns
`hardware_monitor.code=dotnet_framework_472_required`, and the frontend asks
the user to install it and re-scan. It does not emit substitute sensor data.

This Ring0 backend runs in the Windows background Agent, not the GUI/WebView.
Starting the GUI requests UAC only when it must launch that Agent. When the
user enables login startup in the GUI, Windows creates one highest-privilege
logon task whose action is `monitor-agent.exe`; later logons reuse that task
without elevating the GUI or showing another UAC prompt.

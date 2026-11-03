# Windows LibreHardwareMonitor helper

The Windows Agent runs this short-lived helper to read hardware sensors. It uses
LibreHardwareMonitorLib 0.9.6 and PawnIO. The Windows package publishes the
helper with a self-contained .NET 8 runtime, so no separate .NET installation
is required. The build requires a .NET 8 SDK and NuGet access.

The Windows client checks for PawnIO at startup and offers to run the installer
bundled with the package. Installation requires administrator approval. Declining
continues startup, although some sensors will be unavailable. The build obtains
the installer from the LibreHardwareMonitor 0.9.6 release and checks its SHA-256.
The elevated background Agent and its login task continue to own sensor access.

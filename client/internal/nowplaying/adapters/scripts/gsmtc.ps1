# Windows PowerShell 5.1 / WinRT. Read-only; polling fallback every two seconds.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
function Emit($current, $code) {
    @{current=$current; error_code=$code} | ConvertTo-Json -Compress -Depth 5 | ForEach-Object { [Console]::WriteLine($_) }
}
try {
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    $managerType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager, Windows.Media.Control, ContentType=WindowsRuntime]
    $propertiesType = [Windows.Media.Control.GlobalSystemMediaTransportControlsSessionMediaProperties, Windows.Media.Control, ContentType=WindowsRuntime]
    $asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
        $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetGenericArguments().Count -eq 1 -and
        $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1'
    } | Select-Object -First 1
    function Await($operation, $resultType) {
        $task = $asTask.MakeGenericMethod($resultType).Invoke($null, @($operation))
        if (-not $task.Wait(5000)) { throw 'music_timeout' }
        return $task.Result
    }
    $manager = Await ($managerType::RequestAsync()) $managerType
    # SourceAppUserModelId is an identifier; the media flyout shows the Start
    # menu display name instead. Cache lookups for the polling loop.
    $appNames = @{}
    function Get-FriendlyAppName($aumid) {
        if ([string]::IsNullOrWhiteSpace($aumid)) { return $null }
        if ($appNames.ContainsKey($aumid)) { return $appNames[$aumid] }
        $name = $null
        try {
            $entry = Get-StartApps -ErrorAction SilentlyContinue | Where-Object { $_.AppID -eq $aumid } | Select-Object -First 1
            if ($null -ne $entry -and $entry.Name) { $name = $entry.Name }
        } catch {}
        if (-not $name) {
            try {
                $props = Get-ItemProperty -Path ('Registry::HKEY_CURRENT_USER\Software\Classes\AppUserModelId\' + $aumid) -ErrorAction Stop
                if ($props.DisplayName -and -not $props.DisplayName.StartsWith('@')) { $name = $props.DisplayName }
            } catch {}
        }
        if ($name) { $appNames[$aumid] = $name }
        return $name
    }
} catch {
    $code = 'music_unsupported'
    if ($_.Exception.ToString() -match 'Access.*denied|0x80070005') { $code = 'music_permission_denied' }
    Emit $null $code
    exit 1
}
while ($true) {
    try {
        $session = $manager.GetCurrentSession()
        if ($null -eq $session) { Emit $null $null }
        else {
            $media = Await ($session.TryGetMediaPropertiesAsync()) $propertiesType
            $status = $session.GetPlaybackInfo().PlaybackStatus.ToString().ToLowerInvariant()
            # All explicit non-playing GSMTC statuses map to NotPlaying.
            if ($status -eq 'playing') { $status = 'playing' }
            elseif ($status -in @('closed','opened','changing','stopped','paused')) { $status = 'not_playing' }
            else { throw 'unknown_playback_status' }
            $timeline = $session.GetTimelineProperties()
            $duration = $null; $position = $null
            if ($null -ne $timeline -and $timeline.EndTime -gt $timeline.StartTime) {
                $duration = ($timeline.EndTime - $timeline.StartTime).TotalSeconds
                $position = ($timeline.Position - $timeline.StartTime).TotalSeconds
                # Most apps only refresh Position when the timeline changes, so
                # playing progress must be extrapolated from LastUpdatedTime.
                if ($status -eq 'playing' -and $null -ne $timeline.LastUpdatedTime -and $timeline.LastUpdatedTime.Year -gt 2000) {
                    $position += ([DateTimeOffset]::UtcNow - $timeline.LastUpdatedTime).TotalSeconds
                }
                if ($position -lt 0) { $position = 0 }
                if ($position -gt $duration) { $position = $duration }
            }
            $source = Get-FriendlyAppName $session.SourceAppUserModelId
            if (-not $source) { $source = $session.SourceAppUserModelId }
            Emit @{title=$media.Title;artist=$media.Artist;source=$source;
                   state=$status;duration=$duration;position=$position} $null
        }
    } catch {
        $code = 'music_read_failed'
        if ($_.Exception.ToString() -match 'Access.*denied|0x80070005') { $code = 'music_permission_denied' }
        Emit $null $code
    }
    Start-Sleep -Seconds 2
}

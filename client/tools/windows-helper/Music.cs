using System.Globalization;
using System.Runtime.InteropServices;
using System.Text;
using System.Text.Json.Serialization;
using Microsoft.Win32;
using Windows.Media.Control;

namespace ESP32Monitor.WindowsHelper;

// Read-only SMTC access. HelperHost runs this entire delegate on an independent
// Task.Run, including synchronous WinRT entry points. An unfinished task is kept,
// not cancelled/abandoned and replaced with more calls into a stuck broker.
internal sealed class Music
{
    private readonly Dictionary<string, string?> AppNames = new(StringComparer.OrdinalIgnoreCase);
    private GlobalSystemMediaTransportControlsSessionManager? manager;
    private readonly Func<string, string?> startAppName;
    private readonly Func<string, string?> registeredAppName;

    internal Music(Func<string, string?>? startAppName = null, Func<string, string?>? registeredAppName = null)
    {
        this.startAppName = startAppName ?? StartAppName;
        this.registeredAppName = registeredAppName ?? RegisteredAppName;
    }

    public async Task<object?> ReadAsync()
    {
        manager ??= await GlobalSystemMediaTransportControlsSessionManager.RequestAsync().AsTask().ConfigureAwait(false);
        var session = manager.GetCurrentSession();
        return session == null ? null : await ReadSessionAsync(session).ConfigureAwait(false);
    }

    private async Task<Current> ReadSessionAsync(GlobalSystemMediaTransportControlsSession session)
    {
        var playback = session.GetPlaybackInfo();
        var state = playback?.PlaybackStatus == GlobalSystemMediaTransportControlsSessionPlaybackStatus.Playing
            ? "playing"
            : "not_playing";
        var media = await session.TryGetMediaPropertiesAsync().AsTask().ConfigureAwait(false);
        var current = new Current
        {
            Title = Clean(media?.Title),
            Artist = Clean(media?.Artist),
            Source = FriendlyName(session.SourceAppUserModelId) ?? session.SourceAppUserModelId,
            State = state
        };
        var timeline = session.GetTimelineProperties();
        if (timeline != null)
        {
            var duration = (timeline.EndTime - timeline.StartTime).TotalSeconds;
            if (duration > 0)
            {
                var position = (timeline.Position - timeline.StartTime).TotalSeconds;
                if (state == "playing" && timeline.LastUpdatedTime.Year > 2000)
                {
                    position += (DateTimeOffset.UtcNow - timeline.LastUpdatedTime).TotalSeconds;
                }
                current.Duration = duration;
                current.Position = Math.Clamp(position, 0, duration);
            }
        }
        return current;
    }

    internal string? FriendlyName(string? aumid)
    {
        if (string.IsNullOrWhiteSpace(aumid))
        {
            return null;
        }
        if (AppNames.TryGetValue(aumid, out var cached))
        {
            return cached;
        }
        // Match the former Get-StartApps -> registry -> raw AUMID order.
        // This runs inside the isolated music task, never on the sensor worker.
        string? name = null;
        try { name = startAppName(aumid); } catch { }
        if (string.IsNullOrWhiteSpace(name))
        {
            try { name = registeredAppName(aumid); } catch { }
        }
        // Do not permanently cache a miss: Shell may not be ready at login.
        if (string.IsNullOrWhiteSpace(name)) return null;
        AppNames[aumid] = name;
        return name;
    }

    private static string? StartAppName(string aumid)
    {
        // AppsFolder exposes the same AppUserModel.ID / display-name mapping as
        // Get-StartApps, including unpackaged apps registered by shortcuts.
        // Shell automation is native COM; it does not spawn PowerShell.
        object? shell = null, folder = null, items = null;
        try
        {
            var type = Type.GetTypeFromProgID("Shell.Application");
            if (type == null) return null;
            shell = Activator.CreateInstance(type);
            if (shell == null) return null;
            folder = ((dynamic)shell).NameSpace("shell:AppsFolder");
            if (folder == null) return null;
            items = ((dynamic)folder).Items();
            int count = ((dynamic)items).Count;
            for (var i = 0; i < count; i++)
            {
                object? item = ((dynamic)items).Item(i);
                try
                {
                    if (item != null && string.Equals(
                        ((dynamic)item).ExtendedProperty("System.AppUserModel.ID") as string,
                        aumid, StringComparison.OrdinalIgnoreCase))
                    {
                        return ((dynamic)item).Name as string;
                    }
                }
                finally { if (item != null) Marshal.ReleaseComObject(item); }
            }
            return null;
        }
        finally
        {
            if (items != null) Marshal.ReleaseComObject(items);
            if (folder != null) Marshal.ReleaseComObject(folder);
            if (shell != null) Marshal.ReleaseComObject(shell);
        }
    }

    private static string? RegisteredAppName(string aumid)
    {
        using var key = Registry.CurrentUser.OpenSubKey($@"Software\Classes\AppUserModelId\{aumid}");
        return key?.GetValue("DisplayName") is string value && value.Length > 0 && !value.StartsWith("@", StringComparison.Ordinal)
            ? value : null;
    }

    internal static string? Clean(string? value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            return null;
        }
        var builder = new StringBuilder(Math.Min(value.Length, 160));
        foreach (var rune in value.EnumerateRunes())
        {
            if (Rune.IsControl(rune) || Rune.GetUnicodeCategory(rune) == UnicodeCategory.Format)
            {
                continue;
            }
            if (builder.Length + rune.Utf16SequenceLength > 160)
            {
                break;
            }
            builder.Append(rune.ToString());
        }
        var text = builder.ToString().Trim();
        return text.Length == 0 ? null : text;
    }

    internal static string Classify(Exception exception)
    {
        if (exception is TimeoutException or OperationCanceledException)
        {
            return "music_timeout";
        }
        if (exception is UnauthorizedAccessException)
        {
            return "music_permission_denied";
        }
        var hr = exception.HResult;
        if (hr == unchecked((int)0x80070005))
        {
            return "music_permission_denied";
        }
        if (hr == unchecked((int)0x80040154) || exception is TypeLoadException or DllNotFoundException)
        {
            return "music_unsupported";
        }
        return "music_read_failed";
    }

    internal sealed class Current
    {
        [JsonPropertyName("title"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public string? Title { get; set; }
        [JsonPropertyName("artist"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public string? Artist { get; set; }
        [JsonPropertyName("source"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public string? Source { get; set; }
        [JsonPropertyName("state"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public string? State { get; set; }
        [JsonPropertyName("duration"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public double? Duration { get; set; }
        [JsonPropertyName("position"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public double? Position { get; set; }
    }
}

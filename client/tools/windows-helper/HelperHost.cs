using System.Text.Json;
using System.Threading.Channels;

namespace ESP32Monitor.WindowsHelper;

internal sealed class HelperHost
{
    internal static readonly TimeSpan SensorTimeout = TimeSpan.FromSeconds(10);
    internal static readonly TimeSpan MusicTimeout = TimeSpan.FromSeconds(3);
    private static readonly JsonSerializerOptions Json = new() { IncludeFields = true };
    private readonly Func<object> readSensors;
    private readonly Action closeSensors;
    private readonly Func<Task<object?>> readMusic;
    private readonly TextWriter output;
    private readonly TimeSpan sensorTimeout;
    private readonly TimeSpan musicTimeout;
    private readonly TimeSpan pollInterval;
    private readonly TimeSpan shutdownTimeout;
    private readonly object outputLock = new();
    // At most one active scan and one pending request; stdin never waits on LHM.
    private readonly Channel<string> sensors = Channel.CreateBounded<string>(new BoundedChannelOptions(1)
    {
        SingleReader = true,
        SingleWriter = true,
        FullMode = BoundedChannelFullMode.Wait
    });
    private readonly Channel<bool> musicChanged = Channel.CreateBounded<bool>(new BoundedChannelOptions(1)
    {
        SingleReader = true,
        SingleWriter = true,
        FullMode = BoundedChannelFullMode.DropOldest
    });
    private bool subscribed;
    private int subscriptionVersion;
    private int sensorTimedOut;
    private CancellationToken stop;

    internal HelperHost(Func<object> readSensors, Action closeSensors, Func<Task<object?>> readMusic,
        TextWriter output, TimeSpan? sensorTimeout = null, TimeSpan? musicTimeout = null,
        TimeSpan? pollInterval = null, TimeSpan? shutdownTimeout = null)
    {
        this.readSensors = readSensors;
        this.closeSensors = closeSensors;
        this.readMusic = readMusic;
        this.output = output;
        this.sensorTimeout = sensorTimeout ?? SensorTimeout;
        this.musicTimeout = musicTimeout ?? MusicTimeout;
        this.pollInterval = pollInterval ?? TimeSpan.FromSeconds(1);
        this.shutdownTimeout = shutdownTimeout ?? TimeSpan.FromMilliseconds(500);
    }

    internal async Task RunAsync(TextReader input, Func<bool>? parentAlive = null, CancellationToken cancellation = default)
    {
        using var cts = CancellationTokenSource.CreateLinkedTokenSource(cancellation);
        stop = cts.Token;
        // Console.In can synchronously block even through ReadLineAsync. Keep it
        // separate from both workers and the independent parent-exit watcher.
        var inputTask = Task.Run(() => ReadInputAsync(input));
        var sensorTask = Task.Run(RunSensorsAsync);
        var musicTask = Task.Run(RunMusicAsync);
        var parentTask = Task.Run(() => WatchParentAsync(parentAlive, stop));
        try
        {
            var finished = await Task.WhenAny(inputTask, sensorTask, musicTask, parentTask).ConfigureAwait(false);
            await finished.ConfigureAwait(false);
        }
        catch (OperationCanceledException) when (stop.IsCancellationRequested) { }
        finally
        {
            cts.Cancel();
            sensors.Writer.TryComplete();
            musicChanged.Writer.TryComplete();
            // A wedged native worker (or Console.In) must not hold process exit.
            // Healthy sensors close here; stuck scans close only if they return,
            // never concurrently with Computer.Open/Update.
            try
            {
                await Task.WhenAll(inputTask, sensorTask, musicTask, parentTask)
                    .WaitAsync(shutdownTimeout).ConfigureAwait(false);
            }
            catch (TimeoutException) { }
            catch (OperationCanceledException) { }
        }
    }

    private async Task ReadInputAsync(TextReader input)
    {
        while (!stop.IsCancellationRequested)
        {
            var line = await input.ReadLineAsync(stop).ConfigureAwait(false);
            if (line == null) return;
            try
            {
                using var document = JsonDocument.Parse(line);
                var root = document.RootElement;
                if (root.ValueKind != JsonValueKind.Object ||
                    !StringProperty(root, "channel", out var channel) ||
                    !StringProperty(root, "id", out var id) || string.IsNullOrEmpty(id) ||
                    !StringProperty(root, "method", out var method)) continue;
                if (channel == "sensors" && method == "sensors.read")
                {
                    if (Volatile.Read(ref sensorTimedOut) != 0 || !sensors.Writer.TryWrite(id!))
                        EmitSensor(id!, null, "sensors_busy");
                }
                else if (channel == "music" && (method == "music.subscribe" || method == "music.unsubscribe"))
                {
                    lock (outputLock)
                    {
                        var enabled = method == "music.subscribe";
                        if (subscribed != enabled)
                        {
                            subscribed = enabled;
                            subscriptionVersion++;
                        }
                    }
                    musicChanged.Writer.TryWrite(true);
                }
            }
            catch (JsonException) { } // Invalid requests are not protocol responses/events.
        }
    }

    private static bool StringProperty(JsonElement root, string name, out string? value)
    {
        value = null;
        if (!root.TryGetProperty(name, out var property) || property.ValueKind != JsonValueKind.String) return false;
        value = property.GetString();
        return true;
    }

    private async Task RunSensorsAsync()
    {
        Task<object>? pending = null;
        try
        {
            await foreach (var id in sensors.Reader.ReadAllAsync(stop).ConfigureAwait(false))
            {
                stop.ThrowIfCancellationRequested();
                // Only this loop starts hardware work, and it never advances past
                // an unfinished read, even after its response deadline expires.
                pending = Task.Run(readSensors);
                try
                {
                    var data = await pending.WaitAsync(sensorTimeout, stop).ConfigureAwait(false);
                    EmitSensor(id, data, null);
                }
                catch (TimeoutException) when (!pending.IsFaulted)
                {
                    Interlocked.Exchange(ref sensorTimedOut, 1);
                    EmitSensor(id, null, "sensors_timeout"); // Flush before Go kills/restarts us.
                    while (await sensors.Reader.WaitToReadAsync(stop).ConfigureAwait(false))
                        while (sensors.Reader.TryRead(out var queued)) EmitSensor(queued, null, "sensors_busy");
                    return;
                }
                catch (OperationCanceledException) when (stop.IsCancellationRequested) { break; }
                catch (Exception) { EmitSensor(id, null, "lhm_scan_failed"); }
                pending = null;
            }
        }
        finally
        {
            if (pending != null)
            {
                try { await pending.ConfigureAwait(false); } catch { }
            }
            try { closeSensors(); } catch { }
        }
    }

    private async Task RunMusicAsync()
    {
        Task<object?>? pending = null;
        var pendingVersion = 0;
        while (!stop.IsCancellationRequested)
        {
            bool enabled;
            int version;
            lock (outputLock)
            {
                enabled = subscribed;
                version = subscriptionVersion;
            }
            if (!enabled)
            {
                await musicChanged.Reader.ReadAsync(stop).ConfigureAwait(false);
                continue;
            }
            if (pending == null)
            {
                pendingVersion = version;
                // Includes manager creation and ALL synchronous WinRT calls. Do
                // not call the delegate on this controller's async continuation.
                pending = Task.Run(readMusic);
                _ = pending.ContinueWith(static task => { _ = task.Exception; }, CancellationToken.None,
                    TaskContinuationOptions.OnlyOnFaulted | TaskContinuationOptions.ExecuteSynchronously, TaskScheduler.Default);
            }
            var operation = pending;
            try
            {
                var current = await operation.WaitAsync(musicTimeout, stop).ConfigureAwait(false);
                EmitMusic(pendingVersion, current, null);
                pending = null;
            }
            catch (OperationCanceledException) when (stop.IsCancellationRequested) { return; }
            catch (Exception exception)
            {
                // Timeout/error health belongs to the retained operation, even
                // if a new subscription began while its wait was in progress.
                EmitMusic(null, null, Music.Classify(exception));
                // A synchronous hang or an unfinished WinRT operation retains
                // the same task across timeout heartbeats AND unsubscribe/resubscribe.
                if (operation.IsCompleted) pending = null;
            }
            await Task.Delay(pollInterval, stop).ConfigureAwait(false);
        }
    }

    internal static async Task WatchParentAsync(Func<bool>? parentAlive, CancellationToken stop)
    {
        while (!stop.IsCancellationRequested)
        {
            if (parentAlive != null && !parentAlive()) return;
            await Task.Delay(TimeSpan.FromMilliseconds(500), stop).ConfigureAwait(false);
        }
    }

    private void EmitSensor(string id, object? data, string? error)
    {
        var payload = new Dictionary<string, object?> { ["channel"] = "sensors", ["id"] = id };
        if (error == null) payload["data"] = data;
        else payload["error_code"] = error;
        lock (outputLock) Write(payload);
    }

    private void EmitMusic(int? version, object? current, string? error)
    {
        lock (outputLock)
        {
            if (!subscribed || (version.HasValue && version.Value != subscriptionVersion)) return;
            var payload = new Dictionary<string, object?> { ["channel"] = "music", ["id"] = "", ["current"] = current };
            if (error != null) payload["error_code"] = error;
            Write(payload);
        }
    }

    private void Write(object payload)
    {
        if (stop.IsCancellationRequested) return;
        output.WriteLine(JsonSerializer.Serialize(payload, Json));
        output.Flush();
    }
}

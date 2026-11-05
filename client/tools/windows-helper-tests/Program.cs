using System.Collections.Concurrent;
using System.Diagnostics;
using System.Text;
using System.Text.Json;
using System.Threading.Channels;
using ESP32Monitor.WindowsHelper;

var tests = new (string Name, Func<Task> Run)[]
{
    ("wire envelopes, nulls, UTF8 and synchronized flush", Protocol),
    ("sensor scan errors recover without stopping music", SensorErrors),
    ("bounded sensor queue and non-overlapping scans", SensorQueue),
    ("sensor timeout isolates music and bounds EOF shutdown", SensorHang),
    ("synchronous music hang reuses one task across subscriptions", MusicHang),
    ("unfinished async music operation is never replaced", AsyncMusicHang),
    ("unsubscribe discards stale results", StaleMusic),
    ("idle and repeated error heartbeats", Heartbeats),
    ("parent exit with both workers and stdin blocked", ParentExit),
    ("graceful shutdown bounds a stuck sensor Close", CloseHang),
    ("music sanitization and error classification preserved", MusicFields),
    ("Start apps display name precedence, cache and fallback", MusicAppNames)
};
foreach (var test in tests)
{
    await test.Run().WaitAsync(TimeSpan.FromSeconds(10));
    Console.WriteLine("PASS " + test.Name);
}
Console.WriteLine($"Passed {tests.Length} hardware-free tests.");
if (Environment.GetEnvironmentVariable("WINDOWS_HELPER_EXPECT_APP_ID") is string appId)
{
    var expected = Environment.GetEnvironmentVariable("WINDOWS_HELPER_EXPECT_APP_NAME");
    // Exercise the real Shell lookup on a thread-pool thread, like HelperHost.
    var actual = await Task.Run(() => new Music().FriendlyName(appId)).WaitAsync(TimeSpan.FromSeconds(5));
    Check(!string.IsNullOrWhiteSpace(expected) && actual == expected, $"Native app name: expected '{expected}', got '{actual}'");
    Console.WriteLine($"PASS native app name: {appId} -> {actual}");
}

static HelperHost Host(Func<object> sensors, Func<Task<object?>> music, RecordingWriter output, Action? close = null,
    int sensorMs = 400, int musicMs = 60) => new(sensors, close ?? (() => { }), music, output,
        TimeSpan.FromMilliseconds(sensorMs), TimeSpan.FromMilliseconds(musicMs),
        TimeSpan.FromMilliseconds(15), TimeSpan.FromMilliseconds(80));
static void Check(bool condition, string message)
{
    if (!condition) throw new Exception(message);
}
static async Task Until(Func<bool> condition)
{
    var deadline = Stopwatch.StartNew();
    while (!condition())
    {
        if (deadline.Elapsed > TimeSpan.FromSeconds(3)) throw new TimeoutException("Test condition was not reached");
        await Task.Delay(5);
    }
}
static async Task Barrier(QueueInput input, RecordingWriter output, string id)
{
    input.Sensor(id);
    await Until(() => output.HasId(id));
}

static async Task Protocol()
{
    var input = new QueueInput();
    var output = new RecordingWriter();
    var calls = 0;
    var closed = 0;
    var host = Host(() => new Snapshot(), () =>
    {
        Interlocked.Increment(ref calls);
        return Task.FromResult<object?>(new Music.Current { Title = "曲目\n\"title\"", State = "not_playing", Position = 0 });
    }, output, () => Interlocked.Increment(ref closed));
    var run = host.RunAsync(input);
    input.Send("not json");
    input.Send("[]");
    input.Send("{\"channel\":\"music\",\"id\":\"\",\"method\":\"music.subscribe\"}");
    input.Send("{\"channel\":\"music\",\"id\":5,\"method\":\"music.subscribe\"}");
    await Barrier(input, output, "initial");
    Check(calls == 0, "music must initially be off; empty/non-string IDs must be ignored");
    input.Music(true);
    await Until(() => output.Music().Length > 0);
    for (var i = 0; i < 20; i++) await Barrier(input, output, "sensor\"曲" + i);
    input.Music(false);
    await Barrier(input, output, "unsubscribed");
    var count = output.Music().Length;
    await Task.Delay(100);
    Check(output.Music().Length == count, "unsubscribe must suppress output");
    input.Eof();
    await run;
    Check(closed == 1, "sensor close must run exactly once");
    foreach (var row in output.Rows())
    {
        if (row.GetProperty("channel").GetString() == "sensors")
        {
            Check(row.GetProperty("id").GetString() != "", "sensor response needs request ID");
            var data = row.GetProperty("data");
            Check(data.GetProperty("schema").GetInt32() == 1, "existing snapshot must be nested in data");
            Check(data.GetProperty("cpu_temperature_celsius").ValueKind == JsonValueKind.Null, "LHM null fields must be retained");
        }
        else
        {
            Check(row.GetProperty("id").GetString() == "", "only music events, no control acknowledgement");
            var current = row.GetProperty("current");
            Check(current.GetProperty("title").GetString() == "曲目\n\"title\"", "metadata must survive JSONL escaping");
            Check(!current.TryGetProperty("artist", out _), "existing music null omission must be retained");
            Check(current.GetProperty("position").GetInt32() == 0, "zero position must not be dropped");
        }
    }
    Check(output.Flushes == output.Rows().Length, "every JSONL response must be flushed");
}

static async Task SensorErrors()
{
    var input = new QueueInput();
    var output = new RecordingWriter();
    var scans = 0;
    var run = Host(() => Interlocked.Increment(ref scans) == 1 ? throw new InvalidOperationException() : new Snapshot(),
        () => Task.FromResult<object?>(null), output).RunAsync(input);
    input.Music(true);
    await Barrier(input, output, "failed");
    Check(output.Error("failed") == "lhm_scan_failed", "scan errors must be classified");
    await Barrier(input, output, "recovered");
    Check(output.ById("recovered").TryGetProperty("data", out _), "ordinary read errors must not disable later scans");
    await Until(() => output.Music().Length > 0);
    input.Eof();
    await run;
}

static async Task SensorQueue()
{
    using var release = new ManualResetEventSlim();
    var input = new QueueInput();
    var output = new RecordingWriter();
    var calls = 0;
    var active = 0;
    var overlap = false;
    var run = Host(() =>
    {
        if (Interlocked.Increment(ref active) != 1) overlap = true;
        Interlocked.Increment(ref calls);
        release.Wait();
        Interlocked.Decrement(ref active);
        return new Snapshot();
    }, () => Task.FromResult<object?>(null), output, sensorMs: 2000).RunAsync(input);
    try
    {
        input.Sensor("active");
        await Until(() => Volatile.Read(ref calls) == 1);
        input.Sensor("queued");
        input.Sensor("full");
        await Until(() => output.HasId("full"));
        Check(output.Error("full") == "sensors_busy", "queue must be bounded to one pending scan");
        release.Set();
        await Until(() => output.HasId("queued"));
        Check(calls == 2 && !overlap, "sensor reads must be strictly serialized");
    }
    finally { release.Set(); input.Eof(); await run; }
}

static async Task SensorHang()
{
    using var release = new ManualResetEventSlim();
    var input = new QueueInput();
    var output = new RecordingWriter();
    var calls = 0;
    var closed = 0;
    var run = Host(() => { Interlocked.Increment(ref calls); release.Wait(); return new Snapshot(); },
        () => Task.FromResult<object?>(null), output, () => Interlocked.Increment(ref closed), sensorMs: 60).RunAsync(input);
    try
    {
        input.Sensor("hung");
        await Until(() => Volatile.Read(ref calls) == 1);
        input.Sensor("queued");
        input.Music(true);
        await Until(() => output.HasId("hung") && output.HasId("queued"));
        Check(output.Error("hung") == "sensors_timeout", "timeout must be flushed with request ID");
        Check(output.Error("queued") == "sensors_busy", "timed out worker must reject queued scans");
        await Barrier(input, output, "later");
        Check(output.Error("later") == "sensors_busy" && calls == 1, "must not overlap a timed-out scan");
        await Until(() => output.Music().Length >= 3);
        Check(!run.IsCompleted && closed == 0, "sensor timeout must not exit or close active Computer");
        input.Eof();
        await run.WaitAsync(TimeSpan.FromSeconds(1));
        Check(closed == 0, "shutdown must not race Close against an unfinished scan");
    }
    finally { release.Set(); input.Eof(); await run; }
    await Until(() => Volatile.Read(ref closed) == 1);
}

static async Task MusicHang()
{
    using var release = new ManualResetEventSlim();
    var input = new QueueInput();
    var output = new RecordingWriter();
    var calls = 0;
    var run = Host(() => new Snapshot(), () =>
    {
        Interlocked.Increment(ref calls);
        release.Wait(); // Synchronous hang BEFORE returning a Task (like a WinRT entry point).
        return Task.FromResult<object?>(null);
    }, output).RunAsync(input);
    try
    {
        input.Music(true);
        await Until(() => output.Music().Length >= 3);
        Check(output.Music().All(row => row.GetProperty("error_code").GetString() == "music_timeout"), "stuck SMTC must emit timeout heartbeats");
        await Barrier(input, output, "independent");
        for (var i = 0; i < 4; i++)
        {
            input.Music(false);
            await Barrier(input, output, "off" + i);
            var count = output.Music().Length;
            await Task.Delay(100);
            Check(output.Music().Length == count, "hung read must not ignore unsubscribe");
            input.Music(true);
            await Until(() => output.Music().Length > count);
        }
        Check(calls == 1, "subscription toggles must not accumulate hung workers");
        Check(!run.IsCompleted, "music timeout must not kill the helper");
        input.Eof();
        await run.WaitAsync(TimeSpan.FromSeconds(1));
    }
    finally { release.Set(); input.Eof(); await run; }
}

static async Task AsyncMusicHang()
{
    var pending = new TaskCompletionSource<object?>(TaskCreationOptions.RunContinuationsAsynchronously);
    var input = new QueueInput();
    var output = new RecordingWriter();
    var calls = 0;
    var run = Host(() => new Snapshot(), () => { Interlocked.Increment(ref calls); return pending.Task; }, output).RunAsync(input);
    input.Music(true);
    await Until(() => output.Music().Length >= 3);
    Check(calls == 1, "an unfinished async WinRT operation must not be abandoned on timeout");
    await Barrier(input, output, "async-independent");
    input.Eof();
    await run;
    pending.SetResult(null);
}

static async Task StaleMusic()
{
    var pending = new TaskCompletionSource<object?>(TaskCreationOptions.RunContinuationsAsynchronously);
    var input = new QueueInput();
    var output = new RecordingWriter();
    var calls = 0;
    var run = Host(() => new Snapshot(), () => Interlocked.Increment(ref calls) == 1
        ? pending.Task : Task.FromResult<object?>(new Music.Current { Title = "fresh" }), output).RunAsync(input);
    input.Music(true);
    await Until(() => Volatile.Read(ref calls) == 1);
    input.Music(false);
    input.Music(true);
    await Barrier(input, output, "resubscribed");
    pending.SetResult(new Music.Current { Title = "stale" });
    await Until(() => output.Music().Any(row => row.GetProperty("current").ValueKind == JsonValueKind.Object));
    Check(output.Music().All(row => row.GetProperty("current").ValueKind == JsonValueKind.Null ||
        row.GetProperty("current").GetProperty("title").GetString() == "fresh"), "stale pre-unsubscribe metadata must be suppressed");
    input.Eof();
    await run;
}

static async Task Heartbeats()
{
    foreach (var fail in new[] { false, true })
    {
        var input = new QueueInput();
        var output = new RecordingWriter();
        var run = Host(() => new Snapshot(), () => fail ? throw new UnauthorizedAccessException() : Task.FromResult<object?>(null), output).RunAsync(input);
        input.Music(true);
        await Until(() => output.Music().Length >= 4);
        foreach (var row in output.Music())
        {
            Check(row.GetProperty("current").ValueKind == JsonValueKind.Null, "idle/errors must preserve current:null");
            Check(fail == row.TryGetProperty("error_code", out _), "errors must not look like healthy idle");
            if (fail) Check(row.GetProperty("error_code").GetString() == "music_permission_denied", "error classification changed");
        }
        Check(!run.IsCompleted, "repeated errors must not terminate the helper");
        input.Eof();
        await run;
    }
    Check(HelperHost.MusicTimeout + TimeSpan.FromSeconds(1) <= TimeSpan.FromSeconds(5), "default error heartbeat bound exceeds 5 seconds");
    Check(HelperHost.SensorTimeout == TimeSpan.FromSeconds(10), "sensor operation deadline must be 10 seconds");
}

static async Task ParentExit()
{
    using var release = new ManualResetEventSlim();
    var input = new BlockingInput(release);
    var output = new RecordingWriter();
    var parentAlive = 1;
    var calls = 0;
    var run = Host(() => { Interlocked.Increment(ref calls); release.Wait(); return new Snapshot(); },
        () => { Interlocked.Increment(ref calls); release.Wait(); return Task.FromResult<object?>(null); }, output).RunAsync(input,
        () => Volatile.Read(ref parentAlive) == 1);
    try
    {
        await Until(() => Volatile.Read(ref calls) == 2 && input.Blocked);
        Interlocked.Exchange(ref parentAlive, 0);
        await run.WaitAsync(TimeSpan.FromSeconds(2));
    }
    finally { release.Set(); Interlocked.Exchange(ref parentAlive, 0); await run; }
}

static async Task CloseHang()
{
    using var release = new ManualResetEventSlim();
    var input = new QueueInput();
    var output = new RecordingWriter();
    var closing = 0;
    var run = Host(() => new Snapshot(), () => Task.FromResult<object?>(null), output,
        () => { Interlocked.Increment(ref closing); release.Wait(); }).RunAsync(input);
    try
    {
        await Barrier(input, output, "read");
        input.Eof();
        await run.WaitAsync(TimeSpan.FromSeconds(1));
        Check(closing == 1, "healthy EOF must attempt Close, but bound it");
    }
    finally { release.Set(); input.Eof(); await run; }
}

static Task MusicAppNames()
{
    var shellCalls = 0;
    var registryCalls = 0;
    var music = new Music(id => { shellCalls++; return id == "top.izuna.foliamajor" ? "Folia" : null; },
        _ => { registryCalls++; return "Registry name"; });
    Check(music.FriendlyName("top.izuna.foliamajor") == "Folia", "Start apps display name must take precedence over registry/AUMID");
    Check(music.FriendlyName("TOP.IZUNA.FOLIAMAJOR") == "Folia" && shellCalls == 1 && registryCalls == 0,
        "successful names must be cached case-insensitively without re-enumerating Shell");
    Check(music.FriendlyName(null) == null && music.FriendlyName(" ") == null && shellCalls == 1, "empty AUMID must not trigger lookups");
    Check(music.FriendlyName("other") == "Registry name" && registryCalls == 1, "missing Start entry must fall back to registry");
    var unavailable = new Music(_ => throw new InvalidOperationException(), _ => "Registry fallback");
    Check(unavailable.FriendlyName("app") == "Registry fallback", "Shell failure must not break the registry fallback");
    var attempts = 0;
    var delayed = new Music(_ => ++attempts == 1 ? null : "Late app", _ => throw new UnauthorizedAccessException());
    Check(delayed.FriendlyName("late") == null, "no display name must preserve caller's raw AUMID fallback");
    Check(delayed.FriendlyName("late") == "Late app" && attempts == 2, "do not cache misses while Shell is starting");
    return Task.CompletedTask;
}

static Task MusicFields()
{
    Check(Music.Clean(" \t曲\u200b目\r\n ") == "曲目", "control/format sanitization changed");
    Check(Music.Clean(" \t ") == null, "blank music fields must be null");
    Check(Music.Clean(new string('a', 159) + "\U0001D11E") == new string('a', 159), "must not split UTF16 surrogate pairs");
    Check(Music.Classify(new TimeoutException()) == "music_timeout", "timeout classification");
    Check(Music.Classify(new OperationCanceledException()) == "music_timeout", "cancel classification");
    Check(Music.Classify(new System.Runtime.InteropServices.COMException("", unchecked((int)0x80070005))) == "music_permission_denied", "COM permission classification");
    Check(Music.Classify(new TypeLoadException()) == "music_unsupported", "unsupported classification");
    Check(Music.Classify(new Exception()) == "music_read_failed", "generic classification");
    return Task.CompletedTask;
}

sealed class Snapshot
{
    public bool ok = true;
    public int schema = 1;
    public double? cpu_temperature_celsius = null;
    public string[] warnings = [];
}

sealed class QueueInput : TextReader
{
    private readonly Channel<string> lines = Channel.CreateUnbounded<string>();
    public void Send(string line) => lines.Writer.TryWrite(line);
    public void Sensor(string id) => Send(JsonSerializer.Serialize(new { channel = "sensors", id, method = "sensors.read" }));
    public void Music(bool on) => Send(JsonSerializer.Serialize(new { channel = "music", id = "control", method = on ? "music.subscribe" : "music.unsubscribe" }));
    public void Eof() => lines.Writer.TryComplete();
    public override async ValueTask<string?> ReadLineAsync(CancellationToken cancellationToken)
    {
        if (!await lines.Reader.WaitToReadAsync(cancellationToken)) return null;
        return await lines.Reader.ReadAsync(cancellationToken);
    }
}

sealed class BlockingInput(ManualResetEventSlim release) : TextReader
{
    private int reads;
    public volatile bool Blocked;
    public override ValueTask<string?> ReadLineAsync(CancellationToken cancellationToken)
    {
        if (++reads == 1) return ValueTask.FromResult<string?>("{\"channel\":\"sensors\",\"id\":\"hung\",\"method\":\"sensors.read\"}");
        if (reads == 2) return ValueTask.FromResult<string?>("{\"channel\":\"music\",\"id\":\"control\",\"method\":\"music.subscribe\"}");
        Blocked = true;
        release.Wait();
        return ValueTask.FromResult<string?>(null);
    }
}

// Deliberately NOT a synchronized TextWriter: the host must serialize a whole
// WriteLine + Flush transaction, not just individual writes. Only flushed lines
// become visible to the tests, as they would to Go over stdout.
sealed class RecordingWriter : TextWriter
{
    private readonly ConcurrentQueue<string> lines = new();
    private readonly StringBuilder buffer = new();
    private int writing;
    public int Flushes;
    public override Encoding Encoding => Encoding.UTF8;
    public override void WriteLine(string? value)
    {
        if (Interlocked.Increment(ref writing) != 1) throw new Exception("Concurrent JSONL writes");
        foreach (var c in value!) { buffer.Append(c); Thread.Yield(); }
        buffer.Append('\n');
    }
    public override void Flush()
    {
        var line = buffer.ToString();
        buffer.Clear();
        Interlocked.Increment(ref Flushes);
        Interlocked.Decrement(ref writing);
        lines.Enqueue(line);
    }
    public JsonElement[] Rows() => lines.Select(line =>
    {
        if (line.Count(c => c == '\n') != 1) throw new Exception("Mixed or non-compact JSONL");
        using var document = JsonDocument.Parse(line);
        return document.RootElement.Clone();
    }).ToArray();
    public JsonElement[] Music() => Rows().Where(row => row.GetProperty("channel").GetString() == "music").ToArray();
    public bool HasId(string id) => Rows().Any(row => row.GetProperty("id").GetString() == id);
    public JsonElement ById(string id) => Rows().Single(row => row.GetProperty("id").GetString() == id);
    public string? Error(string id) => ById(id).GetProperty("error_code").GetString();
}

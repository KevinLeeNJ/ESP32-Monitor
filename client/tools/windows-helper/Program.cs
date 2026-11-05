using System.Diagnostics;
using System.Text;
using System.Text.Json;

namespace ESP32Monitor.WindowsHelper;

internal static class Program
{
    private static async Task<int> Main(string[] args)
    {
        Console.InputEncoding = new UTF8Encoding(false);
        Console.OutputEncoding = new UTF8Encoding(false);
        var diagnostic = false;
        var parentPid = 0;
        for (var i = 0; i < args.Length; i++)
        {
            if (args[i] == "--diagnostic") diagnostic = true;
            else if (args[i] == "--parent" && i + 1 < args.Length && int.TryParse(args[++i], out parentPid) && parentPid > 0) { }
            else if (args[i].StartsWith("--parent=", StringComparison.Ordinal) && int.TryParse(args[i][9..], out parentPid) && parentPid > 0) { }
            else
            {
                Console.Error.WriteLine("Usage: windows-helper [--parent <pid>] [--diagnostic]");
                return 2;
            }
        }

        using var cancellation = new CancellationTokenSource();
        Console.CancelKeyPress += (_, e) => { e.Cancel = true; cancellation.Cancel(); };
        var sensors = new Sensors();
        Func<bool>? parentAlive = parentPid > 0 ? () => ParentAlive(parentPid) : null;
        try
        {
            if (diagnostic) return await DiagnosticAsync(sensors, parentAlive, cancellation.Token).ConfigureAwait(false);
            var music = new Music();
            var host = new HelperHost(sensors.Read, sensors.Close, music.ReadAsync, Console.Out);
            await host.RunAsync(Console.In, parentAlive, cancellation.Token).ConfigureAwait(false);
            return 0;
        }
        catch (OperationCanceledException) when (cancellation.IsCancellationRequested) { return 0; }
        catch (IOException) { return 0; } // The owning Agent closed its pipe.
        catch (Exception exception)
        {
            Console.Error.WriteLine(exception.GetType().Name + ": " + exception.Message);
            return 1;
        }
    }

    private static bool ParentAlive(int pid)
    {
        try
        {
            using var process = Process.GetProcessById(pid);
            return !process.HasExited;
        }
        catch { return false; }
    }

    private static async Task<int> DiagnosticAsync(Sensors sensors, Func<bool>? parentAlive, CancellationToken cancellation)
    {
        using var stop = CancellationTokenSource.CreateLinkedTokenSource(cancellation);
        var worker = Task.Run(() =>
        {
            try { return sensors.ReadDiagnostic(); }
            finally { sensors.Close(); }
        });
        var parent = Task.Run(() => HelperHost.WatchParentAsync(parentAlive, stop.Token));
        try
        {
            var result = worker.WaitAsync(HelperHost.SensorTimeout, stop.Token);
            if (await Task.WhenAny(result, parent).ConfigureAwait(false) == parent) return 0;
            var data = await result.ConfigureAwait(false);
            Console.Out.WriteLine(JsonSerializer.Serialize(data, new JsonSerializerOptions { IncludeFields = true }));
            Console.Out.Flush();
            return 0;
        }
        catch (OperationCanceledException) when (stop.IsCancellationRequested) { return 0; }
        catch (Exception exception)
        {
            var code = exception is TimeoutException && !worker.IsFaulted ? "sensors_timeout" : "lhm_scan_failed";
            Console.Out.WriteLine(JsonSerializer.Serialize(new { ok = false, schema = 1, error_code = code }));
            Console.Out.Flush();
            return 1;
        }
        finally { stop.Cancel(); }
    }
}

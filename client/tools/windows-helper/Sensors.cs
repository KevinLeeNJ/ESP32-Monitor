using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Security.Principal;
#nullable disable
using LibreHardwareMonitor.Hardware;
using LibreHardwareMonitor.Hardware.Storage;

namespace ESP32Monitor.WindowsHelper
{
    // Only the serialized sensor worker accesses this retained hardware graph.
    // Lazy caches Open failures too: a failed Open is not retried in this process.
    internal sealed class Sensors
    {
        private const string Backend = "LibreHardwareMonitor 0.9.6 (PawnIO)";
        private readonly Lazy<Computer> opened;
        private Computer computer;

        public Sensors()
        {
            opened = new Lazy<Computer>(() =>
            {
                computer = new Computer
                {
                    IsCpuEnabled = true,
                    IsGpuEnabled = true,
                    IsStorageEnabled = true
                };
                computer.Open();
                return computer;
            });
        }

        public object Read() => Snapshot.Create(opened.Value);

        public object ReadDiagnostic() => Diagnostic.Create(opened.Value);

        public void Close()
        {
            // Also clean up a partially opened Computer, but never force lazy Open.
            try { computer?.Close(); } catch { }
        }

        private static bool IsElevated()
        {
            try
            {
                using (WindowsIdentity identity = WindowsIdentity.GetCurrent())
                    return new WindowsPrincipal(identity).IsInRole(WindowsBuiltInRole.Administrator);
            }
            catch
            {
                return false;
            }
        }

        private static List<string> BaseWarnings()
        {
            var warnings = new List<string>();
            if (!IsElevated())
                warnings.Add("not_elevated");
            return warnings;
        }

        private sealed class Snapshot
        {
            public bool ok = true;
            public int schema = 1;
            public string backend = Backend;
            public bool elevated;
            public string runtime;
            public List<string> warnings = new List<string>();
            public double? cpu_temperature_celsius;
            public List<Gpu> gpus = new List<Gpu>();
            public List<Disk> disks = new List<Disk>();

            public static Snapshot Create(Computer computer)
            {
                var snapshot = new Snapshot
                {
                    elevated = IsElevated(),
                    runtime = Environment.Version.ToString(),
                    warnings = BaseWarnings()
                };
                foreach (IHardware hardware in computer.Hardware)
                    Visit(hardware, snapshot);

                if (snapshot.cpu_temperature_celsius == null)
                    snapshot.warnings.Add("cpu_temperature_unavailable");
                if (snapshot.disks.Count == 0)
                    snapshot.warnings.Add("storage_unavailable");
                return snapshot;
            }

            private static void Visit(IHardware hardware, Snapshot snapshot)
            {
                hardware.Update();
                switch (hardware.HardwareType)
                {
                    case HardwareType.Cpu:
                        double? cpu = PreferredCpuTemperature(hardware);
                        if (cpu.HasValue && snapshot.cpu_temperature_celsius == null)
                            snapshot.cpu_temperature_celsius = Math.Round(cpu.Value, 1, MidpointRounding.AwayFromZero);
                        break;
                    case HardwareType.GpuNvidia:
                    case HardwareType.GpuAmd:
                    case HardwareType.GpuIntel:
                        AddGpu(hardware, snapshot);
                        break;
                    case HardwareType.Storage:
                        AddDisk(hardware, snapshot);
                        break;
                }
                foreach (IHardware child in hardware.SubHardware)
                    Visit(child, snapshot);
            }

            private static void AddGpu(IHardware hardware, Snapshot snapshot)
            {
                var temperatures = ValidTemperatures(hardware);
                double? selected = SelectGpuTemperature(temperatures);
                if (selected == null)
                    snapshot.warnings.Add("gpu_temperature_unavailable");
                snapshot.gpus.Add(new Gpu
                {
                    identifier = hardware.Identifier.ToString(),
                    model = hardware.Name,
                    temperature_celsius = selected.HasValue ? Math.Round(selected.Value, 1, MidpointRounding.AwayFromZero) : (double?)null
                });
            }

            private static void AddDisk(IHardware hardware, Snapshot snapshot)
            {
                var storage = hardware as StorageDevice;
                if (storage == null)
                    return;
                var disk = new Disk
                {
                    identifier = hardware.Identifier.ToString(),
                    device = "PHYSICALDRIVE" + storage.Storage.DriveNumber.ToString(CultureInfo.InvariantCulture),
                    model = hardware.Name ?? string.Empty
                };

                double? temperature = PrimaryDiskTemperature(hardware);
                if (temperature.HasValue)
                    disk.temperature_celsius = Math.Round(temperature.Value, 1, MidpointRounding.AwayFromZero);
                snapshot.disks.Add(disk);
            }

            private static double? PreferredCpuTemperature(IHardware hardware)
            {
                var temperatures = ValidTemperatures(hardware);
                if (temperatures.Count == 0)
                    return null;
                var preferred = temperatures.Where(t => IsPreferredCpuTemperature(t.Key)).ToList();
                var selected = preferred.Count > 0 ? preferred : temperatures;
                return selected.Average(t => t.Value);
            }

            private static double? PrimaryDiskTemperature(IHardware hardware)
            {
                foreach (string expected in new[] { "Composite Temperature", "Temperature" })
                {
                    foreach (ISensor sensor in hardware.Sensors)
                    {
                        if (sensor.SensorType != SensorType.Temperature || !sensor.Value.HasValue)
                            continue;
                        double value = sensor.Value.Value;
                        if (string.Equals(sensor.Name, expected, StringComparison.OrdinalIgnoreCase) && IsValidTemperature(value))
                            return value;
                    }
                }
                return null;
            }

            private static List<KeyValuePair<string, double>> ValidTemperatures(IHardware hardware)
            {
                var values = new List<KeyValuePair<string, double>>();
                CollectTemperatures(hardware, values);
                return values;
            }

            private static void CollectTemperatures(IHardware hardware, List<KeyValuePair<string, double>> temperatures)
            {
                foreach (ISensor sensor in hardware.Sensors)
                {
                    if (sensor.SensorType != SensorType.Temperature || !sensor.Value.HasValue)
                        continue;
                    double value = sensor.Value.Value;
                    if (!IsValidTemperature(value))
                        continue;
                    temperatures.Add(new KeyValuePair<string, double>(sensor.Name ?? string.Empty, value));
                }
                foreach (IHardware child in hardware.SubHardware)
                    CollectTemperatures(child, temperatures);
            }

            private static bool IsPreferredCpuTemperature(string name)
            {
                string lower = name.ToLowerInvariant();
                return lower.Contains("package") || lower.Contains("tctl") || lower.Contains("tdie");
            }

            private static double? SelectGpuTemperature(List<KeyValuePair<string, double>> temperatures)
            {
                if (temperatures.Count == 0)
                    return null;
                var core = temperatures.FirstOrDefault(t => string.Equals(t.Key, "GPU Core", StringComparison.OrdinalIgnoreCase));
                if (!string.IsNullOrEmpty(core.Key))
                    return core.Value;
                core = temperatures.FirstOrDefault(t => t.Key.IndexOf("GPU Core", StringComparison.OrdinalIgnoreCase) >= 0);
                if (!string.IsNullOrEmpty(core.Key))
                    return core.Value;
                foreach (var candidate in temperatures)
                {
                    string lower = candidate.Key.ToLowerInvariant();
                    if (lower.Contains("hot spot") || lower.Contains("memory") || lower.Contains("warning") || lower.Contains("critical"))
                        continue;
                    return candidate.Value;
                }
                return temperatures[0].Value;
            }

            private static bool IsValidTemperature(double value) => value > 0 && value < 150;
        }

        // Read-only support command: lists exactly what LHM discovered and which
        // sensors have values, without changing normal Agent output.
        private sealed class Diagnostic
        {
            public bool ok = true;
            public int schema = 1;
            public string backend = Backend;
            public bool elevated;
            public string runtime;
            public List<string> warnings = new List<string>();
            public List<HardwareDiagnostic> hardware = new List<HardwareDiagnostic>();

            public static Diagnostic Create(Computer computer)
            {
                var diagnostic = new Diagnostic
                {
                    elevated = IsElevated(),
                    runtime = Environment.Version.ToString(),
                    warnings = BaseWarnings()
                };
                foreach (IHardware hardware in computer.Hardware)
                    Visit(hardware, diagnostic.hardware);
                return diagnostic;
            }

            private static void Visit(IHardware hardware, List<HardwareDiagnostic> rows)
            {
                hardware.Update();
                var row = new HardwareDiagnostic
                {
                    hardware_type = hardware.HardwareType.ToString(),
                    identifier = hardware.Identifier.ToString(),
                    name = hardware.Name
                };
                foreach (ISensor sensor in hardware.Sensors)
                {
                    row.sensors.Add(new SensorDiagnostic
                    {
                        identifier = sensor.Identifier.ToString(),
                        name = sensor.Name,
                        sensor_type = sensor.SensorType.ToString(),
                        value = sensor.Value
                    });
                }
                rows.Add(row);
                foreach (IHardware child in hardware.SubHardware)
                    Visit(child, rows);
            }
        }

        private sealed class Gpu
        {
            public string identifier;
            public string model;
            public double? temperature_celsius;
        }

        private sealed class Disk
        {
            public string identifier;
            public string device;
            public string model;
            public double? temperature_celsius;
        }

        private sealed class HardwareDiagnostic
        {
            public string hardware_type;
            public string identifier;
            public string name;
            public List<SensorDiagnostic> sensors = new List<SensorDiagnostic>();
        }

        private sealed class SensorDiagnostic
        {
            public string identifier;
            public string name;
            public string sensor_type;
            public float? value;
        }
    }
}

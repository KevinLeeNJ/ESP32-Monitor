"""Check the production firmware's TWDT recovery contract."""
from pathlib import Path
import re

repo = Path(__file__).resolve().parents[2]
defaults = (repo / "firmware/sdkconfig.defaults").read_text()
main = (repo / "firmware/main/main.c").read_text()
weather = (repo / "firmware/components/firmware/monitor_weather.c").read_text()
devices = (repo / "firmware/components/firmware/monitor_devices.c").read_text()

def enabled(name):
    assert re.search(rf"^{re.escape(name)}=y$", defaults, re.M), name

for setting in (
    "CONFIG_ESP_TASK_WDT_EN",
    "CONFIG_ESP_TASK_WDT_INIT",
    "CONFIG_ESP_TASK_WDT_PANIC",
    "CONFIG_ESP_TASK_WDT_CHECK_IDLE_TASK_CPU0",
    "CONFIG_ESP_TASK_WDT_CHECK_IDLE_TASK_CPU1",
    "CONFIG_ESP_SYSTEM_PANIC_PRINT_REBOOT",
):
    enabled(setting)
assert re.search(r"^CONFIG_ESP_TASK_WDT_TIMEOUT_S=5$", defaults, re.M)

def task_body(source, name):
    start = source.index(f"static void {name}(void *arg)")
    brace = source.index("{", start)
    depth, end = 1, brace + 1
    while depth:
        depth += (source[end] == "{") - (source[end] == "}")
        end += 1
    return source[brace:end]

assert "esp_task_wdt_add(NULL)" in main
assert "esp_task_wdt_reset()" in main
for source, task in ((weather, "weather_task"), (devices, "devices_task")):
    body = task_body(source, task)
    assert "esp_task_wdt_add(NULL)" in body, f"{task} must subscribe"
    assert "esp_task_wdt_reset()" in source, f"{task} must feed"

assert "TASK_WDT_WAIT_MS" not in main
assert "ESP_PM_CPU_FREQ_MAX" not in main
assert "&reasons, remaining" in main
# Idle notification waits unsubscribe; active work keeps the five-second TWDT.
for source in (main, weather, devices):
    assert "ESP_ERROR_CHECK(esp_task_wdt_delete(NULL))" in source
    assert "ESP_ERROR_CHECK(esp_task_wdt_add(NULL))" in source
assert "&notified, portMAX_DELAY" in weather
assert "if (wait > pdMS_TO_TICKS(1000))" not in devices
assert "#define HTTP_TIMEOUT_MS 4000" in weather
assert "#define REQUEST_US (4LL * 1000000)" in devices
assert re.search(r".timeout_ms = 4000", devices)
assert "esp_task_wdt_reset()" in weather and "esp_task_wdt_reset()" in devices
print("TWDT enabled with panic reboot; display/weather/Devices watch active work and unsubscribe for event/deadline idle waits: PASS")

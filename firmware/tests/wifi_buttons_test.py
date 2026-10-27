"""Exercise the production GPIO ISR with level-triggered wakeup registers."""
from pathlib import Path
import re
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[2]
source = (repo / 'firmware/components/firmware/monitor_wifi_manager.cpp').read_text()

def function(name):
    match = re.search(r'^static void (?:IRAM_ATTR )?' + name + r'\(', source, re.M)
    assert match, name
    end = source.index('{', match.start()) + 1
    depth = 1
    while depth:
        depth += (source[end] == '{') - (source[end] == '}')
        end += 1
    return source[match.start():end]

prefix = r'''
#include <cassert>
#include <cstdint>
#include <csetjmp>
#include <vector>
#define IRAM_ATTR
#define CONFIG_PM_SLP_DISABLE_GPIO 1
#define BOOT_BUTTON_GPIO 0
#define KEY_BUTTON_GPIO 18
#define BTN_BOOT 1U
#define BTN_KEY 2U
#define GPIO_INTR_LOW_LEVEL 4
#define GPIO_INTR_HIGH_LEVEL 5
#define pdFALSE 0
#define eSetBits 0
using BaseType_t = int;
struct Pin { bool level = true, enabled = true, wakeup = false; int type = 0; };
static Pin pins[19];
static unsigned notifications, queued, yields;
static void *s_manager_task = reinterpret_cast<void *>(1);
static int gpio_intr_disable(int pin) { pins[pin].enabled = false; return 0; }
static int gpio_intr_enable(int pin) { pins[pin].enabled = true; return 0; }
static int gpio_sleep_sel_dis(int) { return 0; }
static int gpio_wakeup_disable(int pin) { pins[pin].wakeup = false; return 0; }
static int gpio_wakeup_enable(int pin, int type) {
    assert(!pins[pin].enabled); // No interrupt may fire while changing levels.
    pins[pin].wakeup = true;
    pins[pin].type = type; // ESP-IDF uses the same type for IRQ and wakeup.
    return 0;
}
static int esp_sleep_enable_gpio_wakeup() { return 0; }
static void xTaskNotifyFromISR(void *task, unsigned bit, int, int *wake) {
    assert(task == s_manager_task && !pins[bit == BTN_BOOT ? 0 : 18].enabled);
    ++notifications; queued |= bit; *wake = 1;
}
static void portYIELD_FROM_ISR() { ++yields; }
'''
task_prefix = r'''
#include "monitor_page_buttons.h"
#define MONITOR_REFRESH_PAGE_NEXT 128U
#define MONITOR_REFRESH_PAGE_PREVIOUS 256U
static unsigned page_next, page_previous;
static void monitor_display_refresh_request_reason(unsigned reason) {
    if (reason == MONITOR_REFRESH_PAGE_NEXT) ++page_next;
    if (reason == MONITOR_REFRESH_PAGE_PREVIOUS) ++page_previous;
}

#define ESP_OK 0
#define GPIO_MODE_INPUT 0
#define GPIO_PULLUP_ENABLE 1
#define GPIO_PULLDOWN_DISABLE 0
#define GPIO_INTR_ANYEDGE 3
#define CONFIG_HOLD_MS 3000
#define CONFIG_MONITOR_PM_PROFILE 1
#define portMAX_DELAY UINT32_MAX
#define pdMS_TO_TICKS(ms) (ms)
using TickType_t = uint32_t;
struct gpio_config_t { uint64_t pin_bit_mask; int mode,pull_up_en,pull_down_en,intr_type; };
static unsigned config_actions, profile_actions;
static uint64_t elapsed_ms;
struct Event { uint64_t at; bool boot,key; };
static std::vector<Event> timeline;
static size_t position;
static std::jmp_buf finished;
static int gpio_config(const gpio_config_t *) { return ESP_OK; }
static int gpio_get_level(int pin) { return pins[pin].level; }
static void vTaskDelete(void *) { assert(false); }
static void vTaskDelay(unsigned ms) { elapsed_ms += ms; }
static TickType_t xTaskGetTickCount() { return static_cast<TickType_t>(elapsed_ms); }
static void *xTaskGetCurrentTaskHandle() { return reinterpret_cast<void *>(1); }
static int gpio_isr_handler_add(int, void (*)(void *), void *) { return ESP_OK; }
static void process_manager_events(uint32_t) {}
static int enter_config_mode() { ++config_actions; return ESP_OK; }
#if CONFIG_MONITOR_PM_PROFILE
static void monitor_pm_profile_key_hold() { ++profile_actions; }
#endif
static bool xTaskNotifyWait(uint32_t, uint32_t, uint32_t *events, TickType_t wait) {
    *events = 0;
    if (position == timeline.size()) {
        if (wait == portMAX_DELAY) std::longjmp(finished, 1);
        elapsed_ms += wait; return false;
    }
    const Event& event = timeline[position];
    if (event.at - elapsed_ms > wait) { elapsed_ms += wait; return false; }
    elapsed_ms = event.at;
    if (pins[0].level == event.boot) *events |= BTN_BOOT;
    if (pins[18].level == event.key) *events |= BTN_KEY;
    pins[0].level = !event.boot; pins[18].level = !event.key;
    ++position;
    return true;
}
'''
tests = r'''
static bool active(int pin) {
    return pins[pin].type == GPIO_INTR_HIGH_LEVEL ? pins[pin].level : !pins[pin].level;
}
static void fire(int pin) {
    unsigned calls = 0;
    // A level remains asserted after clearing status. With the old ISR this
    // loop never lets the task run, reproducing the interrupt watchdog cause.
    while (pins[pin].enabled && active(pin)) {
        assert(++calls <= 1);
        button_gpio_isr(reinterpret_cast<void *>(uintptr_t(pin == 0 ? BTN_BOOT : BTN_KEY)));
    }
}
static void debounce() { configure_wake_levels(!pins[0].level, !pins[18].level); }
int main() {
    debounce();
    assert(pins[0].enabled && pins[18].enabled && !active(0) && !active(18));
    for (unsigned cycle = 0; cycle < 100; ++cycle) {
        unsigned before = notifications;
        pins[0].level = pins[18].level = false;
        fire(0); fire(18);
        assert(notifications == before + 2 && queued == (BTN_BOOT | BTN_KEY));
        // Contact bounce while masked never retriggers either ISR.
        for (unsigned i = 0; i < 20; ++i) {
            pins[0].level = pins[18].level = i % 2;
            fire(0); fire(18);
        }
        pins[0].level = pins[18].level = false;
        debounce();
        assert(!active(0) && !active(18));
        // Holding both buttons does not repeat notifications.
        fire(0); fire(18);
        assert(notifications == before + 2);
        // Releasing one button must not disable its peer (or the panel TE).
        pins[18].level = true; fire(18);
        assert(!pins[18].enabled && pins[0].enabled);
        debounce();
        pins[0].level = true; fire(0); debounce();
        assert(notifications == before + 4 && !active(0) && !active(18));
        queued = 0;
    }
    assert(notifications == yields);
}
'''
task_tests = r'''
static void scenario(std::vector<Event> events, unsigned profiles, unsigned configs,
                     uint64_t start = 0, bool boot = false, bool key = false) {
    config_actions = profile_actions = page_next = page_previous = 0; elapsed_ms = start; position = 0;
    timeline = events; pins[0].level = !boot; pins[18].level = !key;
    if (!setjmp(finished)) button_task(nullptr);
    assert(profile_actions == profiles * CONFIG_MONITOR_PM_PROFILE && config_actions == configs);
}
int main() {
    irq_main();
    scenario({{100,false,true},{500,false,false}}, 0, 0); // KEY previous
    assert(page_previous == 1 && page_next == 0);
    scenario({{100,true,false},{500,false,false}}, 0, 0); // BOOT next
    assert(page_next == 1 && page_previous == 0);
    scenario({{100,false,true},{1000,false,false}}, 0, 0);
    assert(page_previous == 1);
    scenario({{100,false,true},{10000,false,false}}, 1, 0); // Long hold fires once.
    assert(page_next == 0 && page_previous == 0);
    scenario({{100,false,true},{4000,false,false},{5000,false,true},{9000,false,false}}, 2, 0);
    scenario({{100,false,true},{1000,true,true},{6000,false,true},{11000,false,false}}, 0, 1);
    assert(page_next == 0 && page_previous == 0);
    scenario({{100,true,false},{1000,true,true},{6000,false,true},{11000,false,false}}, 0, 1);
    scenario({{100,true,false},{5000,false,false}}, 0, 0);
    scenario({{5000,false,false}}, 1, 0, 0, false, true); // Already held at task startup.
    scenario({{5000,false,false}}, 0, 1, 0, true, true);
    uint64_t wrap = UINT32_MAX - 1000ULL;
    scenario({{wrap+100,false,true},{wrap+5000,false,false}}, 1, 0, wrap);
}
'''
with tempfile.TemporaryDirectory() as temp:
    unit = Path(temp) / 'wifi_buttons.cc'
    for enabled in (0, 1):
        config = task_prefix.replace('#define CONFIG_MONITOR_PM_PROFILE 1',
                                     f'#define CONFIG_MONITOR_PM_PROFILE {enabled}')
        unit.write_text(prefix + config + function('button_gpio_isr') + function('configure_wake_levels') +
                        function('button_task') + tests.replace('int main()', 'static void irq_main()') + task_tests)
        subprocess.run(['c++', '-std=c++17', '-Wall', '-Wextra', '-Werror', '-I', str(repo / 'firmware/components/firmware'), str(unit), '-o', str(Path(temp) / 'test')], check=True)
        subprocess.run([str(Path(temp) / 'test')], check=True)
print('Button IRQ and gesture regression passed with profiling enabled/disabled: bounce, rearm, KEY/chord priority, repeat holds, startup holds and tick wrap')

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
with tempfile.TemporaryDirectory() as temp:
    unit = Path(temp) / 'wifi_buttons.cc'
    unit.write_text(prefix + function('button_gpio_isr') + function('configure_wake_levels') + tests)
    subprocess.run(['c++', '-std=c++17', '-Wall', '-Wextra', '-Werror', str(unit), '-o', str(Path(temp) / 'test')], check=True)
    subprocess.run([str(Path(temp) / 'test')], check=True)
print('Button IRQ regression passed: 100 press/hold/release cycles, contact bounce, per-pin masking and level rearm')

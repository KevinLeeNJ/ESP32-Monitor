"""Run the firmware's actual mode/cadence/priority functions with fake SPI/RTOS.

Hardware timing and visible flicker still require board validation. Extracting
functions keeps this host test on the production implementation, not a copy.
"""
from pathlib import Path
import re
import subprocess
import sys
import tempfile

repo = Path(__file__).resolve().parents[2]
main = (repo / 'firmware/main/main.c').read_text()
driver = (repo / 'firmware/components/firmware/display_port.c').read_text()
header = (repo / 'firmware/components/firmware/display_port.h').read_text()

def function(source, name):
    match = re.search(r'^static [^\n]*\b' + name + r'\(|^(?:esp_err_t|bool) ' + name + r'\(', source, re.M)
    assert match, name
    start = match.start()
    end = source.index('{', start) + 1
    depth = 1
    while depth:
        depth += (source[end] == '{') - (source[end] == '}')
        end += 1
    return source[start:end]

prefix = r'''
#include <assert.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/time.h>
typedef int BaseType_t;
#define ESP_ERROR_CHECK(c) assert((c)==ESP_OK)
static bool watchdog_active=true;
static int esp_task_wdt_delete(void *task) { (void)task; assert(watchdog_active); watchdog_active=false; return 0; }
static int esp_task_wdt_add(void *task) { (void)task; assert(!watchdog_active); watchdog_active=true; return 0; }
typedef unsigned UBaseType_t;
typedef uint32_t TickType_t;
typedef int esp_err_t;
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_ERR_INVALID_ARG -2
#define ESP_RETURN_ON_FALSE(c,e,...) do { if (!(c)) return e; } while(0)
#define ESP_RETURN_ON_ERROR(c,...) do { int e=(c); if(e) return e; } while(0)
#define ESP_LOGI(...) ((void)0)
#define ESP_LOGE(...) ((void)0)
#define ESP_LOGW(...) ((void)0)
#define pdMS_TO_TICKS(x) (x)
#define portTICK_PERIOD_MS 1
#define portMAX_DELAY UINT32_MAX
#include "monitor_animation.h"
#define configTICK_RATE_HZ 1000U
#define MONITOR_REFRESH_CLOCK 1U
#define MONITOR_REFRESH_LVGL 64U
#define TAG "test"
typedef struct { int mosi, sclk, dc, cs, rst, te; } rlcd_pins_t;
typedef struct { bool mode_known, animation_mode, te_enabled; int frame_ready;
    rlcd_pins_t pins; int awake_lock; bool awake_lock_held; } rlcd_port_t;
static unsigned pm_acquires, pm_releases, pm_active;
static uint64_t retained_pins;
static int esp_pm_lock_acquire(int lock) {
    (void)lock; ++pm_acquires; ++pm_active; return ESP_OK;
}
static int esp_pm_lock_release(int lock) {
    (void)lock; assert(pm_active); ++pm_releases; --pm_active; return ESP_OK;
}
static int gpio_sleep_sel_dis(int pin) {
    assert(pin >= 0 && pin < 64); retained_pins |= 1ULL << pin; return ESP_OK;
}
typedef struct { unsigned remaining; } lv_obj_t;
typedef struct { lv_obj_t root; } monitor_lvgl_t;
typedef struct { char time_text[8], date_text[16]; } display_snapshot_t;
static uint32_t tick, priority=1, first_clock_roll, first_draw;
static unsigned command_count, fail_command, failed_draw, draws, notifications, idle_notifications;
static bool clock_valid=true, storm;
static uint32_t timer_due = UINT32_MAX;
static bool timer_dirty;
static uint32_t monitor_lvgl_process_timers(monitor_lvgl_t *r) {
    (void)r;
    if (tick >= timer_due && timer_due != UINT32_MAX) { timer_dirty=true; timer_due=UINT32_MAX; }
    return timer_due == UINT32_MAX ? UINT32_MAX : timer_due-tick;
}
static bool monitor_lvgl_frame_pending(const monitor_lvgl_t *r) { (void)r; return timer_dirty; }
static bool te_signal, te_token;
static unsigned te_waits, irq_enabled;
static int gpio_intr_disable(int pin) { (void)pin; irq_enabled=0; return 0; }
static int gpio_intr_enable(int pin) { (void)pin; irq_enabled=1; return 0; }
static int xSemaphoreTake(int semaphore, uint32_t timeout) {
    (void)semaphore;
    if (!timeout) { bool token=te_token; te_token=false; return token; }
    ++te_waits; tick += te_signal ? 29 : timeout; return te_signal;
}
static bool rlcd_port_frame_sync_active(const rlcd_port_t *port);
static void wait_panel_frame(rlcd_port_t *port);
static uint8_t commands[256];
static uint32_t command_at[256];
static uint32_t xTaskGetTickCount(void) { return tick; }
static void vTaskDelay(uint32_t duration) {
    if (duration==300 || duration==100) assert(!pm_active && !irq_enabled);
    tick += duration;
}
static void vTaskDelayUntil(uint32_t *scheduled, uint32_t period) {
    *scheduled += period; assert((uint32_t)(*scheduled - tick) <= MONITOR_UI_ANIMATION_FRAME_MS); tick=*scheduled;
}
static unsigned uxTaskPriorityGet(void *task) { (void)task; return priority; }
static void vTaskPrioritySet(void *task, unsigned p) { (void)task; priority=p; }
static int fake_gettimeofday(struct timeval *tv, void *tz) {
    (void)tz; tv->tv_sec=tick/1000; tv->tv_usec=(tick%1000)*1000; return 0;
}
#define gettimeofday fake_gettimeofday
static int xTaskNotifyWait(uint32_t in, uint32_t out, uint32_t *reasons, uint32_t timeout) {
    (void)in; (void)out; notifications++;
    if (!watchdog_active) ++idle_notifications;
    tick += storm && timeout > 1 ? 1 : timeout;
    *reasons=storm?2:0; return storm;
}
static void feed_task_watchdog(void) {}
static bool monitor_clock_is_valid(void) { return clock_valid; }
static int send_cmd(rlcd_port_t *p, uint8_t cmd) {
    (void)p; assert(command_count<256);
    if(cmd==0x38 || cmd==0x39) assert(pm_active);
    commands[command_count]=cmd; command_at[command_count++]=tick;
    return command_count==fail_command ? ESP_FAIL : ESP_OK;
}
static int send_data(rlcd_port_t *p,const uint8_t *data,size_t len) {
    (void)p; assert(len==1 || len==2 || len==4);
    uint8_t cmd=commands[command_count-1];
    if(cmd==0xd8) { assert(len==2 && data[0]==0xa6 && data[1]==0xe9); return ESP_OK; }
    if(cmd==0xb2) { assert(len==1 && data[0]==0x12); return ESP_OK; }
    if(len==4) for(unsigned i=1;i<4;i++) assert(data[i]==data[0]);
    uint8_t expected=cmd==0xc1?0x69:cmd==0xc4?0x4b:cmd==0xc9?0:0x19;
    assert(data[0]==expected);
    return ESP_OK;
}
static int send_data1(rlcd_port_t *p,uint8_t data) { return send_data(p,&data,1); }
static lv_obj_t *monitor_lvgl_root(monitor_lvgl_t *r) { return &r->root; }
typedef struct monitor_pages monitor_pages_t;
static unsigned second_page_marker, second_page;
/* The marker address means "activity page visible"; NULL is the monitor page. */
static monitor_pages_t *monitor_pages_from_first(lv_obj_t *r) {
    (void)r; return second_page ? (monitor_pages_t *)&second_page_marker : NULL;
}
static bool monitor_pages_is_second(const monitor_pages_t *p) {
    return p == (const monitor_pages_t *)&second_page_marker;
}
static bool monitor_pages_active(const monitor_pages_t *p) { (void)p; return false; }
static monitor_pages_t *second_pages(void) {
    return second_page ? (monitor_pages_t *)&second_page_marker : NULL;
}
static void monitor_pages_resume(monitor_pages_t *p) { (void)p; }
typedef struct { int unused; } page_perf_t;
static int commit_page_timed(monitor_lvgl_t *, monitor_pages_t *, page_perf_t *);
static void report_page_perf(const page_perf_t *p) { (void)p; }
static void monitor_pages_settle(monitor_pages_t *p) { (void)p; }
static bool monitor_ui_animation_active(lv_obj_t *r) { return r->remaining>0; }
static void monitor_ui_set_animations_enabled(lv_obj_t *r,bool enabled) { if(!enabled) r->remaining=0; }
static void monitor_ui_advance(lv_obj_t *r,uint32_t elapsed) { assert(elapsed>=31); if(r->remaining) r->remaining--; }
static rlcd_port_t *test_port;
static int commit_monitor_frame(monitor_lvgl_t *r) {
    rlcd_port_t *p = test_port;
    assert(priority>=5);
    if(r->root.remaining) assert(p->mode_known && p->animation_mode);
    if(!draws) first_draw=tick;
    wait_panel_frame(p);
    tick+=7; draws++; return draws==failed_draw ? ESP_FAIL : ESP_OK;
}
static int commit_page_timed(monitor_lvgl_t *r, monitor_pages_t *p, page_perf_t *perf) {
    (void)p; (void)perf; return commit_monitor_frame(r);
}
static void monitor_clock_get_text(char *t,size_t n,char *d,size_t dn) {
    snprintf(t,n,"00:%02u",(tick/60000)%60); snprintf(d,dn,"DATE");
}
static void apply_display_snapshot(monitor_lvgl_t *r,const display_snapshot_t *s,bool devices) {
    (void)s; (void)devices; first_clock_roll=tick;
    if(r->root.remaining<10) r->root.remaining=10;
}
'''
constants = '\n'.join(re.findall(r'^#define (?:ANIMATION_TASK_PRIORITY|MINUTE_PREPARE_MS) .*$', main, re.M))
constants += '\n' + re.search(r'^#define RLCD_HPM_PREPARE_MS .*$', header, re.M).group()
code = prefix + constants + '\n'
for source, names in [(driver, ['retain_panel_pins', 'set_panel_awake', 'set_frame_sync', 'rlcd_port_frame_sync_active', 'wait_panel_frame',
                               'set_frame_rates', 'rlcd_port_set_animation_mode']),
                       (main, ['restore_panel_lpm', 'wait_animation_frame', 'raise_animation_priority',
                               'wall_time_us', 'milliseconds_to_minute', 'minute_needs_hpm',
                               'visible_page_animation', 'wait_display_refresh',
                               'update_animation_clock', 'present_monitor'])]:
    code += '\n'.join(function(source, name) for name in names) + '\n'
code += r'''
static void reset(void) {
    watchdog_active=true; idle_notifications=0; tick=0; priority=1; command_count=fail_command=failed_draw=draws=notifications=0;
    first_clock_roll=first_draw=0; clock_valid=true;storm=false;
    pm_acquires=pm_releases=pm_active=0;
    te_signal=te_token=false;te_waits=irq_enabled=0;second_page=0;
}
int main(void) {
    {
        rlcd_port_t p={0};
        tick=1000; timer_due=1025; timer_dirty=false; clock_valid=false;
        assert(wait_display_refresh(&p, NULL, NULL)==MONITOR_REFRESH_LVGL);
        assert(tick==1025 && timer_dirty);
        timer_dirty=false; clock_valid=true;
        puts("LVGL timer deadline wakes idle owner without producer notification: PASS");
    }
    rlcd_port_t p={.mode_known=true};
    test_port=&p;
    rlcd_pins_t pins={.mosi=12,.sclk=11,.dc=5,.cs=40,.rst=41,.te=6};
    const uint64_t outputs=(1ULL<<12)|(1ULL<<11)|(1ULL<<5)|(1ULL<<40)|(1ULL<<41);
    assert(retain_panel_pins(&pins)==ESP_OK && retained_pins==(outputs|(1ULL<<6)));
    retained_pins=0; pins.te=-1;
    assert(retain_panel_pins(&pins)==ESP_OK && retained_pins==outputs);
    puts("RST, CS, DC, clock, MOSI and optional TE retain active configuration in Light Sleep: PASS");
    assert(MONITOR_UI_ANIMATION_FPS==32 && MONITOR_UI_ANIMATION_FRAME_MS==32);
    assert(set_frame_rates(&p)==ESP_OK);
    assert(command_count==2 && commands[0]==0xd8 && commands[1]==0xb2);
    reset();
    puts("Panel OSCSET=A6 E9, FRCTRL=12: 32Hz HPM, 1Hz LPM, shared 32fps configuration: PASS");
    assert(rlcd_port_set_animation_mode(&p,true)==ESP_OK);
    const uint8_t high[]={0x38};
    assert(tick==300 && command_count==1 && !memcmp(commands,high,1));
    assert(pm_acquires==2 && pm_releases==1 && pm_active==1 && p.awake_lock_held);
    assert(command_at[0]==0);
    assert(rlcd_port_set_animation_mode(&p,true)==ESP_OK && tick==300 && command_count==1);
    assert(pm_acquires==2);
    assert(rlcd_port_set_animation_mode(&p,false)==ESP_OK);
    const uint8_t low[]={0x39};
    assert(tick==400 && command_count==2 && !memcmp(commands+1,low,1));
    assert(command_at[1]==300);
    assert(pm_releases==2 && !pm_active && !p.awake_lock_held);
    puts("Full HPM/LPM settling delays allow sleep with TE disabled; commands and resumed HPM stay protected: PASS");
    for(unsigned fault=1;fault<=1;fault++) {
        reset();p=(rlcd_port_t){.mode_known=true};fail_command=fault;
        assert(rlcd_port_set_animation_mode(&p,true)==ESP_FAIL && !p.mode_known);
        fail_command=0;
        unsigned next=command_count;
        assert(rlcd_port_set_animation_mode(&p,false)==ESP_OK);
        assert(commands[next]==0x38 && p.mode_known && !p.animation_mode);
        assert(pm_acquires==2 && pm_releases==2 && !pm_active);
    }
    for(unsigned fault=1;fault<=1;fault++) {
        reset();p=(rlcd_port_t){.mode_known=true,.animation_mode=true};fail_command=fault;
        assert(rlcd_port_set_animation_mode(&p,false)==ESP_FAIL && !p.mode_known);
        fail_command=0;
        assert(restore_panel_lpm(&p)==ESP_OK && p.mode_known && !p.animation_mode);
    }
    puts("Failures at HPM/LPM command recover through HPM to LPM: PASS");
    for(unsigned base=1;base<=7;base+=6) {
        reset();priority=base;tick=1000;p=(rlcd_port_t){.mode_known=true};
        assert(wait_display_refresh(&p, NULL, NULL)&MONITOR_REFRESH_CLOCK);
        assert(tick==60000 && command_at[0]==59620 && p.animation_mode && priority==base);
        assert(notifications==2 && idle_notifications==1 && watchdog_active);
        assert(command_at[0]+RLCD_HPM_PREPARE_MS==59920);
        reset();priority=base;tick=59620;storm=true;p=(rlcd_port_t){.mode_known=true};
        assert(wait_display_refresh(&p, NULL, NULL)==3 && tick==60000 && notifications==80 && priority==base);
    }
    assert(watchdog_active);
    reset();clock_valid=false;storm=true;p=(rlcd_port_t){.mode_known=true};
    assert(wait_display_refresh(&p, NULL, NULL)==2 && tick==1 && idle_notifications==1 && watchdog_active);
    puts("Idle display waits directly for minute preparation or notifications, restoring TWDT before active work: PASS");
    puts("Minute preparation at :59.620, ready at :59.920, dispatch at :00.000; notification storm does not delay it: PASS");
    for(unsigned base=1;base<=7;base+=6) {
        reset();priority=base;tick=59620;storm=true;second_page=1;p=(rlcd_port_t){.mode_known=true};
        assert(wait_display_refresh(&p, NULL, second_pages())==2 && tick==59621);
        assert(command_count==0 && !p.animation_mode && !pm_acquires && priority==base && watchdog_active);
    }
    puts("Minute window with the activity page visible skips HPM preparation and its no-sleep lock: PASS");
    for(unsigned base=1;base<=7;base+=6) for(unsigned fault=0;fault<=1;fault++) {
        reset();priority=base;tick=1000;failed_draw=fault?3:0;
        p=(rlcd_port_t){.mode_known=true};monitor_lvgl_t r={.root.remaining=12};
        assert(present_monitor(&r,&p,NULL)==(fault?ESP_FAIL:ESP_OK));
        assert(first_draw==1300 && priority==base && !p.animation_mode && !r.root.remaining);
    }
    puts("All animation frames run at high priority after HPM settling; success and draw failure restore priority and LPM: PASS");
    reset();tick=1000;p=(rlcd_port_t){.mode_known=true};
    monitor_lvgl_t static_frame={0};
    assert(present_monitor(&static_frame,&p,NULL)==ESP_OK);
    assert(draws==1 && command_count==0 && !pm_acquires && !p.animation_mode && tick==1007);
    puts("Static update commits directly in known LPM without HPM switching, settling or final-scan waits: PASS");
    reset();tick=1000;te_signal=true;
    p=(rlcd_port_t){.mode_known=true,.frame_ready=1};
    monitor_lvgl_t synced={.root.remaining=12};
    assert(present_monitor(&synced,&p,NULL)==ESP_OK);
    assert(te_waits==13 && !irq_enabled && !p.animation_mode && !p.te_enabled);
    assert(tick==1900); /* 300 HPM + 13*(29 TE + 7 transfer) + 32 final scan + 100 LPM */
    puts("TE presentation drives the timeline without an additional nominal frame sleep: PASS");
    reset();p=(rlcd_port_t){.mode_known=true,.animation_mode=true,.frame_ready=1};
    set_frame_sync(&p,true);te_token=true;te_signal=true;
    wait_panel_frame(&p);
    assert(tick==29 && !te_token && rlcd_port_frame_sync_active(&p));
    te_signal=false;wait_panel_frame(&p);
    assert(tick==129 && !rlcd_port_frame_sync_active(&p) && !irq_enabled);
    wait_panel_frame(&p);assert(tick==129);
    assert(rlcd_port_set_animation_mode(&p,false)==ESP_OK);
    assert(rlcd_port_set_animation_mode(&p,true)==ESP_OK && rlcd_port_frame_sync_active(&p));
    puts("Stale TE tokens drain; missing TE times out once, falls back, and recovers on next HPM: PASS");
    reset();tick=1000;fail_command=1;
    p=(rlcd_port_t){.mode_known=true};monitor_lvgl_t failed={.root.remaining=12};
    assert(present_monitor(&failed,&p,NULL)==ESP_FAIL);
    assert(priority==1 && p.mode_known && !p.animation_mode && !failed.root.remaining);
    puts("Failed HPM preparation cancels the timeline and restores priority/LPM: PASS");
    reset();tick=59470;p=(rlcd_port_t){.mode_known=true,.animation_mode=true};
    monitor_lvgl_t imminent={.root.remaining=1};
    assert(present_monitor(&imminent,&p,NULL)==ESP_OK && p.animation_mode);
    assert(wait_display_refresh(&p, NULL, NULL)&MONITOR_REFRESH_CLOCK);
    assert(tick==60000 && command_count==0 && priority==1);
    puts("Device animation ending near minute retains HPM, avoiding a late LPM/HPM round trip: PASS");
    reset();tick=59470;second_page=1;p=(rlcd_port_t){.mode_known=true,.animation_mode=true};
    monitor_lvgl_t hidden={.root.remaining=1};
    assert(present_monitor(&hidden,&p,NULL)==ESP_OK && !p.animation_mode);
    assert(draws==1 && command_count==1 && commands[0]==0x39 && tick==59577 && !pm_active && priority==1);
    puts("Timeline while the activity page is visible runs one LPM frame and leaves HPM: PASS");
    reset();tick=59000;p=(rlcd_port_t){.mode_known=true,.animation_mode=true};
    monitor_lvgl_t r={.root.remaining=70};
    display_snapshot_t s={.time_text="00:00",.date_text="DATE"};
    assert(present_monitor(&r,&p,&s)==ESP_OK);
    assert(!strcmp(s.time_text,"00:01") && first_clock_roll>=60000 && first_clock_roll<=60032);
    assert(priority==1 && !p.animation_mode);
    puts("Device transition crossing a minute merges clock roll within one frame: PASS");
    reset();uint32_t scheduled=0, previous=0;
    for(unsigned i=0;i<320;i++) {
        tick+=i%2?15:7;wait_animation_frame(&scheduled,UINT32_MAX);
        assert(tick==monitor_animation_frame_time(i+1,1000));
        assert(tick-previous==31 || tick-previous==32);previous=tick;
    }
    assert(tick==10000); // 320 frames in 10s, no rounded-period drift
    tick=10040;wait_animation_frame(&scheduled,UINT32_MAX);assert(tick==10063);
    scheduled=UINT32_MAX-10;tick=scheduled+7;
    wait_animation_frame(&scheduled,UINT32_MAX);assert(tick==21);
    scheduled=0;tick=119993;
    wait_animation_frame(&scheduled,7);assert(tick==120000);
    puts("32fps fractional cadence: 320 frames/10s, 31/32ms intervals, overrun skipping, tick wrap and timeout cap: PASS");
    return 0;
}
'''
with tempfile.TemporaryDirectory(prefix='monitor-animation-') as directory:
    source = Path(directory) / 'test.c'
    source.write_text(code)
    binary = Path(directory) / 'test'
    compiler = sys.argv[1] if len(sys.argv) > 1 else 'cc'
    subprocess.run([compiler, '-std=c11', '-I'+str(repo/'firmware/include'), '-Wall', '-Wextra', '-Werror', str(source), '-o', str(binary)], check=True)
    subprocess.run([str(binary)], check=True)

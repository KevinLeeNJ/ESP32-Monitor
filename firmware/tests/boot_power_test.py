"""Exercise the production boot transaction across long waits and fresh timelines."""
import sys
from host_harness import REPO, defines, function, run
source = (REPO/'firmware/main/main.c').read_text()
code = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
typedef int esp_err_t;
typedef unsigned TickType_t, UBaseType_t;
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_ERROR_CHECK(c) assert((c)==ESP_OK)
#define ESP_LOGI(...) ((void)0)
#define ESP_LOGE(...) ((void)0)
#define pdMS_TO_TICKS(x) (x)
#define portTICK_PERIOD_MS 1
#define UINT32_MAX 4294967295U
#define MONITOR_UI_ANIMATION_FRAME_MS 32
static unsigned tick, priority, stage, mode_changes, idle_waits, draws, fresh_draws, scenario;
static unsigned last_feed;
static bool watched, low_power;
typedef struct { unsigned remaining; } lv_obj_t;
typedef struct { lv_obj_t root; } monitor_lvgl_t;
typedef struct { bool hpm; } rlcd_port_t;
typedef struct { unsigned version; } devices_t;
typedef struct { devices_t devices; unsigned stage; } display_snapshot_t;
static unsigned xTaskGetTickCount(void) { return tick; }
static unsigned uxTaskPriorityGet(void *p) { (void)p; return priority; }
static void vTaskPrioritySet(void *p, unsigned n) { (void)p; priority=n; }
static void vTaskDelay(unsigned n) { tick+=n; }
static int esp_task_wdt_delete(void *p) { (void)p; assert(watched); watched=false; return 0; }
static int esp_task_wdt_add(void *p) { (void)p; assert(!watched); watched=true; last_feed=tick; return 0; }
static void feed_task_watchdog(void) { assert(watched && tick-last_feed<5000); last_feed=tick; }
static lv_obj_t *monitor_lvgl_root(monitor_lvgl_t *r) { return &r->root; }
static bool monitor_ui_animation_active(lv_obj_t *r) { return r->remaining>0; }
static bool monitor_ui_advance(lv_obj_t *r, unsigned ms) {
    if (!r->remaining) return false;
    /* Panel settling and the previous long idle must not skip a fresh frame. */
    assert(ms<100);
    if (ms>=32) --r->remaining;
    return ms>=32;
}
static void monitor_ui_set_animations_enabled(lv_obj_t *r, bool enabled) { if(!enabled)r->remaining=0; }
static void capture_display_snapshot(display_snapshot_t *s) { s->stage=stage; s->devices.version=stage; }
static bool display_snapshot_equal(const display_snapshot_t *a,const display_snapshot_t *b) { return a->stage==b->stage; }
static void apply_display_snapshot(monitor_lvgl_t *r,const display_snapshot_t *s,bool devices) {
    (void)devices; if(s->stage)r->root.remaining=3;
}
static bool boot_data_complete(const display_snapshot_t *s) { return s->stage==2; }
static int rlcd_port_set_animation_mode(rlcd_port_t *p,bool high) {
    assert(watched);
    if(p->hpm==high)return 0;
    ++mode_changes; tick+=high?300:100;
    if (scenario==2 && high && stage==1) return ESP_FAIL;
    p->hpm=high; low_power=!high; return 0;
}
static int restore_panel_lpm(rlcd_port_t *p) { return rlcd_port_set_animation_mode(p,false); }
static bool rlcd_port_frame_sync_active(rlcd_port_t *p) { (void)p; return false; }
static void wait_animation_frame(unsigned *epoch,unsigned limit) {
    unsigned n=32<limit?32:limit; tick+=n; *epoch=tick;
}
static int commit_monitor_frame(monitor_lvgl_t *r) {
    assert(watched); ++draws;
    if(r->root.remaining==3)++fresh_draws;
    return scenario==3 && draws==2?ESP_FAIL:ESP_OK;
}
static int xTaskNotifyWait(unsigned a,unsigned b,unsigned *reasons,unsigned wait) {
    (void)a;(void)b;*reasons=0;
    assert(!watched && low_power && wait>5000); ++idle_waits;
    if(stage==0 && scenario!=1){ tick=5000; stage=1; }
    else if(stage==1 && scenario==0){ tick=30000; stage=2; }
    else tick+=wait;
    return 1;
}
'''
code += defines(source, ['BOOT_ANIMATION_TIMEOUT_MS', 'ANIMATION_TASK_PRIORITY'])+'\n'
code += function(source, 'present_boot_monitor')+'\n'
code += r'''
int main(void) {
    for(scenario=0;scenario<4;++scenario) {
        tick=stage=mode_changes=idle_waits=draws=fresh_draws=last_feed=0;
        priority=1;watched=true;low_power=true;
        monitor_lvgl_t renderer={0};rlcd_port_t port={0};display_snapshot_t displayed={0};
        int result=present_boot_monitor(&renderer,&port,&displayed);
        assert(watched && !port.hpm && low_power && priority==1 && idle_waits>0);
        if(scenario==0)assert(result==0 && tick<31000 && fresh_draws==2 && mode_changes==6);
        if(scenario==1)assert(result==0 && tick==120000 && idle_waits==1 && mode_changes==2);
        if(scenario>=2)assert(result==ESP_FAIL && tick<6000);
    }
    puts("Boot: long LPM idle without TWDT wake, absolute timeout, full fresh timelines after HPM settling, resume/draw errors restore LPM: PASS");
}
'''
run(code, sys.argv[1] if len(sys.argv)>1 else 'cc')

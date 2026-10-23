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
#define TAG "test"
typedef struct { bool mode_known, animation_mode, te_enabled; int frame_ready; struct { int te; } pins; } rlcd_port_t;
typedef struct { unsigned remaining; } lv_obj_t;
typedef struct { lv_obj_t root; } monitor_renderer_t;
typedef struct { char time_text[8], date_text[16]; } display_snapshot_t;
static uint32_t tick, priority=1, first_clock_roll, first_draw;
static unsigned command_count, fail_command, failed_draw, draws, notifications;
static bool clock_valid=true, storm;
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
static void vTaskDelay(uint32_t duration) { tick += duration; }
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
    tick += storm && timeout > 1 ? 1 : timeout;
    *reasons=storm?2:0; return storm;
}
static bool monitor_clock_is_valid(void) { return clock_valid; }
static int send_cmd(rlcd_port_t *p, uint8_t cmd) {
    (void)p; assert(command_count<256);
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
static lv_obj_t *monitor_renderer_root(monitor_renderer_t *r) { return &r->root; }
static bool monitor_ui_animation_active(lv_obj_t *r) { return r->remaining>0; }
static void monitor_ui_set_animations_enabled(lv_obj_t *r,bool enabled) { if(!enabled) r->remaining=0; }
static void monitor_ui_advance(lv_obj_t *r,uint32_t elapsed) { assert(elapsed>=31); if(r->remaining) r->remaining--; }
static int commit_monitor_frame(monitor_renderer_t *r,rlcd_port_t *p) {
    assert(priority>=5);
    if(r->root.remaining) assert(p->mode_known && p->animation_mode);
    if(!draws) first_draw=tick;
    wait_panel_frame(p);
    tick+=7; draws++; return draws==failed_draw ? ESP_FAIL : ESP_OK;
}
static void monitor_clock_get_text(char *t,size_t n,char *d,size_t dn) {
    snprintf(t,n,"00:%02u",(tick/60000)%60); snprintf(d,dn,"DATE");
}
static void apply_display_snapshot(monitor_renderer_t *r,const display_snapshot_t *s,bool devices) {
    (void)s; (void)devices; first_clock_roll=tick;
    if(r->root.remaining<10) r->root.remaining=10;
}
'''
constants = '\n'.join(re.findall(r'^#define (?:ANIMATION_TASK_PRIORITY|MINUTE_PREPARE_MS) .*$', main, re.M))
constants += '\n' + re.search(r'^#define RLCD_HPM_PREPARE_MS .*$', header, re.M).group()
code = prefix + constants + '\n'
for source, names in [(driver, ['set_frame_sync', 'rlcd_port_frame_sync_active', 'wait_panel_frame',
                               'set_frame_rates', 'set_mode_voltages', 'rlcd_port_set_animation_mode']),
                       (main, ['restore_panel_lpm', 'wait_animation_frame', 'raise_animation_priority',
                               'wall_time_us', 'milliseconds_to_minute', 'minute_needs_hpm',
                               'wait_display_refresh', 'update_animation_clock', 'present_monitor'])]:
    code += '\n'.join(function(source, name) for name in names) + '\n'
code += r'''
static void reset(void) {
    tick=0; priority=1; command_count=fail_command=failed_draw=draws=notifications=0;
    first_clock_roll=first_draw=0; clock_valid=true;storm=false;
    te_signal=te_token=false;te_waits=irq_enabled=0;
}
int main(void) {
    rlcd_port_t p={.mode_known=true};
    assert(MONITOR_UI_ANIMATION_FPS==32 && MONITOR_UI_ANIMATION_FRAME_MS==32);
    assert(set_frame_rates(&p)==ESP_OK);
    assert(command_count==2 && commands[0]==0xd8 && commands[1]==0xb2);
    reset();
    puts("Panel OSCSET=A6 E9, FRCTRL=12: 32Hz HPM, 1Hz LPM, shared 32fps configuration: PASS");
    assert(rlcd_port_set_animation_mode(&p,true)==ESP_OK);
    const uint8_t high[]={0x38,0xc1,0xc2,0xc4,0xc5,0xc9};
    assert(tick==320 && command_count==6 && !memcmp(commands,high,6));
    assert(command_at[0]==0 && command_at[1]==300);
    assert(rlcd_port_set_animation_mode(&p,true)==ESP_OK && tick==320 && command_count==6);
    assert(rlcd_port_set_animation_mode(&p,false)==ESP_OK);
    const uint8_t low[]={0xc1,0xc2,0xc4,0xc5,0xc9,0x39};
    assert(tick==440 && command_count==12 && !memcmp(commands+6,low,6));
    assert(command_at[6]==320 && command_at[11]==340);
    puts("Datasheet HPM/LPM command order, settling and idempotence: PASS");
    for(unsigned fault=1;fault<=6;fault++) {
        reset();p=(rlcd_port_t){.mode_known=true};fail_command=fault;
        assert(rlcd_port_set_animation_mode(&p,true)==ESP_FAIL && !p.mode_known);
        fail_command=0;
        unsigned next=command_count;
        assert(rlcd_port_set_animation_mode(&p,false)==ESP_OK);
        assert(commands[next]==0x38 && p.mode_known && !p.animation_mode);
    }
    for(unsigned fault=1;fault<=6;fault++) {
        reset();p=(rlcd_port_t){.mode_known=true,.animation_mode=true};fail_command=fault;
        assert(rlcd_port_set_animation_mode(&p,false)==ESP_FAIL && !p.mode_known);
        fail_command=0;
        assert(restore_panel_lpm(&p)==ESP_OK && p.mode_known && !p.animation_mode);
    }
    puts("Failures at each HPM/LPM command recover through HPM to LPM: PASS");
    for(unsigned base=1;base<=7;base+=6) {
        reset();priority=base;tick=1000;p=(rlcd_port_t){.mode_known=true};
        assert(wait_display_refresh(&p)&MONITOR_REFRESH_CLOCK);
        assert(tick==60000 && command_at[0]==59600 && p.animation_mode && priority==base);
        assert(command_at[1]==59900);
        reset();priority=base;tick=59600;storm=true;p=(rlcd_port_t){.mode_known=true};
        assert(wait_display_refresh(&p)==3 && tick==60000 && notifications==80 && priority==base);
    }
    puts("Minute preparation at :59.600, ready at :59.920, dispatch at :00.000; notification storm does not delay it: PASS");
    for(unsigned base=1;base<=7;base+=6) for(unsigned fault=0;fault<=1;fault++) {
        reset();priority=base;tick=1000;failed_draw=fault?3:0;
        p=(rlcd_port_t){.mode_known=true};monitor_renderer_t r={.root.remaining=12};
        assert(present_monitor(&r,&p,NULL)==(fault?ESP_FAIL:ESP_OK));
        assert(first_draw==1320 && priority==base && !p.animation_mode && !r.root.remaining);
    }
    puts("All animation frames run at high priority after HPM settling; success and draw failure restore priority and LPM: PASS");
    reset();tick=1000;te_signal=true;
    p=(rlcd_port_t){.mode_known=true,.frame_ready=1};
    monitor_renderer_t synced={.root.remaining=12};
    assert(present_monitor(&synced,&p,NULL)==ESP_OK);
    assert(te_waits==13 && !irq_enabled && !p.animation_mode && !p.te_enabled);
    assert(tick==1940); /* 320 HPM + 13*(29 TE + 7 transfer) + 32 final scan + 120 LPM */
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
    p=(rlcd_port_t){.mode_known=true};monitor_renderer_t failed={.root.remaining=12};
    assert(present_monitor(&failed,&p,NULL)==ESP_FAIL);
    assert(priority==1 && p.mode_known && !p.animation_mode && !failed.root.remaining);
    puts("Failed HPM preparation cancels the timeline and restores priority/LPM: PASS");
    reset();tick=59470;p=(rlcd_port_t){.mode_known=true,.animation_mode=true};
    monitor_renderer_t imminent={.root.remaining=1};
    assert(present_monitor(&imminent,&p,NULL)==ESP_OK && p.animation_mode);
    assert(wait_display_refresh(&p)&MONITOR_REFRESH_CLOCK);
    assert(tick==60000 && command_count==0 && priority==1);
    puts("Device animation ending near minute retains HPM, avoiding a late LPM/HPM round trip: PASS");
    reset();tick=59000;p=(rlcd_port_t){.mode_known=true,.animation_mode=true};
    monitor_renderer_t r={.root.remaining=70};
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

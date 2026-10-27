/* Real LVGL animation frames: dirty coverage, multi-flush accumulation and
 * reproducible host comparison against the previous byte scan. */
#include "monitor_render.h"
#include "monitor_pages.h"
#include <assert.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <time.h>
static uint8_t committed[MONITOR_FRAME_BYTES];
static volatile unsigned sink;
static double now_us(void) {
    struct timespec t; timespec_get(&t,TIME_UTC);
    return t.tv_sec*1000000.0+t.tv_nsec/1000.0;
}
static unsigned baseline(const uint8_t *f) {
    unsigned p1=200,p2=0,t1=25,t2=0;
    for(unsigned i=0;i<15000;++i)if(f[i]!=committed[i]) {
        unsigned p=i/75,t=(i%75)/3;
        if(p<p1)p1=p;
        if(p>p2)p2=p;
        if(t<t1)t1=t;
        if(t>t2)t2=t;
    }
    return p1==200?0:(p2-p1+1)*(t2-t1+1)*3;
}
static unsigned frames,old_bytes,new_bytes,windows;
static double old_us,new_us,render_us;
static void frame(monitor_renderer_t *r) {
    double start=now_us(); assert(monitor_renderer_render(r));render_us+=now_us()-start;
    monitor_damage_plan_t plan;
    monitor_damage_plan(&r->dirty,r->packed,committed,&plan);
    for(unsigned i=0;i<5000;++i)if(memcmp(r->packed+i*3,committed+i*3,3)) {
        assert(r->dirty.bits[i/8]&(1U<<(i%8)));
        bool covered=false;
        for(unsigned n=0;n<plan.count;++n) {
            monitor_window_t w=plan.windows[n];
            if(i/25>=w.p1 && i/25<=w.p2 && i%25>=w.t1 && i%25<=w.t2)covered=true;
        }
        assert(covered);
    }
    unsigned before=baseline(r->packed);
    assert(plan.count<=8 && plan.bytes<=before+8*MONITOR_WINDOW_COST);
    start=now_us();for(unsigned k=0;k<50;++k)sink=baseline(r->packed);old_us+=(now_us()-start)/50;
    start=now_us();for(unsigned k=0;k<50;++k){monitor_damage_plan(&r->dirty,r->packed,committed,&plan);sink=plan.bytes;}
    new_us+=(now_us()-start)/50;
    ++frames;old_bytes+=before;new_bytes+=plan.bytes;windows+=plan.count;
    memcpy(committed,r->packed,sizeof(committed));memset(&r->dirty,0,sizeof(r->dirty));
}
static lv_obj_t *waiting(lv_obj_t *parent) { return monitor_ui_create_waiting(parent, MONITOR_FRAME_NORMAL); }
static void count_delete(lv_event_t *e) { ++*(unsigned *)lv_event_get_user_data(e); }
static void timer_label(lv_timer_t *timer) {
    lv_label_set_text(lv_timer_get_user_data(timer), "timer fired");
    lv_timer_delete(timer);
}
static void test_timer_render(void) {
    monitor_renderer_t r;
    assert(monitor_renderer_init(&r, waiting));
    assert(lv_display_get_refr_timer(r.display) == NULL);
    lv_obj_t *label = lv_label_create(r.root);
    lv_label_set_text(label, "waiting");
    assert(monitor_renderer_render(&r));
    memset(&r.dirty, 0, sizeof(r.dirty));
    lv_timer_t *timer = lv_timer_create(timer_label, 25, label);
    lv_timer_handler();
    lv_tick_inc(24);
    lv_timer_handler();
    assert(!strcmp(lv_label_get_text(label), "waiting"));
    lv_tick_inc(1);
    lv_timer_handler();
    assert(!strcmp(lv_label_get_text(label), "timer fired"));
    assert(monitor_renderer_render(&r));
    bool dirty = false;
    for (unsigned i = 0; i < sizeof(r.dirty.bits); ++i) dirty |= r.dirty.bits[i] != 0;
    assert(dirty);
    (void)timer;
    unsigned deletions = 0;
    lv_obj_add_event_cb(label, count_delete, LV_EVENT_DELETE, &deletions);
    lv_obj_delete_async(label);
    lv_timer_handler();
    assert(deletions == 1);
    /* No periodic refresh timer may keep a quiet display awake. */
    lv_timer_handler();
    assert(lv_timer_handler() == LV_NO_TIMER_READY);
    monitor_renderer_deinit(&r);
    puts("Real LVGL timers: deadline, dirty frame, async deletion, idle indefinite sleep: PASS");
}
static void test_roll_damage(monitor_frame_variant_t variant) {
    monitor_renderer_t r;assert(monitor_renderer_init(&r,waiting));
    monitor_ui_set_variant(r.root,variant);
    assert(monitor_renderer_render(&r));
    unsigned deletions=0;
    lv_obj_t *empty=lv_obj_get_child_by_name(r.root,"empty_devices");assert(empty);
    lv_obj_add_event_cb(empty,count_delete,LV_EVENT_DELETE,&deletions);
    monitor_ui_snapshot_t ui={.time_text="19:59",.wifi_state=MONITOR_WIFI_OFFLINE,
        .battery_percent=78,.weather_condition=MONITOR_WEATHER_LIGHT_RAIN,
        .temperature_tenths_c=245,.humidity_percent=58,.aqi=42};
    monitor_ui_apply_snapshot(r.root,&ui);
    monitor_devices_snapshot_t pending={.status=MONITOR_DEVICES_WAITING,.version=1};
    monitor_ui_apply_devices_snapshot(r.root,&pending); /* deferred until rolls finish */
    unsigned last_tiles=0;
    for(unsigned n=0;n<16;++n) {
        memset(&r.dirty,0,sizeof(r.dirty));
        monitor_ui_advance(r.root,29);assert(monitor_renderer_render(&r));
        unsigned tiles=0;for(unsigned i=0;i<625;++i)tiles+=(unsigned)__builtin_popcount(r.dirty.bits[i]);
        if(!monitor_ui_animation_active(r.root) && tiles)last_tiles=tiles;
    }
    assert(!deletions && last_tiles>0 && last_tiles<1500);
    assert(!strcmp(lv_label_get_text(empty),"WAITING FOR DATA"));
    pending.status=MONITOR_DEVICES_UNAVAILABLE;
    monitor_ui_apply_devices_snapshot(r.root,&pending);
    assert(!deletions && !strcmp(lv_label_get_text(empty),"DATA UNAVAILABLE"));
    printf("Startup roll final redraw: %u tiles, empty label reused\n",last_tiles);
    monitor_renderer_deinit(&r);
}
static void test_page_transition(void) {
    monitor_renderer_t r;
    assert(monitor_renderer_init(&r, monitor_ui_create));
    assert(monitor_renderer_render(&r));
    uint8_t first[MONITOR_FRAME_BYTES];
    memcpy(first, r.packed, sizeof(first));
    monitor_pages_t *pages = monitor_pages_create(r.root, MONITOR_FRAME_NORMAL, NULL);
    assert(pages && !monitor_pages_active(pages) && !monitor_pages_is_second(pages));
    assert(monitor_renderer_render(&r));
    unsigned full_frames = 0, frames = 0, verified = 0, quiet_frames = 0, band_tiles = 0;
    uint16_t *first_pixels = malloc(MONITOR_RGB565_BYTES);
    uint16_t *second_pixels = malloc(MONITOR_RGB565_BYTES);
    uint16_t *actual = malloc(MONITOR_RGB565_BYTES);
    assert(first_pixels && second_pixels && actual);
    monitor_unpack_to_rgb565(first, 400, 300, MONITOR_FRAME_NORMAL, first_pixels);
    for(unsigned direction = 0; direction < 2; ++direction) {
        assert(monitor_pages_switch(pages, direction == 0));
        assert(!monitor_pages_switch(pages, direction == 0));
        int32_t start_pos = monitor_pages_position(pages);
        lv_tick_inc(320); /* HPM preparation: no LVGL timer runs in this interval. */
        monitor_pages_resume(pages);
        lv_anim_refr_now();
        assert(monitor_pages_active(pages) && monitor_pages_position(pages) == start_pos);
        while(monitor_pages_active(pages)) {
            memset(&r.dirty, 0, sizeof(r.dirty));
            lv_tick_inc(32);
            lv_anim_refr_now();
            lv_timer_handler();
            assert(monitor_renderer_render(&r));
            unsigned tiles = 0;
            for(unsigned i = 0; i < sizeof(r.dirty.bits); ++i)
                tiles += (unsigned)__builtin_popcount(r.dirty.bits[i]);
            if(tiles > 4000) ++full_frames;
            else band_tiles += tiles;
            if(!tiles) ++quiet_frames;
            if(direction == 1 && monitor_pages_active(pages)) {
                int edge = monitor_pages_position(pages);
                if(edge > 8 && edge < 390) {
                    monitor_unpack_to_rgb565(r.packed, 400, 300, MONITOR_FRAME_NORMAL, actual);
                    for(int y = 0; y < 300; ++y) for(int x = 0; x < 400; ++x) {
                        if(x < edge - 3) assert(actual[y * 400 + x] == first_pixels[y * 400 + x]);
                        if(x > edge + 3) assert(actual[y * 400 + x] == second_pixels[y * 400 + x]);
                    }
                    ++verified;
                }
            }
            assert(++frames < 64);
        }
        assert(monitor_pages_is_second(pages) == (direction == 0));
        if(direction == 0) monitor_unpack_to_rgb565(r.packed, 400, 300, MONITOR_FRAME_NORMAL, second_pixels);
    }
    assert(verified > 0);
    free(first_pixels); free(second_pixels); free(actual);
    assert(!memcmp(first, r.packed, sizeof(first)));
    /* Only the first frame of each sweep may touch the whole screen; every later
     * frame carries just the columns the edge crossed, and a repeated column is a
     * no-op that leaves the damage bitmap empty. */
    assert(full_frames == 2);
    assert(quiet_frames >= 1 && band_tiles <= 2 * MONITOR_TILE_COUNT);
    assert(lv_timer_handler() == LV_NO_TIMER_READY);
    assert(monitor_pages_switch(pages, true));
    monitor_pages_settle(pages);
    assert(!monitor_pages_active(pages) && monitor_pages_is_second(pages));
    assert(monitor_pages_switch(pages, true)); /* NEXT from page two wraps to one. */
    monitor_pages_settle(pages);
    assert(!monitor_pages_is_second(pages));
    assert(monitor_pages_switch(pages, false)); /* PREVIOUS from page one wraps to two. */
    monitor_pages_settle(pages);
    assert(monitor_pages_is_second(pages));
    assert(monitor_pages_switch(pages, false));
    /* Rebuilding the first page removes its sibling and cancels animation. */
    assert(monitor_renderer_rebuild(&r, monitor_ui_create));
    assert(!monitor_pages_from_first(r.root));
    assert(lv_obj_get_child_count(lv_obj_get_parent(r.root)) == 1);
    monitor_renderer_deinit(&r);
    lv_tick_inc(500);
    lv_timer_handler();
    printf("Page transitions: %u frames, %u full redraws, %u no-op frames, %u band tiles, idle timer stopped\n",
           frames, full_frames, quiet_frames, band_tiles);
}

int main(void) {
    test_timer_render();
    test_page_transition();
    test_roll_damage(MONITOR_FRAME_NORMAL);
    test_roll_damage(MONITOR_FRAME_INVERTED);
    monitor_renderer_t r;assert(monitor_renderer_init(&r,monitor_ui_create));
    frame(&r); /* initial full invalidation */
    frames=old_bytes=new_bytes=windows=0;old_us=new_us=render_us=0;
    monitor_ui_snapshot_t ui={.time_text="19:59",.wifi_state=MONITOR_WIFI_CONNECTED,
        .battery_percent=78,.temperature_tenths_c=245,.humidity_percent=58,.aqi=42};
    monitor_ui_apply_snapshot(r.root,&ui);
    for(unsigned n=0;n<24;++n) {monitor_ui_advance(r.root,monitor_animation_frame_time(n+1,1000)-monitor_animation_frame_time(n,1000));frame(&r);}
    ui.time_text="20:00";ui.temperature_tenths_c=257;
    monitor_ui_apply_snapshot(r.root,&ui);
    monitor_devices_snapshot_t devices={.count=1,.status=MONITOR_DEVICES_READY};
    strcpy(devices.devices[0].name,"NAS");strcpy(devices.devices[0].uptime,"2d");
    monitor_ui_apply_devices_snapshot(r.root,&devices);
    for(unsigned n=0;n<32;++n) {monitor_ui_advance(r.root,monitor_animation_frame_time(n+1,1000)-monitor_animation_frame_time(n,1000));frame(&r);}
    printf("Real LVGL host animation: %u frames, old/new plan CPU %.2f/%.2f us/frame; old/new payload %.1f/%.1f bytes/frame; windows %.2f/frame; render %.2f us/frame\n",
        frames,old_us/frames,new_us/frames,(double)old_bytes/frames,(double)new_bytes/frames,(double)windows/frames,render_us/frames);
    memset(&r.dirty,0,sizeof(r.dirty));assert(monitor_renderer_render(&r));
    monitor_damage_plan_t plan;monitor_damage_plan(&r.dirty,r.packed,committed,&plan);
    assert(!plan.count && !plan.compared_tiles);
    monitor_renderer_invalidate_all(&r);assert(r.force_full);frame(&r);
    monitor_renderer_deinit(&r);
}

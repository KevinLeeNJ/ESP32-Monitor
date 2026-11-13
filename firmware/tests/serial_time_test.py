"""Exercise authenticated serial RTT selection, stale replies and timeouts."""
from host_harness import REPO, function, run

source = (REPO / 'firmware/components/firmware/monitor_serial_service.c').read_text(encoding='utf-8')
# Timestamp capture must precede decoding and transmission, including lock waits.
rx = function(source, 'serial_task')
assert rx.index('usb_serial_jtag_read_bytes(') < rx.index('ctx->rx_us = esp_timer_get_time()') < rx.index('monitor_serial_feed(')
tx = function(source, 'tx_bytes')
assert tx.index('xSemaphoreTake(') < tx.index('usb_serial_jtag_wait_tx_done(') < tx.index('*sent_us = esp_timer_get_time()') < tx.index('usb_serial_jtag_write_bytes(')

code = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "monitor_serial_wire.h"
#define TAG "serial"
#define ESP_LOGI(...) ((void)0)
typedef struct {
    int64_t rx_us, time_t0_us, time_retry_us, time_best_rtt_us;
    int64_t time_best_utc_us, time_best_rx_us;
    uint32_t tx_seq, time_request_seq;
    unsigned time_attempts, time_samples;
    bool time_synced;
} serial_ctx_t;
static int64_t mono_us, applied_utc, applied_rx;
static bool needs_sync, apply_ok = true, send_ok = true;
static unsigned applies;
static int64_t esp_timer_get_time(void) { return mono_us; }
static bool monitor_clock_needs_sync(void) { return needs_sync; }
static bool monitor_clock_apply_serial_time(int64_t utc, int64_t rx) {
    applies++; applied_utc = utc; applied_rx = rx; return apply_ok;
}
static bool send_frame(serial_ctx_t *ctx, uint8_t type, const uint8_t *p, size_t n, bool auth) {
    assert(type == MONITOR_SERIAL_TIME_REQUEST && auth && n == 2 && !memcmp(p, "?\n", 2));
    if (!send_ok) return false;
    ctx->tx_seq++; ctx->time_t0_us = mono_us; return true;
}
'''
code += function(source, 'handle_time_response')
code += function(source, 'sync_serial_time')
code += r'''
static void reply(serial_ctx_t *ctx, int64_t rtt, uint32_t request) {
    char line[32];
    int n = snprintf(line, sizeof line, "%u 1792972800123\n", request);
    ctx->rx_us = ctx->time_t0_us + rtt;
    mono_us = ctx->rx_us + 200000; // Parsing runs late; RTT must still use the captured RX time.
    handle_time_response(ctx, (const uint8_t *)line, (size_t)n);
}
int main(void) {
    serial_ctx_t ctx = {0};
    mono_us = 1000000;
    const int64_t rtts[] = {40000, 18000, 62000, 12000, 28000};
    int64_t best_rx = 0;
    for (unsigned i = 0; i < 5; i++) {
        sync_serial_time(&ctx);
        assert(ctx.time_request_seq && ctx.time_attempts == i + 1);
        reply(&ctx, rtts[i], ctx.time_request_seq);
        if (i == 3) best_rx = ctx.rx_us;
    }
    sync_serial_time(&ctx);
    assert(applies == 1 && ctx.time_synced && applied_rx == best_rx);
    assert(applied_utc == 1792972800129000LL && ctx.time_best_rtt_us == 12000);
    mono_us += 60000001; sync_serial_time(&ctx);
    assert(!ctx.time_request_seq); // Trusted RTC suppresses more requests.
    needs_sync = true; sync_serial_time(&ctx); // Daily 02:00 invalidation.
    assert(ctx.time_request_seq);
    uint32_t request = ctx.time_request_seq;
    reply(&ctx, 10000, request + 1);
    assert(ctx.time_request_seq == request && !ctx.time_samples);
    reply(&ctx, -1, request);
    assert(ctx.time_request_seq == request && !ctx.time_samples);
    reply(&ctx, 1000001, request);
    assert(ctx.time_request_seq == request && !ctx.time_samples);
    handle_time_response(&ctx, (const uint8_t *)"1 9999999999999\n", 16);
    assert(ctx.time_request_seq == request && !ctx.time_samples);
    reply(&ctx, 10000, request);
    reply(&ctx, 10000, request); // Duplicate cannot become a second sample.
    assert(ctx.time_samples == 1);
    // Fewer than three valid replies never set the RTC; retry after a minute.
    for (unsigned i = 1; i < 5; i++) {
        sync_serial_time(&ctx);
        mono_us += 1000001;
    }
    sync_serial_time(&ctx);
    assert(applies == 1 && !ctx.time_request_seq && !ctx.time_samples);
    assert(ctx.time_retry_us > mono_us);
    // Three valid samples out of five attempts are sufficient.
    ctx = (serial_ctx_t){0};
    for (unsigned i = 0; i < 5; i++) {
        sync_serial_time(&ctx);
        if (i < 3) reply(&ctx, 20000, ctx.time_request_seq);
        else mono_us += 1000001;
    }
    sync_serial_time(&ctx);
    assert(applies == 2 && ctx.time_synced);
    ctx = (serial_ctx_t){0}; send_ok = false;
    sync_serial_time(&ctx);
    assert(!ctx.time_request_seq && !ctx.time_attempts && ctx.time_retry_us > mono_us);
}
'''
run(code, libraries=('-I' + str(REPO / 'firmware/src/companion'),))

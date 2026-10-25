#include "monitor_damage.h"
#include <string.h>

void monitor_damage_mark(monitor_damage_t *d, int x1, int y1, int x2, int y2)
{
    if (x1 > x2 || y1 > y2 || x2 < 0 || y2 < 0 || x1 >= 400 || y1 >= 300) return;
    if (x1 < 0) x1 = 0;
    if (y1 < 0) y1 = 0;
    if (x2 > 399) x2 = 399;
    if (y2 > 299) y2 = 299;
    for (int p = x1 / 2; p <= x2 / 2; ++p) {
        for (int t = (299 - y2) / 12; t <= (299 - y1) / 12; ++t) {
            unsigned i = (unsigned)p * 25U + (unsigned)t;
            d->bits[i / 8U] |= (uint8_t)(1U << (i % 8U));
        }
    }
}
static unsigned area(monitor_window_t w)
{
    return (w.p2 - w.p1 + 1U) * (w.t2 - w.t1 + 1U) * 3U;
}
static monitor_window_t join(monitor_window_t a, monitor_window_t b)
{
    return (monitor_window_t){a.p1 < b.p1 ? a.p1 : b.p1, a.p2 > b.p2 ? a.p2 : b.p2,
                              a.t1 < b.t1 ? a.t1 : b.t1, a.t2 > b.t2 ? a.t2 : b.t2};
}
static void add_run(monitor_damage_plan_t *plan, monitor_window_t w)
{
    /* Join identical spans in adjacent pages before considering any gaps. */
    for (unsigned n = 0; n < plan->count; ++n) {
        monitor_window_t *v = &plan->windows[n];
        if (v->t1 == w.t1 && v->t2 == w.t2 && v->p2 + 1U == w.p1) {
            v->p2 = w.p2;
            return;
        }
    }
    if (plan->count < MONITOR_MAX_WINDOWS) {
        plan->windows[plan->count++] = w;
        return;
    }
    /* Nine candidates, eight slots: merge the pair with the least added
     * payload. Keep distant widgets separate even for fragmented glyphs. */
    unsigned best_a = 0, best_b = 1;
    int best_cost = 15001;
    for (unsigned a = 0; a < plan->count; ++a) {
        for (unsigned b = a + 1; b <= plan->count; ++b) {
            monitor_window_t z = b == plan->count ? w : plan->windows[b];
            int cost = (int)area(join(plan->windows[a], z))
                     - (int)area(plan->windows[a]) - (int)area(z);
            if (cost < best_cost) { best_cost = cost; best_a = a; best_b = b; }
        }
    }
    if (best_b == plan->count) plan->windows[best_a] = join(plan->windows[best_a], w);
    else {
        plan->windows[best_a] = join(plan->windows[best_a], plan->windows[best_b]);
        plan->windows[best_b] = w;
    }
}
void monitor_damage_plan(const monitor_damage_t *d, const uint8_t *f,
                         const uint8_t *old, monitor_damage_plan_t *plan)
{
    memset(plan, 0, sizeof(*plan));
    /* Enumerate only set bits; no frame reads for clean tiles. Build horizontal
     * runs in panel order, then greedily merge only when total cost decreases. */
    monitor_window_t bounds = {199, 0, 24, 0}, run = {0};
    bool pending = false;
    for (unsigned b = 0; b < MONITOR_DIRTY_BYTES; ++b) {
        unsigned bits = d->bits[b];
        while (bits) {
            unsigned bit = (unsigned)__builtin_ctz(bits);
            bits &= bits - 1U;
            unsigned i = b * 8U + bit;
            ++plan->compared_tiles;
            if (memcmp(f + i * 3U, old + i * 3U, 3) == 0) continue;
            ++plan->changed_tiles;
            monitor_window_t w = {(uint8_t)(i / 25U), (uint8_t)(i / 25U),
                                  (uint8_t)(i % 25U), (uint8_t)(i % 25U)};
            if (plan->changed_tiles == 1) bounds = w;
            else bounds = join(bounds, w);
            if (pending && run.p1 == w.p1 && run.t2 + 1U == w.t1) run.t2 = w.t2;
            else {
                if (pending) add_run(plan, run);
                run = w;
                pending = true;
            }
        }
    }
    if (pending) add_run(plan, run);
    if (!plan->changed_tiles) return;
    /* Consolidate expanded windows, including overlaps, without unbounded lists. */
    for (unsigned a = 0; a < plan->count; ++a) {
        for (unsigned b = a + 1; b < plan->count;) {
            monitor_window_t w = join(plan->windows[a], plan->windows[b]);
            if (area(w) <= area(plan->windows[a]) + area(plan->windows[b]) + MONITOR_WINDOW_COST) {
                plan->windows[a] = w;
                plan->windows[b] = plan->windows[--plan->count];
                a = (unsigned)-1; /* restart after expansion */
                break;
            }
            ++b;
        }
    }
    unsigned bytes = 0;
    for (unsigned n = 0; n < plan->count; ++n) bytes += area(plan->windows[n]);
    if (area(bounds) + MONITOR_WINDOW_COST <= bytes + plan->count * MONITOR_WINDOW_COST) {
        plan->count = 1;
        plan->windows[0] = bounds;
        bytes = area(bounds);
    }
    if (bytes + plan->count * MONITOR_WINDOW_COST >= 15000U + MONITOR_WINDOW_COST) {
        plan->count = 1;
        plan->windows[0] = (monitor_window_t){0,199,0,24};
        bytes = 15000;
    }
    plan->bytes = (uint16_t)bytes;
}

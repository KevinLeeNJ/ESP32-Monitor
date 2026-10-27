#pragma once
#include <stdint.h>

/* Debounced levels: bit 0 BOOT, bit 1 KEY. Any chord or long hold cancels
 * the candidate, including a chord released one key at a time. */
typedef struct { uint32_t down_at; uint8_t previous, candidate; } monitor_page_buttons_t;
static inline void monitor_page_buttons_init(monitor_page_buttons_t *s, uint8_t held)
{ s->down_at = 0; s->previous = held; s->candidate = 0; }
static inline uint8_t monitor_page_buttons_update(monitor_page_buttons_t *s, uint8_t held,
                                                   uint32_t now, uint32_t max_ticks)
{
    uint8_t action = 0;
    if(held == 3) s->candidate = 0;
    else if(s->previous == 0 && (held == 1 || held == 2)) {
        s->candidate = held;
        s->down_at = now;
    } else if(held == 0 && s->previous == s->candidate && s->candidate != 0) {
        if((uint32_t)(now - s->down_at) < max_ticks) action = s->candidate;
        s->candidate = 0;
    } else if(held != s->previous) s->candidate = 0;
    s->previous = held;
    return action;
}

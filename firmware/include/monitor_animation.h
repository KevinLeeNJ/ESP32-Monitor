#pragma once

#include <stdint.h>

/* ST7305 v0.2 pp76/92: OSCSET=A6 E9, FRCTRL=12 selects HPM 32Hz,
 * LPM 1Hz. Share this nominal rate between the panel and frame producers. */
#define MONITOR_UI_ANIMATION_FPS 32U
/* Round up a single scan wait; periodic scheduling uses fractional deadlines. */
#define MONITOR_UI_ANIMATION_FRAME_MS ((1000U + MONITOR_UI_ANIMATION_FPS - 1U) / MONITOR_UI_ANIMATION_FPS)

static inline uint32_t monitor_animation_frame_time(uint32_t frame, uint32_t timebase_hz)
{
    return (uint32_t)(((uint64_t)frame * timebase_hz + MONITOR_UI_ANIMATION_FPS - 1U) /
                      MONITOR_UI_ANIMATION_FPS);
}

/* Absolute offset from the animation epoch, strictly after elapsed. Skip
 * expired slots after an overrun rather than producing catch-up bursts. */
static inline uint32_t monitor_animation_next_frame(uint32_t elapsed, uint32_t timebase_hz)
{
    uint32_t frame = (uint32_t)((uint64_t)elapsed * MONITOR_UI_ANIMATION_FPS / timebase_hz) + 1U;
    return monitor_animation_frame_time(frame, timebase_hz);
}

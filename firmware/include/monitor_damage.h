#pragma once
#include <stdbool.h>
#include <stdint.h>

#define MONITOR_TILE_PAGES 200U
#define MONITOR_TILE_COLUMNS 25U
#define MONITOR_TILE_COUNT 5000U
#define MONITOR_DIRTY_BYTES 625U
#define MONITOR_MAX_WINDOWS 8U
/* Equivalent payload budget for seven small SPI command/parameter transactions.
 * At 10 MHz, 96 bytes = 76.8 us. Also bounds extra pixels sent by merging. */
#define MONITOR_WINDOW_COST 96U

typedef struct { uint8_t bits[MONITOR_DIRTY_BYTES]; } monitor_damage_t;
/* Inclusive packed coordinates: page=x/2, tile=(299-y)/12.
 * Each page has 25 contiguous 3-byte tiles. Hardware columns run 0x2a-t. */
typedef struct { uint8_t p1, p2, t1, t2; } monitor_window_t;
typedef struct {
    monitor_window_t windows[MONITOR_MAX_WINDOWS];
    uint16_t compared_tiles;
    uint16_t changed_tiles;
    uint16_t bytes;
    uint8_t count;
} monitor_damage_plan_t;
void monitor_damage_mark(monitor_damage_t *dirty, int x1, int y1, int x2, int y2);
void monitor_damage_plan(const monitor_damage_t *dirty, const uint8_t *frame,
                         const uint8_t *committed, monitor_damage_plan_t *plan);

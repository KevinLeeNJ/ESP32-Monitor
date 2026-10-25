#pragma once

#include "freertos/FreeRTOS.h"

/* atomic.h's empty fallback creates unused static function definitions on
 * Xtensa. Keep any port-provided definition, otherwise use static inline.
 * This header is force-included only when compiling LVGL's FreeRTOS adapter. */
#ifndef portFORCE_INLINE
#define portFORCE_INLINE inline
#include "atomic.h"
#undef portFORCE_INLINE
#else
#include "atomic.h"
#endif

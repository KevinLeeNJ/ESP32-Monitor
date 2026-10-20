#pragma once

#ifdef __cplusplus
extern "C" {
#endif

/* Register the current task as the sole owner of display rendering. */
void monitor_display_refresh_register(void);

/* Wake the display task after a user-visible monitor value has changed. */
void monitor_display_refresh_request(void);

#ifdef __cplusplus
}
#endif

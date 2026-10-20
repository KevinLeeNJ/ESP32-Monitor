#include "monitor_display_refresh.h"

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

static TaskHandle_t volatile s_display_task;

void monitor_display_refresh_register(void)
{
    s_display_task = xTaskGetCurrentTaskHandle();
}

void monitor_display_refresh_request_reason(uint32_t reason_bits)
{
    TaskHandle_t display_task = s_display_task;
    if (display_task != NULL) xTaskNotify(display_task, reason_bits, eSetBits);
}

void monitor_display_refresh_request(void)
{
    monitor_display_refresh_request_reason(MONITOR_REFRESH_RECOVER);
}

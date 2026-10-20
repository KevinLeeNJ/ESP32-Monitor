#include "monitor_display_refresh.h"

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

static TaskHandle_t volatile s_display_task;

void monitor_display_refresh_register(void)
{
    s_display_task = xTaskGetCurrentTaskHandle();
}

void monitor_display_refresh_request(void)
{
    TaskHandle_t display_task = s_display_task;
    if (display_task != NULL) {
        xTaskNotifyGive(display_task);
    }
}

#include "lvgl.h"
#include "esp_heap_caps.h"

/* UI objects and software drawing buffers must leave internal RAM for
 * Wi-Fi, task stacks and the configuration server. */
void lv_mem_init(void) {}
void lv_mem_deinit(void) {}

lv_mem_pool_t lv_mem_add_pool(void *mem, size_t bytes)
{
    LV_UNUSED(mem);
    LV_UNUSED(bytes);
    return NULL;
}

void lv_mem_remove_pool(lv_mem_pool_t pool) { LV_UNUSED(pool); }

void *lv_malloc_core(size_t size)
{
    return heap_caps_malloc_prefer(size, 2, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT,
                                  MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
}

void *lv_realloc_core(void *ptr, size_t size)
{
    return heap_caps_realloc_prefer(ptr, size, 2, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT,
                                   MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
}

void lv_free_core(void *ptr) { heap_caps_free(ptr); }

void lv_mem_monitor_core(lv_mem_monitor_t *monitor) { LV_UNUSED(monitor); }
lv_result_t lv_mem_test_core(void) { return LV_RESULT_OK; }

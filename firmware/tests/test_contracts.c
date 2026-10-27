/* The same contract cases execute with real Unity on host and ESP32-S3.
 * Assertions derive from API contracts, not copied production algorithms. */
#include "unity.h"
#include <limits.h>
#include <string.h>
#include "monitor_ui.h"
#include "monitor_devices_data.h"
#include "monitor_schedule.h"
#include "monitor_display_refresh.h"
#include "monitor_animation.h"
#include "cJSON.h"

#ifdef ESP_PLATFORM
#define CONTRACT(symbol, description, tags) TEST_CASE(description, tags)
#else
#define CONTRACT(symbol, description, tags) static void symbol(void)
void setUp(void) {}
void tearDown(void) {}
#endif

CONTRACT(schedule_boundaries, "schedule coalesces boundaries and time corrections", "[schedule]")
{
    const uint32_t all = MONITOR_SCHEDULE_DEVICES | MONITOR_SCHEDULE_SENSORS | MONITOR_SCHEDULE_WEATHER;
    TEST_ASSERT_EQUAL_UINT32(all, monitor_schedule_due(-1, 0));
    TEST_ASSERT_EQUAL_UINT32(0, monitor_schedule_due(0, 0));
    TEST_ASSERT_EQUAL_UINT32(0, monitor_schedule_due(900, 929));
    /* Sensors run at :30; minute/quarter-hour boundaries keep the API cadence. */
    TEST_ASSERT_EQUAL_UINT32(MONITOR_SCHEDULE_DEVICES | MONITOR_SCHEDULE_SENSORS, monitor_schedule_due(929, 930));
    TEST_ASSERT_EQUAL_UINT32(MONITOR_SCHEDULE_DEVICES, monitor_schedule_due(959, 960));
    TEST_ASSERT_EQUAL_UINT32(MONITOR_SCHEDULE_DEVICES | MONITOR_SCHEDULE_WEATHER, monitor_schedule_due(1799, 1800));
    TEST_ASSERT_EQUAL_UINT32(all, monitor_schedule_due(86400, 86399));
    TEST_ASSERT_EQUAL_UINT32(all, monitor_schedule_due(0, 4102444800LL));
    /* Every microsecond remainder, including just before a tick and a slot. */
    for (int64_t us = 0; us < 30000000; us += 997) {
        uint32_t delay = monitor_schedule_delay_ms(4102444800000000LL + us);
        TEST_ASSERT_TRUE(delay >= 1 && delay <= 30000);
        int64_t wake = us + delay * 1000LL;
        TEST_ASSERT_TRUE(wake >= 30000000 && wake < 30001000);
    }
    TEST_ASSERT_EQUAL_UINT32(1, monitor_schedule_delay_ms(29999999));
    TEST_ASSERT_EQUAL_UINT32(30000, monitor_schedule_delay_ms(30000000));
}

CONTRACT(refresh_combinations, "display batches every combination of producer reasons", "[display]")
{
    /* All 128 combinations of producer and LVGL timer reason bits. */
    for (uint32_t reasons = 0; reasons < 128; ++reasons) {
        bool immediate = (reasons & (MONITOR_REFRESH_CLOCK | MONITOR_REFRESH_WIFI | MONITOR_REFRESH_RECOVER | MONITOR_REFRESH_LVGL)) != 0;
        TEST_ASSERT_EQUAL(immediate, monitor_display_refresh_should_render(reasons, true));
        TEST_ASSERT_TRUE(monitor_display_refresh_should_render(reasons, false));
    }
    TEST_ASSERT_TRUE(monitor_display_refresh_should_render(MONITOR_REFRESH_PAGE_NEXT, true));
    TEST_ASSERT_TRUE(monitor_display_refresh_should_render(MONITOR_REFRESH_PAGE_PREVIOUS, true));
}

CONTRACT(animation_cadence, "animation deadlines skip overruns at different tick rates", "[animation]")
{
    const uint32_t rates[] = {100, 1000, 1000000};
    for (unsigned i = 0; i < sizeof(rates) / sizeof(*rates); ++i) {
        uint32_t hz = rates[i];
        TEST_ASSERT_EQUAL_UINT32(hz, monitor_animation_frame_time(32, hz));
        TEST_ASSERT_EQUAL_UINT32(10 * hz, monitor_animation_frame_time(320, hz));
        for (uint32_t elapsed = 0; elapsed < 2 * hz; elapsed += hz / 100) {
            uint32_t next = monitor_animation_next_frame(elapsed, hz);
            TEST_ASSERT_TRUE(next > elapsed);
            TEST_ASSERT_TRUE(next - elapsed <= (hz + 31) / 32);
        }
    }
    TEST_ASSERT_EQUAL_UINT32(10063, monitor_animation_next_frame(10040, 1000));
    TEST_ASSERT_EQUAL_UINT32(1342177280, monitor_animation_frame_time(UINT32_MAX, 10));
}

CONTRACT(pack_invalid, "invalid I1 flushes leave destination and guards intact", "[pack]")
{
    uint8_t dest[18], before[18], pixels[8] = {0};
    memset(dest, 0xa5, sizeof(dest)); memcpy(before, dest, sizeof(dest));
    const int cases[][7] = {
        {0,8,1,0,0,0,0}, {16,0,1,0,0,0,0}, {15,8,1,0,0,0,0},
        {16,7,1,0,0,0,0}, {16,8,1,-1,0,0,0}, {16,8,1,0,-1,0,0},
        {16,8,1,2,0,1,0}, {16,8,1,0,2,0,1}, {16,8,1,0,0,16,0},
        {16,8,1,0,0,0,8}, {16,8,0,0,0,0,0}, {16,8,1,0,0,8,0}
    };
    for (unsigned i = 0; i < sizeof(cases) / sizeof(*cases); ++i) {
        const int *c = cases[i];
        monitor_pack_i1_region(pixels,c[2],c[0],c[1],c[3],c[4],c[5],c[6],dest+1);
        TEST_ASSERT_EQUAL_MEMORY(before, dest, sizeof(dest));
    }
    monitor_pack_i1_region(NULL,1,16,8,0,0,0,0,dest+1);
    monitor_pack_i1_region(pixels,1,16,8,0,0,0,0,NULL);
    TEST_ASSERT_EQUAL_MEMORY(before, dest, sizeof(dest));
}

CONTRACT(pack_pixels, "each panel pixel has unique mapping and inverted polarity", "[pack]")
{
    /* Exhaust all positions of a 16x8 panel; isolate each black pixel. */
    for (unsigned y = 0; y < 8; ++y) for (unsigned x = 0; x < 16; ++x) {
        uint8_t dest[18], black = 0;
        uint16_t normal[128], inverted[128];
        memset(dest, 0xff, sizeof(dest));
        monitor_pack_i1_region(&black,1,16,8,x,y,x,y,dest+1);
        TEST_ASSERT_EQUAL_HEX8(0xff, dest[0]);
        TEST_ASSERT_EQUAL_HEX8(0xff, dest[17]);
        monitor_unpack_to_rgb565(dest+1,16,8,MONITOR_FRAME_NORMAL,normal);
        monitor_unpack_to_rgb565(dest+1,16,8,MONITOR_FRAME_INVERTED,inverted);
        for (unsigned i = 0; i < 128; ++i) {
            TEST_ASSERT_EQUAL_HEX16(i == y*16+x ? 0 : 0xffff, normal[i]);
            TEST_ASSERT_EQUAL_HEX16(normal[i] ^ 0xffff, inverted[i]);
        }
        /* Drawing white restores exactly the original frame. */
        uint8_t white = 0x80;
        monitor_pack_i1_region(&white,1,16,8,x,y,x,y,dest+1);
        for (unsigned i = 0; i < sizeof(dest); ++i) TEST_ASSERT_EQUAL_HEX8(0xff,dest[i]);
    }
}

CONTRACT(config_boundaries, "URL normalization and token validation honor limits", "[devices]")
{
    TEST_ASSERT_FALSE(monitor_devices_normalize_url(NULL));
    TEST_ASSERT_FALSE(monitor_devices_valid_token(NULL));
    char token[66]; memset(token,'A',64); token[64]=0;
    TEST_ASSERT_TRUE(monitor_devices_valid_token(token));
    for (unsigned i = 0; i < 64; ++i) {
        char saved=token[i]; token[i]='g';
        TEST_ASSERT_FALSE(monitor_devices_valid_token(token)); token[i]=saved;
    }
    token[63]=0; TEST_ASSERT_FALSE(monitor_devices_valid_token(token));
    token[63]='0';token[64]='0';token[65]=0; TEST_ASSERT_FALSE(monitor_devices_valid_token(token));
    char url[MONITOR_DEVICES_URL_MAX+2];
    memcpy(url,"https://",8); memset(url+8,'a',sizeof(url)-9); url[sizeof(url)-1]=0;
    TEST_ASSERT_FALSE(monitor_devices_normalize_url(url));
    url[MONITOR_DEVICES_URL_MAX]=0; TEST_ASSERT_TRUE(monitor_devices_normalize_url(url));
    strcpy(url,"http://[::1]:1///"); TEST_ASSERT_TRUE(monitor_devices_normalize_url(url));
    TEST_ASSERT_EQUAL_STRING("http://[::1]:1",url);
    TEST_ASSERT_TRUE(monitor_devices_normalize_url(url));
    const char *bad[] = {"http://host:00065536", "http://host:99999999999999999999", "http://host:0", "http://host\r\nX", "http://host/%20", "http://@host", "http://host#fragment"};
    for (unsigned i=0; i<sizeof(bad)/sizeof(*bad); ++i) {
        strcpy(url,bad[i]); TEST_ASSERT_FALSE_MESSAGE(monitor_devices_normalize_url(url),bad[i]);
    }
}

static const char empty[] = "{\"device_count\":0,\"devices\":[]}";
static void reject(const char *body, size_t len)
{
    monitor_devices_snapshot_t out, before;
    memset(&out,0xa5,sizeof(out)); memcpy(&before,&out,sizeof(out));
    TEST_ASSERT_FALSE(monitor_devices_parse(body,len,&out));
    TEST_ASSERT_EQUAL_MEMORY(&before,&out,sizeof(out));
}

CONTRACT(parser_lengths, "JSON truncation and size failures preserve cached snapshot", "[devices]")
{
    reject(NULL,0); reject(empty,0);
    TEST_ASSERT_FALSE(monitor_devices_parse(empty,strlen(empty),NULL));
    for (size_t n=1; n<strlen(empty); ++n) {
        char truncated[sizeof(empty)]; memcpy(truncated,empty,n); truncated[n]=0;
        reject(truncated,n);
    }
    char body[MONITOR_DEVICES_BODY_MAX+2];
    memcpy(body,empty,strlen(empty));
    memset(body+strlen(empty),' ',MONITOR_DEVICES_BODY_MAX-strlen(empty));
    body[MONITOR_DEVICES_BODY_MAX]=0;
    monitor_devices_snapshot_t out;
    TEST_ASSERT_TRUE(monitor_devices_parse(body,MONITOR_DEVICES_BODY_MAX,&out));
    body[MONITOR_DEVICES_BODY_MAX]=' '; body[MONITOR_DEVICES_BODY_MAX+1]=0;
    reject(body,MONITOR_DEVICES_BODY_MAX+1);
    char embedded[sizeof(empty)]; memcpy(embedded,empty,sizeof(empty)); embedded[2]=0;
    reject(embedded,sizeof(empty)-1);
}

static cJSON *summary(unsigned count)
{
    static const char device[] = "{\"name\":\"ABCDEFGH\",\"uptime\":\"12345678\",\"metric_count\":3,\"metrics\":[{\"name\":\"CPU\",\"value\":\"12345678\",\"percent\":0},{\"name\":\"MEM\",\"value\":\"100%\",\"percent\":100},{\"name\":\"DISK\",\"value\":\"--\",\"percent\":null}]}";
    cJSON *root=cJSON_CreateObject(), *devices=cJSON_CreateArray();
    cJSON_AddNumberToObject(root,"device_count",count);
    cJSON_AddItemToObject(root,"devices",devices);
    for (unsigned i=0; i<count; ++i) cJSON_AddItemToArray(devices,cJSON_Parse(device));
    return root;
}

CONTRACT(parser_capacity, "all device counts accept boundary metrics and own their data", "[devices]")
{
    for (unsigned count=0; count<=4; ++count) {
        cJSON *root=summary(count); char *body=cJSON_PrintUnformatted(root);
        TEST_ASSERT_NOT_NULL(body);
        monitor_devices_snapshot_t out;
        memset(&out,0xa5,sizeof(out));
        TEST_ASSERT_TRUE(monitor_devices_parse(body,strlen(body),&out));
        memset(body,'?',strlen(body)); cJSON_free(body); cJSON_Delete(root);
        TEST_ASSERT_EQUAL_UINT(count,out.count);
        TEST_ASSERT_EQUAL(MONITOR_DEVICES_READY,out.status);
        for (unsigned i=0; i<count; ++i) {
            TEST_ASSERT_EQUAL_STRING("ABCDEFGH",out.devices[i].name);
            TEST_ASSERT_EQUAL_STRING("12345678",out.devices[i].uptime);
            TEST_ASSERT_EQUAL_STRING("12345678",out.devices[i].metrics[0].value);
            TEST_ASSERT_TRUE(out.devices[i].metrics[0].percent_valid);
            TEST_ASSERT_EQUAL_INT(0,out.devices[i].metrics[0].percent);
            TEST_ASSERT_EQUAL_INT(100,out.devices[i].metrics[1].percent);
            TEST_ASSERT_FALSE(out.devices[i].metrics[2].percent_valid);
        }
        TEST_ASSERT_TRUE(monitor_devices_parse(empty,strlen(empty),&out));
        TEST_ASSERT_EQUAL_UINT(0,out.count);
        TEST_ASSERT_EQUAL_UINT(0,out.version);
        TEST_ASSERT_EQUAL(MONITOR_DEVICES_READY,out.status);
        for (unsigned i=0;i<MONITOR_DEVICES_MAX;++i)
            TEST_ASSERT_EQUAL_STRING("",out.devices[i].name);
    }
}

CONTRACT(parser_schema, "invalid nested schema never commits a partial snapshot", "[devices]")
{
    for (unsigned variant=0; variant<16; ++variant) {
        cJSON *root=summary(4);
        cJSON *devices=cJSON_GetObjectItem(root,"devices");
        /* Corrupt the last device, after three valid candidates were decoded. */
        cJSON *d=cJSON_GetArrayItem(devices,3);
        cJSON *metrics=cJSON_GetObjectItem(d,"metrics"), *m=cJSON_GetArrayItem(metrics,2);
        switch (variant) {
            case 0: cJSON_AddNumberToObject(root,"device_count",4); break;
            case 1: cJSON_AddStringToObject(d,"name","dup"); break;
            case 2: cJSON_AddNumberToObject(m,"percent",0); break;
            case 3: cJSON_DeleteItemFromObject(m,"percent"); break;
            case 4: cJSON_ReplaceItemInObject(m,"percent",cJSON_CreateBool(1)); break;
            case 5: cJSON_ReplaceItemInObject(m,"percent",cJSON_CreateNumber(-1)); break;
            case 6: cJSON_ReplaceItemInObject(m,"percent",cJSON_CreateNumber(101)); break;
            case 7: cJSON_ReplaceItemInObject(m,"percent",cJSON_CreateNumber(0.5)); break;
            case 8: cJSON_ReplaceItemInObject(d,"name",cJSON_CreateString("")); break;
            case 9: cJSON_ReplaceItemInObject(d,"name",cJSON_CreateString("123456789")); break;
            case 10: cJSON_ReplaceItemInObject(d,"uptime",cJSON_CreateString("\n")); break;
            case 11: cJSON_ReplaceItemInObject(m,"value",cJSON_CreateString("中文")); break;
            case 12: cJSON_ReplaceItemInObject(m,"name",cJSON_CreateString("CPU")); break;
            case 13: cJSON_DeleteItemFromArray(metrics,0); break;
            case 14: cJSON_ReplaceItemInObject(d,"metric_count",cJSON_CreateNumber(2)); break;
            case 15: cJSON_ReplaceItemInObject(root,"device_count",cJSON_CreateNumber(5)); break;
        }
        char *body=cJSON_PrintUnformatted(root); TEST_ASSERT_NOT_NULL(body);
        reject(body,strlen(body)); cJSON_free(body); cJSON_Delete(root);
    }
    const char *bad[]={"[]","null","{}","{\"device_count\":0,\"devices\":[]} trailing",
        "{\"device_count\":0,\"devices\":[],\"extra\":\"\\u0000\"}",
        "{\"device_count\":1e999,\"devices\":[]}"};
    for (unsigned i=0; i<sizeof(bad)/sizeof(*bad); ++i) reject(bad[i],strlen(bad[i]));
}

CONTRACT(parser_depth, "JSON depth guard ignores braces inside escaped strings", "[devices]")
{
    char body[256]; monitor_devices_snapshot_t out;
    /* The unknown extra field may nest to the maximum depth of 16. */
    for (unsigned depth=14; depth<=15; ++depth) {
        strcpy(body,"{\"device_count\":0,\"devices\":[],\"extra\":");
        size_t n=strlen(body);
        memset(body+n,'[',depth); n+=depth;
        memset(body+n,']',depth); n+=depth;
        body[n++]='}';body[n]=0;
        /* extra arrays are siblings of devices: depth is root + depth. */
        TEST_ASSERT_TRUE(monitor_devices_parse(body,n,&out));
    }
    strcpy(body,"{\"device_count\":0,\"devices\":[],\"extra\":");
    size_t n=strlen(body); memset(body+n,'[',16);n+=16;
    memset(body+n,']',16);n+=16;body[n++]='}';body[n]=0; reject(body,n);
    const char *quoted="{\"device_count\":0,\"devices\":[],\"extra\":\"[[{\\\"}]]\"}";
    TEST_ASSERT_TRUE(monitor_devices_parse(quoted,strlen(quoted),&out));
}

#ifndef ESP_PLATFORM
int main(void)
{
    UNITY_BEGIN();
    RUN_TEST(schedule_boundaries);
    RUN_TEST(refresh_combinations);
    RUN_TEST(animation_cadence);
    RUN_TEST(pack_invalid);
    RUN_TEST(pack_pixels);
    RUN_TEST(config_boundaries);
    RUN_TEST(parser_lengths);
    RUN_TEST(parser_capacity);
    RUN_TEST(parser_schema);
    RUN_TEST(parser_depth);
    return UNITY_END();
}
#endif

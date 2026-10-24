/* Include the complete production translation unit, not extracted methods.
 * Only the SDK boundary is faked, so signatures and error paths compile too. */
#include "unity.h"
#include <string.h>
#include "../components/firmware/monitor_rtc_pcf85063.c"

static uint8_t registers[16];
static unsigned reads,writes,adds,removes,gives;
static unsigned fail_read,fail_write;
static bool mutex_ok,lock_ok,held;
static int add_error;
SemaphoreHandle_t xSemaphoreCreateMutex(void) { return mutex_ok ? (void *)1 : NULL; }
int xSemaphoreTake(SemaphoreHandle_t lock,unsigned timeout)
{
    TEST_ASSERT_NOT_NULL(lock); TEST_ASSERT_EQUAL_UINT(200,timeout);
    TEST_ASSERT_FALSE(held); if (lock_ok) held=true; return lock_ok;
}
int xSemaphoreGive(SemaphoreHandle_t lock)
{
    TEST_ASSERT_NOT_NULL(lock); TEST_ASSERT_TRUE(held); held=false; ++gives; return 1;
}
esp_err_t i2c_master_bus_add_device(i2c_master_bus_handle_t bus,const i2c_device_config_t *cfg,i2c_master_dev_handle_t *out)
{
    TEST_ASSERT_NOT_NULL(bus); TEST_ASSERT_EQUAL_HEX8(0x51,cfg->device_address);
    TEST_ASSERT_EQUAL_INT(100000,cfg->scl_speed_hz); ++adds;
    if (add_error) return add_error;
    *out=(void *)2; return ESP_OK;
}
esp_err_t i2c_master_bus_rm_device(i2c_master_dev_handle_t dev)
{ TEST_ASSERT_NOT_NULL(dev); ++removes; return ESP_OK; }
esp_err_t i2c_master_transmit_receive(i2c_master_dev_handle_t dev,const uint8_t *in,size_t n,uint8_t *out,size_t len,int timeout)
{
    TEST_ASSERT_NOT_NULL(dev); TEST_ASSERT_EQUAL_UINT(1,n); TEST_ASSERT_EQUAL_INT(100,timeout);
    ++reads; if (reads==fail_read) return ESP_FAIL;
    TEST_ASSERT_TRUE(in[0]+len<=sizeof(registers)); memcpy(out,registers+in[0],len); return ESP_OK;
}
esp_err_t i2c_master_transmit(i2c_master_dev_handle_t dev,const uint8_t *in,size_t len,int timeout)
{
    TEST_ASSERT_NOT_NULL(dev); TEST_ASSERT_EQUAL_INT(100,timeout);
    ++writes; if (writes==fail_write) return ESP_FAIL;
    TEST_ASSERT_TRUE(len>=2 && in[0]+len-1<=sizeof(registers));
    memcpy(registers+in[0],in+1,len-1); return ESP_OK;
}
void setUp(void)
{
    memset(registers,0,sizeof(registers));
    s_device=NULL;s_lock=NULL; reads=writes=adds=removes=gives=0;
    fail_read=fail_write=0;mutex_ok=lock_ok=true; held=false;add_error=0;
}
void tearDown(void) { TEST_ASSERT_FALSE(held); }
static void init(void) { TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_init((void *)1)); }
static void date(unsigned year,unsigned month,unsigned day)
{
    uint8_t r[]={0x59,0x58,0x23,to_bcd(day),0x06,to_bcd(month),to_bcd(year-2000)};
    memcpy(registers+4,r,7);
}
static void test_initialization(void)
{
    TEST_ASSERT_EQUAL(ESP_ERR_INVALID_ARG,monitor_rtc_init(NULL));
    mutex_ok=false; TEST_ASSERT_EQUAL(ESP_ERR_NO_MEM,monitor_rtc_init((void *)1));
    mutex_ok=true;add_error=ESP_FAIL;
    TEST_ASSERT_EQUAL(ESP_FAIL,monitor_rtc_init((void *)1));
    add_error=0;registers[0]=0xc7;init();
    TEST_ASSERT_EQUAL_HEX8(0x05,registers[0]); /* clear test/reserved/12h, preserve CIE/CAP */
    unsigned before=adds;init();TEST_ASSERT_EQUAL_UINT(before,adds);
}
static void test_init_retry_after_io_failure(void)
{
    fail_read=1; TEST_ASSERT_EQUAL(ESP_FAIL,monitor_rtc_init((void *)1));
    fail_read=0;registers[0]=0x02;init();
    TEST_ASSERT_EQUAL_HEX8(0,registers[0]); /* retry must actually configure 24h */
    TEST_ASSERT_EQUAL_UINT(1,removes);
}
static void test_init_retry_after_write_failure(void)
{
    fail_write=1;registers[0]=2;
    TEST_ASSERT_EQUAL(ESP_FAIL,monitor_rtc_init((void *)1));
    fail_write=0;init();TEST_ASSERT_EQUAL_HEX8(0,registers[0]);
}
static void test_calendar(void)
{
    init(); monitor_rtc_sample_t out;
    date(2024,2,29);TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));
    TEST_ASSERT_TRUE(out.present && out.running && out.time_valid && !out.oscillator_stopped);
    TEST_ASSERT_EQUAL_INT64(1709251139LL,out.utc_epoch_s);
    const unsigned bad[][3]={{2025,2,29},{2024,2,30},{2024,4,31},{2024,0,1},{2024,13,1},{2024,1,0},{2023,12,31}};
    for (unsigned i=0;i<sizeof(bad)/sizeof(*bad);++i) {
        date(bad[i][0],bad[i][1],bad[i][2]);
        TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));TEST_ASSERT_FALSE(out.time_valid);
    }
    for (unsigned month=1;month<=12;++month) {
        date(2024,month,1);TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));TEST_ASSERT_TRUE(out.time_valid);
    }
    const uint8_t invalid_bcd[]={0x6a,0x6a,0x2a,0x3a,0x07,0x1a,0xfa};
    for (unsigned reg=4;reg<=10;++reg) {
        date(2024,1,1);registers[reg]=invalid_bcd[reg-4];
        TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));TEST_ASSERT_FALSE(out.time_valid);
    }
}
static void test_clock_flags_and_io(void)
{
    monitor_rtc_sample_t out;
    TEST_ASSERT_EQUAL(ESP_ERR_INVALID_STATE,monitor_rtc_read(&out));
    init();date(2024,1,1);
    registers[4]|=0x80;TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));
    TEST_ASSERT_TRUE(out.oscillator_stopped);TEST_ASSERT_FALSE(out.time_valid);
    registers[4]&=0x7f;registers[0]=0x20;
    TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));TEST_ASSERT_FALSE(out.running || out.time_valid);
    registers[0]=2;TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));TEST_ASSERT_FALSE(out.time_valid);
    registers[0]=0xc5;TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));
    TEST_ASSERT_FALSE(out.running || out.time_valid); /* STOP=0, but EXT_TEST=1 */
    lock_ok=false;TEST_ASSERT_EQUAL(ESP_ERR_TIMEOUT,monitor_rtc_read(&out));lock_ok=true;
    fail_read=reads+1;TEST_ASSERT_EQUAL(ESP_FAIL,monitor_rtc_read(&out));TEST_ASSERT_FALSE(out.present);
    fail_read=reads+2;TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));TEST_ASSERT_FALSE(out.time_valid);
    TEST_ASSERT_EQUAL(ESP_ERR_INVALID_STATE,monitor_rtc_read(NULL));
}
static void test_write_roundtrip(void)
{
    init(); const int64_t dates[]={1704067200LL,1709251139LL,4102444799LL};
    for (unsigned i=0;i<sizeof(dates)/sizeof(*dates);++i) {
        TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_write_utc(dates[i]));
        monitor_rtc_sample_t out;TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));
        TEST_ASSERT_TRUE(out.time_valid);TEST_ASSERT_EQUAL_INT64(dates[i],out.utc_epoch_s);
    }
    unsigned before=writes;
    TEST_ASSERT_EQUAL(ESP_ERR_INVALID_ARG,monitor_rtc_write_utc(1704067199LL));
    TEST_ASSERT_EQUAL(ESP_ERR_INVALID_ARG,monitor_rtc_write_utc(4102444800LL));
    TEST_ASSERT_EQUAL_UINT(before,writes);
    lock_ok=false;TEST_ASSERT_EQUAL(ESP_ERR_TIMEOUT,monitor_rtc_write_utc(dates[0]));
}
static void test_write_read_failure_preserves_control(void)
{
    init();registers[0]=0x80;unsigned before=writes;fail_read=reads+1;
    TEST_ASSERT_EQUAL(ESP_FAIL,monitor_rtc_write_utc(1704067200LL));
    TEST_ASSERT_EQUAL_UINT(before,writes);
    TEST_ASSERT_EQUAL_HEX8(0x80,registers[0]);
}
static void test_write_exits_external_test_mode(void)
{
    init();registers[0]=0xc7;
    TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_write_utc(1704067200LL));
    TEST_ASSERT_EQUAL_HEX8(0x05,registers[0]);
    monitor_rtc_sample_t out;
    TEST_ASSERT_EQUAL(ESP_OK,monitor_rtc_read(&out));
    TEST_ASSERT_TRUE(out.running && out.time_valid);
}
static void test_write_failures_release_lock_and_restart(void)
{
    for (unsigned stage=1;stage<=3;++stage) {
        setUp();init();registers[0]=0x05;fail_write=writes+stage;
        TEST_ASSERT_EQUAL(ESP_FAIL,monitor_rtc_write_utc(1704067200LL));
        TEST_ASSERT_FALSE(held);
        if (stage<3) TEST_ASSERT_EQUAL_HEX8(0x05,registers[0]);
    }
}
int main(void)
{
    UNITY_BEGIN();
    RUN_TEST(test_initialization);
    RUN_TEST(test_init_retry_after_io_failure);
    RUN_TEST(test_init_retry_after_write_failure);
    RUN_TEST(test_calendar);
    RUN_TEST(test_clock_flags_and_io);
    RUN_TEST(test_write_roundtrip);
    RUN_TEST(test_write_read_failure_preserves_control);
    RUN_TEST(test_write_exits_external_test_mode);
    RUN_TEST(test_write_failures_release_lock_and_restart);
    return UNITY_END();
}

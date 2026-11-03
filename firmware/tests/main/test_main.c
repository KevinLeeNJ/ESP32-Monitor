#include "unity.h"

/* The board application reuses the host regression bodies so protocol and
 * session changes are exercised on the target, not only on the simulator. */
void monitor_test_companion_discovery(void);
void monitor_test_companion_view(void);
void monitor_test_lan_session(void);
void monitor_test_serial_session(void);

TEST_CASE("companion discovery and pairing wire", "[companion][board]")
{
    monitor_test_companion_discovery();
}

TEST_CASE("companion snapshot projection", "[companion][board]")
{
    monitor_test_companion_view();
}

TEST_CASE("LAN session lifecycle", "[companion][lan][board]")
{
    monitor_test_lan_session();
}

TEST_CASE("serial session lifecycle", "[companion][serial][board]")
{
    monitor_test_serial_session();
}

void app_main(void)
{
    unity_run_menu();
}

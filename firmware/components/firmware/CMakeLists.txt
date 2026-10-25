idf_component_register(
    SRCS
        "display_port.c"
        "monitor_lvgl.c"
        "monitor_devices.c"
        "monitor_wifi_manager.cpp"
        "monitor_clock.c"
        "monitor_sensors.c"
        "monitor_weather.c"
        "monitor_display_refresh.c"
        "monitor_i2c.c"
        "monitor_rtc_pcf85063.c"
    INCLUDE_DIRS "."
    REQUIRES esp_lcd esp_driver_i2c monitor_core lvgl
    PRIV_REQUIRES
        esp_driver_spi esp_driver_gpio esp_timer esp_pm esp_wifi_connect
        esp_netif lwip esp_adc esp_http_client esp_system mbedtls cjson zlib
        nvs_flash esp_driver_usb_serial_jtag
)

if(CONFIG_MONITOR_PM_PROFILE)
    target_sources(${COMPONENT_LIB} PRIVATE
        "monitor_pm_profile.c" "monitor_pm_profile_report.c")
endif()

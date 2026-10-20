idf_component_register(
    SRCS "main.c" "../components/firmware/monitor_wifi_manager.cpp" "../components/firmware/monitor_clock.c" "../components/firmware/monitor_sensors.c" "../components/firmware/monitor_weather.c" "../components/firmware/monitor_display_refresh.c" "../components/firmware/monitor_i2c.c" "../components/firmware/monitor_rtc_pcf85063.c"
    INCLUDE_DIRS "." "../components/firmware"
    REQUIRES monitor_core firmware esp_wifi_connect esp_netif lwip esp_driver_i2c esp_driver_gpio esp_timer esp_adc esp_http_client mbedtls cjson
)

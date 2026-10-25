idf_component_register(
    SRCS "main.c"
    INCLUDE_DIRS "."
    PRIV_REQUIRES firmware esp_pm esp_driver_gpio esp_system nvs_flash
)

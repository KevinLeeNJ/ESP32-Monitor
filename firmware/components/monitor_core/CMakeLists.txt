idf_component_register(
    SRCS
        "../../src/display/monitor_pack.c"
        "../../src/display/monitor_render.c"
        "../../app/ui/monitor_ui.c"
        "../../app/ui/weather_assets.c"
        "../../app/ui/weather_zh_font.c"
    INCLUDE_DIRS
        "../../include"
        "../../app/ui"
    REQUIRES lvgl
)

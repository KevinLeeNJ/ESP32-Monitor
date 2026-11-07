message("78/esp_wifi_connect: IDF_VERSION_MAJOR is ${IDF_VERSION_MAJOR}")

# The pages are embedded gzipped and served with Content-Encoding: gzip, which
# keeps only about a quarter of the HTML's uncompressed flash footprint.
# Compress at configure time so a page edit triggers a rebuild. The IDF
# requirements scan also includes this file in script mode, where the
# CURRENT_* directory variables point at the build tree, so skip it there.
if(NOT CMAKE_SCRIPT_MODE_FILE)
    idf_build_get_property(python PYTHON)
    set(embedded_pages "")
    foreach(page "wifi_configuration.html" "wifi_configuration_done.html")
        set(page_source "${CMAKE_CURRENT_LIST_DIR}/assets/${page}")
        set(page_asset "${CMAKE_CURRENT_BINARY_DIR}/assets/${page}.gz")
        execute_process(
            COMMAND "${python}" "${CMAKE_CURRENT_LIST_DIR}/tools/gzip_asset.py"
                    "${page_source}" "${page_asset}"
            RESULT_VARIABLE page_result)
        if(NOT page_result EQUAL 0)
            message(FATAL_ERROR "Failed to compress ${page}")
        endif()
        list(APPEND embedded_pages "${page_asset}")
        set_property(DIRECTORY APPEND PROPERTY CMAKE_CONFIGURE_DEPENDS "${page_source}")
    endforeach()
endif()

if(IDF_VERSION_MAJOR GREATER_EQUAL 6)
    # v6
    message("78/esp_wifi_connect: idf_component_register for v6 used")

    idf_component_register(
        SRCS
            "wifi_manager.cc"
            "wifi_configuration_ap.cc"
            "wifi_station.cc"
            "ssid_manager.cc"
            "dns_server.cc"
        INCLUDE_DIRS
            "include"
        EMBED_FILES
            ${embedded_pages}
        REQUIRES monitor_core "esp_timer" "esp_http_server" "esp_wifi" "nvs_flash" "cjson"
    )
else()
    # v5
    message("78/esp_wifi_connect: idf_component_register for v5 used")

    idf_component_register(
        SRCS
            "wifi_manager.cc"
            "wifi_configuration_ap.cc"
            "wifi_station.cc"
            "ssid_manager.cc"
            "dns_server.cc"
        INCLUDE_DIRS
            "include"
        EMBED_FILES
            ${embedded_pages}
        REQUIRES monitor_core "esp_timer" "esp_http_server" "esp_wifi" "nvs_flash" "cjson" "json"
    )
endif()


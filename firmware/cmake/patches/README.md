# ESP-IDF 本机兼容补丁

`esp-idf-wifi-private-includes.patch` 适用于本项目当前使用的 ESP-IDF 6.1 开发版（`9a97f6c54ec`）。

该版本的 `esp_wifi` 和 `wpa_supplicant` 已声明相互的 `PRIV_REQUIRES`，但将依赖组件内部的头文件路径放在了自己的 `PRIV_INCLUDE_DIRS` 中，触发组件归属检查警告。补丁改用 `idf_component_get_property(... COMPONENT_DIR)` 和 `target_include_directories(... PRIVATE ...)` 导入依赖组件内部头文件；保留私有依赖和头文件可见范围，不关闭检查。

补丁已应用到本机 `/Users/kevinlee/esp/esp-idf`。如更换 SDK，先确认新版是否仍有此问题。需要应用时，在仓库根目录执行：

```bash
git -C "$IDF_PATH" apply --check "$PWD/firmware/cmake/patches/esp-idf-wifi-private-includes.patch"
git -C "$IDF_PATH" apply "$PWD/firmware/cmake/patches/esp-idf-wifi-private-includes.patch"
```

项目源码的组件归属另由 `firmware/components/firmware/CMakeLists.txt` 修复：业务源码由 `firmware` 编译，`main` 通过私有依赖使用其公开头文件。

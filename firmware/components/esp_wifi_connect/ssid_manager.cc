#include "ssid_manager.h"

#include <algorithm>
#include <esp_log.h>
#include <nvs_flash.h>

#define TAG "SsidManager"
#define NVS_NAMESPACE "wifi"
#define MAX_WIFI_SSID_COUNT 10

static std::string MakeWifiKey(const char* prefix, int index) {
    if (index <= 0) {
        return prefix;
    }
    return std::string(prefix) + std::to_string(index);
}

SsidManager::SsidManager() {
    LoadFromNvs();
}

SsidManager::~SsidManager() {
}

void SsidManager::Clear() {
    ssid_list_.clear();
    SaveToNvs();
}

void SsidManager::LoadFromNvs() {
    ssid_list_.clear();

    // Load ssid / password / channel from NVS namespace "wifi"
    // ssid, ssid1, ... ssid9
    // password, password1, ... password9
    // channel, channel1, ... channel9 (uint8, optional; missing means unknown)
    nvs_handle_t nvs_handle;
    auto ret = nvs_open(NVS_NAMESPACE, NVS_READONLY, &nvs_handle);
    if (ret != ESP_OK) {
        // The namespace doesn't exist, just return
        ESP_LOGW(TAG, "NVS namespace %s doesn't exist", NVS_NAMESPACE);
        return;
    }
    for (int i = 0; i < MAX_WIFI_SSID_COUNT; i++) {
        auto ssid_key = MakeWifiKey("ssid", i);
        auto password_key = MakeWifiKey("password", i);
        auto channel_key = MakeWifiKey("channel", i);

        char ssid[33];
        char password[65];
        size_t length = sizeof(ssid);
        if (nvs_get_str(nvs_handle, ssid_key.c_str(), ssid, &length) != ESP_OK) {
            continue;
        }
        length = sizeof(password);
        if (nvs_get_str(nvs_handle, password_key.c_str(), password, &length) != ESP_OK) {
            continue;
        }
        uint8_t channel = 0;
        nvs_get_u8(nvs_handle, channel_key.c_str(), &channel);
        ssid_list_.push_back({ssid, password, channel});
    }
    nvs_close(nvs_handle);
}

bool SsidManager::SaveToNvs() {
    nvs_handle_t nvs_handle;
    esp_err_t err = nvs_open(NVS_NAMESPACE, NVS_READWRITE, &nvs_handle);
    if (err != ESP_OK) return false;
    for (int i = 0; i < MAX_WIFI_SSID_COUNT && err == ESP_OK; i++) {
        auto ssid_key = MakeWifiKey("ssid", i);
        auto password_key = MakeWifiKey("password", i);
        auto channel_key = MakeWifiKey("channel", i);
        if (i < ssid_list_.size()) {
            err = nvs_set_str(nvs_handle, ssid_key.c_str(), ssid_list_[i].ssid.c_str());
            if (err == ESP_OK) err = nvs_set_str(nvs_handle, password_key.c_str(), ssid_list_[i].password.c_str());
            if (err == ESP_OK) err = nvs_set_u8(nvs_handle, channel_key.c_str(), ssid_list_[i].channel);
        } else {
            for (const auto& key : {ssid_key, password_key, channel_key}) {
                if (err != ESP_OK) break;
                err = nvs_erase_key(nvs_handle, key.c_str());
                if (err == ESP_ERR_NVS_NOT_FOUND) err = ESP_OK;
            }
        }
    }
    if (err == ESP_OK) err = nvs_commit(nvs_handle);
    nvs_close(nvs_handle);
    return err == ESP_OK;
}

bool SsidManager::AddSsid(const std::string& ssid, const std::string& password, uint8_t channel) {
    auto previous = ssid_list_;
    // A newly saved network is also the first candidate on the next connection.
    for (auto it = ssid_list_.begin(); it != ssid_list_.end(); ++it) {
        if (it->ssid != ssid) continue;
        if (!channel) channel = it->channel;
        ssid_list_.erase(it);
        break;
    }
    if (ssid_list_.size() >= MAX_WIFI_SSID_COUNT) ssid_list_.pop_back();
    ssid_list_.insert(ssid_list_.begin(), {ssid, password, channel});
    if (SaveToNvs()) return true;
    ssid_list_ = std::move(previous);
    return false;
}

void SsidManager::UpdateSsidChannel(const std::string& ssid, uint8_t channel) {
    if (channel == 0) {
        return;
    }
    for (auto& item : ssid_list_) {
        if (item.ssid != ssid) {
            continue;
        }
        if (item.channel != channel) {
            ESP_LOGI(TAG, "Updated channel for %s: %u -> %u", ssid.c_str(), item.channel, channel);
            item.channel = channel;
            SaveToNvs();
        }
        return;
    }
}

void SsidManager::RemoveSsid(int index) {
    if (index < 0 || index >= ssid_list_.size()) {
        ESP_LOGW(TAG, "Invalid index %d", index);
        return;
    }
    ssid_list_.erase(ssid_list_.begin() + index);
    SaveToNvs();
}

void SsidManager::SetDefaultSsid(int index) {
    if (index < 0 || index >= ssid_list_.size()) {
        ESP_LOGW(TAG, "Invalid index %d", index);
        return;
    }
    // Move the ssid at index to the front of the list
    auto item = ssid_list_[index];
    ssid_list_.erase(ssid_list_.begin() + index);
    ssid_list_.insert(ssid_list_.begin(), item);
    SaveToNvs();
}

std::vector<uint8_t> SsidManager::GetSavedChannels() const {
    std::vector<uint8_t> channels;
    for (const auto& item : ssid_list_) {
        if (item.channel == 0) {
            continue;
        }
        if (std::find(channels.begin(), channels.end(), item.channel) == channels.end()) {
            channels.push_back(item.channel);
        }
    }
    return channels;
}

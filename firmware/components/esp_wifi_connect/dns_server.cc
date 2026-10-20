#include "dns_server.h"
#include <esp_log.h>
#include <lwip/sockets.h>
#include <lwip/netdb.h>

#define TAG "DnsServer"

DnsServer::DnsServer() {
    stopped_sem_ = xSemaphoreCreateBinary();
}

DnsServer::~DnsServer() {
    Stop();
}

void DnsServer::Start(esp_ip4_addr_t gateway) {
    // If already running, stop first
    if (running_) {
        if (!Stop()) return;
    }

    ESP_LOGI(TAG, "Starting DNS server");
    gateway_ = gateway;

    fd_ = socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP);
    if (fd_ < 0) {
        ESP_LOGE(TAG, "Failed to create socket");
        return;
    }

    struct sockaddr_in server_addr;
    memset(&server_addr, 0, sizeof(server_addr));
    server_addr.sin_family = AF_INET;
    server_addr.sin_addr.s_addr = htonl(INADDR_ANY);
    server_addr.sin_port = htons(port_);

    if (bind(fd_, (struct sockaddr *)&server_addr, sizeof(server_addr)) < 0) {
        ESP_LOGE(TAG, "failed to bind port %d", port_);
        close(fd_);
        fd_ = -1;
        return;
    }

    if (stopped_sem_ == nullptr) { close(fd_); fd_ = -1; return; }
    xSemaphoreTake(stopped_sem_, 0);
    running_ = true;
    if (xTaskCreate([](void* arg) {
        DnsServer* dns_server = static_cast<DnsServer*>(arg);
        dns_server->Run();
        vTaskDelete(NULL);
    }, "DnsServerTask", 4096, this, 5, &task_handle_) != pdPASS) {
        running_ = false; task_handle_ = nullptr; close(fd_); fd_ = -1;
    }
}

bool DnsServer::Stop() {
    if (!running_ && task_handle_ == nullptr) return true;
    ESP_LOGI(TAG, "Stopping DNS server");
    running_ = false;
    if (fd_ >= 0) shutdown(fd_, SHUT_RDWR);
    if (task_handle_ != nullptr &&
        xSemaphoreTake(stopped_sem_, pdMS_TO_TICKS(1000)) != pdTRUE) {
        ESP_LOGE(TAG, "DNS task did not exit within 1 s; retaining its context");
        return false;
    }
    task_handle_ = nullptr;
    if (fd_ >= 0) { close(fd_); fd_ = -1; }
    return true;
}

void DnsServer::Run() {
    char buffer[512];
    unsigned consecutive_errors = 0;
    while (running_) {
        struct sockaddr_in client_addr;
        socklen_t client_addr_len = sizeof(client_addr);
        int len = recvfrom(fd_, buffer, sizeof(buffer), 0, (struct sockaddr *)&client_addr, &client_addr_len);
        if (len < 0) {
            if (!running_) {
                // Socket was closed during Stop(), exit gracefully
                break;
            }
            const int error = errno;
            if (error == EINTR || error == EAGAIN || error == EWOULDBLOCK) {
                vTaskDelay(pdMS_TO_TICKS(100));
                continue;
            }
            ESP_LOGW(TAG, "recvfrom failed, errno=%d; backing off", error);
            vTaskDelay(pdMS_TO_TICKS(250));
            if (++consecutive_errors >= 3) break;
            continue;
        }

        if (!running_) {
            break;
        }

        consecutive_errors = 0;
        // Simple DNS response: point all queries to 192.168.4.1
        buffer[2] |= 0x80;  // Set response flag
        buffer[3] |= 0x80;  // Set Recursion Available
        buffer[7] = 1;      // Set answer count to 1

        // Add answer section
        memcpy(&buffer[len], "\xc0\x0c", 2);  // Name pointer
        len += 2;
        memcpy(&buffer[len], "\x00\x01\x00\x01\x00\x00\x00\x1c\x00\x04", 10);  // Type, class, TTL, data length
        len += 10;
        memcpy(&buffer[len], &gateway_.addr, 4);  // 192.168.4.1
        len += 4;
        ESP_LOGI(TAG, "Sending DNS response to %s", inet_ntoa(gateway_.addr));

        sendto(fd_, buffer, len, 0, (struct sockaddr *)&client_addr, client_addr_len);
    }

    ESP_LOGI(TAG, "DNS server task exiting");
    xSemaphoreGive(stopped_sem_);
}

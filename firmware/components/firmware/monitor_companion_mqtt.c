#include "monitor_companion_mqtt.h"
#include "monitor_companion_config.h"
#include "monitor_pairing_store.h"
#include "monitor_lan_session.h"
#include "monitor_mqtt_wire.h"
#include "monitor_display_refresh.h"
#include "monitor_serial_service.h"
#include "esp_log.h"
#include "esp_heap_caps.h"
#include "esp_random.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "freertos/semphr.h"
#include <arpa/inet.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

#define TAG "companion_mqtt"
#define RX_CAPACITY MONITOR_MQTT_MAX_PACKET
#define PING_INTERVAL_MS 20000u
#define CONFIG_POLL_MS 5000u

typedef struct {
    char address[16], host[37], device[37], pairing[37], session[37], token[44];
    uint16_t port, packet_id;
    int socket;
    char prefix[160];
} mqtt_job_t;

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static SemaphoreHandle_t s_data_lock;
static monitor_companion_mqtt_snapshot_t *s_data;
static bool s_busy, s_stop, s_revoke;
static int s_socket = -1;
static TaskHandle_t s_worker;
static void (*s_work)(void *);
static void *s_work_arg;
static bool s_work_busy;

static uint64_t monotonic_ms(void) { return (uint64_t)esp_timer_get_time()/1000; }
static void log_heap(const char *where)
{
    ESP_LOGI(TAG, "heap %s: internal free=%u largest=%u psram free=%u", where,
             (unsigned)heap_caps_get_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT),
             (unsigned)heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT),
             (unsigned)heap_caps_get_free_size(MALLOC_CAP_SPIRAM));
}
static void make_uuid(char out[37])
{
    uint8_t b[16]; esp_fill_random(b,sizeof b); b[6]=(b[6]&15)|64; b[8]=(b[8]&63)|128;
    snprintf(out,37,"%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
             b[0],b[1],b[2],b[3],b[4],b[5],b[6],b[7],b[8],b[9],b[10],b[11],b[12],b[13],b[14],b[15]);
}
static bool stopped(void)
{
    portENTER_CRITICAL(&s_lock); bool stop=s_stop; portEXIT_CRITICAL(&s_lock); return stop;
}
static bool send_message(void *ctx, const char *suffix, const uint8_t *payload, size_t len, unsigned qos)
{
    mqtt_job_t *job=ctx; char topic[256];
    int n=snprintf(topic,sizeof topic,"%s/up/%s",job->prefix,suffix);
    if (n<0 || (size_t)n>=sizeof topic) return false;
    if (++job->packet_id == 0) ++job->packet_id;
    return monitor_mqtt_publish(job->socket,topic,payload,len,qos,job->packet_id);
}
static bool apply(void *ctx, const esp32monitor_companion_Snapshot *snapshot,
                  const esp32monitor_companion_Heartbeat *heartbeat)
{
    return monitor_companion_sink_apply(ctx, snapshot, heartbeat);
}
static void set_connection(bool connected, bool clear)
{
    monitor_companion_sink_connection(connected, clear);
}

void monitor_companion_sink_init(void)
{
    portENTER_CRITICAL(&s_lock);
    bool create = !s_data_lock;
    portEXIT_CRITICAL(&s_lock);
    if (!create) return;
    SemaphoreHandle_t lock = xSemaphoreCreateMutex();
    monitor_companion_mqtt_snapshot_t *data =
        heap_caps_calloc(1, sizeof *data, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!data) data = calloc(1, sizeof *data);
    if (!lock || !data) { free(data); if (lock) vSemaphoreDelete(lock); return; }
    portENTER_CRITICAL(&s_lock);
    if (!s_data_lock) { s_data_lock = lock; s_data = data; lock = NULL; data = NULL; }
    portEXIT_CRITICAL(&s_lock);
    if (lock) vSemaphoreDelete(lock);
    free(data);
}

bool monitor_companion_sink_apply(void *ctx, const esp32monitor_companion_Snapshot *snapshot,
                                  const esp32monitor_companion_Heartbeat *heartbeat)
{
    (void)ctx;
    monitor_companion_sink_init();
    portENTER_CRITICAL(&s_lock); SemaphoreHandle_t lock = s_data_lock; portEXIT_CRITICAL(&s_lock);
    if (!lock || xSemaphoreTake(lock,pdMS_TO_TICKS(1000)) != pdTRUE) return false;
    if (snapshot) {
        s_data->snapshot=*snapshot; s_data->has_snapshot=true; s_data->has_heartbeat=false;
        s_data->snapshot_received_ms=monotonic_ms();
    }
    if (heartbeat) {
        s_data->heartbeat=*heartbeat; s_data->has_heartbeat=true;
        s_data->heartbeat_received_ms=monotonic_ms();
    }
    s_data->generation++;
    xSemaphoreGive(lock);
    monitor_display_refresh_request_reason(MONITOR_REFRESH_RECOVER);
    return true;
}

void monitor_companion_sink_connection(bool connected, bool clear)
{
    monitor_companion_sink_init();
    portENTER_CRITICAL(&s_lock); SemaphoreHandle_t lock = s_data_lock; portEXIT_CRITICAL(&s_lock);
    if (!lock || xSemaphoreTake(lock,pdMS_TO_TICKS(1000)) != pdTRUE) return;
    bool changed=s_data->connected != connected || (clear && s_data->has_snapshot);
    s_data->connected=connected;
    if (clear) { s_data->has_snapshot=false; s_data->has_heartbeat=false; }
    if (changed) s_data->generation++;
    xSemaphoreGive(lock);
    if (changed) monitor_display_refresh_request_reason(MONITOR_REFRESH_RECOVER);
}
bool monitor_companion_mqtt_get(monitor_companion_mqtt_snapshot_t *out)
{
    if (!out) return false;
    portENTER_CRITICAL(&s_lock); SemaphoreHandle_t lock=s_data_lock; portEXIT_CRITICAL(&s_lock);
    if (!lock || xSemaphoreTake(lock,pdMS_TO_TICKS(1000)) != pdTRUE) return false;
    *out=*s_data;
    xSemaphoreGive(lock);
    return true;
}
uint32_t monitor_companion_mqtt_generation(void)
{
    portENTER_CRITICAL(&s_lock); SemaphoreHandle_t lock=s_data_lock; portEXIT_CRITICAL(&s_lock);
    if (!lock || xSemaphoreTake(lock,pdMS_TO_TICKS(100)) != pdTRUE) return 0;
    uint32_t generation=s_data->generation;
    xSemaphoreGive(lock);
    return generation;
}

static int dial(const char *address, uint16_t port)
{
    int fd=socket(AF_INET,SOCK_STREAM,IPPROTO_IP); if(fd<0)return -1;
    int flags=fcntl(fd,F_GETFL,0);
    if (flags<0 || fcntl(fd,F_SETFL,flags|O_NONBLOCK)<0) { close(fd); return -1; }
    struct sockaddr_in peer={.sin_family=AF_INET,.sin_port=htons(port)};
    if (inet_pton(AF_INET,address,&peer.sin_addr)!=1) { close(fd); return -1; }
    int result=connect(fd,(struct sockaddr *)&peer,sizeof peer);
    if (result<0) {
        fd_set writable;FD_ZERO(&writable);FD_SET(fd,&writable); struct timeval timeout={4,0};
        int error=0; socklen_t length=sizeof error;
        if (select(fd+1,NULL,&writable,NULL,&timeout)<=0 || getsockopt(fd,SOL_SOCKET,SO_ERROR,&error,&length)<0 || error) { close(fd);return -1; }
    }
    if (fcntl(fd,F_SETFL,flags)<0) { close(fd);return -1; }
    struct timeval timeout={4,0};
    if (setsockopt(fd,SOL_SOCKET,SO_SNDTIMEO,&timeout,sizeof timeout)<0) { close(fd);return -1; }
    return fd;
}
static bool read_handshake(int fd, uint8_t expected, uint16_t id, uint8_t *buffer)
{
    monitor_mqtt_packet_t packet;
    if (monitor_mqtt_read(fd,&packet,buffer,RX_CAPACITY,5000)!=1 || packet.header!=expected) return false;
    if (expected==0x20) return packet.length==2 && packet.body[0]==0 && packet.body[1]==0;
    return packet.length==3 && packet.body[0]==(uint8_t)(id>>8) && packet.body[1]==(uint8_t)id && packet.body[2]<=1;
}

static void run_session(void *arg)
{
    mqtt_job_t *job = arg;
    const char *why="resource";
    bool host_revoked=false, peer_goodbye=false;
    log_heap("worker start");
    /* Protocol data is CPU-only; preserve internal RAM for Wi-Fi and task stacks. */
    uint8_t *rx=heap_caps_malloc_prefer(RX_CAPACITY,2,
        MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT,MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    esp32monitor_companion_Config *config=heap_caps_calloc_prefer(1,sizeof *config,2,
        MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT,MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    esp32monitor_companion_Config *candidate=heap_caps_calloc_prefer(1,sizeof *candidate,2,
        MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT,MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    monitor_lan_session_t *session=NULL;
    if (!rx || !config || !candidate) goto done;
    job->socket=dial(job->address,job->port);
    if (job->socket<0 || stopped()) { why=stopped()?"stop":"dial"; goto done; }
    portENTER_CRITICAL(&s_lock);s_socket=job->socket;portEXIT_CRITICAL(&s_lock);
    int n=snprintf(job->prefix,sizeof job->prefix,"emon/v1/%s/%s/%s",job->host,job->device,job->session);
    if (n<0 || (size_t)n>=sizeof job->prefix) { why="prefix"; goto done; }
    config->protocol_version=1;strcpy(config->device_id,job->device);strcpy(config->session_id,job->session);
    make_uuid(config->request_id);make_uuid(config->config_epoch);config->config_revision=1;
    if (monitor_companion_config_read(config)!=ESP_OK) { why="config_read"; goto done; }
    char open_id[37];make_uuid(open_id);
    session=monitor_lan_create(job->host,job->device,job->pairing,job->session,open_id,config,send_message,apply,job,monotonic_ms());
    if (!session) { why="session_create"; goto done; }
    char topic[256];
    n=snprintf(topic,sizeof topic,"%s/up/presence",job->prefix);
    if (n<0 || (size_t)n>=sizeof topic) { why="will_topic"; goto done; }
    size_t will_len=0;
    if (!monitor_lan_presence(job->host,job->device,job->session,false,rx,1024,&will_len) ||
        !monitor_mqtt_connect_packet(job->socket,job->device,job->session,job->token,topic,rx,will_len) ||
        !read_handshake(job->socket,0x20,0,rx)) { why="connect"; goto done; }
    n=snprintf(topic,sizeof topic,"%s/down/#",job->prefix);
    if (n<0 || (size_t)n>=sizeof topic || !monitor_mqtt_subscribe(job->socket,1,topic) ||
        !read_handshake(job->socket,0x90,1,rx) || !monitor_lan_start(session,time(NULL))) { why="subscribe/start"; goto done; }
    ESP_LOGI(TAG,"authenticated MQTT transport connected");
    uint64_t last_ping=monotonic_ms(), last_config=last_ping;
    while (!stopped() && monitor_lan_alive(session,monotonic_ms())) {
        monitor_mqtt_packet_t packet;
        int result=monitor_mqtt_read(job->socket,&packet,rx,RX_CAPACITY,1000);
        if (result<0) { why="read"; break; }
        if (result==1) {
            if ((packet.header>>4)==3) {
                const uint8_t *payload;size_t length;uint16_t packet_id;
                if (!monitor_mqtt_parse_publish(&packet,topic,sizeof topic,&payload,&length,&packet_id)) { why="publish_parse"; break; }
                size_t prefix_len=strlen(job->prefix);
                if (strncmp(topic,job->prefix,prefix_len) || strncmp(topic+prefix_len,"/down/",6)) { why="topic"; break; }
                /* MQTT receipt and application acceptance are separate. Send
                 * PUBACK before processing can publish an application ACK. */
                if (packet_id && !monitor_mqtt_puback(job->socket,packet_id)) { why="puback"; break; }
                if (!monitor_lan_receive(session,topic+prefix_len+6,payload,length,monotonic_ms(),time(NULL))) {
                    ESP_LOGW(TAG,"rejected down/%s len=%u state=%d",topic+prefix_len+6,(unsigned)length,(int)monitor_lan_state(session));
                    why="lan_receive"; break;
                }
                set_connection(monitor_lan_state(session)==MONITOR_LAN_CONNECTED,false);
                if (monitor_lan_state(session)==MONITOR_LAN_CLOSED) {
                    const char *reason=monitor_lan_close_reason(session);
                    host_revoked=reason && !strcmp(reason,"host_revoked");
                    peer_goodbye=!host_revoked;
                    break;
                }
            } else if (packet.header==0x40) {
                if (packet.length!=2 || (!packet.body[0] && !packet.body[1])) { why="puback_packet"; break; }
            } else if (packet.header==0xd0) {
                if (packet.length!=0) { why="pingresp"; break; }
            } else { why="unexpected_packet"; break; }
        }
        uint64_t now=monotonic_ms();
        if (now-last_ping>=PING_INTERVAL_MS) { if (!monitor_mqtt_ping(job->socket)) { why="ping"; break; } last_ping=now; }
        if (now-last_config>=CONFIG_POLL_MS && monitor_lan_state(session)>=MONITOR_LAN_SNAPSHOT) {
            *candidate=*config;
            if (monitor_companion_config_read(candidate)!=ESP_OK) { why="config_poll"; break; }
            if (memcmp(&candidate->weather,&config->weather,sizeof config->weather) ||
                memcmp(&candidate->devices,&config->devices,sizeof config->devices) ||
                memcmp(&candidate->time,&config->time,sizeof config->time)) {
                if (config->config_revision==UINT32_MAX) { why="revision"; break; }
                candidate->config_revision++;make_uuid(candidate->request_id);
                set_connection(false,true);
                if (!monitor_lan_update_config(session,candidate,now,time(NULL))) { why="config_update"; break; }
                *config=*candidate;
            }
            last_config=now;
        }
    }
    portENTER_CRITICAL(&s_lock); bool revoke=s_revoke; portEXIT_CRITICAL(&s_lock);
    if (revoke && session && monitor_lan_state(session)!=MONITOR_LAN_CLOSED) {
        (void)monitor_lan_goodbye(session,"pairing_revoked",time(NULL));
        why="revoked";
    }
    else if (host_revoked) why="revoked";
    else if (peer_goodbye) why="goodbye";
    else if (stopped()) why="stop";
    else if (!monitor_lan_alive(session,monotonic_ms())) why="expired";
    (void)monitor_mqtt_disconnect(job->socket);
done:
    ESP_LOGW(TAG,"session ended: %s",why);
    log_heap("worker end");
    set_connection(false,true);
    if (host_revoked) {
        esp_err_t removed=monitor_pairing_store_revoke();
        if (removed==ESP_OK) ESP_LOGI(TAG,"pairing record revoked by host");
        else ESP_LOGE(TAG,"pairing record revoke failed: %s",esp_err_to_name(removed));
    }
    portENTER_CRITICAL(&s_lock);s_socket=-1;portEXIT_CRITICAL(&s_lock);
    if (job->socket>=0) close(job->socket);
    monitor_lan_destroy(session);
    if (config) { memset(config,0,sizeof *config); free(config); }
    if (candidate) { memset(candidate,0,sizeof *candidate); free(candidate); }
    free(rx);memset(job,0,sizeof *job);free(job);
    portENTER_CRITICAL(&s_lock);s_busy=false;s_stop=false;s_revoke=false;portEXIT_CRITICAL(&s_lock);
    log_heap("worker freed");
    ESP_LOGI(TAG,"MQTT session closed");
}

static void worker(void *arg)
{
    (void)arg;
    for (;;) {
        ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
        portENTER_CRITICAL(&s_lock);
        void (*work)(void *) = s_work;
        void *work_arg = s_work_arg;
        s_work = NULL;
        s_work_arg = NULL;
        portEXIT_CRITICAL(&s_lock);
        if (work) work(work_arg);
        /* Pairing uses this task's notification bits for physical buttons. */
        (void)ulTaskNotifyTake(pdTRUE, 0);
        portENTER_CRITICAL(&s_lock);
        s_work_busy = false;
        portEXIT_CRITICAL(&s_lock);
    }
}

esp_err_t monitor_companion_worker_submit(void (*work)(void *), void *arg)
{
    if (!work || !s_worker) return ESP_ERR_INVALID_STATE;
    portENTER_CRITICAL(&s_lock);
    bool busy = s_work_busy;
    if (!busy) { s_work_busy = true; s_work = work; s_work_arg = arg; }
    portEXIT_CRITICAL(&s_lock);
    if (busy) return ESP_ERR_INVALID_STATE;
    xTaskNotifyGive(s_worker);
    return ESP_OK;
}

esp_err_t monitor_companion_mqtt_init(void)
{
    if (s_worker) return ESP_OK;
    /* Session setup measured 3.3 KB; keep room for snapshot/pairing decode. */
    if (xTaskCreate(worker, "companion_mqtt", 10240, NULL, 3, &s_worker) != pdPASS) {
        ESP_LOGE(TAG, "worker task create failed: internal free=%u largest=%u",
                 (unsigned)heap_caps_get_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT),
                 (unsigned)heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

void monitor_companion_mqtt_stop(void)
{
    portENTER_CRITICAL(&s_lock);s_stop=true;int fd=s_socket;portEXIT_CRITICAL(&s_lock);
    if (fd>=0) shutdown(fd,SHUT_RDWR);
}

void monitor_companion_mqtt_revoke(void)
{
    /* No shutdown: the worker still has to publish up/goodbye before closing. */
    portENTER_CRITICAL(&s_lock);s_stop=true;s_revoke=true;portEXIT_CRITICAL(&s_lock);
}

bool monitor_companion_mqtt_running(void)
{
    portENTER_CRITICAL(&s_lock);bool busy=s_busy;portEXIT_CRITICAL(&s_lock);
    return busy;
}

bool monitor_companion_mqtt_wait_stopped(uint32_t timeout_ms)
{
    TickType_t start = xTaskGetTickCount();
    while (monitor_companion_mqtt_running()) {
        if ((xTaskGetTickCount() - start) * portTICK_PERIOD_MS >= timeout_ms) return false;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    return true;
}

esp_err_t monitor_companion_mqtt_start(const char *ipv4, uint16_t port, const char *host,
                                       const char *device, const char *pairing,
                                       const char *session, const char *token)
{
    struct in_addr ip;
    if (!ipv4 || inet_pton(AF_INET,ipv4,&ip)!=1 || !port || !host || !device || !pairing || !session || !token ||
        strlen(host)!=36 || strlen(device)!=36 || strlen(pairing)!=36 || strlen(session)!=36 || strlen(token)!=43) return ESP_ERR_INVALID_ARG;
    if (monitor_serial_active() || !s_worker) return ESP_ERR_INVALID_STATE;
    portENTER_CRITICAL(&s_lock);bool busy=s_busy;if(!busy){s_busy=true;s_stop=false;s_revoke=false;}portEXIT_CRITICAL(&s_lock);
    if (busy) return ESP_ERR_INVALID_STATE;
    monitor_companion_sink_init();
    mqtt_job_t *job=calloc(1,sizeof *job);
    if (!s_data_lock || !s_data || !job) { free(job); goto fail; }
    strcpy(job->address,ipv4);strcpy(job->host,host);strcpy(job->device,device);strcpy(job->pairing,pairing);strcpy(job->session,session);strcpy(job->token,token);job->port=port;job->socket=-1;
    if (monitor_companion_worker_submit(run_session, job) != ESP_OK) {
        memset(job, 0, sizeof *job); free(job);
        portENTER_CRITICAL(&s_lock); s_busy=false; portEXIT_CRITICAL(&s_lock);
        return ESP_ERR_INVALID_STATE;
    }
    return ESP_OK;
fail:
    portENTER_CRITICAL(&s_lock);s_busy=false;portEXIT_CRITICAL(&s_lock);
    return ESP_ERR_NO_MEM;
}

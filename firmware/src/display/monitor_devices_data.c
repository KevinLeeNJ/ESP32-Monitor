#include "monitor_devices_data.h"
#include "cJSON.h"
#include <ctype.h>
#include <math.h>
#include <string.h>

bool monitor_devices_normalize_url(char *url)
{
    if (!url || strlen(url) > MONITOR_DEVICES_URL_MAX) return false;
    if (!url[0]) return true;
    char *host;
    if (strncmp(url, "https://", 8) == 0) host = url + 8;
    else if (strncmp(url, "http://", 7) == 0) host = url + 7;
    else return false;
    size_t n = strlen(host);
    while (n && host[n - 1] == '/') --n;
    if (!n) return false;
    for (size_t i = 0; i < n; ++i) {
        unsigned char c = host[i];
        if (c <= 32 || c >= 127 || strchr("/@?#\\%", c)) return false;
    }
    char *port = NULL;
    if (host[0] == '[') {
        char *end = memchr(host, ']', n);
        if (!end || end == host + 1 || !memchr(host, ':', end - host)) return false;
        for (char *p = host + 1; p < end; ++p)
            if (!isxdigit((unsigned char)*p) && *p != ':' && *p != '.') return false;
        if (end < host + n - 1) {
            if (end[1] != ':') return false;
            port = end + 2;
        }
    } else {
        char *end = memchr(host, ':', n);
        if (!end) end = host + n;
        if (end == host || *host == '.' || *host == '-' || end[-1] == '.' || end[-1] == '-') return false;
        for (char *p = host; p < end; ++p)
            if (!isalnum((unsigned char)*p) && *p != '.' && *p != '-') return false;
        if (end != host + n) port = end + 1;
    }
    if (port) {
        unsigned value = 0;
        if (port == host + n) return false;
        for (char *p = port; p < host + n; ++p) {
            if (*p < '0' || *p > '9') return false;
            value = value * 10 + (unsigned)(*p - '0');
            if (value > 65535) return false;
        }
        if (!value) return false;
    }
    host[n] = '\0';
    return true;
}

bool monitor_devices_valid_token(const char *token)
{
    /* The current Server generates and requires exactly 64 hex characters. */
    if (!token || strlen(token) != 64) return false;
    for (const char *p = token; *p; ++p) if (!isxdigit((unsigned char)*p)) return false;
    return true;
}

static const cJSON *field(const cJSON *obj, const char *name)
{
    const cJSON *found = NULL, *item;
    cJSON_ArrayForEach(item, obj) {
        if (item->string && strcmp(item->string, name) == 0) {
            if (found) return NULL;
            found = item;
        }
    }
    return found;
}
static bool integer(const cJSON *v, int min, int max)
{
    return cJSON_IsNumber(v) && isfinite(v->valuedouble) &&
           v->valuedouble >= min && v->valuedouble <= max && floor(v->valuedouble) == v->valuedouble;
}
static bool text(const cJSON *v, char *out, size_t size)
{
    if (!cJSON_IsString(v) || !v->valuestring) return false;
    size_t n = strlen(v->valuestring);
    if (!n || n >= size) return false;
    for (size_t i = 0; i < n; ++i)
        if ((unsigned char)v->valuestring[i] < 32 || (unsigned char)v->valuestring[i] > 126) return false;
    memcpy(out, v->valuestring, n + 1);
    return true;
}
bool monitor_devices_parse(const char *body, size_t len, monitor_devices_snapshot_t *out)
{
    if (!body || !out || !len || len > MONITOR_DEVICES_BODY_MAX || memchr(body, 0, len)) return false;
    /* cJSON strings cannot represent embedded NULs. Reject those escapes. */
    for (size_t i = 0; i + 6 <= len; ++i)
        if (memcmp(body + i, "\\u0000", 6) == 0) return false;
    /* Bound parser recursion before handing untrusted JSON to cJSON. */
    unsigned depth = 0;
    bool quoted = false, escaped = false;
    for (size_t i = 0; i < len; ++i) {
        char c = body[i];
        if (quoted) {
            if (escaped) escaped = false;
            else if (c == '\\') escaped = true;
            else if (c == '"') quoted = false;
        } else if (c == '"') quoted = true;
        else if (c == '{' || c == '[') { if (++depth > 16) return false; }
        else if (c == '}' || c == ']') { if (!depth) return false; --depth; }
    }
    if (depth || quoted) return false;
    const char *end;
    cJSON *root = cJSON_ParseWithLengthOpts(body, len + 1, &end, true);
    if (!root) return false;
    monitor_devices_snapshot_t candidate = {.status = MONITOR_DEVICES_READY};
    bool valid = false;
    const cJSON *count = field(root, "device_count"), *devices = field(root, "devices");
    if (!cJSON_IsObject(root) || !integer(count, 0, 4) || !cJSON_IsArray(devices) ||
        cJSON_GetArraySize(devices) != count->valueint) goto done;
    candidate.count = count->valueint;
    for (int i = 0; i < candidate.count; ++i) {
        const cJSON *d = cJSON_GetArrayItem(devices, i);
        monitor_device_t *target = &candidate.devices[i];
        const cJSON *mc = field(d, "metric_count"), *metrics = field(d, "metrics");
        if (!cJSON_IsObject(d) || !text(field(d, "name"), target->name, sizeof(target->name)) ||
            !text(field(d, "uptime"), target->uptime, sizeof(target->uptime)) || !integer(mc, 3, 3) ||
            !cJSON_IsArray(metrics) || cJSON_GetArraySize(metrics) != 3) goto done;
        target->metric_count = 3;
        static const char *names[] = {"CPU", "MEM", "DISK"};
        for (int j = 0; j < 3; ++j) {
            const cJSON *m = cJSON_GetArrayItem(metrics, j), *p = field(m, "percent");
            monitor_device_metric_t *t = &target->metrics[j];
            if (!cJSON_IsObject(m) || !text(field(m, "name"), t->name, sizeof(t->name)) ||
                strcmp(t->name, names[j]) || !text(field(m, "value"), t->value, sizeof(t->value)) ||
                (!cJSON_IsNull(p) && !integer(p, 0, 100))) goto done;
            t->percent_valid = !cJSON_IsNull(p);
            t->percent = t->percent_valid ? p->valueint : 0;
        }
    }
    *out = candidate;
    valid = true;
done:
    cJSON_Delete(root);
    return valid;
}

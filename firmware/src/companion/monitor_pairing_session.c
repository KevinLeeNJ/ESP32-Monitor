#include "monitor_pairing_session.h"
#include <string.h>

static void clear(monitor_pairing_session_t *s, monitor_pairing_state_t state)
{
    memset(s, 0, sizeof(*s));
    s->state = state;
}

void monitor_pairing_tick(monitor_pairing_session_t *s, uint64_t now)
{
    if (s && (s->state == MONITOR_PAIR_WAITING || s->state == MONITOR_PAIR_CONFIRMED) && now >= s->deadline_ms)
        clear(s, MONITOR_PAIR_EXPIRED);
}

void monitor_pairing_cancel(monitor_pairing_session_t *s)
{
    if (s && (s->state == MONITOR_PAIR_WAITING || s->state == MONITOR_PAIR_CONFIRMED))
        clear(s, MONITOR_PAIR_CANCELLED);
}

bool monitor_pairing_begin(monitor_pairing_session_t *s, const esp32monitor_companion_PairResponse *r,
                           const char *nonce, const char *device, const char *host, uint64_t now,
                           monitor_pairing_hash_t hash)
{
    if (!s || !r || s->state != MONITOR_PAIR_IDLE || strcmp(r->state, "pending") ||
        !monitor_pairing_validate_response(r, host) || UINT64_MAX - now < (uint64_t)r->expires_in_ms) return false;
    char code[7];
    if (!monitor_pairing_code(nonce, r->host_nonce, device, host, r->pairing_id, hash, code)) return false;
    s->initial = *r;
    s->deadline_ms = now + (uint64_t)r->expires_in_ms;
    memcpy(s->code, code, sizeof(code));
    s->state = MONITOR_PAIR_WAITING;
    return true;
}

bool monitor_pairing_confirm(monitor_pairing_session_t *s, uint64_t now)
{
    monitor_pairing_tick(s, now);
    if (!s || s->state != MONITOR_PAIR_WAITING) return false;
    s->state = MONITOR_PAIR_CONFIRMED;
    return true;
}

bool monitor_pairing_apply(monitor_pairing_session_t *s, const esp32monitor_companion_PairResponse *r, uint64_t now)
{
    monitor_pairing_tick(s, now);
    if (!s || !r || (s->state != MONITOR_PAIR_WAITING && s->state != MONITOR_PAIR_CONFIRMED) ||
        !monitor_pairing_validate_response(r, s->initial.host_id) ||
        strcmp(r->pairing_id, s->initial.pairing_id) || strcmp(r->host_nonce, s->initial.host_nonce) ||
        strcmp(r->pairing_ticket, s->initial.pairing_ticket) || strcmp(r->host_name, s->initial.host_name)) return false;
    if (!strcmp(r->state, "rejected")) { clear(s, MONITOR_PAIR_REJECTED); return true; }
    if (!strcmp(r->state, "approved")) {
        if (s->state != MONITOR_PAIR_CONFIRMED) return false;
        s->approved = *r;
        memset(&s->initial, 0, sizeof(s->initial));
        memset(s->code, 0, sizeof(s->code));
        s->state = MONITOR_PAIR_APPROVED;
        return true;
    }
    /* A later response can shorten but cannot extend the pairing window. */
    uint64_t remaining = (uint64_t)r->expires_in_ms;
    if (remaining < s->deadline_ms - now) s->deadline_ms = now + remaining;
    return true;
}

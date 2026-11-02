#include "monitor_proto_strict.h"
#include "pb_common.h"
#include <math.h>
#include <string.h>

static bool varint(const uint8_t **p, const uint8_t *end, uint64_t *v)
{
    *v = 0;
    for (unsigned i = 0; i < 10 && *p < end; i++) {
        uint8_t c = *(*p)++;
        if (i == 9 && c > 1) return false;
        *v |= (uint64_t)(c & 127) << (7 * i);
        if (!(c & 128)) return true;
    }
    return false;
}

static bool text_ok(const uint8_t *s, size_t n)
{
    size_t i = 0;
    while (i < n) {
        uint32_t cp; unsigned extra;
        uint8_t c = s[i++];
        if (c < 0x80) { if (c < 0x20 || c == 0x7f) return false; continue; }
        if (c >= 0xc2 && c <= 0xdf) { cp = c & 31; extra = 1; }
        else if (c >= 0xe0 && c <= 0xef) { cp = c & 15; extra = 2; }
        else if (c >= 0xf0 && c <= 0xf4) { cp = c & 7; extra = 3; }
        else return false;
        if (extra > n - i) return false;
        for (unsigned k = 0; k < extra; k++) { c = s[i++]; if ((c & 0xc0) != 0x80) return false; cp = (cp << 6) | (c & 63); }
        if ((extra == 2 && cp < 0x800) || (extra == 3 && cp < 0x10000) ||
            (cp >= 0xd800 && cp <= 0xdfff) || cp > 0x10ffff || (cp >= 0x80 && cp <= 0x9f)) return false;
    }
    return true;
}

static bool scan(const uint8_t *p, size_t len, const pb_msgdesc_t *desc, unsigned depth)
{
    if (depth > 12 || desc->field_count > 64) return false;
    const uint8_t *end = p + len;
    uint64_t seen = 0; bool oneof = false;
    while (p < end) {
        uint64_t tagwire, value;
        if (!varint(&p, end, &tagwire) || tagwire > UINT32_MAX || (tagwire >> 3) == 0) return false;
        unsigned tag = (unsigned)(tagwire >> 3), wire = tagwire & 7;
        pb_field_iter_t f;
        bool known = pb_field_iter_begin(&f, desc, NULL) && pb_field_iter_find(&f, tag);
        if (known) {
            if (PB_HTYPE(f.type) != PB_HTYPE_REPEATED) {
                if (seen & (UINT64_C(1) << f.index)) return false;
                seen |= UINT64_C(1) << f.index;
            }
            if (PB_HTYPE(f.type) == PB_HTYPE_ONEOF) { if (oneof) return false; oneof = true; }
            unsigned type = PB_LTYPE(f.type);
            unsigned expected = type <= PB_LTYPE_SVARINT ? 0 : type == PB_LTYPE_FIXED64 ? 1 : type == PB_LTYPE_FIXED32 ? 5 : 2;
            if (wire != expected) return false;
        }
        if (wire == 0) {
            if (!varint(&p, end, &value)) return false;
            if (known && ((PB_LTYPE(f.type) == PB_LTYPE_BOOL && value > 1) ||
                ((PB_LTYPE(f.type) == PB_LTYPE_UVARINT || PB_LTYPE(f.type) == PB_LTYPE_SVARINT) && f.data_size == 4 && value > UINT32_MAX))) return false;
        } else if (wire == 1 || wire == 5) {
            size_t n = wire == 1 ? 8 : 4;
            if ((size_t)(end-p) < n) return false;
            if (known && wire == 1 && PB_LTYPE(f.type) == PB_LTYPE_FIXED64) {
                uint64_t bits = 0; for (unsigned i = 0; i < 8; i++) bits |= (uint64_t)p[i] << (8*i);
                double number; memcpy(&number, &bits, sizeof number); if (!isfinite(number)) return false;
            }
            p += n;
        } else if (wire == 2) {
            if (!varint(&p, end, &value) || value > (uint64_t)(end-p)) return false;
            if (known && PB_LTYPE(f.type) == PB_LTYPE_STRING && !text_ok(p, (size_t)value)) return false;
            if (known && PB_LTYPE_IS_SUBMSG(f.type) && !scan(p, (size_t)value, f.submsg_desc, depth+1)) return false;
            p += (size_t)value;
        } else return false;
    }
    return true;
}

bool monitor_proto_strict(const uint8_t *data, size_t length, const pb_msgdesc_t *desc)
{
    return data && desc && length && scan(data, length, desc, 0);
}

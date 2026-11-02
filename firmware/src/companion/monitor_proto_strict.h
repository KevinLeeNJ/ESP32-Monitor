#pragma once
#include "pb.h"
#include <stdbool.h>
#include <stddef.h>
/* Reject duplicate singular fields, conflicting oneofs and malformed values. */
bool monitor_proto_strict(const uint8_t *data, size_t length, const pb_msgdesc_t *desc);

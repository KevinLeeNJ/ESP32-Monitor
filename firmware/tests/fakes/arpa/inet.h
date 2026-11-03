#pragma once

/* Host regression shim: ESP-IDF provides arpa/inet.h through lwIP. */
#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include_next <arpa/inet.h>
#endif

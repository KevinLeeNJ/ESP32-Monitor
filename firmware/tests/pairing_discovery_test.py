"""Verify the actual discovery offer matcher rejects unrelated or expired peers."""
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import host_port

root = Path(__file__).resolve().parents[2] / 'firmware'
src = (root / 'components/firmware/monitor_pairing_discovery.c').read_text(encoding='utf-8')
def function(name):
    match = re.search(r'^static bool ' + name + r'\(', src, re.M)
    assert match
    end = src.index('{', match.start()) + 1
    depth = 1
    while depth:
        depth += (src[end] == '{') - (src[end] == '}')
        end += 1
    return src[match.start():end]
prefix = r'''
#include "companion.pb.h"
#include <assert.h>
#include <arpa/inet.h>
#include <string.h>
#define OFFER_TTL_US (10LL * 1000000)
typedef struct { bool used; struct sockaddr_in peer; int64_t created; esp32monitor_companion_Discovery request; char response_nonce[44]; } pending_t;
typedef struct { char host_id[37], pairing_id[37], serial_key[44]; } monitor_pairing_record_t;
static int64_t now;
static int64_t esp_timer_get_time(void) { return now; }
static bool monitor_pairing_sha256(void) { return true; }
static bool monitor_pairing_hmac(void) { return true; }
static bool monitor_companion_valid_uuid(const char *s) { return !strcmp(s,"valid-session"); }
static bool monitor_discovery_verify(const esp32monitor_companion_Discovery *d, const char *key,
                                     bool (*hash)(void), bool (*hmac)(void)) {
    (void)hash; (void)hmac; return d->has_mac && !strcmp(d->mac,"valid-mac") && !strcmp(key,"secret");
}
'''
tests = r'''
int main(void) {
 pending_t c={0}; c.used=true;c.peer.sin_addr.s_addr=42;c.peer.sin_port=7;c.created=100;
 strcpy(c.request.request_id,"request");strcpy(c.request.host_id,"host");strcpy(c.request.nonce,"nonce");strcpy(c.response_nonce,"response");
 esp32monitor_companion_Discovery d=esp32monitor_companion_Discovery_init_zero;
 strcpy(d.type,"monitor_connect_offer");strcpy(d.request_id,"request");strcpy(d.host_id,"host");strcpy(d.nonce,"nonce");
 d.has_device_id=d.has_response_nonce=d.has_host_port=d.has_pairing_open=d.has_device_name=d.has_firmware_version=true;
 strcpy(d.device_id,"device");strcpy(d.response_nonce,"response");d.host_port=18080;d.pairing_open=true;
 strcpy(d.device_name,"ESP32 Monitor");strcpy(d.firmware_version,"1.0.0");
 struct sockaddr_in peer=c.peer; now=101;
 assert(match_offer(&c,&d,&peer,"device"));
 peer.sin_port++;assert(!match_offer(&c,&d,&peer,"device"));peer=c.peer;
 strcpy(d.response_nonce,"other");assert(!match_offer(&c,&d,&peer,"device"));strcpy(d.response_nonce,"response");
 d.host_port=65536;assert(!match_offer(&c,&d,&peer,"device"));d.host_port=18080;
 d.has_mac=true;assert(!match_offer(&c,&d,&peer,"device"));d.has_mac=false;
 now=100+OFFER_TTL_US;assert(!match_offer(&c,&d,&peer,"device"));
 now=101; monitor_pairing_record_t record={0};strcpy(record.host_id,"host");strcpy(record.pairing_id,"pair");strcpy(record.serial_key,"secret");
 d.has_host_port=false;d.has_mqtt_port=true;d.mqtt_port=18883;d.has_session_id=true;strcpy(d.session_id,"valid-session");
 d.has_pairing_id=true;strcpy(d.pairing_id,"pair");d.pairing_open=false;d.has_mac=true;strcpy(d.mac,"valid-mac");
 assert(match_paired_offer(&c,&d,&peer,"device",&record));
 d.mac[0]='x';assert(!match_paired_offer(&c,&d,&peer,"device",&record));d.mac[0]='v';
 d.mqtt_port=0;assert(!match_paired_offer(&c,&d,&peer,"device",&record));d.mqtt_port=18883;
 peer.sin_port++;assert(!match_paired_offer(&c,&d,&peer,"device",&record));
 return 0;
}
'''
with tempfile.TemporaryDirectory() as tmp:
    path = Path(tmp) / 'test.c'
    path.write_text(prefix + function('match_offer') + function('match_paired_offer') + tests)
    binary = host_port.binary(tmp)
    subprocess.run([host_port.cc(sys.argv[1] if len(sys.argv) > 1 else None), '-std=c11', '-Wall', '-Wextra', '-Werror',
                    '-I'+str(root/'src/companion'), '-I'+str(root/'third_party/nanopb'),
                    '-I'+str(root/'tests/fakes'),
                    str(path), '-o', str(binary), *host_port.winsock_libraries()], check=True)
    host_port.run(binary)

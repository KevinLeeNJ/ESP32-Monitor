"""Exercise the production pairing store against a small in-memory NVS fake."""
import pathlib
import subprocess
import sys
import tempfile

root = pathlib.Path(__file__).resolve().parents[2]
source = root / 'firmware'
with tempfile.TemporaryDirectory() as temp:
    p = pathlib.Path(temp)
    (p / 'esp_err.h').write_text('''#pragma once
typedef int esp_err_t;
#define ESP_OK 0
#define ESP_ERR_INVALID_ARG 1
#define ESP_ERR_INVALID_STATE 2
#define ESP_ERR_NVS_NOT_FOUND 3
''')
    (p / 'esp_random.h').write_text('#pragma once\n#include <stddef.h>\nvoid esp_fill_random(void *, size_t);\n')
    (p / 'nvs.h').write_text('''#pragma once
#include "esp_err.h"
#include <stddef.h>
typedef int nvs_handle_t;
#define NVS_READWRITE 1
#define NVS_READONLY 0
esp_err_t nvs_open(const char *, int, nvs_handle_t *);
void nvs_close(nvs_handle_t);
esp_err_t nvs_get_str(nvs_handle_t, const char *, char *, size_t *);
esp_err_t nvs_set_str(nvs_handle_t, const char *, const char *);
esp_err_t nvs_get_blob(nvs_handle_t, const char *, void *, size_t *);
esp_err_t nvs_set_blob(nvs_handle_t, const char *, const void *, size_t);
esp_err_t nvs_erase_key(nvs_handle_t, const char *);
esp_err_t nvs_commit(nvs_handle_t);
''')
    (p / 'test.c').write_text(r'''
#include "monitor_pairing_store.h"
#include "monitor_discovery_auth.h"
#include "monitor_pairing_crypto.h"
#include "nvs.h"
#include <assert.h>
#include <stdint.h>
#include <string.h>
static char id[37]; static unsigned char blob[2048]; static size_t blob_len;
void esp_fill_random(void *p,size_t n) { memset(p, 7, n); }
bool monitor_pairing_sha256(const uint8_t *data,size_t len,uint8_t digest[32]) { (void)data;(void)len; memset(digest,1,32); return true; }
bool monitor_pairing_hmac(const uint8_t key[32],const uint8_t *data,size_t len,uint8_t digest[32]) { (void)key;(void)data;(void)len; memset(digest,2,32); return true; }
bool monitor_discovery_key(const char *serial_key,const char *host_id,const char *device_id,
                           const char *pairing_id,monitor_pairing_hash_t hash,
                           monitor_companion_hmac_t hmac,uint8_t out[32]) {
    (void)serial_key;(void)host_id;(void)device_id;(void)pairing_id;(void)hash;(void)hmac;
    memset(out,0x5a,32); return true;
}
int nvs_open(const char *name,int mode,nvs_handle_t *h) { (void)mode; assert(!strcmp(name,"companion")); *h=1; return 0; }
void nvs_close(nvs_handle_t h) { (void)h; }
int nvs_commit(nvs_handle_t h) { (void)h; return 0; }
int nvs_get_str(nvs_handle_t h,const char *key,char *out,size_t *n) { (void)h;(void)key; if(!id[0]) return 3; assert(*n>=sizeof id); memcpy(out,id,sizeof id); *n=sizeof id; return 0; }
int nvs_set_str(nvs_handle_t h,const char *key,const char *v) { (void)h;(void)key; strcpy(id,v); return 0; }
int nvs_get_blob(nvs_handle_t h,const char *key,void *out,size_t *n) { (void)h;(void)key; if(!blob_len) return 3; assert(*n>=blob_len); memcpy(out,blob,blob_len); *n=blob_len; return 0; }
int nvs_set_blob(nvs_handle_t h,const char *key,const void *v,size_t n) { (void)h;(void)key; assert(n<=sizeof blob); memcpy(blob,v,n);blob_len=n;return 0; }
int nvs_erase_key(nvs_handle_t h,const char *key) { (void)h;(void)key; if(!blob_len)return 3;blob_len=0;return 0; }
int main(void) {
 char a[37],b[37]; assert(!monitor_pairing_identity(a)); assert(!monitor_pairing_identity(b)); assert(!strcmp(a,b));
 monitor_pairing_session_t s = {0}; assert(monitor_pairing_store_approved(&s)==ESP_ERR_INVALID_ARG);
 s.state=MONITOR_PAIR_APPROVED; esp32monitor_companion_PairResponse *r=&s.approved;
 r->protocol_version=1; strcpy(r->state,"approved"); strcpy(r->host_id,"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee");
 strcpy(r->pairing_id,"12345678-1234-1234-1234-123456789abc"); strcpy(r->host_name,"host"); r->expires_in_ms=1000;
 strcpy(r->host_nonce,"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); strcpy(r->pairing_ticket,r->host_nonce);
 r->has_device_token=r->has_serial_key=true; strcpy(r->device_token,r->host_nonce); strcpy(r->serial_key,r->host_nonce);
 assert(!monitor_pairing_store_approved(&s)); monitor_pairing_record_t record={0}; assert(!monitor_pairing_store_load(&record));
 assert(!strcmp(record.host_id,r->host_id)); assert(!strcmp(record.serial_key,r->serial_key));
 monitor_pairing_revocation_t rev[4]={0}; assert(monitor_pairing_store_revocations(rev,4)==0);
 /* A corrupt blob must not change the caller's record. */
 unsigned char saved[2048]; size_t saved_len=blob_len; memcpy(saved,blob,blob_len);
 blob[0]=42; assert(monitor_pairing_store_load(&record)==ESP_ERR_INVALID_STATE); assert(!strcmp(record.host_id,r->host_id));
 memcpy(blob,saved,saved_len); blob_len=saved_len;
 /* Revocation keeps an authenticated tombstone for the former host. */
 assert(!monitor_pairing_store_revoke()); assert(monitor_pairing_store_load(&record)==ESP_ERR_NVS_NOT_FOUND);
 assert(monitor_pairing_store_revocations(rev,4)==1);
 assert(!strcmp(rev[0].host_id,r->host_id)); assert(!strcmp(rev[0].pairing_id,r->pairing_id));
 assert(!monitor_pairing_store_revoke()); assert(monitor_pairing_store_revocations(rev,4)==1);
 /* Pairing another host also tombstones the previous pairing. */
 strcpy(r->pairing_id,"12345678-1234-1234-1234-123456789abd"); assert(!monitor_pairing_store_approved(&s));
 assert(!monitor_pairing_store_load(&record)); assert(!strcmp(record.pairing_id,r->pairing_id));
 assert(monitor_pairing_store_revocations(rev,4)==1);
 strcpy(r->pairing_id,"12345678-1234-1234-1234-123456789abe"); assert(!monitor_pairing_store_approved(&s));
 assert(monitor_pairing_store_revocations(rev,4)==2);
 /* Version 1 blobs migrate without losing the active pairing. */
 struct { uint32_t version; monitor_pairing_record_t pair; } old = {.version=1};
 old.pair=record; memcpy(blob,&old,sizeof old); blob_len=sizeof old;
 monitor_pairing_record_t migrated={0}; assert(!monitor_pairing_store_load(&migrated));
 assert(!strcmp(migrated.pairing_id,record.pairing_id));
 assert(!monitor_pairing_store_revoke()); assert(monitor_pairing_store_revocations(rev,4)==1);
 assert(!monitor_pairing_identity(b) && !strcmp(a,b)); assert(!monitor_pairing_store_revoke()); return 0;
}
''')
    cmd = [sys.argv[1], '-std=c11', '-D_POSIX_C_SOURCE=200809L', '-Wall', '-Wextra', '-Werror',
           '-I'+str(p), '-I'+str(source/'components/firmware'), '-I'+str(source/'src/companion'),
           '-I'+str(source/'third_party/nanopb'), str(p/'test.c'),
           str(source/'components/firmware/monitor_pairing_store.c'), str(source/'src/companion/monitor_pairing_code.c'),
           '-o', str(p/'test')]
    subprocess.run(cmd, check=True)
    subprocess.run([str(p/'test')], check=True)

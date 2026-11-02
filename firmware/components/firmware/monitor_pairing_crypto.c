#include "monitor_pairing_crypto.h"
#include "psa/crypto.h"

bool monitor_pairing_sha256(const uint8_t *data, size_t len, uint8_t digest[32])
{
    size_t written = 0;
    return data && digest && psa_crypto_init() == PSA_SUCCESS &&
           psa_hash_compute(PSA_ALG_SHA_256, data, len, digest, 32, &written) == PSA_SUCCESS && written == 32;
}

bool monitor_pairing_hmac(const uint8_t key[32], const uint8_t *data, size_t len, uint8_t digest[32])
{
    if (!key || !data || !digest || psa_crypto_init() != PSA_SUCCESS) return false;
    psa_key_attributes_t attributes = PSA_KEY_ATTRIBUTES_INIT;
    psa_set_key_type(&attributes, PSA_KEY_TYPE_HMAC);
    psa_set_key_bits(&attributes, 256);
    psa_set_key_usage_flags(&attributes, PSA_KEY_USAGE_SIGN_MESSAGE);
    psa_set_key_algorithm(&attributes, PSA_ALG_HMAC(PSA_ALG_SHA_256));
    psa_key_id_t id = 0;
    psa_status_t status = psa_import_key(&attributes, key, 32, &id);
    psa_reset_key_attributes(&attributes);
    if (status != PSA_SUCCESS) return false;
    size_t written = 0;
    status = psa_mac_compute(id, PSA_ALG_HMAC(PSA_ALG_SHA_256), data, len, digest, 32, &written);
    psa_destroy_key(id);
    return status == PSA_SUCCESS && written == 32;
}

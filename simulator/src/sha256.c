#include "sha256.h"

#include <stdint.h>
#include <string.h>

typedef struct {
    uint32_t state[8];
    uint64_t bit_len;
    uint8_t block[64];
    uint32_t block_len;
} sha256_ctx;

static uint32_t rotr(uint32_t x, uint32_t n)
{
    return (x >> n) | (x << (32U - n));
}

static uint32_t load_be32(const uint8_t *p)
{
    return ((uint32_t)p[0] << 24U) | ((uint32_t)p[1] << 16U) |
           ((uint32_t)p[2] << 8U) | (uint32_t)p[3];
}

static void store_be32(uint8_t *p, uint32_t v)
{
    p[0] = (uint8_t)(v >> 24U);
    p[1] = (uint8_t)(v >> 16U);
    p[2] = (uint8_t)(v >> 8U);
    p[3] = (uint8_t)v;
}

static void sha256_transform(sha256_ctx *ctx, const uint8_t block[64])
{
    static const uint32_t k[64] = {
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
        0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
        0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
        0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
        0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
        0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
        0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
        0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U
    };
    uint32_t w[64];
    for(uint32_t i = 0; i < 16; ++i) w[i] = load_be32(block + i * 4U);
    for(uint32_t i = 16; i < 64; ++i) {
        uint32_t s0 = rotr(w[i-15],7) ^ rotr(w[i-15],18) ^ (w[i-15] >> 3U);
        uint32_t s1 = rotr(w[i-2],17) ^ rotr(w[i-2],19) ^ (w[i-2] >> 10U);
        w[i] = w[i-16] + s0 + w[i-7] + s1;
    }
    uint32_t a=ctx->state[0],b=ctx->state[1],c=ctx->state[2],d=ctx->state[3];
    uint32_t e=ctx->state[4],f=ctx->state[5],g=ctx->state[6],h=ctx->state[7];
    for(uint32_t i=0;i<64;++i) {
        uint32_t s1=rotr(e,6)^rotr(e,11)^rotr(e,25);
        uint32_t ch=(e&f)^((~e)&g);
        uint32_t t1=h+s1+ch+k[i]+w[i];
        uint32_t s0=rotr(a,2)^rotr(a,13)^rotr(a,22);
        uint32_t maj=(a&b)^(a&c)^(b&c);
        uint32_t t2=s0+maj;
        h=g;g=f;f=e;e=d+t1;d=c;c=b;b=a;a=t1+t2;
    }
    ctx->state[0]+=a;ctx->state[1]+=b;ctx->state[2]+=c;ctx->state[3]+=d;
    ctx->state[4]+=e;ctx->state[5]+=f;ctx->state[6]+=g;ctx->state[7]+=h;
}

static void sha256_init(sha256_ctx *ctx)
{
    static const uint32_t init[8] = {
        0x6a09e667U,0xbb67ae85U,0x3c6ef372U,0xa54ff53aU,
        0x510e527fU,0x9b05688cU,0x1f83d9abU,0x5be0cd19U
    };
    memcpy(ctx->state, init, sizeof(init));
    ctx->bit_len = 0;
    ctx->block_len = 0;
}

static void sha256_update(sha256_ctx *ctx, const uint8_t *data, uint32_t len)
{
    for(uint32_t i=0;i<len;++i) {
        ctx->block[ctx->block_len++] = data[i];
        if(ctx->block_len == 64U) {
            sha256_transform(ctx, ctx->block);
            ctx->bit_len += 512U;
            ctx->block_len = 0;
        }
    }
}

static void sha256_final(sha256_ctx *ctx, uint8_t digest[32])
{
    uint32_t i = ctx->block_len;
    ctx->block[i++] = 0x80U;
    if(i > 56U) {
        while(i < 64U) ctx->block[i++] = 0;
        sha256_transform(ctx, ctx->block);
        i = 0;
    }
    while(i < 56U) ctx->block[i++] = 0;
    uint64_t bits = ctx->bit_len + (uint64_t)ctx->block_len * 8U;
    for(uint32_t j=0;j<8;++j) ctx->block[63U-j] = (uint8_t)(bits >> (j*8U));
    sha256_transform(ctx, ctx->block);
    for(i=0;i<8;++i) store_be32(digest + i*4U, ctx->state[i]);
}

void monitor_sha256(const uint8_t *data, uint32_t len, char out_hex[65])
{
    static const char hex[] = "0123456789abcdef";
    sha256_ctx ctx;
    uint8_t digest[32];
    sha256_init(&ctx);
    sha256_update(&ctx, data, len);
    sha256_final(&ctx, digest);
    for(uint32_t i=0;i<32;++i) {
        out_hex[i*2U] = hex[digest[i] >> 4U];
        out_hex[i*2U+1U] = hex[digest[i] & 0x0fU];
    }
    out_hex[64] = '\0';
}

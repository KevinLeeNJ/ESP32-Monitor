"""Execute actual RLCD window/commit functions against deterministic SPI fakes."""
from pathlib import Path
import re
import subprocess
import sys
import tempfile
repo = Path(__file__).resolve().parents[2]
source = (repo/'firmware/components/firmware/display_port.c').read_text()

def function(name):
    match = re.search(r'^(?:static )?esp_err_t '+name+r'\(', source, re.M)
    assert match, name
    end = source.index('{', match.start())+1
    depth = 1
    while depth:
        depth += (source[end]=='{')-(source[end]=='}')
        end += 1
    return source[match.start():end]

code = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_ERR_INVALID_ARG -2
#define ESP_ERR_INVALID_SIZE -3
#define RLCD_PACKED_BYTES 15000U
#define ESP_RETURN_ON_FALSE(c,e,...) do {if(!(c))return e;}while(0)
#define ESP_RETURN_ON_ERROR(c,...) do {int e=(c);if(e)return e;}while(0)
typedef int esp_err_t;
typedef struct {uint8_t *packed,*staging;int io;} rlcd_port_t;
static unsigned commands,args,transfers,waits,fail_command;
static uint8_t window[4];
static uint32_t bytes;
static bool fail_transfer,fail_wait;
static int send_cmd(rlcd_port_t *p,uint8_t cmd) {
    (void)p;assert(cmd==(commands%2?0x2b:0x2a));++commands;
    return commands==fail_command?ESP_FAIL:ESP_OK;
}
static int send_data1(rlcd_port_t *p,uint8_t value) {
    (void)p;assert(args<4);window[args++]=value;return ESP_OK;
}
static void wait_panel_frame(rlcd_port_t *p) {(void)p;++waits;}
static int esp_lcd_panel_io_tx_color(int io,int cmd,const uint8_t *data,uint32_t len) {
    assert(io==1 && cmd==0x2c && data && waits==1);++transfers;bytes=len;
    return fail_transfer?ESP_FAIL:ESP_OK;
}
static int wait_color_done(rlcd_port_t *p) {(void)p;return fail_wait?ESP_FAIL:ESP_OK;}
'''
code += '\n'.join(function(name) for name in ['set_window','rlcd_port_draw','rlcd_port_draw_diff'])
code += r'''
static void reset(void) {
    commands=args=transfers=waits=fail_command=bytes=0;fail_transfer=fail_wait=false;
    memset(window,0,sizeof(window));
}
int main(void) {
    uint8_t old[15000],frame[15000],staging[15002],before[15000];
    memset(old,0xff,sizeof(old));memcpy(frame,old,sizeof(old));memset(staging,0xa5,sizeof(staging));
    rlcd_port_t p={.packed=old,.staging=staging+1,.io=1};
    assert(rlcd_port_draw_diff(&p,frame,15000)==ESP_OK && waits==1 && !transfers);
    reset();assert(rlcd_port_draw_diff(NULL,frame,15000)==ESP_ERR_INVALID_ARG);
    assert(rlcd_port_draw_diff(&p,NULL,15000)==ESP_ERR_INVALID_ARG);
    assert(rlcd_port_draw_diff(&p,frame,14999)==ESP_ERR_INVALID_SIZE && !waits);
    /* Every physical tile and both extreme pages exercise reversed X addresses. */
    for(unsigned page=0;page<200;page+=199)for(unsigned tile=0;tile<25;++tile) {
        reset();memset(old,0xff,15000);memcpy(frame,old,15000);
        unsigned offset=page*75+tile*3;frame[offset+1]=0;
        assert(rlcd_port_draw_diff(&p,frame,15000)==ESP_OK);
        assert(window[0]==0x2a-tile && window[1]==0x2a-tile && window[2]==page && window[3]==page);
        assert(bytes==3 && transfers==1 && !memcmp(frame,old,15000));
        assert(!memcmp(p.staging,frame+offset,3));
    }
    reset();memset(old,0xff,15000);memcpy(frame,old,15000);
    frame[2*75+3]=0x42;frame[4*75+9]=0x24;
    assert(rlcd_port_draw_diff(&p,frame,15000)==ESP_OK && bytes==27);
    assert(window[0]==0x27 && window[1]==0x29 && window[2]==2 && window[3]==4);
    for(unsigned row=0;row<3;++row)assert(!memcmp(p.staging+row*9,frame+(row+2)*75+3,9));
    for(unsigned full=0;full<2;++full)for(unsigned fault=0;fault<4;++fault) {
        reset();memset(old,0xff,15000);memcpy(before,old,15000);memcpy(frame,old,15000);
        frame[0]=0;if(full)frame[14999]=0;
        if(fault<2)fail_command=fault+1;
        if(fault==2)fail_transfer=true;
        if(fault==3)fail_wait=true;
        assert(rlcd_port_draw_diff(&p,frame,15000)==ESP_FAIL);
        assert(!memcmp(before,old,15000)); /* failed transfer must remain dirty */
        reset();assert(rlcd_port_draw_diff(&p,frame,15000)==ESP_OK);
        assert(bytes==(full?15000U:3U) && !memcmp(frame,old,15000));
    }
    assert(staging[0]==0xa5 && staging[15001]==0xa5);
    puts("RLCD unchanged frames, all X tiles, page edges, rectangular staging, full-frame fallback, window/SPI/completion failures and retry cache: PASS");
}
'''
with tempfile.TemporaryDirectory(prefix='monitor-diff-') as directory:
    unit=Path(directory)/'test.c';unit.write_text(code)
    binary=Path(directory)/'test'
    compiler=sys.argv[1] if len(sys.argv)>1 else 'cc'
    subprocess.run([compiler,'-std=c11','-Wall','-Wextra','-Werror',str(unit),'-o',str(binary)],check=True)
    subprocess.run([str(binary)],check=True,timeout=5)

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
#include "monitor_damage.h"
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_ERR_INVALID_ARG -2
#define ESP_ERR_INVALID_SIZE -3
#define RLCD_PACKED_BYTES 15000U
#define ESP_RETURN_ON_FALSE(c,e,...) do {if(!(c))return e;}while(0)
#define ESP_RETURN_ON_ERROR(c,...) do {int e=(c);if(e)return e;}while(0)
typedef int esp_err_t;
typedef struct {uint8_t *packed,*staging;int io; bool tx_pending,recover_full;} rlcd_port_t;
static unsigned commands,args,transfers,waits,fail_command;
static uint8_t window[32];
static uint8_t panel[15000];
static uint32_t bytes;
static bool fail_transfer,fail_wait;
static int send_cmd(rlcd_port_t *p,uint8_t cmd) {
    (void)p;assert(cmd==(commands%2?0x2b:0x2a));++commands;
    return commands==fail_command?ESP_FAIL:ESP_OK;
}
static int send_data1(rlcd_port_t *p,uint8_t value) {
    (void)p;assert(args<32);window[args++]=value;return ESP_OK;
}
static void wait_panel_frame(rlcd_port_t *p) {(void)p;++waits;}
static int esp_lcd_panel_io_tx_color(int io,int cmd,const uint8_t *data,uint32_t len) {
    assert(io==1 && cmd==0x2c && data && waits==1);++transfers;bytes+=len;
    if (!fail_transfer && !fail_wait) {
        unsigned t1=0x2a-window[args-3], t2=0x2a-window[args-4], off=0;
        for(unsigned p=window[args-2];p<=window[args-1];++p) {
            unsigned n=(t2-t1+1)*3;memcpy(panel+p*75+t1*3,data+off,n);off+=n;
        }
        assert(off==len);
    }
    return fail_transfer?ESP_FAIL:ESP_OK;
}
static int wait_color_done(rlcd_port_t *p) {(void)p;return fail_wait?ESP_FAIL:ESP_OK;}
'''
code += '\n'.join(function(name) for name in ['set_window','submit_windows','rlcd_port_draw_dirty','rlcd_port_draw'])
code += r'''
static void reset(void) {
    commands=args=transfers=waits=fail_command=bytes=0;fail_transfer=fail_wait=false;
    memset(window,0,sizeof(window));
}
int main(void) {
    uint8_t old[15000],frame[15000],staging[15002];
    memset(old,0xff,sizeof(old));memcpy(frame,old,sizeof(old));memcpy(panel,old,sizeof(old));
    memset(staging,0xa5,sizeof(staging));
    rlcd_port_t p={.packed=old,.staging=staging+1,.io=1};
    monitor_damage_t d={0};
    monitor_damage_plan_t plan;
    assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_OK && waits==1 && !transfers);
    reset();assert(rlcd_port_draw_dirty(NULL,frame,15000,&d,false)==ESP_ERR_INVALID_ARG);
    assert(rlcd_port_draw_dirty(&p,NULL,15000,&d,false)==ESP_ERR_INVALID_ARG);
    assert(rlcd_port_draw_dirty(&p,frame,14999,&d,false)==ESP_ERR_INVALID_SIZE);
    /* Every tile, pixel origin and inverted Y mapping. Overlap is idempotent. */
    for(unsigned page=0;page<200;++page)for(unsigned tile=0;tile<25;++tile) {
        reset();memset(&d,0,sizeof(d));
        unsigned offset=page*75+tile*3;frame[offset+1]^=0xff;
        int y=299-(int)tile*12;
        monitor_damage_mark(&d,page*2,y,page*2+1,y);
        monitor_damage_mark(&d,page*2,y,page*2+1,y);
        monitor_damage_plan(&d,frame,old,&plan);
        assert(plan.compared_tiles==1 && plan.changed_tiles==1);
        assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_OK);
        assert(window[0]==0x2a-tile && window[1]==0x2a-tile && window[2]==page && window[3]==page);
        assert(bytes==3 && transfers==1 && !memcmp(frame,old,15000));
        assert(!memcmp(panel,old,15000));
    }
    /* Nonaligned crossing of both tile axes, clipping and unchanged dirty. */
    memset(&d,0,sizeof(d));monitor_damage_mark(&d,1,11,2,12);
    monitor_damage_plan(&d,frame,old,&plan);assert(plan.compared_tiles==4 && !plan.count);
    monitor_damage_mark(&d,-10,-10,399,299);
    monitor_damage_plan(&d,frame,old,&plan);assert(plan.compared_tiles==5000 && !plan.count);
    /* Three distant UI regions must not become one enclosing rectangle. */
    reset();memset(&d,0,sizeof(d));
    monitor_damage_mark(&d,0,0,15,23);monitor_damage_mark(&d,384,0,399,23);
    monitor_damage_mark(&d,0,276,15,299);
    for(unsigned i=0;i<5000;++i)if(d.bits[i/8]&(1U<<(i%8)))frame[i*3]^=1;
    monitor_damage_plan(&d,frame,old,&plan);assert(plan.count==3 && plan.bytes==144 && plan.compared_tiles==48);
    assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_OK && bytes==144 && transfers==3 && waits==1);
    assert(!memcmp(panel,frame,15000));
    reset();assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_OK && !transfers);
    /* Sparse glyph-like changes in the same three regions retain separate
     * clusters even when their individual runs exceed the window limit. */
    for(unsigned i=0;i<5000;++i)if((d.bits[i/8]&(1U<<(i%8))) && i%3)frame[i*3]^=1;
    monitor_damage_plan(&d,frame,old,&plan);
    assert(plan.count>=3 && plan.bytes<1000);
    reset();assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_OK);
    assert(!memcmp(panel,frame,15000));
    /* Full invalidation is an unconditional full write. */
    reset();assert(rlcd_port_draw(&p,frame,15000)==ESP_OK && bytes==15000 && transfers==1);
    /* Fragmentation and full-screen changes remain bounded. */
    memset(&d,0xff,sizeof(d));memset(frame,0,15000);memset(old,0xff,15000);
    monitor_damage_plan(&d,frame,old,&plan);assert(plan.count==1 && plan.bytes==15000);
    reset();assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_OK);
    /* Fail after one successful window; retry drains timed-out DMA, then full
     * recovery even if no new LVGL area is produced. Never reuse pending DMA. */
    for(unsigned fault=0;fault<4;++fault) {
        reset();memset(&d,0,sizeof(d));
        frame[0]^=1;frame[14999]^=1;
        monitor_damage_mark(&d,0,299,0,299);monitor_damage_mark(&d,399,0,399,0);
        if(fault<2)fail_command=fault+1;
        if(fault==2)fail_transfer=true;
        if(fault==3)fail_wait=true;
        uint8_t before[15000];memcpy(before,old,15000);
        assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_FAIL);
        assert(!memcmp(before,old,15000) && p.recover_full);
        if(fault==3) {
            unsigned saved_transfers=transfers,saved_commands=commands;
            uint8_t saved[15000];memcpy(saved,p.staging,15000);
            assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_FAIL);
            assert(transfers==saved_transfers && commands==saved_commands && !memcmp(saved,p.staging,15000));
        }
        reset();memset(&d,0,sizeof(d));
        assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_OK);
        assert(bytes==15000 && !memcmp(frame,old,15000) && !memcmp(panel,frame,15000));
    }
    /* Second-window failure commits only the successful first window. */
    reset();memset(&d,0xff,sizeof(d));frame[0]^=1;frame[14999]^=1;fail_command=3;
    assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_FAIL && transfers==1);
    assert(old[0]==frame[0] && old[14999]!=frame[14999]);
    reset();assert(rlcd_port_draw_dirty(&p,frame,15000,&d,false)==ESP_OK && bytes==15000);
    assert(staging[0]==0xa5 && staging[15001]==0xa5);
    puts("RLCD 5000 tile mappings, overlaps, empty dirty, three regions, full fallback, DMA/window failure recovery: PASS");
}
'''
with tempfile.TemporaryDirectory(prefix='monitor-diff-') as directory:
    unit=Path(directory)/'test.c';unit.write_text(code)
    binary=Path(directory)/'test'
    compiler=sys.argv[1] if len(sys.argv)>1 else 'cc'
    subprocess.run([compiler,'-std=c11','-Wall','-Wextra','-Werror','-I'+str(repo/'firmware/include'),str(repo/'firmware/src/display/monitor_damage.c'),str(unit),'-o',str(binary)],check=True)
    subprocess.run([str(binary)],check=True,timeout=5)

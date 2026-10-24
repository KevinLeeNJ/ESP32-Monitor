"""Exercise production sensor conversion and I/O failure paths without a board."""
from pathlib import Path
import re
import subprocess
import sys
import tempfile

repo = Path(__file__).resolve().parents[2]
source = (repo / 'firmware/components/firmware/monitor_sensors.c').read_text()

def function(name):
    match = re.search(r'^static [^\n]*\b' + name + r'\(', source, re.M)
    assert match, name
    end = source.index('{', match.start()) + 1
    depth = 1
    while depth:
        depth += (source[end] == '{') - (source[end] == '}')
        end += 1
    return source[match.start():end]

code = r'''
#include <assert.h>
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "monitor_sensors.h"
#define BATTERY_ADC_CHANNEL 3
#define BATTERY_DIVIDER_RATIO 3.0f
#define portENTER_CRITICAL(p) ((void)(p))
#define portEXIT_CRITICAL(p) ((void)(p))
#define pdMS_TO_TICKS(ms) (ms)
static int s_data_mux, s_adc, s_shtc3;
static void *s_adc_cali=(void *)1;
static monitor_sensor_data_t s_data;
static unsigned sends, receives, delays;
static unsigned fail_send;
static bool fail_receive, fail_adc, fail_cali;
static int millivolts;
static uint8_t sample[6];
static int adc_oneshot_read(int adc,int channel,int *raw) {
    (void)adc; assert(channel==3); *raw=1234; return fail_adc?ESP_FAIL:ESP_OK;
}
static int adc_cali_raw_to_voltage(void *cali,int raw,int *mv) {
    assert(cali && raw==1234); *mv=millivolts; return fail_cali?ESP_FAIL:ESP_OK;
}
static int i2c_master_transmit(int dev,const uint8_t *data,size_t n,int timeout) {
    (void)dev;assert(n==2 && timeout==100); ++sends;
    const uint8_t expected[][2]={{0x35,0x17},{0x78,0x66},{0xb0,0x98}};
    assert(sends<=3 && !memcmp(data,expected[sends-1],2));
    return sends==fail_send?ESP_FAIL:ESP_OK;
}
static int i2c_master_receive(int dev,uint8_t *data,size_t n,int timeout) {
    (void)dev;assert(n==6 && timeout==100);++receives;
    memcpy(data,sample,6);return fail_receive?ESP_FAIL:ESP_OK;
}
static void vTaskDelay(unsigned ms) {assert(ms==1 || ms==20);delays+=ms;}
'''
code += '\n'.join(function(name) for name in [
    'battery_percent_from_voltage', 'read_battery', 'shtc3_crc', 'read_environment'])
code += r'''
static void reset(void) {
    sends=receives=delays=fail_send=0;fail_receive=false;
    s_data=(monitor_sensor_data_t){.temperature_tenths_c=245,.humidity_percent=58};
}
int main(void) {
    /* Independent datasheet CRC reference: BE EF -> 92 (SHTC3 polynomial 0x31). */
    const uint8_t vector[]={0xbe,0xef}; assert(shtc3_crc(vector)==0x92);
    assert(battery_percent_from_voltage(3.3f)==0);
    assert(battery_percent_from_voltage(3.85f)==50);
    assert(battery_percent_from_voltage(4.2f)==100);
    int previous=-1;
    for (unsigned mv=2500;mv<=5000;++mv) {
        int pct=battery_percent_from_voltage(mv/1000.0f);
        assert(pct>=previous && pct>=0 && pct<=100);previous=pct;
    }
    bool changed=false;millivolts=1400;
    assert(read_battery(&changed) && changed && s_data.battery_percent==100 && s_data.battery_valid);
    changed=true;assert(read_battery(&changed) && !changed);
    assert(read_battery(NULL));
    monitor_sensor_data_t before=s_data;
    fail_adc=true;assert(!read_battery(&changed));fail_adc=false;
    assert(!memcmp(&before,&s_data,sizeof(before)));
    fail_cali=true;assert(!read_battery(&changed));fail_cali=false;
    assert(!memcmp(&before,&s_data,sizeof(before)));
    s_adc_cali=NULL;assert(!read_battery(&changed));s_adc_cali=(void *)1;
    assert(!memcmp(&before,&s_data,sizeof(before)));
    puts("Battery curve endpoints, monotonicity, unchanged values, ADC/calibration failures: PASS");
    /* Raw zero, midpoint and full scale check the physical-unit conversion. */
    const unsigned raw[]={0,32768,65535};
    const int temperature[]={-450,425,1300}, humidity[]={0,50,100};
    for (unsigned i=0;i<3;++i) {
        reset();sample[0]=sample[3]=raw[i]>>8;sample[1]=sample[4]=raw[i];
        sample[2]=shtc3_crc(sample);sample[5]=shtc3_crc(sample+3);
        assert(read_environment(&changed) && changed && sends==3 && receives==1 && delays==21);
        assert(s_data.temperature_tenths_c==temperature[i] && s_data.humidity_percent==humidity[i] && s_data.environment_valid);
        sends=receives=0;changed=true;
        assert(read_environment(&changed) && !changed);
    }
    /* Corrupt each CRC in turn; command/read errors must still send sleep. */
    for (unsigned fault=0;fault<5;++fault) {
        reset();before=s_data;
        sample[2]=shtc3_crc(sample);sample[5]=shtc3_crc(sample+3);
        if(fault<2)sample[fault?5:2]^=1;
        if(fault==2)fail_send=2;
        if(fault==3)fail_receive=true;
        if(fault==4)fail_send=1;
        assert(!read_environment(&changed));
        assert(sends==(fault==4?1:3));
        assert(!memcmp(&before,&s_data,sizeof(before)));
    }
    reset();sample[2]=shtc3_crc(sample);sample[5]=shtc3_crc(sample+3);
    assert(read_environment(NULL));
    puts("SHTC3 CRC, negative/midpoint/max temperature, humidity limits, duplicate data, wake/measure/read failures and sleep cleanup: PASS");
}
'''
with tempfile.TemporaryDirectory(prefix='monitor-sensors-') as directory:
    unit = Path(directory) / 'test.c'
    unit.write_text(code)
    binary = Path(directory) / 'test'
    compiler = sys.argv[1] if len(sys.argv) > 1 else 'cc'
    subprocess.run([compiler, '-std=c11', '-Wall', '-Wextra', '-Werror',
                    '-I'+str(repo/'firmware/tests/fakes'),
                    '-I'+str(repo/'firmware/components/firmware'),
                    str(unit), '-lm', '-o', str(binary)], check=True)
    subprocess.run([str(binary)], check=True, timeout=5)

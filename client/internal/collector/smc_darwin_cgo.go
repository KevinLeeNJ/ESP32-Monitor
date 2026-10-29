//go:build darwin && cgo

package collector

// SMC protocol layout and temperature key mappings are adapted from Stats.
// Copyright (c) 2019 Serhiy Mytrovtsiy; see client/third_party/stats/LICENSE.

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/IOKitLib.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>
#include <math.h>
typedef struct { uint8_t major, minor, build, reserved; uint16_t release; } vers_t;
typedef struct { uint16_t version,length; uint32_t cpu,gpu,mem; } limit_t;
typedef struct __attribute__((packed)) { uint32_t size,type; uint8_t attr; } info_t;
typedef struct { uint32_t key; vers_t vers; limit_t limits; info_t info; uint16_t padding; uint8_t result,status,command; uint32_t data32; uint8_t bytes[32]; } smcdata;
static uint32_t keycode(const char *p) { return ((uint32_t)p[0]<<24)|((uint32_t)p[1]<<16)|((uint32_t)p[2]<<8)|(uint8_t)p[3]; }
static io_connect_t smc_open(void) {
 io_service_t svc=IOServiceGetMatchingService(kIOMainPortDefault,IOServiceMatching("AppleSMC")); if (!svc) return 0;
 io_connect_t c=0; kern_return_t rc=IOServiceOpen(svc,mach_task_self(),0,&c); IOObjectRelease(svc); return rc==KERN_SUCCESS?c:0;
}
static int smc_read(io_connect_t c,const char *key,double *value) {
 smcdata in={0}, out={0}; size_t size=sizeof(out); in.key=keycode(key); in.command=9;
 if(IOConnectCallStructMethod(c,2,&in,sizeof(in),&out,&size)!=KERN_SUCCESS||out.result||out.info.size==0||out.info.size>32) return 0;
 in.info=out.info; in.command=5; size=sizeof(out); memset(&out,0,sizeof(out));
 if(IOConnectCallStructMethod(c,2,&in,sizeof(in),&out,&size)!=KERN_SUCCESS||out.result) return 0;
 uint32_t t=in.info.type; unsigned char *b=out.bytes;
 if(t==keycode("flt ") && in.info.size>=4) { float f; memcpy(&f,b,4); *value=f; }
 else if(t==keycode("sp78") && in.info.size>=2) { *value=(int16_t)((b[0]<<8)|b[1])/256.0; }
 else if(t==keycode("fpe2") && in.info.size>=2) { *value=((b[0]<<8)|b[1])/4.0; }
 else if(t==keycode("ui8 ") && in.info.size>=1) { *value=b[0]; }
 else if(t==keycode("ui16") && in.info.size>=2) { *value=(b[0]<<8)|b[1]; }
 else return 0;
 return isfinite(*value);
}
*/
import "C"
import (
	"fmt"
	"strings"
	"unsafe"
)

func readSMC(model string) (map[string]float64, map[string]float64) {
	c := C.smc_open()
	if c == 0 {
		return nil, nil
	}
	defer C.IOServiceClose(c)
	read := func(key string) (float64, bool) {
		p := C.CString(key)
		defer C.free(unsafe.Pointer(p))
		var v C.double
		ok := C.smc_read(c, p, &v) != 0
		return float64(v), ok
	}
	cpuKeys, gpuKeys := sensorKeys(model)
	cpu := map[string]float64{}
	gpu := map[string]float64{}
	for _, key := range cpuKeys {
		if v, ok := read(key); ok && v >= 10 && v < 110 {
			cpu[key] = v
		}
	}
	for _, key := range gpuKeys {
		if v, ok := read(key); ok && v > 0 && v < 110 {
			gpu[key] = v
		}
	}
	return cpu, gpu
}
func sensorKeys(model string) ([]string, []string) {
	if strings.Contains(model, "M5") {
		return strings.Fields("Tp00 Tp04 Tp08 Tp0C Tp0G Tp0K Tp0O Tp0R Tp0U Tp0X Tp0a Tp0d Tp0g Tp0j Tp0m Tp0p Tp0u Tp0y"), strings.Fields("Tg0U Tg0X Tg0d Tg0g Tg0j Tg1Y Tg1c Tg1g")
	}
	if strings.Contains(model, "M6") {
		return strings.Fields("Tp0j Tp0g Tp09 Tp07 Tp0m Tp0L Tp0I Tp0d Tp0E Tp0G Tp0b Tp05 Te07 Te08 Te09 Te0c Te0h Te0i"), strings.Fields("Tg1e Tg0d Tg0c Tg0b Tg1c Tg1d Tg07 Tg1f Tg09 Tg0a Tg1g Tg1h")
	}
	if strings.Contains(model, "M4") {
		gpu := strings.Fields("Tg0G Tg0H Tg0K Tg0L Tg0d Tg0e Tg0j Tg0k")
		if strings.Contains(model, "Pro") || strings.Contains(model, "Max") {
			gpu = strings.Fields("Tg1U Tg1k Tg0K Tg0L Tg0d Tg0e Tg0j Tg0k")
		}
		return strings.Fields("Te05 Te0S Te09 Te0H Tp01 Tp05 Tp09 Tp0D Tp0V Tp0Y Tp0b Tp0e"), gpu
	}
	if strings.Contains(model, "M3") {
		return strings.Fields("Te05 Te0L Te0P Te0S Tf04 Tf09 Tf0A Tf0B Tf0D Tf0E Tf44 Tf49 Tf4A Tf4B Tf4D Tf4E"), strings.Fields("Tf14 Tf18 Tf19 Tf1A Tf24 Tf28 Tf29 Tf2A")
	}
	if strings.Contains(model, "M2") {
		return strings.Fields("Tp1h Tp1t Tp1p Tp1l Tp01 Tp05 Tp09 Tp0D Tp0X Tp0b Tp0f Tp0j"), strings.Fields("Tg0f Tg0j")
	}
	if strings.Contains(model, "M1") {
		return strings.Fields("Tp09 Tp0T Tp01 Tp05 Tp0D Tp0H Tp0L Tp0P Tp0X Tp0b"), strings.Fields("Tg05 Tg0D Tg0L Tg0T")
	}
	cpu := []string{"TCAC", "TCBC"}
	for i := 0; i < 10; i++ {
		cpu = append(cpu, fmt.Sprintf("TC%dc", i), fmt.Sprintf("TC%dC", i))
	}
	return cpu, []string{"TGDD", "TCGC"}
}

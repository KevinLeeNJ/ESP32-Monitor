//go:build darwin

package collector

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDarwinLiveProbe(t *testing.T) {
	if os.Getenv("MONITOR_NATIVE_LIVE_TEST") != "1" {
		t.Skip("explicit live test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	b, r := newNativeCollector().Gather(ctx, []string{"darwin_cpu", "darwin_gpu", "darwin_disk", "darwin_fan"})
	for _, m := range b.Metrics {
		t.Logf("%s tags=%v fields=%v", m.Name, m.Tags, m.Fields)
	}
	t.Logf("unavailable: %v", r)
	if len(b.Metrics) == 0 {
		t.Fatal("no native samples")
	}
}

func TestDarwinLivePublicIP(t *testing.T) {
	if os.Getenv("MONITOR_PUBLIC_IP_LIVE_TEST") != "1" {
		t.Skip("explicit network test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ip, err := publicIPv4(ctx)
	if err != nil {
		t.Fatalf("public IPv4 lookup: %v", err)
	}
	if ip == "" {
		t.Fatal("empty public IPv4")
	}
	t.Log("received valid public IPv4")
}

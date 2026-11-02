package transport

import (
	"net"
	"testing"
)

func TestSubnetCandidatesBounded(t *testing.T) {
	candidates := subnetCandidates(net.ParseIP("192.168.110.2"), net.CIDRMask(24, 32))
	if len(candidates) != 253 {
		t.Fatalf("got %d candidates, want 253", len(candidates))
	}
	foundBoard := false
	for _, ip := range candidates {
		if ip.String() == "192.168.110.169" {
			foundBoard = true
		}
		if ip.String() == "192.168.110.2" || ip.String() == "192.168.110.0" || ip.String() == "192.168.110.255" {
			t.Fatalf("invalid target %s", ip)
		}
	}
	if !foundBoard {
		t.Fatal("local board address missing")
	}
	large := subnetCandidates(net.ParseIP("10.4.5.6"), net.CIDRMask(16, 32))
	if len(large) != 253 || large[0].String() != "10.4.5.1" {
		t.Fatal("large subnet was not bounded to /24")
	}
	if len(subnetCandidates(net.ParseIP("127.0.0.1"), net.CIDRMask(8, 32))) != 0 {
		t.Fatal("loopback must not be scanned")
	}
	if len(subnetCandidates(net.ParseIP("192.168.0.1"), net.CIDRMask(31, 32))) != 0 {
		t.Fatal("point-to-point mask must not be scanned")
	}
}

package api

import (
	"reflect"
	"testing"

	"it-platform/backend/internal/netbird"
)

func TestOfficePortsEmptyMeansEverything(t *testing.T) {
	rules, err := parseOfficePorts("")
	if err != nil || !reflect.DeepEqual(rules, []netbird.PolicyRule{{Protocol: "all"}}) {
		t.Fatalf("got %+v, %v", rules, err)
	}
}

func TestOfficePortsGroupedByProtocol(t *testing.T) {
	rules, err := parseOfficePorts("tcp/445, TCP/3389,udp/5060,udp/20000-20999,icmp")
	if err != nil {
		t.Fatal(err)
	}
	want := []netbird.PolicyRule{
		{Protocol: "tcp", Ports: []string{"445", "3389"}},
		{Protocol: "udp", Ports: []string{"5060"}, PortRanges: []netbird.PortRange{{Start: 20000, End: 20999}}},
		{Protocol: "icmp"},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("got %+v", rules)
	}
}

func TestOfficePortsRefusesNonsense(t *testing.T) {
	for _, bad := range []string{"445", "tcp/0", "tcp/70000", "tcp/200-100", "http/80", "tcp/445;rm", "tcp/"} {
		if _, err := parseOfficePorts(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

package api

import "testing"

func TestParseOfficeNetwork(t *testing.T) {
	routes := `default via 10.195.6.93 dev wlo1 proto dhcp src 10.195.6.74 metric 600
10.195.6.0/24 dev wlo1 proto kernel scope link src 10.195.6.74 metric 600
10.201.28.0/24 dev br-f8d778c90f80 proto kernel scope link src 10.201.28.16
172.17.0.0/16 dev docker0 proto kernel scope link src 172.17.0.1 linkdown
`
	if got := parseOfficeNetwork(routes); got != "10.195.6.0/24" {
		t.Fatalf("got %q", got)
	}
	if got := parseOfficeNetwork("172.17.0.0/16 dev docker0 scope link\n"); got != "" {
		t.Fatalf("no default route: got %q", got)
	}
}

func TestValidOfficeNetwork(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.0/24":  "192.168.1.0/24",
		"192.168.1.77/24": "192.168.1.0/24",
		"10.0.0.0/8":      "", // contains the platform's own edge network
		"10.10.0.0/16":    "10.10.0.0/16",
		"8.8.8.0/24":      "",
		"10.201.28.0/24":  "",
		"10.0.0.0/7":      "",
		"192.168.1.1/32":  "",
		"garbage":         "",
	} {
		p, ok := validOfficeNetwork(in)
		got := ""
		if ok {
			got = p.String()
		}
		if got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

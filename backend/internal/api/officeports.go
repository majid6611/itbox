package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"it-platform/backend/internal/netbird"
)

// officePortRe is one allowed thing on the office network: "tcp/445",
// "udp/20000-20999" or "icmp" (ping).
var officePortRe = regexp.MustCompile(`^(?:(tcp|udp)/(\d{1,5})(?:-(\d{1,5}))?|icmp)$`)

// parseOfficePorts turns the saved list ("" = everything) into the policy
// rules that allow exactly that. It is also the validator for what an admin
// types, so a bad entry is refused rather than quietly opening or closing
// something.
func parseOfficePorts(list string) ([]netbird.PolicyRule, error) {
	list = strings.TrimSpace(list)
	if list == "" {
		return []netbird.PolicyRule{{Protocol: "all"}}, nil
	}
	tcp := netbird.PolicyRule{Protocol: "tcp"}
	udp := netbird.PolicyRule{Protocol: "udp"}
	icmp := false
	for _, item := range strings.Split(list, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		m := officePortRe.FindStringSubmatch(item)
		if m == nil {
			return nil, fmt.Errorf("%q isn't a port like tcp/445, udp/5000-5100 or icmp", item)
		}
		if item == "icmp" {
			icmp = true
			continue
		}
		start, _ := strconv.Atoi(m[2])
		end := start
		if m[3] != "" {
			end, _ = strconv.Atoi(m[3])
		}
		if start < 1 || end > 65535 || end < start {
			return nil, fmt.Errorf("%q has a port outside 1-65535", item)
		}
		r := &tcp
		if m[1] == "udp" {
			r = &udp
		}
		if start == end {
			r.Ports = append(r.Ports, strconv.Itoa(start))
		} else {
			r.PortRanges = append(r.PortRanges, netbird.PortRange{Start: start, End: end})
		}
	}
	var rules []netbird.PolicyRule
	for _, r := range []netbird.PolicyRule{tcp, udp} {
		if len(r.Ports) > 0 || len(r.PortRanges) > 0 {
			rules = append(rules, r)
		}
	}
	if icmp {
		rules = append(rules, netbird.PolicyRule{Protocol: "icmp"})
	}
	return rules, nil
}

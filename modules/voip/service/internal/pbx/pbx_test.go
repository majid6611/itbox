package pbx

import (
	"strings"
	"testing"
)

var testExts = []Extension{
	{Number: "101", Name: "Ann", InMainGroup: true},
	{Number: "102", Name: "Bob"},
	{Number: "103", Name: "Cy"},
}

func dialplan(ivr IVR) string {
	return Render(testExts, Settings{}, ivr)["extensions.conf"]
}

func TestNoMenuRingsMainGroupDirectly(t *testing.T) {
	d := dialplan(IVR{})
	if strings.Contains(d, "[itp-ivr]") {
		t.Fatal("menu context written although none is set up")
	}
	if !strings.Contains(d, "[itp-main]\nexten => s,1,NoOp(main number)\n same => n,Goto(itp-ring,s,1)") {
		t.Fatalf("main number should ring the group directly:\n%s", d)
	}
}

func TestMenuNeedsGreetingAndKeys(t *testing.T) {
	opts := []IVROption{{Digit: "1", Label: "IT", Targets: []string{"102", "103"}}}
	for name, ivr := range map[string]IVR{
		"disabled":    {Enabled: false, HasGreeting: true, Options: opts},
		"no greeting": {Enabled: true, HasGreeting: false, Options: opts},
		"no keys":     {Enabled: true, HasGreeting: true},
		"only keys for deleted extensions": {Enabled: true, HasGreeting: true,
			Options: []IVROption{{Digit: "1", Label: "Gone", Targets: []string{"999"}}}},
	} {
		if strings.Contains(dialplan(ivr), "[itp-ivr]") {
			t.Errorf("%s: menu should not be offered", name)
		}
	}
}

func TestMenuOneLevel(t *testing.T) {
	d := dialplan(IVR{Enabled: true, HasGreeting: true, Options: []IVROption{
		{Digit: "1", Label: "IT support", Targets: []string{"102", "103"}},
		{Digit: "2", Label: "Accounting", Targets: []string{"101"}},
	}})
	if !strings.Contains(d, "exten => *50,1,Goto(itp-ivr,s,1)") {
		t.Errorf("staff need *50 to try the menu:\n%s", d)
	}
	for _, want := range []string{
		"[itp-main]\nexten => s,1,NoOp(main number)\n same => n,Goto(itp-ivr,s,1)",
		"[itp-ivr]",
		"Background(" + GreetingBase + ")",
		"exten => 1,1,NoOp(menu key 1)\n same => n,Dial(PJSIP/102&PJSIP/103,25)\n same => n,VoiceMail(0@default,u)", // a group falls back to the shared mailbox
		"exten => 2,1,NoOp(menu key 2)\n same => n,Dial(PJSIP/101,25)\n same => n,VoiceMail(101@default,u)",         // one person: their own mailbox
		"exten => 0,1,Goto(itp-ring,s,1)",
		"exten => t,1,Set(TRIES=",
		"exten => i,1,Playback(pbx-invalid)",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("missing %q in:\n%s", want, d)
		}
	}
	// One level: no key may lead into another menu.
	menu := d[strings.Index(d, "[itp-ivr]"):]
	if strings.Count(menu, "Goto(itp-ivr") != 0 || strings.Count(menu, "Background(") != 1 {
		t.Errorf("menu must not nest:\n%s", menu)
	}
}

func TestMenuDropsDeletedTargets(t *testing.T) {
	d := dialplan(IVR{Enabled: true, HasGreeting: true, Options: []IVROption{
		{Digit: "1", Label: "IT", Targets: []string{"102", "999"}},
	}})
	if !strings.Contains(d, "Dial(PJSIP/102,25)") || strings.Contains(d, "999") {
		t.Fatalf("a deleted extension must be left out of the dial string:\n%s", d)
	}
}

func TestStaffDialingZeroSkipsTheMenu(t *testing.T) {
	d := dialplan(IVR{Enabled: true, HasGreeting: true, Options: []IVROption{{Digit: "1", Label: "IT", Targets: []string{"102"}}}})
	internal := d[strings.Index(d, "[itp-internal]"):strings.Index(d, "[itp-main]")]
	if !strings.Contains(internal, "exten => 0,1,Goto(itp-ring,s,1)") {
		t.Fatalf("internal 0 should ring the main group, not play the menu:\n%s", internal)
	}
}

func TestRecordCodeOnlyForTheChosenExtension(t *testing.T) {
	if strings.Contains(dialplan(IVR{}), "*99") {
		t.Fatal("*99 must not exist when no recorder is chosen")
	}
	d := dialplan(IVR{Recorder: "101"})
	if !strings.Contains(d, `exten => *99,1,GotoIf($["${CHANNEL(endpoint)}" = "101"]?ok)`) {
		t.Fatalf("*99 must check the calling endpoint:\n%s", d)
	}
	if !strings.Contains(d, "Record("+GreetingBase+".wav,5,120,k)") {
		t.Fatalf("recording should go to the greeting file:\n%s", d)
	}
}

func TestPhonesAreHeldToTheLanACLButTheOutsideLineIsNot(t *testing.T) {
	exts := []Extension{{Number: "101", Name: "A", Password: "pw"}}
	st := Settings{TrunkEnabled: true, TrunkHost: "sip.example.com", TrunkUsername: "u", TrunkPassword: "p"}
	out := renderPJSIP(exts, st, true)
	if n := strings.Count(out, "acl = itp-lan"); n != 1 {
		t.Fatalf("want the source ACL on the one extension and nowhere else, got %d:\n%s", n, out)
	}
	if i := strings.Index(out, "[trunk]"); i < 0 || strings.Contains(out[i:], "acl = itp-lan") {
		t.Fatalf("the outside line calls in from a public address and must not get the LAN ACL:\n%s", out)
	}
}

package api

import "testing"

func TestPrefixQuery(t *testing.T) {
	cases := map[string]string{
		"onboard":       "onboard:*",
		"onboard guide": "onboard:* & guide:*",
		"  a'b | c&d  ": "ab:* & cd:*",
		"!!! ???":       "",
		"":              "",
		"راهنما شرکت":   "راهنما:* & شرکت:*",
		"x:* | y":       "x:* & y:*",
	}
	for in, want := range cases {
		if got := prefixQuery(in); got != want {
			t.Errorf("prefixQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanSnippet(t *testing.T) {
	got := cleanSnippet("a &amp; <<b>>\n  c")
	if got != "a & <<b>> c" {
		t.Errorf("got %q", got)
	}
}

func TestRuleAllows(t *testing.T) {
	if !ruleAllows(nil, "any", true) {
		t.Error("a page with no rules is open to everyone")
	}
	rules := []WikiPermissionRule{{Group: "hr", Access: "write"}, {Group: "dev", Access: "read"}}
	if !ruleAllows(rules, "hr", true) || !ruleAllows(rules, "hr", false) {
		t.Error("write grants write and read")
	}
	if !ruleAllows(rules, "dev", false) || ruleAllows(rules, "dev", true) {
		t.Error("read grants read only")
	}
	if ruleAllows(rules, "sales", false) || ruleAllows(rules, "", false) {
		t.Error("an unlisted group (or none) is shut out")
	}
}

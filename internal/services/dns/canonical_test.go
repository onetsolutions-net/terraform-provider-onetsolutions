package dns

import (
	"context"
	"testing"
)

func TestSameName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want bool
	}{
		{"@", "example.com", true},
		{"", "@", true},
		{"@", "Example.COM.", true},
		{"www", "www.example.com", true},
		{"WWW.example.com.", "www", true},
		{"a.b", "a.b.example.com", true},
		{"www", "web.example.com", false},
		{"@", "www.example.com", false},
		{"example.com.example.com", "example.com", false},
	}
	for _, tc := range cases {
		if got := sameName(tc.a, tc.b, "example.com"); got != tc.want {
			t.Errorf("sameName(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSameContent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		recordType, a, b string
		want             bool
	}{
		{"TXT", "v=spf1 -all", `"v=spf1 -all"`, true},
		{"TXT", "Hello", "hello", false},
		{"TXT", `"a" "b"`, `"a" "b"`, true},
		{"TXT", "v=DKIM1; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA", `"v=DKIM1; p=MIIBIjANBg" "kqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA"`, true},
		{"TXT", `"ab"`, `"a" "b"`, true},
		{"TXT", `"a"  "b"`, `"a""b"`, false},
		{"TXT", `say "hi"`, `"say \"hi\""`, true},
		{"TXT", `"a" b`, `"a" "b"`, false},
		{"CNAME", "Target.Example.net.", "target.example.net", true},
		{"MX", "mail.example.net", "mail2.example.net", false},
		{"SRV", "10 5060 SIP.example.net.", "10 5060 sip.example.net", true},
		{"AAAA", "2001:db8:0:0::1", "2001:db8::1", true},
		{"A", "203.0.113.1", "203.0.113.2", false},
		{"CAA", `0 issue "letsencrypt.org"`, `0 issue "letsencrypt.org"`, true},
	}
	for _, tc := range cases {
		if got := sameContent(tc.recordType, tc.a, tc.b); got != tc.want {
			t.Errorf("sameContent(%s, %q, %q) = %v, want %v", tc.recordType, tc.a, tc.b, got, tc.want)
		}
	}
	if importedContent("TXT", `"x y" "z"`) != "x yz" || importedContent("MX", "m.example.net") != "m.example.net" {
		t.Error("importedContent must unquote TXT values only")
	}
	if importedName("www.example.com", "example.com") != "www" || importedName("example.com", "example.com") != "@" {
		t.Error("importedName must return relative names and @")
	}
}

func TestRecordStringSemanticEquality(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	equal := func(a, b recordString) bool {
		ok, diags := a.StringSemanticEquals(ctx, b)
		if diags.HasError() {
			t.Fatal(diags)
		}
		return ok
	}
	if !equal(newName("WWW."), newName("www")) || equal(newName("www"), newName("web")) {
		t.Error("names compare ignoring case and the trailing dot")
	}
	if !equal(newContent(`"hello world"`), newContent("hello world")) || !equal(newContent("Mail.Example.net."), newContent("mail.example.net")) {
		t.Error("contents compare ignoring quotes and a host name's trailing dot and case")
	}
	if equal(newContent("Hello world"), newContent("hello world")) {
		t.Error("a text keeps its case")
	}
	if newName("a").Equal(newName("A")) || newName("a").Equal(newContent("a")) {
		t.Error("Equal must stay strict")
	}
}

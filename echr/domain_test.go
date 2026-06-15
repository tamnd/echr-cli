package echr

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network. The client's
// HTTP behaviour is covered in echr_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "echr" {
		t.Errorf("Scheme = %q, want echr", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "echr" {
		t.Errorf("Identity.Binary = %q, want echr", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct{ in, typ, id string }{
		{"001-248971", "case", "001-248971"},
		{"001-123456", "case", "001-123456"},
		{"CASE OF JONES v. UK", "case", "CASE OF JONES v. UK"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("case", "001-248971")
	want := `https://hudoc.echr.coe.int/eng#{"itemid":["001-248971"]}`
	if err != nil || got != want {
		t.Errorf("Locate = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("judgment", "001-248971")
	if err == nil {
		t.Error("expected error for unknown resource type")
	}
}

// TestHostWiring mounts the driver in a kit Host and checks the round trip:
// a record mints to its URI and a bare id resolves back to the same URI.
func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	c := &Case{
		ID:      "001-248971",
		DocName: "CASE OF JONES v. UNITED KINGDOM",
	}
	u, err := h.Mint(c)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := "echr://case/001-248971"; u.String() != want {
		t.Errorf("Mint = %q, want %q", u.String(), want)
	}

	got, err := h.ResolveOn("echr", "001-111111")
	if err != nil || got.String() != "echr://case/001-111111" {
		t.Errorf("ResolveOn = (%q, %v), want echr://case/001-111111", got.String(), err)
	}
}

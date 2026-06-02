package cmd

import "testing"

func TestDeriveBundleName(t *testing.T) {
	cases := []struct {
		name      string
		prefix    string
		namespace string
		want      string
	}{
		{"namespace only", "", "obra-fk-lite", "obra-fk-lite"},
		{"prefix wins over namespace", "supa", "some-repo", "supa"},
		{"lowercase and trim", "", "  MyPack  ", "mypack"},
		{"spaces to dashes", "", "cool skills pack", "cool-skills-pack"},
		{"reserved inbox is rejected", "", "inbox", ""},
		{"empty stays empty", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := deriveBundleName(c.prefix, c.namespace); got != c.want {
				t.Errorf("deriveBundleName(%q, %q) = %q, want %q", c.prefix, c.namespace, got, c.want)
			}
		})
	}
}

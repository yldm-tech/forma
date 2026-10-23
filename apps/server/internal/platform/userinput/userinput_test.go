package userinput

import "testing"

func TestValidEmailFormat(t *testing.T) {
	cases := map[string]bool{
		"ada@example.com":            true,
		"ada.lovelace+x@ex-ample.co": true,
		"o'brien@example.ie":         true,
		"ada@example":                false,
		".ada@example.com":           false,
		"ada..x@example.com":         false,
		"ada.@example.com":           false,
		"ada@example.com\n":          false,
		"ada@-example.com":           false,
		"not-an-email":               false,
	}
	for email, want := range cases {
		if got := ValidEmailFormat(email); got != want {
			t.Errorf("ValidEmailFormat(%q) = %v, want %v", email, got, want)
		}
	}
}

func TestValidNameFormat(t *testing.T) {
	cases := map[string]bool{
		"Ada Lovelace":      true,
		"Zoë O'Brien-Smith": true,
		"山田 太郎":             true,
		"Acme & Co. (2)":    true,
		"<script>":          false,
		"ada@example.com":   false,
		"Ada_Lovelace":      false,
	}
	for name, want := range cases {
		if got := ValidNameFormat(name); got != want {
			t.Errorf("ValidNameFormat(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestUTF16Len(t *testing.T) {
	if n := UTF16Len("a😀"); n != 3 {
		t.Fatalf("UTF16Len = %d, want 3: an astral character is two code units", n)
	}
}

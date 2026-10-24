package main

import "testing"

func TestSeedFlags(t *testing.T) {
	f, err := parseSeedFlags([]string{"--clear"})
	if err != nil || !f.clear {
		t.Fatalf("parseSeedFlags = %+v, %v", f, err)
	}
	if f, err := parseSeedFlags(nil); err != nil || f.clear {
		t.Fatalf("defaults = %+v, %v", f, err)
	}
	for _, args := range [][]string{{"--nope"}, {"extra"}, {"--contract"}} {
		if _, err := parseSeedFlags(args); err == nil {
			t.Errorf("parseSeedFlags(%q) accepted", args)
		}
	}
}

func TestSeedNeedsAllowSeed(t *testing.T) {
	cases := map[string]struct {
		allowed bool
		fails   bool
	}{"1": {allowed: true}, "0": {}, "": {}, "true": {fails: true}}
	for v, want := range cases {
		allowed, err := seedAllowed(v)
		if allowed != want.allowed || (err != nil) != want.fails {
			t.Errorf("seedAllowed(%q) = %v, %v", v, allowed, err)
		}
	}

	// Refused before any database is opened.
	t.Setenv("ALLOW_SEED", "0")
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none")
	if code := run([]string{"seed"}); code != 1 {
		t.Errorf("run(seed) without ALLOW_SEED = %d, want 1", code)
	}
}

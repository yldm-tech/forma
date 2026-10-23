package config

import "testing"

func TestLoadUnsplash(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Unsplash.Configured() {
		t.Error("Unsplash is configured without UNSPLASH_ACCESS_KEY")
	}
	t.Setenv("UNSPLASH_ACCESS_KEY", "access-key")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if !cfg.Unsplash.Configured() || cfg.Unsplash.AccessKey != "access-key" {
		t.Errorf("Unsplash = %+v, want the key loaded", cfg.Unsplash)
	}
}

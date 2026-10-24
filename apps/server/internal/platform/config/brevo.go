package config

import (
	"fmt"
	"strconv"
)

// BrevoConfig is forma2's Brevo contact sync, Forma Cloud's CRM: a user becomes a Brevo contact when they verify their address, the contact follows an email change and is removed with the account. Without BREVO_API_KEY nothing is synced.
type BrevoConfig struct {
	APIKey Secret `env:"BREVO_API_KEY"`
	// ListID is the Brevo list new contacts are added to, forma2's BREVO_LIST_ID; unset adds them to none.
	ListID string `env:"BREVO_LIST_ID"`
}

// Configured reports whether contacts are synced.
func (b BrevoConfig) Configured() bool {
	return b.APIKey != ""
}

// List is the list id contacts join, or 0 for none.
func (b BrevoConfig) List() int64 {
	id, _ := strconv.ParseInt(b.ListID, 10, 64)
	return id
}

// validate refuses a list id that is not a positive number. forma2 parsed it leniently and silently added contacts to no list when it could not; a list id that cannot be used is a configuration mistake to see at startup.
func (b BrevoConfig) validate() error {
	if b.ListID == "" {
		return nil
	}
	if id, err := strconv.ParseInt(b.ListID, 10, 64); err != nil || id < 1 {
		return fmt.Errorf("BREVO_LIST_ID must be a positive number, got %q", b.ListID)
	}
	return nil
}

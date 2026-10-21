package auth

import (
	_ "embed"
	"strings"
	"sync"
)

// personalEmailDomains is forma2's curated list of personal and free mail providers (apps/web/modules/auth/lib/personal-email-domains.ts). Forma Cloud refuses them at sign-up so that every organization is tied to a company domain.
var personalEmailDomains = map[string]bool{
	// Google
	"gmail.com": true, "googlemail.com": true,
	// Microsoft
	"outlook.com": true, "hotmail.com": true, "hotmail.fr": true, "live.com": true, "live.nl": true, "msn.com": true,
	// Yahoo / AOL
	"yahoo.com": true, "yahoo.co.uk": true, "yahoo.fr": true, "ymail.com": true, "rocketmail.com": true, "aol.com": true,
	// Apple
	"icloud.com": true, "me.com": true, "mac.com": true, "privaterelay.appleid.com": true,
	// Privacy-focused and relay services
	"proton.me": true, "protonmail.com": true, "pm.me": true, "passmail.com": true, "mailbox.org": true, "posteo.de": true,
	"fastmail.com": true, "mozmail.com": true, "duck.com": true, "anonaddy.com": true, "8alias.com": true, "keemail.me": true,
	"ik.me": true, "murena.io": true, "firemail.cc": true,
	// Burner providers
	"cock.li": true, "tmpmailtor.com": true, "allwebemails.com": true,
	// Other mainstream and international providers
	"gmx.com": true, "gmx.net": true, "yandex.com": true, "yandex.ru": true, "zoho.com": true, "zohomail.com": true, "mail.com": true,
	"null.net": true, "qq.com": true, "foxmail.com": true, "163.com": true, "126.com": true, "139.com": true, "yeah.net": true,
	"emailn.de": true, "sfr.fr": true, "orange.fr": true, "ukr.net": true, "centrum.cz": true, "wp.pl": true, "earthlink.net": true,
	"roadrunner.com": true,
	// Lookalikes of gmail.com
	"gmaill.com": true, "gmail.cz": true,
}

//go:embed disposable_email_domains.txt
var disposableEmailDomainList string

// disposableEmailDomains is the vendored disposable-email-domains list (about 121,000 throwaway-mail domains), which forma2 blocked together with personalEmailDomains. The set is built on first use, so processes that never check a sign-up do not pay for it.
var disposableEmailDomains = sync.OnceValue(func() map[string]bool {
	set := make(map[string]bool, 125_000)
	for _, line := range strings.Split(disposableEmailDomainList, "\n") {
		if line = strings.ToLower(strings.TrimSpace(line)); line != "" && !strings.HasPrefix(line, "#") {
			set[line] = true
		}
	}
	return set
})

// isBlockedEmailDomain reports whether an address belongs to a personal or disposable mail provider. Malformed input (no "@", an empty local part or domain) is not blocked, and trailing dots are ignored so "gmail.com." cannot slip past.
func isBlockedEmailDomain(email string) bool {
	normalized := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndexByte(normalized, '@')
	if at <= 0 || at == len(normalized)-1 {
		return false
	}
	domain := strings.TrimRight(normalized[at+1:], ".")
	return personalEmailDomains[domain] || disposableEmailDomains()[domain]
}

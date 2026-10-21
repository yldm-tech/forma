package auth

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
)

// Input rules from forma2's packages/types/user.ts (ZUserName, ZUserEmail, ZUserPassword, ZUserLocale). Lengths are counted in UTF-16 code units, as zod counts them.
const (
	passwordMinLength = 8
	passwordMaxLength = 128
	emailMaxLength    = 255
)

// nameFormat is ZUserName's allow-list: letters, combining marks, space, and ' , . & ( ) digits -. JavaScript's \d without the u flag is ASCII only.
var nameFormat = regexp.MustCompile(`^[\p{L}\p{M} ',.&()0-9-]+$`)

// nameDisallowed is every run of characters ZUserName does not allow.
var nameDisallowed = regexp.MustCompile(`[^\p{L}\p{M} ',.&()0-9-]+`)

// normalizeUserName is forma2's normalizeUserName for untrusted display names, such as an identity provider's: disallowed characters become one space, whitespace collapses, and the result is trimmed, so whatever is left satisfies ZUserName. It returns "" when nothing survives.
func normalizeUserName(name string) string {
	return strings.Join(strings.Fields(nameDisallowed.ReplaceAllString(name, " ")), " ")
}

// emailFormat is zod 4's email pattern without its two lookaheads, which RE2 lacks; validEmail checks those separately.
var emailFormat = regexp.MustCompile(`^[A-Za-z0-9_'+\-.]*[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$`)

// userLocales is ZUserLocale.
var userLocales = []string{
	"de-DE", "en-US", "es-ES", "fr-FR", "hu-HU", "ja-JP", "nl-NL", "pt-BR", "pt-PT", "ro-RO", "ru-RU", "sv-SE", "tr-TR", "zh-Hans-CN", "zh-Hant-TW",
}

// fieldErrors collects failed constraints for one request.
type fieldErrors []apperr.FieldError

func (f *fieldErrors) add(field, rule, param string) {
	*f = append(*f, apperr.FieldError{Field: field, Rule: rule, Param: param})
}

// err returns the validation error, or nil when every field passed.
func (f fieldErrors) err() error {
	if len(f) == 0 {
		return nil
	}
	return apperr.Validation(f)
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// checkName validates a display name and returns it trimmed, as ZUserName's trim transform does.
func checkName(errs *fieldErrors, field, name string) string {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		errs.add(field, "min", "1")
	case !nameFormat.MatchString(name):
		errs.add(field, "name_format", "")
	}
	return name
}

func validEmail(email string) bool {
	return !strings.HasPrefix(email, ".") && !strings.Contains(email, "..") && emailFormat.MatchString(email)
}

// checkEmail validates the request's "email" field as ZUserEmail does. The address is not trimmed: zod rejects surrounding whitespace rather than removing it.
func checkEmail(errs *fieldErrors, email string) {
	switch {
	case email == "":
		errs.add("email", "required", "")
	case utf16Len(email) > emailMaxLength:
		errs.add("email", "max", strconv.Itoa(emailMaxLength))
	case !validEmail(email):
		errs.add("email", "email", "")
	}
}

// checkPassword applies ZUserPassword: 8 to 128 characters with at least one uppercase letter and one digit. Its pattern, /^(?=.*[A-Z])(?=.*\d).*$/, has no multiline flag, and "." stops at line terminators, so a password containing one never matches.
func checkPassword(errs *fieldErrors, field, password string) {
	n := utf16Len(password)
	switch {
	case n < passwordMinLength:
		errs.add(field, "min", strconv.Itoa(passwordMinLength))
	case n > passwordMaxLength:
		errs.add(field, "max", strconv.Itoa(passwordMaxLength))
	case !passwordComposed(password):
		errs.add(field, "password_composition", "")
	}
}

func passwordComposed(password string) bool {
	if strings.ContainsAny(password, "\n\r  ") {
		return false
	}
	return strings.ContainsAny(password, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") && strings.ContainsAny(password, "0123456789")
}

// checkLocale accepts an empty locale (the field is optional) or one of ZUserLocale.
func checkLocale(errs *fieldErrors, field, locale string) {
	if locale != "" && !slices.Contains(userLocales, locale) {
		errs.add(field, "oneof", strings.Join(userLocales, " "))
	}
}

// normalizeEmail is how every address is stored and compared: forma2 lowercased at sign-up and email change, and the users table is unique on lower(email).
func normalizeEmail(email string) string {
	return strings.ToLower(email)
}

// emailDomain is the part of an address that is safe to log.
func emailDomain(email string) string {
	if i := strings.LastIndexByte(email, '@'); i >= 0 {
		return email[i+1:]
	}
	return ""
}

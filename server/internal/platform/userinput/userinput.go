// Package userinput holds the rules of forma2's shared user fields (packages/types/user.ts: ZUserName and ZUserEmail) for the modules that accept a person's name or address: sign-up, account changes and invites. Lengths are counted in UTF-16 code units, as zod counts them.
package userinput

import (
	"regexp"
	"strings"
	"unicode/utf16"
)

// EmailMaxLength is ZUserEmail's maximum length.
const EmailMaxLength = 255

// nameFormat is ZUserName's allow-list: letters, combining marks, space, and ' , . & ( ) digits -. JavaScript's \d without the u flag is ASCII only.
var nameFormat = regexp.MustCompile(`^[\p{L}\p{M} ',.&()0-9-]+$`)

// emailFormat is zod 4's email pattern without its two lookaheads, which RE2 lacks; ValidEmailFormat checks those separately.
var emailFormat = regexp.MustCompile(`^[A-Za-z0-9_'+\-.]*[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$`)

// UTF16Len is a string's length as JavaScript counts it.
func UTF16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// ValidNameFormat reports whether an already trimmed, non-empty name uses only the characters ZUserName allows.
func ValidNameFormat(name string) bool {
	return nameFormat.MatchString(name)
}

// ValidEmailFormat is zod 4's z.email(): no leading dot, no consecutive dots, and the pattern above. It does not trim; zod rejects surrounding whitespace rather than removing it.
func ValidEmailFormat(email string) bool {
	return !strings.HasPrefix(email, ".") && !strings.Contains(email, "..") && emailFormat.MatchString(email)
}

package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf16"
)

// MaxEncryptionKeyVersion is the highest key version; sealed values carry it in one byte.
const MaxEncryptionKeyVersion = 255

// EncryptionKey is a 32-byte AES-256 key. It never prints or logs its value.
type EncryptionKey []byte

// UnmarshalText reads forma2's ENCRYPTION_KEY format (apps/web/lib/env.ts and lib/crypto.ts): exactly 32 characters are the key's bytes in Latin-1, anything else must be 64 hex characters. forma2 measured the length in UTF-16 code units and Node's Latin-1 encoding keeps the low byte of each, so a key written for forma2 decodes to the same bytes here.
func (k *EncryptionKey) UnmarshalText(text []byte) error {
	key, err := decodeEncryptionKey(string(text))
	if err != nil {
		return err
	}
	*k = key
	return nil
}

func decodeEncryptionKey(s string) ([]byte, error) {
	if units := utf16.Encode([]rune(s)); len(units) == 32 {
		key := make([]byte, len(units))
		for i, u := range units {
			key[i] = byte(u) //nolint:gosec // keeping the low byte is the point: it is what Node's Latin-1 encoding does
		}
		return key, nil
	}
	if len(s) != 64 {
		return nil, errors.New("must be 64 hex characters (openssl rand -hex 32) or exactly 32 characters")
	}
	key, err := hex.DecodeString(s)
	if err != nil {
		return nil, errors.New("must be 64 hex characters (openssl rand -hex 32) or exactly 32 characters")
	}
	return key, nil
}

func (k EncryptionKey) String() string { return "[redacted]" }

func (k EncryptionKey) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// EncryptionKeyring is a set of retired keys by version.
type EncryptionKeyring map[int]EncryptionKey

// UnmarshalText reads "<version>:<key>" entries separated by commas, the shape of Better Auth's BETTER_AUTH_SECRETS.
func (r *EncryptionKeyring) UnmarshalText(text []byte) error {
	ring := EncryptionKeyring{}
	for entry := range strings.SplitSeq(string(text), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		v, key, ok := strings.Cut(entry, ":")
		if !ok {
			return errors.New(`entries must be "<version>:<key>"`)
		}
		version, err := strconv.Atoi(v)
		if err != nil || version < 1 || version > MaxEncryptionKeyVersion {
			return fmt.Errorf("key version must be between 1 and %d, got %q", MaxEncryptionKeyVersion, v)
		}
		if _, dup := ring[version]; dup {
			return fmt.Errorf("key version %d appears twice", version)
		}
		decoded, err := decodeEncryptionKey(key)
		if err != nil {
			return fmt.Errorf("key version %d %w", version, err)
		}
		ring[version] = decoded
	}
	*r = ring
	return nil
}

func (r EncryptionKeyring) String() string { return fmt.Sprintf("[%d redacted keys]", len(r)) }

func (r EncryptionKeyring) LogValue() slog.Value { return slog.StringValue(r.String()) }

// Package secret holds the primitives for credentials the server issues or checks: opaque tokens stored only as hashes, and bcrypt for passwords.
package secret

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// PasswordCost matches forma2 (bcryptjs cost 12), so its hashes verify unchanged and new ones cost the same.
const PasswordCost = 12

// NewToken returns a URL-safe random token carrying 256 bits of entropy.
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashToken is how an issued token is stored and looked up. Tokens are high-entropy, so a fast hash is enough; a leaked table cannot be replayed.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// bcryptMaxBytes is the most of a password bcrypt reads. The password policy allows 128 characters, so a longer password is cut here, the way forma2's bcryptjs did; CheckPassword reads the same prefix, so the full password still verifies.
const bcryptMaxBytes = 72

// HashPassword bcrypts a password.
func HashPassword(password string) (string, error) {
	b := []byte(password)
	if len(b) > bcryptMaxBytes {
		b = b[:bcryptMaxBytes]
	}
	h, err := bcrypt.GenerateFromPassword(b, PasswordCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

// dummyHash is compared against when no account exists, so a sign-in for an unknown email costs the same as a wrong password and response timing does not reveal which addresses are registered.
var dummyHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("forma-timing-equaliser"), PasswordCost)
	if err != nil {
		panic(err)
	}
	return h
}()

// CheckPassword reports whether password matches hash. An empty hash (no account, or no password set) still spends one bcrypt comparison and returns false.
func CheckPassword(hash, password string) (bool, error) {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check password: %w", err)
	}
	return true, nil
}

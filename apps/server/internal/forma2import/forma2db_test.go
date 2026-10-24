package forma2import_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"text/template"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/yldm-tech/forma/apps/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// The secrets forma2 ran with in these tests: a 32-character ENCRYPTION_KEY (forma2 took it as Latin-1 bytes) and the NEXTAUTH_SECRET Better Auth fell back to.
const (
	encryptionKey  = "0123456789abcdefghijklmnopqrstuv"
	nextAuthSecret = "forma2-better-auth-secret-for-tests-0123456789"

	ownerPassword     = "Owner-Password-1"
	twoFactorPassword = "Twofactor-Password-1"
	legacyPassword    = "Legacy-Password-1"
	modernKeySecret   = "c2VjcmV0LWtleS1mb3ItdGhlLWltcG9ydC10ZXN0cw"
	legacyKey         = "4f3c2b1a0d9e8f7a6b5c4d3e2f1a0b9c"
	notionToken       = "ntn_imported_access_token"
	sheetsAccess      = "ya29.cleartext-access-token"
	sheetsRefresh     = "1//refresh-token-under-cbc"
)

// betterAuthSecretHex and betterAuthTOTPKey are a TwoFactor.secret as better-auth 1.7.0's symmetricEncrypt wrote it under nextAuthSecret, and the key it opens to (the vector of internal/platform/secret/legacy_test.go).
const (
	betterAuthSecretHex = "35f9fee0a496c7e61d9743f66d878bf7d470eef781f566dfb1cb0f4fae03ccb7fce6282c3e4c1efb238c24682997956db22d5975ebe4f858fdea1a9512c88aa6d204595a7672f1f3"
	betterAuthTOTPKey   = "OfBqWQvwnfdc8grnNxXfX4rYQ76-cBb-"
)

// legacySecretGCM and legacyCodesGCM are forma2's legacy User.twoFactorSecret and User.backupCodes as lib/crypto.ts wrote them under encryptionKey: RFC 6238's key in base32, and ["0123456789", null, "abcdef0123"] (the vectors of internal/auth/totp_test.go).
const (
	legacySecretGCM = "3585b5280990daff2823b852df5e8756:5a92c9ef5d095bc285470346f7a5a28eb4533b4bd314f8bc64c3baf96f442b59:b5831f915a49742752c2ae0df2bd1cb4"
	legacyCodesGCM  = "7ebae65205b8d0090a96fc7f37a2e238:536ccb249682420c0864783824b8816ac50632196297b34e25f8e63c8514c7b6:26a0c62d95cf5b70cf5d4fd84de65e51"
)

// betterAuthEncrypt is Better Auth's symmetricEncrypt: XChaCha20-Poly1305 under SHA-256 of the secret, hex of nonce, ciphertext and tag.
func betterAuthEncrypt(t *testing.T, plaintext string) string {
	t.Helper()
	key := sha256.Sum256([]byte(nextAuthSecret))
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	_, _ = rand.Read(nonce)
	return hex.EncodeToString(aead.Seal(nonce, nonce, []byte(plaintext), nil))
}

// forma2CBCEncrypt is forma2's pre-GCM symmetricEncrypt: AES-256-CBC with PKCS#7 padding, hex "iv:ciphertext".
func forma2CBCEncrypt(t *testing.T, plaintext string) string {
	t.Helper()
	block, err := aes.NewCipher([]byte(encryptionKey))
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := append([]byte(plaintext), bytes.Repeat([]byte{byte(pad)}, pad)...)
	iv := make([]byte, aes.BlockSize)
	_, _ = rand.Read(iv)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return hex.EncodeToString(iv) + ":" + hex.EncodeToString(out)
}

// bcryptjs is a password hash as forma2's bcryptjs wrote it, version 2b. forma2 used cost 12; the cost is part of the hash, so the minimum keeps the test fast without changing what is checked.
func bcryptjs(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return "$2b$" + strings.TrimPrefix(string(h), "$2a$")
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// forma2DB is a database with this release's schema (pgtest) and, next to it, forma2's schema built by applying forma2's Prisma migrations from 3db326f in order, the first `migrations` of them, filled with the seed. It returns the database's URL.
func forma2DB(t *testing.T, migrations int) string {
	t.Helper()
	ctx := context.Background()
	dsn := pgtest.DSN(t)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()

	dirs, err := os.ReadDir("testdata/forma2-migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range dirs {
		names = append(names, d.Name())
	}
	slices.Sort(names)
	names = names[:migrations]
	exec(t, conn, `CREATE TABLE _prisma_migrations (id varchar(36) PRIMARY KEY, checksum varchar(64) NOT NULL, finished_at timestamptz, migration_name varchar(255) NOT NULL,
		logs text, rolled_back_at timestamptz, started_at timestamptz NOT NULL DEFAULT now(), applied_steps_count integer NOT NULL DEFAULT 0)`)
	for i, name := range names {
		raw, err := os.ReadFile(filepath.Join("testdata/forma2-migrations", name, "migration.sql"))
		if err != nil {
			t.Fatal(err)
		}
		// The test PostgreSQL has no pgvector; forma2 created the extension but no column uses it.
		sql := strings.Replace(string(raw), `CREATE EXTENSION IF NOT EXISTS "vector";`, "", 1)
		exec(t, conn, sql)
		sum := sha256.Sum256(raw)
		exec(t, conn, `INSERT INTO _prisma_migrations (id, checksum, finished_at, migration_name, applied_steps_count) VALUES ($1, $2, now(), $3, 1)`,
			"00000000-0000-4000-8000-00000000000"+string(rune('0'+i)), hex.EncodeToString(sum[:]), name)
	}

	tmpl, err := template.ParseFiles("testdata/seed.sql.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	notion := mustJSON(t, map[string]any{
		"key":  map[string]any{"access_token": mustEncrypt(t, notionToken), "bot_id": "bot-1", "workspace_name": "Acme Notion", "token_type": "bearer"},
		"data": []any{map[string]any{"surveyId": "clsurvey000000000000000001", "surveyName": "Customer feedback", "databaseId": "db-1", "databaseName": "Feedback", "mapping": []any{}, "createdAt": "2026-09-20T10:00:00.000Z"}},
	})
	sheets := mustJSON(t, map[string]any{
		"key":   map[string]any{"access_token": sheetsAccess, "refresh_token": forma2CBCEncrypt(t, sheetsRefresh), "token_type": "Bearer", "scope": "https://www.googleapis.com/auth/spreadsheets", "expiry_date": 1760000000000},
		"email": "sheets@example.com",
		"data":  []any{},
	})
	// A token encrypted under another key than ENCRYPTION_KEY: forma2 left such a value as stored, and so does the import.
	otherKey, _ := secret.EncryptForma2("xoxb-other-key", []byte("ffffffffffffffffffffffffffffffff"))
	slack := mustJSON(t, map[string]any{"key": map[string]any{"access_token": otherKey, "token_type": "bot", "app_id": "A1", "bot_user_id": "U1", "authed_user": map[string]any{"id": "U2"}, "team": map[string]any{"id": "T1", "name": "Acme"}}, "data": []any{}})
	var seed strings.Builder
	if err := tmpl.Execute(&seed, map[string]string{
		"OwnerPassword": bcryptjs(t, ownerPassword), "TwoFactorPassword": bcryptjs(t, twoFactorPassword), "LegacyPassword": bcryptjs(t, legacyPassword),
		"TwoFactorSecret": betterAuthSecretHex, "TwoFactorCodes": betterAuthEncrypt(t, `["AbCdE-12345","zz9Yx-QwErT"]`),
		"LegacySecret": legacySecretGCM, "LegacyCodes": legacyCodesGCM,
		"ModernKeyHash": bcryptjs(t, modernKeySecret), "ModernKeyLookup": sha256Hex(modernKeySecret), "LegacyKeyHash": sha256Hex(legacyKey),
		"NotionConfig": notion, "SheetsConfig": sheets, "SlackConfig": slack,
	}); err != nil {
		t.Fatal(err)
	}
	exec(t, conn, seed.String())
	if migrations == len(dirs) {
		// The columns and table forma2's later migrations added.
		exec(t, conn, `UPDATE "Organization" SET "workflowRunRetentionDays" = 90, "displayRetentionDays" = 30;
			UPDATE "Integration" SET "lastErrorAt" = '2026-10-01 00:00:00', "lastErrorMessage" = 'Sheet not found', "consecutiveFailures" = 3 WHERE id = 'clintsheets0000000000000001';
			UPDATE "Workflow" SET "triggerSurveyId" = 'clsurvey000000000000000001';
			INSERT INTO "ResponsePipelineOutbox" (id, updated_at, "jobId", event, "responseId", "surveyId", "workspaceId", payload)
			VALUES ('cloutbox00000000000000001', now(), 'job-1', 'responseFinished', 'clresponse000000000000000001', 'clsurvey000000000000000001', 'clws000000000000000000001', '{}')`)
	}
	return dsn
}

func mustEncrypt(t *testing.T, plaintext string) string {
	t.Helper()
	v, err := secret.EncryptForma2(plaintext, []byte(encryptionKey))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func exec(t *testing.T, conn *pgx.Conn, sql string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec: %v\n%s", err, sql)
	}
}

// forma2Checksums fingerprints every forma2 table's rows, so a test can prove the import left them as they were.
func forma2Checksums(t *testing.T, dsn string) map[string]string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	rows, err := conn.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND (table_name ~ '[A-Z]' OR table_name IN ('jwks', '_prisma_migrations'))`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, table := range tables {
		var sum string
		if err := conn.QueryRow(ctx, `SELECT count(*) || ':' || coalesce(md5(string_agg(t::text, ',' ORDER BY t::text)), '') FROM `+pgx.Identifier{table}.Sanitize()+` t`).Scan(&sum); err != nil {
			t.Fatal(err)
		}
		out[table] = sum
	}
	if len(out) != 53 {
		t.Fatalf("found %d forma2 tables", len(out))
	}
	return out
}

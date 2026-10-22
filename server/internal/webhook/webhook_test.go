package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/ssrf"
)

// The signature is forma2's generateStandardWebhookSignature: v1, and the base64 HMAC-SHA256 of id.timestamp.body under the decoded secret; a Standard Webhooks receiver verifies it.
func TestSign(t *testing.T) {
	t.Parallel()
	secret := GenerateSecret()
	if !strings.HasPrefix(secret, "whsec_") || !validSecret(secret) {
		t.Fatalf("secret = %q", secret)
	}
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if len(key) != 32 {
		t.Fatalf("key is %d bytes", len(key))
	}
	ts := time.Unix(1700000000, 0)
	body := []byte(`{"event":"testEndpoint"}`)
	sig, err := Sign(secret, "msg_1", ts, body)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("msg_1.1700000000." + string(body)))
	if want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil)); sig != want {
		t.Fatalf("signature = %s, want %s", sig, want)
	}
	wh, _ := standardwebhooks.NewWebhook(secret)
	h := http.Header{"Webhook-Id": {"msg_1"}, "Webhook-Timestamp": {"1700000000"}, "Webhook-Signature": {sig}}
	if err := wh.VerifyIgnoringTimestamp(body, h); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// The prefix is optional, as in forma2.
	if bare, _ := Sign(strings.TrimPrefix(secret, "whsec_"), "msg_1", ts, body); bare != sig {
		t.Error("a secret without its prefix signs differently")
	}
}

// A secret stored before secrets were checked is decoded as Node's Buffer.from(s, "base64") decoded it.
func TestSignWithALegacySecret(t *testing.T) {
	t.Parallel()
	ts := time.Unix(1700000000, 0)
	for secret, key := range map[string][]byte{
		"not base64!":    {0x9e, 0x8b, 0x5b, 0x6a, 0xc7, 0xba},
		"whsec_abc-_d==": {0x69, 0xb7, 0x3e, 0xfd},
		"YWJj==extra":    []byte("abc"),
	} {
		sig, err := Sign(secret, "id", ts, []byte("b"))
		if err != nil {
			t.Fatalf("Sign(%q): %v", secret, err)
		}
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte("id.1700000000.b"))
		if want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil)); sig != want {
			t.Errorf("Sign(%q) = %s, want %s", secret, sig, want)
		}
	}
	if validSecret("whsec_short") || validSecret("plain") {
		t.Error("an unverifiable secret passed")
	}
}

func TestMessageIDIsStableAcrossAttempts(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("0192f5c8-0000-7000-8000-000000000001")
	a := MessageID("response-pipeline-responseFinished-r1-1700000000000", id, "responseFinished")
	if a != MessageID("response-pipeline-responseFinished-r1-1700000000000", id, "responseFinished") || len(a) != 64 {
		t.Fatalf("message id %q", a)
	}
	sum := sha256.Sum256([]byte("response-pipeline-responseFinished-r1-1700000000000:" + id.String() + ":responseFinished"))
	if a != hex.EncodeToString(sum[:]) {
		t.Error("message id is not sha256(jobKey:webhookId:event)")
	}
	if a == MessageID("response-pipeline-responseFinished-r1-1700000000001", id, "responseFinished") {
		t.Error("a later write must be a new message")
	}
}

func TestIsDiscord(t *testing.T) {
	t.Parallel()
	if !IsDiscord("https://discord.com/api/webhooks/123/abc") || IsDiscord("https://discord.com/api/webhooks/abc/def") || IsDiscord("http://discord.com/api/webhooks/1/x") {
		t.Fatal("Discord detection differs from forma2's")
	}
}

// forma2's createWebhookDeliveryTask: the body, the lower-case Standard Webhooks headers, a signature that verifies, and failures for anything outside 2xx.
func TestDeliver(t *testing.T) {
	t.Parallel()
	guard := ssrf.New(config.Config{DangerouslyAllowWebhookInternalURLs: true})
	d := NewDeliverer(nil, guard)
	d.now = func() time.Time { return time.Unix(1700000000, 0) }
	secret := GenerateSecret()
	rec := newReceiver(t, http.StatusAccepted)
	w := &Webhook{ID: uuid.New(), URL: rec.URL + "/hook?token=abc", Secret: &secret}
	data := []byte(`{"id":"r1","data":{"q":"<b>&</b>"}}`)
	if err := d.Deliver(context.Background(), w, "responseCreated", "job-1", data); err != nil {
		t.Fatal(err)
	}
	got := rec.received()
	if len(got) != 1 {
		t.Fatalf("%d requests", len(got))
	}
	wantBody := `{"webhookId":"` + w.ID.String() + `","event":"responseCreated","data":{"id":"r1","data":{"q":"<b>&</b>"}}}`
	if string(got[0].Body) != wantBody {
		t.Fatalf("body = %s\nwant %s", got[0].Body, wantBody)
	}
	h := got[0].Header
	if h.Get("Content-Type") != "application/json" || h.Get("Webhook-Id") != MessageID("job-1", w.ID, "responseCreated") || h.Get("Webhook-Timestamp") != strconv.Itoa(1700000000) {
		t.Fatalf("headers = %v", h)
	}
	wh, _ := standardwebhooks.NewWebhook(secret)
	if err := wh.VerifyIgnoringTimestamp(got[0].Body, h); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}

	unsigned := &Webhook{ID: uuid.New(), URL: rec.URL}
	if err := d.Deliver(context.Background(), unsigned, "responseCreated", "job-1", data); err != nil {
		t.Fatal(err)
	}
	if got := rec.received(); got[1].Header.Get("Webhook-Signature") != "" {
		t.Error("a webhook without a secret was signed")
	}

	failing := newReceiver(t, http.StatusBadRequest)
	err := d.Deliver(context.Background(), &Webhook{ID: uuid.New(), URL: failing.URL + "/x?token=secret-token"}, "responseCreated", "job-1", data)
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("a 400 must fail the delivery: %v", err)
	}
	strict := NewDeliverer(nil, ssrf.New(config.Config{}))
	err = strict.Deliver(context.Background(), &Webhook{ID: uuid.New(), URL: rec.URL}, "responseCreated", "job-1", data)
	if err == nil || !strings.Contains(err.Error(), ssrf.MessagePrivateAddress) {
		t.Fatalf("a private URL must be refused at delivery: %v", err)
	}
	if strings.Contains(err.Error(), "token") {
		t.Errorf("error leaks the URL: %v", err)
	}
}

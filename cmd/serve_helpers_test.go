package cmd

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"event":"ping"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !verifyWebhookSignature(body, good, secret) {
		t.Error("valid signature should verify")
	}
	if verifyWebhookSignature(body, good, "wrong-secret") {
		t.Error("wrong secret should fail")
	}
	if verifyWebhookSignature(body, "sha256=deadbeef", secret) {
		t.Error("mismatched signature should fail")
	}
	if verifyWebhookSignature(body, "", secret) {
		t.Error("empty signature should fail")
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, map[string]int{"count": 42})
	body := rec.Body.String()
	var got map[string]int
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%q", err, body)
	}
	if got["count"] != 42 {
		t.Errorf("want count=42, got %d", got["count"])
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 418, "teapot")
	if rec.Code != 418 {
		t.Errorf("want status 418, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "teapot") {
		t.Errorf("body missing message: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "error") {
		t.Errorf("body missing error field: %q", rec.Body.String())
	}
}

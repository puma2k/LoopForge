package repo

import (
	"crypto/hmac"
	"crypto/sha256"
	"testing"
	"time"

	"agentflow/internal/domain"
)

func TestWebhookChannelSecretRotateAndHMAC(t *testing.T) {
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.PutUser(domain.User{LoginName: "wh-u"}); err != nil {
		t.Fatal(err)
	}
	u, _ := s.UserByLoginName("wh-u")
	if err := s.PutProject(domain.Project{ID: "p-wh", Name: "P", OwnerID: u.ID}); err != nil {
		t.Fatal(err)
	}
	ch := domain.ProjectWebhookChannel{
		ID: "whc-1", ProjectID: "p-wh", Name: "n", Enabled: true, AuthMode: domain.WebhookAuthHMAC,
		DeliveryTokenPrefix: "lfw_abcd", CreatedAt: time.Now().UTC(),
	}
	secret1 := "whsec_test_secret_one________________"
	if err := s.CreateWebhookChannel(ch, "lfw_delivery_token_aaaaaaaaaaaaaaaa", secret1); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ok, ver, err := s.MatchWebhookSecret(ch.ID, secret1, now)
	if err != nil || !ok || ver != 1 {
		t.Fatalf("match secret: ok=%v ver=%d err=%v", ok, ver, err)
	}
	body := []byte(`{"a":1}`)
	mac := hmac.New(sha256.New, []byte(secret1))
	mac.Write(body)
	ok, ver, err = s.MatchWebhookHMAC(ch.ID, body, mac.Sum(nil), now)
	if err != nil || !ok || ver != 1 {
		t.Fatalf("hmac: ok=%v ver=%d err=%v", ok, ver, err)
	}
	secret2 := "whsec_test_secret_two________________"
	if _, _, err := s.RotateWebhookSecret(ch.ID, secret2, time.Minute); err != nil {
		t.Fatal(err)
	}
	ok1, _, _ := s.MatchWebhookSecret(ch.ID, secret1, now)
	ok2, ver2, _ := s.MatchWebhookSecret(ch.ID, secret2, now)
	if !ok1 || !ok2 || ver2 != 2 {
		t.Fatalf("grace: old=%v new=%v ver=%d", ok1, ok2, ver2)
	}
	if err := s.ClaimWebhookNonceStrict(ch.ID, "n1", now); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimWebhookNonceStrict(ch.ID, "n1", now); err != ErrWebhookReplay {
		t.Fatalf("replay want ErrWebhookReplay got %v", err)
	}
}

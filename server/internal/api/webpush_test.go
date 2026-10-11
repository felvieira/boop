package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chrisgreg/boop/server/internal/devices"
	"github.com/chrisgreg/boop/server/internal/events"
	"github.com/chrisgreg/boop/server/internal/events/levels"
	"github.com/chrisgreg/boop/server/internal/webpush"
)

func TestWebPushEndpoints(t *testing.T) {
	env := newEnv(t)
	wpClient, err := webpush.InitOrLoadKeys(context.Background(), env.server.Settings, env.server.Config.WebPush, env.server.Log)
	if err != nil {
		t.Fatalf("InitOrLoadKeys: %v", err)
	}
	env.server.WebPush = wpClient

	// 1. Get VAPID key
	req, _ := http.NewRequest("GET", env.srv.URL+"/api/v1/push/vapid-key", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/push/vapid-key: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var keyResp struct {
		PublicKey string `json:"public_key"`
	}
	_ = json.NewDecoder(res.Body).Decode(&keyResp)
	if keyResp.PublicKey == "" || keyResp.PublicKey != wpClient.PublicKey() {
		t.Fatalf("expected public key %q, got %q", wpClient.PublicKey(), keyResp.PublicKey)
	}

	// 2. Subscribe
	subPayload := map[string]any{
		"name":     "PWA Test Device",
		"platform": "web",
		"subscription": map[string]any{
			"endpoint": "https://push.example.com/sub/123",
			"keys": map[string]string{
				"p256dh": "test-p256dh-key",
				"auth":   "test-auth-secret",
			},
		},
	}
	subBody, _ := json.Marshal(subPayload)
	reqSub, _ := http.NewRequest("POST", env.srv.URL+"/api/v1/push/subscribe", bytes.NewReader(subBody))
	reqSub.Header.Set("Content-Type", "application/json")
	resSub, err := http.DefaultClient.Do(reqSub)
	if err != nil {
		t.Fatalf("POST /api/v1/push/subscribe: %v", err)
	}
	defer resSub.Body.Close()
	if resSub.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resSub.StatusCode)
	}
	var subRes struct {
		Device     devices.Device `json:"device"`
		Credential string         `json:"credential"`
	}
	_ = json.NewDecoder(resSub.Body).Decode(&subRes)
	if subRes.Device.ID == "" || subRes.Device.Platform != "web" || !subRes.Device.HasToken {
		t.Fatalf("expected registered web device, got %+v", subRes.Device)
	}
	if !strings.HasPrefix(subRes.Credential, "boop_dev_") {
		t.Fatalf("expected boop_dev_ credential, got %q", subRes.Credential)
	}

	// 3. Unsubscribe
	unsubPayload := map[string]string{"endpoint": "https://push.example.com/sub/123"}
	unsubBody, _ := json.Marshal(unsubPayload)
	reqUnsub, _ := http.NewRequest("POST", env.srv.URL+"/api/v1/push/unsubscribe", bytes.NewReader(unsubBody))
	reqUnsub.Header.Set("Content-Type", "application/json")
	resUnsub, err := http.DefaultClient.Do(reqUnsub)
	if err != nil {
		t.Fatalf("POST /api/v1/push/unsubscribe: %v", err)
	}
	defer resUnsub.Body.Close()
	if resUnsub.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resUnsub.StatusCode)
	}
}

func TestSSEEventStream(t *testing.T) {
	env := newEnv(t)
	env.server.EventHub = NewEventHub()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", env.srv.URL+"/api/v1/events/stream", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/events/stream: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	reader := bufio.NewReader(res.Body)

	// Publish an event via EventHub
	go func() {
		time.Sleep(100 * time.Millisecond)
		env.server.EventHub.Publish(events.Event{
			ID:    "evt_test_stream_123",
			Title: "Test SSE Event",
			Level: levels.Info,
		})
	}()

	found := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.Contains(line, "evt_test_stream_123") {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("expected to receive evt_test_stream_123 over SSE stream")
	}
}

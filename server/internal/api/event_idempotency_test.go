package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
)

func TestEventIdempotency(t *testing.T) {
	e := newEnv(t)
	_, firstKey := e.createProject("First")
	_, secondKey := e.createProject("Second")
	post := func(projectKey, requestKey, title string) (int, string) {
		payload, _ := json.Marshal(map[string]string{"title": title})
		req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/api/v1/events", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+projectKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", requestKey)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var body struct{ ID string `json:"id"` }
		_ = json.Unmarshal(raw, &body)
		return res.StatusCode, body.ID
	}

	status, first := post(firstKey, "request-one", "Payment")
	if status != http.StatusCreated || first == "" {
		t.Fatalf("first request: status=%d id=%q", status, first)
	}
	status, replay := post(firstKey, "request-one", "Payment")
	if status != http.StatusOK || replay != first {
		t.Fatalf("replay: status=%d id=%q, original=%q", status, replay, first)
	}
	if status, _ := post(firstKey, "request-one", "Different payment"); status != http.StatusConflict {
		t.Fatalf("different payload: status=%d", status)
	}
	if status, other := post(secondKey, "request-one", "Payment"); status != http.StatusCreated || other == first {
		t.Fatalf("other project: status=%d id=%q", status, other)
	}

	const n = 8
	results := make(chan string, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, id := post(firstKey, "concurrent", "Refund")
			results <- fmt.Sprintf("%d:%s", status, id)
		}()
	}
	wg.Wait()
	close(results)
	ids := map[string]int{}
	created := 0
	for result := range results {
		var code int
		var id string
		_, _ = fmt.Sscanf(result, "%d:%s", &code, &id)
		if code != http.StatusOK && code != http.StatusCreated {
			t.Fatalf("concurrent request: %s", result)
		}
		if code == http.StatusCreated {
			created++
		}
		ids[id]++
	}
	if created != 1 || len(ids) != 1 {
		t.Fatalf("concurrent requests: created=%d ids=%v", created, ids)
	}
	list := e.do("GET", "/api/v1/events", "", nil)
	if got := len(list.body["events"].([]any)); got != 3 {
		t.Fatalf("stored events=%d, want 3", got)
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func writeB4TestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func callB4PortalJSON(t *testing.T, handler http.HandlerFunc, method string, target string, wantStatus int) map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body: %s", method, target, rec.Code, wantStatus, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode json body for %s %s: %v\\nbody: %s", method, target, err, rec.Body.String())
	}
	return body
}

func requireB4String(t *testing.T, body map[string]interface{}, key string, want string) {
	t.Helper()
	if got, _ := body[key].(string); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func requireB4NestedString(t *testing.T, body map[string]interface{}, parent string, key string, want string) {
	t.Helper()
	parentValue, ok := body[parent].(map[string]interface{})
	if !ok {
		t.Fatalf("%s is not an object: %#v", parent, body[parent])
	}
	if got, _ := parentValue[key].(string); got != want {
		t.Fatalf("%s.%s = %q, want %q", parent, key, got, want)
	}
}

func requireB4JSONContains(t *testing.T, body map[string]interface{}, needle string) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal json body: %v", err)
	}
	if !strings.Contains(string(data), needle) {
		t.Fatalf("json body does not contain %q: %s", needle, string(data))
	}
}

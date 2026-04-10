package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestGetRoutingRules(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	store.CreateFileRoutingRule(&db.FileRoutingRule{
		Pattern:        "*.mkv",
		DestinationDir: "/media",
		Priority:       100,
		Enabled:        true,
	})

	req := httptest.NewRequest("GET", "/api/routing/rules", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rules []db.FileRoutingRule
	if err := json.NewDecoder(w.Body).Decode(&rules); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
}

func TestCreateRoutingRule(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	body := `{"pattern":"*.mp4","destination_dir":"/media/video","priority":50,"enabled":true}`
	req := httptest.NewRequest("POST", "/api/routing/rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var rule db.FileRoutingRule
	if err := json.NewDecoder(w.Body).Decode(&rule); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if rule.ID == 0 {
		t.Errorf("expected rule ID to be set, got 0")
	}
	if rule.Pattern != "*.mp4" {
		t.Errorf("expected pattern *.mp4, got %q", rule.Pattern)
	}
}

func TestUpdateRoutingRule(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	rule := &db.FileRoutingRule{
		Pattern:        "*.avi",
		DestinationDir: "/media",
		Priority:       80,
		Enabled:        true,
	}
	store.CreateFileRoutingRule(rule)

	body := `{"pattern":"*.avi","destination_dir":"/media/updated","priority":80,"enabled":true}`
	req := httptest.NewRequest("PUT", "/api/routing/rules/"+intToStr(rule.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteRoutingRule(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	rule := &db.FileRoutingRule{
		Pattern:        "*.avi",
		DestinationDir: "/media",
		Priority:       80,
		Enabled:        true,
		Builtin:        false,
	}
	store.CreateFileRoutingRule(rule)

	req := httptest.NewRequest("DELETE", "/api/routing/rules/"+intToStr(rule.ID), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteBuiltinCatchAll(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	rule := &db.FileRoutingRule{
		Pattern:        "*",
		DestinationDir: "/downloads",
		Priority:       0,
		Builtin:        true,
		Enabled:        true,
	}
	store.CreateFileRoutingRule(rule)

	req := httptest.NewRequest("DELETE", "/api/routing/rules/"+intToStr(rule.ID), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMethodNotAllowed_RoutingRules(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req := httptest.NewRequest("PUT", "/api/routing/rules", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestGetHooks(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	store.CreatePostHook(&db.PostHook{
		Name:     "test hook",
		HookType: "script",
		Config:   `{"command":"/usr/bin/notify"}`,
		Enabled:  true,
	})

	req := httptest.NewRequest("GET", "/api/hooks", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var hooks []db.PostHook
	if err := json.NewDecoder(w.Body).Decode(&hooks); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(hooks) != 1 {
		t.Errorf("expected 1 hook, got %d", len(hooks))
	}
}

func TestCreateHook(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	body := `{"name":"notify","hook_type":"script","config":"{\"command\":\"/usr/bin/notify\"}","enabled":true}`
	req := httptest.NewRequest("POST", "/api/hooks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var hook db.PostHook
	if err := json.NewDecoder(w.Body).Decode(&hook); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if hook.ID == 0 {
		t.Errorf("expected hook ID to be set, got 0")
	}
}

func TestGetHooks_WithScope(t *testing.T) {
	srv, store, cleanup := newTestServerWithStore(t)
	defer cleanup()

	scopeID := int64(42)
	store.CreatePostHook(&db.PostHook{
		Name:     "scoped hook",
		Scope:    "channel",
		ScopeID:  &scopeID,
		HookType: "script",
		Config:   `{"command":"/bin/true"}`,
		Enabled:  true,
	})
	store.CreatePostHook(&db.PostHook{
		Name:     "global hook",
		HookType: "script",
		Config:   `{"command":"/bin/true"}`,
		Enabled:  true,
	})

	req := httptest.NewRequest("GET", "/api/hooks?scope=channel&scope_id=42", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var hooks []db.PostHook
	json.NewDecoder(w.Body).Decode(&hooks)
	if len(hooks) != 1 {
		t.Errorf("expected 1 scoped hook, got %d", len(hooks))
	}
}

func TestMethodNotAllowed_Hooks(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/api/hooks", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestUpdateRoutingRule_NotFound(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	body := `{"pattern":"*.mkv","destination_dir":"/media","priority":100,"enabled":true}`
	req, _ := http.NewRequest(http.MethodPut, "/api/routing/rules/9999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestDeleteRoutingRule_NotFound(t *testing.T) {
	srv, _, cleanup := newTestServerWithStore(t)
	defer cleanup()

	req, _ := http.NewRequest(http.MethodDelete, "/api/routing/rules/9999", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// intToStr converts an int64 to a decimal string.
func intToStr(n int64) string {
	return strconv.FormatInt(n, 10)
}

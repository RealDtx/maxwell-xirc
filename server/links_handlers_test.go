package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/db"
)

func postLinks(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestLinksPreviewAndQueue(t *testing.T) {
	srv, store, cleanup := newTestServerWithEngine(t)
	defer cleanup()
	sv := &db.Server{Name: "Example", Host: "irc.example.net", Port: 6667, Nickname: "me", Enabled: true}
	if err := store.CreateServer(sv); err != nil {
		t.Fatal(err)
	}
	text := "xirc://irc.example.net/%23example-downloads/ExampleBot/5?name=A.mkv&size=1.4G\n" +
		"xirc://irc.unknown.net/%23x/Bot/1\n" +
		"xirc://irc.example.net/%23x/Bot/zero\n"

	w := postLinks(t, srv, "/api/links/preview", text)
	if w.Code != http.StatusOK {
		t.Fatalf("preview status %d: %s", w.Code, w.Body)
	}
	var pv struct {
		Links  []linkRow `json:"links"`
		Errors []struct{ Line, Reason string }
	}
	json.Unmarshal(w.Body.Bytes(), &pv)
	if len(pv.Links) != 2 || len(pv.Errors) != 1 {
		t.Fatalf("preview = %+v", pv)
	}
	if pv.Links[0].Status != "ok" || pv.Links[0].ServerID != sv.ID || pv.Links[0].ServerName != "Example" {
		t.Errorf("row 0 = %+v", pv.Links[0])
	}
	if pv.Links[1].Status != "unknown network" {
		t.Errorf("row 1 = %+v", pv.Links[1])
	}

	w = postLinks(t, srv, "/api/links/queue", text)
	var q struct {
		Queued  int `json:"queued"`
		Skipped []struct{ Line, Reason string }
	}
	json.Unmarshal(w.Body.Bytes(), &q)
	if w.Code != http.StatusOK || q.Queued != 1 || len(q.Skipped) != 2 {
		t.Fatalf("queue = %d %+v", w.Code, q)
	}
	dls, _ := store.GetDownloads("")
	if len(dls) != 1 || dls[0].BotNick != "ExampleBot" || dls[0].PackNumber != 5 || dls[0].Filename != "A.mkv" || dls[0].Filesize == 0 || dls[0].Channel != "#example-downloads" {
		t.Fatalf("downloads = %+v", dls)
	}

	// Same paste again: duplicate is marked and not queued twice.
	w = postLinks(t, srv, "/api/links/preview", text)
	json.Unmarshal(w.Body.Bytes(), &pv)
	if pv.Links[0].Status != "already queued" {
		t.Errorf("second preview row 0 status = %q", pv.Links[0].Status)
	}
	w = postLinks(t, srv, "/api/links/queue", `{"text":"`+strings.Split(text, "\n")[0]+`"}`)
	json.Unmarshal(w.Body.Bytes(), &q)
	if q.Queued != 0 {
		t.Errorf("duplicate queued again: %+v", q)
	}
	if dls, _ := store.GetDownloads(""); len(dls) != 1 {
		t.Errorf("want 1 download, got %d", len(dls))
	}
}

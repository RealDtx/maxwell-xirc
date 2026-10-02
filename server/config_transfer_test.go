package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/db"
)

type transferSeed struct {
	A, B   db.Server
	A1, A2 db.Realm
}

// seedTransfer creates srvA (with a password) holding #a1 (with a channel
// key) and #a2, plus srvB without realms.
func seedTransfer(t *testing.T, store db.Store) transferSeed {
	t.Helper()
	var s transferSeed
	s.A = db.Server{Name: "srvA", Host: "irc.a.example", Port: 6697, SSL: true, Nickname: "nickA",
		AltNicknames: []string{"nickA_"}, AuthMethod: "sasl", AuthPassword: "s3cret-pw", AutoConnect: true, Enabled: true}
	s.B = db.Server{Name: "srvB", Host: "irc.b.example", Port: 6667, Nickname: "nickB", AuthMethod: "none", Enabled: true}
	for _, srv := range []*db.Server{&s.A, &s.B} {
		if err := store.CreateServer(srv); err != nil {
			t.Fatal(err)
		}
	}
	s.A1 = db.Realm{ServerID: s.A.ID, Name: "#a1", DisplayName: "A one", Key: "chan-k3y", SearchCommand: "!s",
		DownloadChannel: "#a1-dl", SearchBot: "BotA", SearchTimeout: 10, AutoJoin: true, Enabled: true}
	s.A2 = db.Realm{ServerID: s.A.ID, Name: "#a2", SearchCommand: "!find", SearchTimeout: 20, Enabled: true}
	for _, r := range []*db.Realm{&s.A1, &s.A2} {
		if err := store.CreateRealm(r); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func decodeExport(t *testing.T, body string) configFile {
	t.Helper()
	var f configFile
	if err := json.Unmarshal([]byte(body), &f); err != nil {
		t.Fatalf("decode export: %v\n%s", err, body)
	}
	return f
}

func TestConfigExport_All(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	seedTransfer(t, srv.store)

	w := adminDo(t, srv, "GET", "/api/config/export", "")
	if w.Code != 200 {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="xirc-config-`) {
		t.Errorf("disposition: %q", cd)
	}
	body := w.Body.String()
	for _, leak := range []string{"s3cret-pw", "chan-k3y", "auth_password", `"key"`, "trusted_networks", "trusted_role"} {
		if strings.Contains(body, leak) {
			t.Errorf("export leaks %q", leak)
		}
	}
	f := decodeExport(t, body)
	if f.Format != "xirc-config" || f.Version != 1 || f.ExportedAt.IsZero() {
		t.Errorf("header: %+v", f)
	}
	if len(f.Servers) != 2 || f.Servers[0].Name != "srvA" || len(f.Servers[0].Realms) != 2 || len(f.Servers[1].Realms) != 0 {
		t.Fatalf("servers: %+v", f.Servers)
	}
	if f.Servers[0].Realms[0].DownloadChannel != "#a1-dl" || f.Servers[0].AuthMethod != "sasl" {
		t.Errorf("fields: %+v", f.Servers[0])
	}
	if f.Settings == nil || f.Settings.Downloads.MaxConcurrent != 2 {
		t.Errorf("settings: %+v", f.Settings)
	}
}

func TestConfigExport_Selection(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	s := seedTransfer(t, srv.store)

	// A single realm pulls in its parent server holding only that realm; no settings.
	f := decodeExport(t, adminDo(t, srv, "GET", fmt.Sprintf("/api/config/export?realms=%d", s.A2.ID), "").Body.String())
	if len(f.Servers) != 1 || f.Servers[0].Name != "srvA" || len(f.Servers[0].Realms) != 1 || f.Servers[0].Realms[0].Name != "#a2" {
		t.Errorf("realm selection: %+v", f.Servers)
	}
	if f.Settings != nil {
		t.Errorf("settings not requested but present")
	}

	// A server selection includes all its realms; settings on request.
	f = decodeExport(t, adminDo(t, srv, "GET", fmt.Sprintf("/api/config/export?servers=%d&settings=1", s.A.ID), "").Body.String())
	if len(f.Servers) != 1 || len(f.Servers[0].Realms) != 2 || f.Settings == nil {
		t.Errorf("server selection: %+v %+v", f.Servers, f.Settings)
	}

	// Settings only.
	f = decodeExport(t, adminDo(t, srv, "GET", "/api/config/export?settings=1", "").Body.String())
	if len(f.Servers) != 0 || f.Settings == nil {
		t.Errorf("settings only: %+v", f)
	}

	if w := adminDo(t, srv, "GET", "/api/config/export?servers=abc", ""); w.Code != 400 {
		t.Errorf("bad id: %d", w.Code)
	}
}

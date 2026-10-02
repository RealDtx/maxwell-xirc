package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
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

func postJSON(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return adminDo(t, srv, "POST", path, body)
}

// previewItems posts body to the preview endpoint and indexes items by key.
func previewItems(t *testing.T, srv *Server, body string) map[string]importItem {
	t.Helper()
	w := postJSON(t, srv, "/api/config/import/preview", body)
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []importItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	m := map[string]importItem{}
	for _, it := range resp.Items {
		m[it.Key] = it
	}
	return m
}

const transferFile = `{"format":"xirc-config","version":1,"exported_at":"2026-10-03T00:00:00Z","servers":[
	{"name":"srvA","host":"irc.a.example","port":7000,"ssl":true,"nickname":"nickA","alt_nicknames":["nickA_"],
	 "auth_method":"sasl","auto_connect":true,"enabled":true,"realms":[
		{"name":"#a1","display_name":"A one","search_command":"!s","download_channel":"#a1-dl","search_bot":"BotA","search_timeout":10,"auto_join":true,"enabled":true},
		{"name":"#a3","display_name":"","search_command":"!s","download_channel":"","search_bot":"","search_timeout":10,"auto_join":true,"enabled":true}]},
	{"name":"srvC","host":"irc.c.example","port":6667,"ssl":false,"nickname":"nickC","alt_nicknames":[],
	 "auth_method":"none","auto_connect":false,"enabled":true,"realms":[
		{"name":"#c1","display_name":"","search_command":"!s","download_channel":"","search_bot":"","search_timeout":10,"auto_join":true,"enabled":true}]}
]}`

func TestConfigImportPreview_Statuses(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	seedTransfer(t, srv.store)

	items := previewItems(t, srv, transferFile)
	want := map[string]string{"srvA": "exists", "srvA/#a1": "exists", "srvA/#a3": "new", "srvC": "new", "srvC/#c1": "new"}
	for k, st := range want {
		if items[k].Status != st {
			t.Errorf("%s: status %q want %q (%+v)", k, items[k].Status, st, items[k])
		}
	}
	if len(items) != len(want) {
		t.Errorf("got %d items, want %d: %+v", len(items), len(want), items)
	}
	if c := items["srvA"].Changes; len(c) != 1 || c[0] != "port: 6697→7000" {
		t.Errorf("srvA changes: %q", c)
	}
	if c := items["srvA/#a1"].Changes; len(c) != 0 {
		t.Errorf("#a1 should be unchanged: %q", c)
	}
	if items["srvA"].Kind != "server" || items["srvA/#a1"].Kind != "realm" {
		t.Errorf("kinds: %+v", items)
	}
}

func TestConfigImportPreview_ItemErrors(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	body := `{"format":"xirc-config","version":1,"servers":[
		{"name":"srvX","host":"h","port":6667,"nickname":"","realms":[{"name":"#x1"}]},
		{"name":"srvY","host":"h","port":6667,"nickname":"n","realms":[{"name":"#y1"},{"name":"#y1"},{"name":""}]},
		{"name":"srvY","host":"h","port":6667,"nickname":"n","realms":[]},
		{"name":"srvZ","host":"h","port":70000,"nickname":"n","realms":[]}
	]}`
	w := postJSON(t, srv, "/api/config/import/preview", body)
	var resp struct {
		Items []importItem `json:"items"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	got := []string{}
	for _, it := range resp.Items {
		got = append(got, it.Key+"="+it.Status+":"+it.Error)
	}
	want := []string{
		"srvX=error:nickname is required",
		"srvX/#x1=error:parent server has errors",
		"srvY=new:",
		"srvY/#y1=new:",
		"srvY/#y1=error:duplicate realm srvY/#y1 in file",
		"srvY/=error:realm name is required",
		"srvY=error:duplicate server srvY in file",
		"srvZ=error:port must be 1–65535",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestConfigImportPreview_Settings(t *testing.T) {
	srv, cfg, _ := newSettingsTestServer(t, 2)
	st := cfg.Editable().Storage
	mk := func(maxConc int) string {
		return fmt.Sprintf(`{"format":"xirc-config","version":1,"servers":[],"settings":{
			"storage":{"downloads_dir":%q,"temp_dir":%q,"min_free_space":"1GB"},
			"downloads":{"max_concurrent":%d},
			"maintenance":{"search_result_retention_days":14,"index_max_files":200000,"interval_hours":6},
			"ui":{"help_default":"first_time"}}}`, st.DownloadsDir, st.TempDir, maxConc)
	}
	it := previewItems(t, srv, mk(5))["settings"]
	if it.Kind != "settings" || it.Status != "exists" || it.Error != "" {
		t.Fatalf("settings item: %+v", it)
	}
	found := false
	for _, c := range it.Changes {
		found = found || c == "downloads.max_concurrent: 2→5"
	}
	if !found {
		t.Errorf("changes: %q", it.Changes)
	}
	if it := previewItems(t, srv, mk(99))["settings"]; it.Status != "error" || !strings.Contains(it.Error, "max_concurrent") {
		t.Errorf("invalid settings: %+v", it)
	}
}

func TestConfigImportPreview_BadFiles(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	for name, body := range map[string]string{
		"not json":       `nope`,
		"foreign json":   `{}`,
		"wrong format":   `{"format":"other","version":1}`,
		"future version": `{"format":"xirc-config","version":2}`,
		"too large":      `{"format":"xirc-config","version":1,"pad":"` + strings.Repeat("x", 1<<20) + `"}`,
	} {
		if w := postJSON(t, srv, "/api/config/import/preview", body); w.Code != 400 {
			t.Errorf("%s: got %d %s", name, w.Code, w.Body.String())
		}
	}
}

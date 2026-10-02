package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/RealDtx/maxwell-irc/config"
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

func applyImport(t *testing.T, srv *Server, file, def, decisions string) map[string]importResult {
	t.Helper()
	if decisions == "" {
		decisions = "{}"
	}
	w := postJSON(t, srv, "/api/config/import/apply", `{"file":`+file+`,"default":"`+def+`","decisions":`+decisions+`}`)
	if w.Code != 200 {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []importResult `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	m := map[string]importResult{}
	for _, it := range resp.Items {
		m[it.Key] = it
	}
	return m
}

func serverByName(t *testing.T, store db.Store, name string) *db.Server {
	t.Helper()
	all, _ := store.GetServers()
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}

func TestConfigImport_DefaultSkip(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	seedTransfer(t, srv.store)
	res := applyImport(t, srv, transferFile, "skip", "")
	want := map[string]string{"srvA": "skipped", "srvA/#a1": "skipped", "srvA/#a3": "created", "srvC": "created", "srvC/#c1": "created"}
	for k, r := range want {
		if res[k].Result != r {
			t.Errorf("%s: %+v want %s", k, res[k], r)
		}
	}
	if a := serverByName(t, srv.store, "srvA"); a.Port != 6697 {
		t.Errorf("skipped server changed: port %d", a.Port)
	}
	c := serverByName(t, srv.store, "srvC")
	if c == nil || c.AuthPassword != "" {
		t.Fatalf("srvC: %+v", c)
	}
	if realms, _ := srv.store.GetRealms(c.ID); len(realms) != 1 || realms[0].Name != "#c1" {
		t.Errorf("srvC realms: %+v", realms)
	}
}

func TestConfigImport_OverwriteKeepsSecrets(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	s := seedTransfer(t, srv.store)
	// Change #a1's display name in the file so the realm really gets written.
	file := strings.Replace(transferFile, `"display_name":"A one"`, `"display_name":"A uno"`, 1)
	res := applyImport(t, srv, file, "overwrite", "")
	if res["srvA"].Result != "updated" || res["srvA/#a1"].Result != "updated" {
		t.Fatalf("results: %+v", res)
	}
	a, _ := srv.store.GetServer(s.A.ID)
	if a.Port != 7000 || a.AuthPassword != "s3cret-pw" {
		t.Errorf("srvA after overwrite: port %d password %q", a.Port, a.AuthPassword)
	}
	r, _ := srv.store.GetRealm(s.A1.ID)
	if r.DisplayName != "A uno" || r.Key != "chan-k3y" {
		t.Errorf("#a1 after overwrite: %+v", r)
	}
	if r2, _ := srv.store.GetRealm(s.A2.ID); r2 == nil {
		t.Errorf("#a2 (absent from file) was deleted")
	}
}

func TestConfigImport_PerItemOverride(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	seedTransfer(t, srv.store)
	res := applyImport(t, srv, transferFile, "skip", `{"srvA":"overwrite","srvC/#c1":"exclude"}`)
	if res["srvA"].Result != "updated" || res["srvA/#a1"].Result != "skipped" || res["srvC/#c1"].Result != "excluded" {
		t.Errorf("results: %+v", res)
	}
	c := serverByName(t, srv.store, "srvC")
	if realms, _ := srv.store.GetRealms(c.ID); len(realms) != 0 {
		t.Errorf("excluded realm created: %+v", realms)
	}
}

func TestConfigImport_ParentHandling(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	s := seedTransfer(t, srv.store)
	res := applyImport(t, srv, transferFile, "skip", `{"srvA":"exclude","srvC":"exclude"}`)
	// Existing parent excluded → its new realm still merges into it.
	if res["srvA/#a3"].Result != "created" {
		t.Errorf("#a3: %+v", res["srvA/#a3"])
	}
	if realms, _ := srv.store.GetRealms(s.A.ID); len(realms) != 3 {
		t.Errorf("srvA realms: %d", len(realms))
	}
	// New parent excluded → realm errors.
	if r := res["srvC/#c1"]; r.Result != "error" || r.Error != "parent server not imported" {
		t.Errorf("#c1: %+v", r)
	}
	if serverByName(t, srv.store, "srvC") != nil {
		t.Errorf("excluded server created")
	}
}

func TestConfigImport_BadRequests(t *testing.T) {
	srv, _, _ := newSettingsTestServer(t, 2)
	for name, body := range map[string]string{
		"bad default":  `{"file":` + transferFile + `,"default":"maybe","decisions":{}}`,
		"bad decision": `{"file":` + transferFile + `,"default":"skip","decisions":{"srvA":"later"}}`,
		"bad version":  `{"file":{"format":"xirc-config","version":9},"default":"skip"}`,
		"no file":      `{"default":"skip"}`,
	} {
		if w := postJSON(t, srv, "/api/config/import/apply", body); w.Code != 400 {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if all, _ := srv.store.GetServers(); len(all) != 0 {
		t.Errorf("bad requests wrote servers: %+v", all)
	}
}

func TestConfigImport_SettingsAppliedAndPersisted(t *testing.T) {
	srv, cfg, path := newSettingsTestServer(t, 2)
	exp := decodeExport(t, adminDo(t, srv, "GET", "/api/config/export?settings=1", "").Body.String())
	exp.Settings.Downloads.MaxConcurrent = 5
	raw, _ := json.Marshal(exp)
	res := applyImport(t, srv, string(raw), "overwrite", "")
	if res["settings"].Result != "updated" {
		t.Fatalf("settings: %+v", res["settings"])
	}
	if cfg.Downloads.MaxConcurrent != 5 {
		t.Errorf("not applied live: %d", cfg.Downloads.MaxConcurrent)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "max_concurrent: 5") {
		t.Errorf("not persisted:\n%s", data)
	}
	if !strings.Contains(string(data), "10.0.0.0/8") {
		t.Errorf("auth section lost:\n%s", data)
	}
}

func TestConfigImport_SettingsEnvLockedKeptSilently(t *testing.T) {
	t.Setenv("XIRC_DOWNLOADS_MAX_CONCURRENT", "3")
	srv, cfg, _ := newSettingsTestServer(t, 3)
	exp := decodeExport(t, adminDo(t, srv, "GET", "/api/config/export?settings=1", "").Body.String())
	exp.Settings.Downloads.MaxConcurrent = 7
	exp.Settings.Maintenance.IntervalHours = 9
	raw, _ := json.Marshal(exp)
	if it := previewItems(t, srv, string(raw))["settings"]; it.Status != "exists" {
		t.Fatalf("preview: %+v", it)
	}
	if res := applyImport(t, srv, string(raw), "overwrite", ""); res["settings"].Result != "updated" {
		t.Fatalf("apply: %+v", res["settings"])
	}
	if cfg.Downloads.MaxConcurrent != 3 || cfg.Maintenance.IntervalHours != 9 {
		t.Errorf("got max_concurrent %d interval %d", cfg.Downloads.MaxConcurrent, cfg.Maintenance.IntervalHours)
	}
}

func TestConfigTransfer_RoundTrip(t *testing.T) {
	src, _, _ := newSettingsTestServer(t, 4)
	seedTransfer(t, src.store)
	first := decodeExport(t, adminDo(t, src, "GET", "/api/config/export", "").Body.String())
	raw, _ := json.Marshal(first)

	dst, _, _ := newSettingsTestServer(t, 2)
	for k, r := range applyImport(t, dst, string(raw), "overwrite", "") {
		if r.Result != "created" && r.Result != "updated" {
			t.Errorf("%s: %+v", k, r)
		}
	}
	second := decodeExport(t, adminDo(t, dst, "GET", "/api/config/export", "").Body.String())
	first.ExportedAt = second.ExportedAt
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Errorf("round trip differs:\n%s\n%s", a, b)
	}
}

func TestConfigTransfer_AdminOnly(t *testing.T) {
	srv, _ := newAuthTestServer(t, config.AuthConfig{TrustedNetworks: []string{"192.168.0.0/16"}, TrustedRole: "user"})
	hdr := map[string]string{"Content-Type": "application/json"}
	for _, c := range []struct{ m, p string }{
		{"GET", "/api/config/export"},
		{"POST", "/api/config/import/preview"},
		{"POST", "/api/config/import/apply"},
	} {
		if w := do(srv, c.m, c.p, "192.168.1.5:1", hdr, `{}`); w.Code != 403 {
			t.Errorf("%s %s as user: %d", c.m, c.p, w.Code)
		}
	}
}

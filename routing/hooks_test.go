package routing

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/maxwell-xirc/xirc/db"
)

func TestRunScriptHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	dir, err := ioutil.TempDir("", "hooks_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	markerFile := filepath.Join(dir, "hook_ran")

	scriptPath := filepath.Join(dir, "hook.sh")
	ioutil.WriteFile(scriptPath, []byte("#!/bin/sh\ntouch "+markerFile+"\n"), 0755)

	hook := db.PostHook{
		HookType: "script",
		Config:   `{"command":"` + scriptPath + `"}`,
	}

	ctx := HookContext{
		FilePath: "/tmp/test.mkv",
		Filename: "test.mkv",
		BotNick:  "bot1",
		Server:   "irc.example.com",
		Channel:  "#test",
		Filesize: 1000,
		Pack:     42,
	}

	result := RunHook(hook, ctx)
	if result.Error != "" {
		t.Fatalf("hook failed: %s", result.Error)
	}

	if _, err := os.Stat(markerFile); os.IsNotExist(err) {
		t.Error("hook script did not execute")
	}
}

func TestRunScriptHook_EnvVars(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	dir, err := ioutil.TempDir("", "hooks_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	outFile := filepath.Join(dir, "env_out")

	scriptPath := filepath.Join(dir, "env_hook.sh")
	ioutil.WriteFile(scriptPath, []byte("#!/bin/sh\necho $XIRC_FILENAME > "+outFile+"\n"), 0755)

	hook := db.PostHook{
		HookType: "script",
		Config:   `{"command":"` + scriptPath + `"}`,
	}

	ctx := HookContext{
		FilePath: "/tmp/movie.mkv",
		Filename: "movie.mkv",
		BotNick:  "bot1",
		Server:   "srv1",
		Channel:  "#ch",
		Filesize: 500,
		Pack:     10,
	}

	RunHook(hook, ctx)

	data, _ := ioutil.ReadFile(outFile)
	if string(data) != "movie.mkv\n" {
		t.Errorf("expected XIRC_FILENAME=movie.mkv, got %q", string(data))
	}
}

func TestRunRenameHook(t *testing.T) {
	dir, err := ioutil.TempDir("", "hooks_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	srcPath := filepath.Join(dir, "Show.S03E07.720p.mkv")
	ioutil.WriteFile(srcPath, []byte("data"), 0644)

	hook := db.PostHook{
		HookType: "rename",
		Config:   `{"match":"(.+)\\.S(\\d+)E(\\d+)\\.(.+)","replace":"S${2}E${3} - $1.$4"}`,
	}

	ctx := HookContext{
		FilePath: srcPath,
		Filename: "Show.S03E07.720p.mkv",
	}

	result := RunHook(hook, ctx)
	if result.Error != "" {
		t.Fatalf("rename hook failed: %s", result.Error)
	}

	expected := filepath.Join(dir, "S03E07 - Show.720p.mkv")
	if result.NewPath != expected {
		t.Errorf("expected %s, got %s", expected, result.NewPath)
	}

	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Error("renamed file does not exist")
	}
}

func TestRunRenameHook_CreatesSubdir(t *testing.T) {
	dir, err := ioutil.TempDir("", "hooks_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	srcPath := filepath.Join(dir, "Show.S03E07.720p.mkv")
	ioutil.WriteFile(srcPath, []byte("data"), 0644)

	hook := db.PostHook{
		HookType: "rename",
		Config:   `{"match":"(.+)\\.S(\\d+)E(\\d+)\\.(.+)","replace":"Season $2/S${2}E${3} - $1.$4"}`,
	}

	ctx := HookContext{
		FilePath: srcPath,
		Filename: "Show.S03E07.720p.mkv",
	}

	result := RunHook(hook, ctx)
	if result.Error != "" {
		t.Fatalf("rename hook failed: %s", result.Error)
	}

	expected := filepath.Join(dir, "Season 03", "S03E07 - Show.720p.mkv")
	if result.NewPath != expected {
		t.Errorf("expected %s, got %s", expected, result.NewPath)
	}

	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Error("renamed file in subdir does not exist")
	}
}

func TestRunScriptHook_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	dir, err := ioutil.TempDir("", "hooks_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	scriptPath := filepath.Join(dir, "slow.sh")
	ioutil.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 30\n"), 0755)

	hook := db.PostHook{
		HookType: "script",
		Config:   `{"command":"` + scriptPath + `","timeout":1}`,
	}

	ctx := HookContext{FilePath: "/tmp/test.mkv", Filename: "test.mkv"}
	result := RunHook(hook, ctx)
	if result.Error == "" {
		t.Error("expected timeout error")
	}
}

func TestRunHook_InvalidType(t *testing.T) {
	hook := db.PostHook{HookType: "unknown", Config: "{}"}
	result := RunHook(hook, HookContext{})
	if result.Error == "" {
		t.Error("expected error for unknown hook type")
	}
}

package routing

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	script := "#!/bin/sh\n" +
		"echo \"$XIRC_FILE\" >> " + outFile + "\n" +
		"echo \"$XIRC_FILENAME\" >> " + outFile + "\n" +
		"echo \"$XIRC_BOT\" >> " + outFile + "\n" +
		"echo \"$XIRC_SERVER\" >> " + outFile + "\n" +
		"echo \"$XIRC_CHANNEL\" >> " + outFile + "\n" +
		"echo \"$XIRC_FILESIZE\" >> " + outFile + "\n" +
		"echo \"$XIRC_PACK\" >> " + outFile + "\n"
	ioutil.WriteFile(scriptPath, []byte(script), 0755)

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

	result := RunHook(hook, ctx)
	if result.Error != "" {
		t.Fatalf("hook failed: %s", result.Error)
	}

	data, err := ioutil.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("expected 7 lines of output, got %d: %q", len(lines), string(data))
	}
	expected := []string{
		"/tmp/movie.mkv", // XIRC_FILE
		"movie.mkv",      // XIRC_FILENAME
		"bot1",           // XIRC_BOT
		"srv1",           // XIRC_SERVER
		"#ch",            // XIRC_CHANNEL
		"500",            // XIRC_FILESIZE
		"10",             // XIRC_PACK
	}
	for i, exp := range expected {
		if lines[i] != exp {
			t.Errorf("line %d: expected %q, got %q", i+1, exp, lines[i])
		}
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
	ioutil.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 5\n"), 0755)

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

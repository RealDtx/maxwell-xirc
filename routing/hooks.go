package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/maxwell-xirc/xirc/db"
)

type HookContext struct {
	FilePath string
	Filename string
	BotNick  string
	Server   string
	Channel  string
	Filesize int64
	Pack     int
}

type HookResult struct {
	Output  string
	Error   string
	NewPath string // Set by rename hooks
}

type scriptConfig struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"` // seconds, 0 = default 60
}

type renameConfig struct {
	Match   string `json:"match"`
	Replace string `json:"replace"`
}

// RunHook executes the given hook with the provided context.
// The caller is responsible for checking hook.Enabled before calling.
func RunHook(hook db.PostHook, ctx HookContext) HookResult {
	switch hook.HookType {
	case "script":
		return runScriptHook(hook.Config, ctx)
	case "rename":
		return runRenameHook(hook.Config, ctx)
	default:
		return HookResult{Error: fmt.Sprintf("unknown hook type: %s", hook.HookType)}
	}
}

func runScriptHook(cfgJSON string, ctx HookContext) HookResult {
	var cfg scriptConfig
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return HookResult{Error: fmt.Sprintf("invalid script config: %v", err)}
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, cfg.Command)
	cmd.Env = append(os.Environ(),
		"XIRC_FILE="+ctx.FilePath,
		"XIRC_FILENAME="+ctx.Filename,
		"XIRC_BOT="+ctx.BotNick,
		"XIRC_SERVER="+ctx.Server,
		"XIRC_CHANNEL="+ctx.Channel,
		"XIRC_FILESIZE="+strconv.FormatInt(ctx.Filesize, 10),
		"XIRC_PACK="+strconv.Itoa(ctx.Pack),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return HookResult{
			Output: string(output),
			Error:  err.Error(),
		}
	}

	return HookResult{Output: string(output)}
}

func runRenameHook(cfgJSON string, ctx HookContext) HookResult {
	var cfg renameConfig
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return HookResult{Error: fmt.Sprintf("invalid rename config: %v", err)}
	}

	re, err := regexp.Compile(cfg.Match)
	if err != nil {
		return HookResult{Error: fmt.Sprintf("invalid rename regex: %v", err)}
	}

	newName := re.ReplaceAllString(ctx.Filename, cfg.Replace)
	if newName == ctx.Filename {
		return HookResult{} // No change
	}

	dir := filepath.Dir(ctx.FilePath)
	newPath := filepath.Join(dir, newName)

	// Create subdirectory if the new name contains path separators
	newDir := filepath.Dir(newPath)
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return HookResult{Error: fmt.Sprintf("creating subdir: %v", err)}
	}

	if err := os.Rename(ctx.FilePath, newPath); err != nil {
		return HookResult{Error: fmt.Sprintf("renaming: %v", err)}
	}

	return HookResult{NewPath: newPath}
}

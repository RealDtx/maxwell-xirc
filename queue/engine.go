package queue

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/dcc"
	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
	"github.com/maxwell-xirc/xirc/routing"
)

type PendingRequest struct {
	DownloadID int64
	ServerID   int64
	BotNick    string
}

type Engine struct {
	mu           sync.RWMutex
	wg           sync.WaitGroup
	queue        *Queue
	store        db.Store
	bus          *irc.EventBus
	storageCfg   *config.StorageConfig
	eventCh      <-chan irc.Event
	stopCh       chan struct{}
	pendingByBot map[string]*PendingRequest // key: "serverID:botNick"
}

func NewEngine(store db.Store, bus *irc.EventBus, storageCfg *config.StorageConfig, maxConcurrent int) *Engine {
	return &Engine{
		queue:        New(store, maxConcurrent),
		store:        store,
		bus:          bus,
		storageCfg:   storageCfg,
		stopCh:       make(chan struct{}),
		pendingByBot: make(map[string]*PendingRequest),
	}
}

func (e *Engine) Queue() *Queue {
	return e.queue
}

func (e *Engine) Start() {
	e.eventCh = e.bus.Subscribe()
	go e.loop()
}

func (e *Engine) Stop() {
	close(e.stopCh)
	e.bus.Unsubscribe(e.eventCh)
	e.wg.Wait()
}

func pendingKey(serverID int64, botNick string) string {
	return fmt.Sprintf("%d:%s", serverID, botNick)
}

func (e *Engine) RegisterPendingRequest(downloadID, serverID int64, botNick string) {
	e.mu.Lock()
	e.pendingByBot[pendingKey(serverID, botNick)] = &PendingRequest{
		DownloadID: downloadID,
		ServerID:   serverID,
		BotNick:    botNick,
	}
	e.mu.Unlock()
}

func (e *Engine) GetPendingRequest(serverID int64, botNick string) *PendingRequest {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.pendingByBot[pendingKey(serverID, botNick)]
}

func (e *Engine) loop() {
	for {
		select {
		case <-e.stopCh:
			return
		case ev, ok := <-e.eventCh:
			if !ok {
				return
			}
			if ev.Type == irc.EventIRCMessage {
				e.handleMessage(ev)
			}
		}
	}
}

func (e *Engine) handleMessage(ev irc.Event) {
	data, ok := ev.Data.(map[string]string)
	if !ok {
		return
	}

	if data["type"] != "ctcp" {
		// Check for passive DCC hint messages from bots
		if data["type"] == "notice" || data["type"] == "privmsg" {
			e.checkBotHintMessage(ev.ServerID, ev.Nick, data["message"])
		}
		return
	}

	msg := data["message"]
	offer, err := dcc.ParseDCCSend(msg)
	if err != nil {
		return // Not a DCC SEND, ignore
	}

	// Atomic read-and-remove from pending
	e.mu.Lock()
	key := pendingKey(ev.ServerID, ev.Nick)
	pending := e.pendingByBot[key]
	if pending != nil {
		delete(e.pendingByBot, key)
	}
	e.mu.Unlock()

	if pending == nil {
		log.Printf("received unexpected DCC SEND from %s — no pending request", ev.Nick)
		return
	}

	// Handle passive DCC
	if offer.Passive {
		log.Printf("passive DCC from %s — marking needs_action", ev.Nick)
		e.queue.MarkNeedsAction(pending.DownloadID, "Bot requires passive DCC. Configure passive DCC port range in settings.")
		e.bus.Publish(irc.Event{
			Type:     irc.EventNotification,
			ServerID: ev.ServerID,
			Data: map[string]string{
				"severity":    "action",
				"message":     fmt.Sprintf("Bot %s requires passive DCC — configure port range in settings", ev.Nick),
				"download_id": fmt.Sprintf("%d", pending.DownloadID),
			},
		})
		return
	}

	// Update download with actual filename and size from offer
	dl, err := e.store.GetDownload(pending.DownloadID)
	if err != nil {
		log.Printf("failed to get download %d: %v", pending.DownloadID, err)
		return
	}
	dl.Filename = offer.Filename
	dl.Filesize = offer.Size
	if err := e.store.UpdateDownload(dl); err != nil {
		log.Printf("failed to update download %d with offer details: %v", pending.DownloadID, err)
		// Non-fatal: continue with the transfer attempt even if metadata update fails
	}

	// Check disk space
	destDir := e.storageCfg.DownloadsDir // File router will move it later
	if err := os.MkdirAll(e.storageCfg.TempDir, 0755); err != nil {
		e.queue.MarkFailed(pending.DownloadID, "failed to create temp dir: "+err.Error())
		return
	}

	if err := dcc.CheckDiskSpace(destDir, offer.Size, e.storageCfg.MinFreeSpace); err != nil {
		e.queue.MarkFailed(pending.DownloadID, err.Error())
		e.bus.Publish(irc.Event{
			Type: irc.EventNotification,
			Data: map[string]string{
				"severity": "critical",
				"message":  fmt.Sprintf("Insufficient disk space for %s: %v", offer.Filename, err),
			},
		})
		return
	}

	// Check for existing .part file (resume)
	tempPath := filepath.Join(e.storageCfg.TempDir, offer.Filename+".part")
	var resumeOffset int64
	if info, err := os.Stat(tempPath); err == nil {
		resumeOffset = info.Size()
	}

	destPath := filepath.Join(e.storageCfg.TempDir, offer.Filename)

	// Start transfer in goroutine
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runTransfer(pending.DownloadID, offer, destPath, resumeOffset)
	}()
}

func (e *Engine) runTransfer(downloadID int64, offer *dcc.DCCOffer, destPath string, resumeOffset int64) {
	progressCh := make(chan dcc.TransferProgress, 64)
	tr := dcc.NewTransfer(offer, destPath, progressCh)
	if resumeOffset > 0 {
		tr.SetResumeOffset(resumeOffset)
	}

	// Progress reporting goroutine
	var progressDone sync.WaitGroup
	progressDone.Add(1)
	go func() {
		defer progressDone.Done()
		var lastFlush time.Time
		for p := range progressCh {
			// Publish to WebSocket
			e.bus.Publish(irc.Event{
				Type: irc.EventDownloadProgress,
				Data: map[string]interface{}{
					"download_id":    downloadID,
					"bytes_received": p.BytesReceived,
					"total_size":     p.TotalSize,
					"speed":          p.Speed,
					"peak_speed":     p.PeakSpeed,
					"average_speed":  p.AverageSpeed,
					"percentage":     p.Percentage,
				},
			})

			// Flush to DB every 5 seconds
			if time.Since(lastFlush) >= 5*time.Second {
				e.queue.UpdateProgress(downloadID, p.BytesReceived, p.PeakSpeed, p.AverageSpeed)
				lastFlush = time.Now()
			}
		}
	}()

	err := tr.Start()
	progressDone.Wait() // drain progress before marking terminal state
	if err != nil {
		log.Printf("transfer failed for download %d: %v", downloadID, err)
		e.queue.MarkFailed(downloadID, err.Error())
		e.bus.Publish(irc.Event{
			Type: irc.EventDownloadStatus,
			Data: map[string]interface{}{
				"download_id": downloadID,
				"status":      "failed",
				"error":       err.Error(),
			},
		})
		return
	}

	e.queue.MarkCompleted(downloadID, destPath, tr.PeakSpeed(), tr.AverageSpeed())

	// Apply file routing rules
	finalPath := destPath
	if rules, err := e.store.GetFileRoutingRules(); err == nil {
		if destDir := routing.MatchRule(offer.Filename, rules); destDir != "" {
			if moved, err := routing.MoveFile(destPath, destDir); err != nil {
				log.Printf("routing move failed for download %d: %v", downloadID, err)
			} else {
				finalPath = moved
			}
		}
	} else {
		log.Printf("failed to load routing rules for download %d: %v", downloadID, err)
	}

	// Run post-download hooks
	dl, dlErr := e.store.GetDownload(downloadID)
	var hooks []db.PostHook
	globalHooks, err := e.store.GetPostHooks("", nil)
	if err != nil {
		log.Printf("failed to load global hooks for download %d: %v", downloadID, err)
	} else {
		hooks = append(hooks, globalHooks...)
	}
	if dlErr == nil && dl != nil {
		serverHooks, err := e.store.GetPostHooks("server", &dl.ServerID)
		if err != nil {
			log.Printf("failed to load server hooks for download %d: %v", downloadID, err)
		} else {
			hooks = append(hooks, serverHooks...)
		}
	}
	hCtx := routing.HookContext{
		FilePath: finalPath,
		Filename: offer.Filename,
		Filesize: offer.Size,
	}
	if dlErr == nil && dl != nil {
		hCtx.BotNick = dl.BotNick
		hCtx.Channel = dl.Channel
		hCtx.Pack = dl.PackNumber
		if srv, err := e.store.GetServer(dl.ServerID); err == nil && srv != nil {
			hCtx.Server = srv.Host
		} else {
			hCtx.Server = fmt.Sprintf("%d", dl.ServerID)
		}
	}
	for _, hook := range hooks {
		if !hook.Enabled {
			continue
		}
		result := routing.RunHook(hook, hCtx)
		if result.Error != "" {
			log.Printf("hook %q failed for download %d: %s", hook.Name, downloadID, result.Error)
		}
		if result.NewPath != "" {
			finalPath = result.NewPath
			hCtx.FilePath = finalPath
		}
	}

	e.bus.Publish(irc.Event{
		Type: irc.EventDownloadStatus,
		Data: map[string]interface{}{
			"download_id": downloadID,
			"status":      "completed",
			"path":        finalPath,
			"peak_speed":  tr.PeakSpeed(),
			"avg_speed":   tr.AverageSpeed(),
		},
	})
}

func (e *Engine) checkBotHintMessage(serverID int64, botNick, message string) {
	hints := []string{"passive", "firewall", "can't connect", "dcc rejected", "unable to connect"}
	msgLower := strings.ToLower(message)
	for _, hint := range hints {
		if strings.Contains(msgLower, hint) {
			pending := e.GetPendingRequest(serverID, botNick)
			if pending != nil {
				e.queue.MarkNeedsAction(pending.DownloadID,
					fmt.Sprintf("Bot message: %s", message))
				e.bus.Publish(irc.Event{
					Type:     irc.EventNotification,
					ServerID: serverID,
					Data: map[string]string{
						"severity": "action",
						"message":  fmt.Sprintf("Bot %s says: %s", botNick, message),
					},
				})
			}
			break
		}
	}
}

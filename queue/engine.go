package queue

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maxwell-xirc/xirc/config"
	"github.com/maxwell-xirc/xirc/dcc"
	"github.com/maxwell-xirc/xirc/db"
	"github.com/maxwell-xirc/xirc/irc"
)

type PendingRequest struct {
	DownloadID int64
	ServerID   int64
	BotNick    string
}

type Engine struct {
	mu              sync.RWMutex
	queue           *Queue
	store           db.Store
	bus             *irc.EventBus
	storageCfg      *config.StorageConfig
	eventCh         <-chan irc.Event
	stopCh          chan struct{}
	pendingByBot    map[string]*PendingRequest // key: "serverID:botNick"
	activeTransfers map[int64]chan struct{}     // key: downloadID, value: cancel channel
}

func NewEngine(store db.Store, bus *irc.EventBus, storageCfg *config.StorageConfig, maxConcurrent int) *Engine {
	return &Engine{
		queue:           New(store, maxConcurrent),
		store:           store,
		bus:             bus,
		storageCfg:      storageCfg,
		stopCh:          make(chan struct{}),
		pendingByBot:    make(map[string]*PendingRequest),
		activeTransfers: make(map[int64]chan struct{}),
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

	e.mu.Lock()
	for _, cancel := range e.activeTransfers {
		close(cancel)
	}
	e.mu.Unlock()
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
	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()

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
		case <-progressTicker.C:
			// Periodic progress flush is handled per-transfer
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

	pending := e.GetPendingRequest(ev.ServerID, ev.Nick)
	if pending == nil {
		log.Printf("received unexpected DCC SEND from %s — no pending request", ev.Nick)
		return
	}

	// Remove from pending
	e.mu.Lock()
	delete(e.pendingByBot, pendingKey(ev.ServerID, ev.Nick))
	e.mu.Unlock()

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
	e.store.UpdateDownload(dl)

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
	go e.runTransfer(pending.DownloadID, offer, destPath, resumeOffset)
}

func (e *Engine) runTransfer(downloadID int64, offer *dcc.DCCOffer, destPath string, resumeOffset int64) {
	progressCh := make(chan dcc.TransferProgress, 64)
	tr := dcc.NewTransfer(offer, destPath, progressCh)
	if resumeOffset > 0 {
		tr.SetResumeOffset(resumeOffset)
	}

	cancelCh := make(chan struct{})
	e.mu.Lock()
	e.activeTransfers[downloadID] = cancelCh
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.activeTransfers, downloadID)
		e.mu.Unlock()
	}()

	// Progress reporting goroutine
	go func() {
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
	e.bus.Publish(irc.Event{
		Type: irc.EventDownloadStatus,
		Data: map[string]interface{}{
			"download_id": downloadID,
			"status":      "completed",
			"path":        destPath,
			"peak_speed":  tr.PeakSpeed(),
			"avg_speed":   tr.AverageSpeed(),
		},
	})
}

func (e *Engine) checkBotHintMessage(serverID int64, botNick, message string) {
	hints := []string{"passive", "firewall", "can't connect", "dcc rejected", "unable to connect"}
	msgLower := toLower(message)
	for _, hint := range hints {
		if contains(msgLower, hint) {
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

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

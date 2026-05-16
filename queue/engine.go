package queue

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RealDtx/maxwell-irc/config"
	"github.com/RealDtx/maxwell-irc/db"
	"github.com/RealDtx/maxwell-irc/dcc"
	"github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/routing"
)

type PendingRequest struct {
	DownloadID int64
	ServerID   int64
	BotNick    string
}

type Engine struct {
	mu                 sync.RWMutex
	wg                 sync.WaitGroup
	queue              *Queue
	store              db.Store
	bus                *irc.EventBus
	ircMgr             *irc.Manager
	storageCfg         *config.StorageConfig
	eventCh            <-chan irc.Event
	stopCh             chan struct{}
	pendingByBot       map[string]*PendingRequest // key: "serverID:botNick"
	transferMu         sync.Mutex
	activeTransfers    map[int64]*dcc.Transfer
	cancelledTransfers map[int64]bool
	activeDestPaths    map[string]bool // base filenames currently being transferred
}

func NewEngine(store db.Store, bus *irc.EventBus, ircMgr *irc.Manager, storageCfg *config.StorageConfig, maxConcurrent int) *Engine {
	return &Engine{
		queue:              New(store, maxConcurrent),
		store:              store,
		bus:                bus,
		ircMgr:             ircMgr,
		storageCfg:         storageCfg,
		stopCh:             make(chan struct{}),
		pendingByBot:       make(map[string]*PendingRequest),
		activeTransfers:    make(map[int64]*dcc.Transfer),
		cancelledTransfers: make(map[int64]bool),
		activeDestPaths:    make(map[string]bool),
	}
}

// CancelTransfer interrupts an active TCP transfer for the given download ID, if one
// is running. It is a no-op when no transfer is active (e.g. download is still queued).
func (e *Engine) CancelTransfer(downloadID int64) {
	e.transferMu.Lock()
	e.cancelledTransfers[downloadID] = true
	tr := e.activeTransfers[downloadID]
	e.transferMu.Unlock()
	if tr != nil {
		tr.Cancel()
	}
}

func (e *Engine) Queue() *Queue {
	return e.queue
}

func (e *Engine) Start() {
	if err := e.queue.RequeueInterrupted(); err != nil {
		log.Printf("warning: failed to requeue interrupted downloads: %v", err)
	}
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

// Dispatch sends the XDCC request for a queued download and registers it as pending.
// If the IRC connection is not available, the download remains "queued" for manual retry.
func (e *Engine) Dispatch(dl *db.Download) error {
	if e.ircMgr == nil {
		return fmt.Errorf("no IRC manager configured")
	}
	conn := e.ircMgr.GetConnection(dl.ServerID)
	if conn == nil {
		return fmt.Errorf("no IRC connection for server %d", dl.ServerID)
	}

	// Register before sending to avoid race if bot responds immediately.
	e.RegisterPendingRequest(dl.ID, dl.ServerID, dl.BotNick)
	if err := conn.RequestPack(dl.Channel, dl.BotNick, dl.PackNumber); err != nil {
		e.mu.Lock()
		delete(e.pendingByBot, pendingKey(dl.ServerID, dl.BotNick))
		e.mu.Unlock()
		return err
	}
	return nil
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

	// Update download with actual filename, size, and status from offer
	dl, err := e.store.GetDownload(pending.DownloadID)
	if err != nil {
		log.Printf("failed to get download %d: %v", pending.DownloadID, err)
		return
	}
	dl.Filename = offer.Filename
	dl.Filesize = offer.Size
	dl.Status = "downloading"
	startedAt := time.Now()
	dl.StartedAt = &startedAt
	if err := e.store.UpdateDownload(dl); err != nil {
		log.Printf("failed to update download %d with offer details: %v", pending.DownloadID, err)
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

	e.transferMu.Lock()
	destPath := e.uniqueDestPathLocked(offer.Filename)
	e.activeDestPaths[filepath.Base(destPath)] = true
	e.transferMu.Unlock()

	// Check for existing .part file (resume) using the unique destPath
	var resumeOffset int64
	if info, err := os.Stat(destPath + ".part"); err == nil {
		resumeOffset = info.Size()
	}

	// Start transfer in goroutine
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runTransfer(pending.DownloadID, offer, destPath, resumeOffset)
	}()
}

// uniqueDestPathLocked returns a unique destination path for filename, inserting
// a numeric counter before the extension when the plain name is already in use
// (active transfer or existing final file on disk). Must be called with transferMu held.
func (e *Engine) uniqueDestPathLocked(filename string) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 0; ; i++ {
		var name string
		if i == 0 {
			name = filename
		} else {
			name = fmt.Sprintf("%s.%d%s", base, i, ext)
		}
		candidate := filepath.Join(e.storageCfg.TempDir, name)
		if e.activeDestPaths[name] {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			continue // final file already exists at this path
		}
		return candidate
	}
}

func (e *Engine) runTransfer(downloadID int64, offer *dcc.DCCOffer, destPath string, resumeOffset int64) {
	progressCh := make(chan dcc.TransferProgress, 64)
	tr := dcc.NewTransfer(offer, destPath, progressCh)
	if resumeOffset > 0 {
		tr.SetResumeOffset(resumeOffset)
	}

	// Register the active transfer so CancelTransfer can reach it.
	e.transferMu.Lock()
	e.activeTransfers[downloadID] = tr
	e.transferMu.Unlock()
	defer func() {
		e.transferMu.Lock()
		delete(e.activeTransfers, downloadID)
		delete(e.cancelledTransfers, downloadID)
		delete(e.activeDestPaths, filepath.Base(destPath))
		e.transferMu.Unlock()
	}()

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
		// If the transfer was cancelled, the DB is already marked "cancelled" by
		// CancelTransfer/Queue.Cancel — don't overwrite with "failed".
		if errors.Is(err, dcc.ErrCancelled) {
			log.Printf("transfer cancelled for download %d", downloadID)
			return
		}
		log.Printf("transfer failed for download %d: %v", downloadID, err)
		e.queue.MarkFailed(downloadID, err.Error())
		if dl, dlErr := e.store.GetDownload(downloadID); dlErr == nil && dl != nil {
			now := time.Now()
			stat := &db.DownloadStat{
				Filename:    dl.Filename,
				SizeBytes:   dl.Filesize,
				ServerID:    dl.ServerID,
				Channel:     dl.Channel,
				BotNick:     dl.BotNick,
				PackNumber:  dl.PackNumber,
				StartedAt:   dl.StartedAt,
				CompletedAt: &now,
				Status:      dl.Status,
				StatsOnly:   dl.StatsOnly,
			}
			if recordErr := e.store.CreateDownloadStat(stat); recordErr != nil {
				log.Printf("warning: could not record download stat: %v", recordErr)
			}
		}
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

	// Record download stat
	if dl, dlErr := e.store.GetDownload(downloadID); dlErr == nil && dl != nil {
		now := time.Now()
		stat := &db.DownloadStat{
			Filename:    dl.Filename,
			SizeBytes:   dl.Filesize,
			ServerID:    dl.ServerID,
			Channel:     dl.Channel,
			BotNick:     dl.BotNick,
			PackNumber:  dl.PackNumber,
			StartedAt:   dl.StartedAt,
			CompletedAt: &now,
			Status:      dl.Status,
			StatsOnly:   dl.StatsOnly,
		}
		if dl.StatsOnly && dl.Status == "completed" {
			os.Remove(dl.DestinationPath)
			stat.Status = "stats_only"
		}
		if recordErr := e.store.CreateDownloadStat(stat); recordErr != nil {
			log.Printf("warning: could not record download stat: %v", recordErr)
		}
	}

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

	// Auto-extract tar archives and route/hook each extracted file
	if dlErr == nil && dl != nil && dl.AutoExtract && routing.IsArchive(offer.Filename) {
		extractDir := filepath.Dir(finalPath)

		// Check available disk space before attempting extraction
		var skipExtract bool
		if info, statErr := os.Stat(finalPath); statErr == nil {
			var fsStat syscall.Statfs_t
			if fsErr := syscall.Statfs(extractDir, &fsStat); fsErr == nil {
				available := fsStat.Bavail * uint64(fsStat.Bsize)
				needed := uint64(info.Size())
				if available < needed {
					log.Printf("auto-extract skipped for download %d (%s): insufficient disk space (need %d, have %d)", downloadID, offer.Filename, needed, available)
					spaceErr := fmt.Errorf("insufficient disk space for extraction: need %d bytes, have %d bytes available", needed, available)
					if markErr := e.queue.SetExtractionError(downloadID, spaceErr.Error()); markErr != nil {
						log.Printf("could not record extraction error for download %d: %v", downloadID, markErr)
					}
					skipExtract = true
				}
			}
		}
		if !skipExtract {
			extracted, extractErr := routing.Extract(finalPath, extractDir)
			if extractErr != nil {
				log.Printf("auto-extract failed for download %d (%s): %v", downloadID, offer.Filename, extractErr)
				errMsg := fmt.Sprintf("extraction failed: %v", extractErr)
				if markErr := e.queue.SetExtractionError(downloadID, errMsg); markErr != nil {
					log.Printf("auto-extract: could not record extraction error for download %d: %v", downloadID, markErr)
				}
				// Archive remains untouched. Fall through to the completed WS event.
			} else {
				extractRules, _ := e.store.GetFileRoutingRules()
				for _, ef := range extracted {
					efName := filepath.Base(ef)
					efFinal := ef
					if len(extractRules) > 0 {
						if destDir := routing.MatchRule(efName, extractRules); destDir != "" {
							if moved, err := routing.MoveFile(ef, destDir); err != nil {
								log.Printf("routing extracted file %q: %v", efName, err)
							} else {
								efFinal = moved
							}
						}
					}
					// If the file wasn't routed out of extractDir, flatten it to extractDir
					// so that subdirectories created by tar don't linger.
					if efFinal == ef && filepath.Dir(efFinal) != extractDir {
						flat := filepath.Join(extractDir, efName)
						if err := os.Rename(efFinal, flat); err == nil {
							efFinal = flat
						}
					}
					efCtx := hCtx
					efCtx.FilePath = efFinal
					efCtx.Filename = efName
					if info, err := os.Stat(efFinal); err == nil {
						efCtx.Filesize = info.Size()
					}
					for _, hook := range hooks {
						if !hook.Enabled {
							continue
						}
						result := routing.RunHook(hook, efCtx)
						if result.Error != "" {
							log.Printf("hook %q failed for extracted file %q: %s", hook.Name, efName, result.Error)
						}
					}
				}
				// Remove subdirectories created by tar — all files have been routed
				// or flattened, so these should now be empty.
				routing.RemoveEmptyDirs(extractDir)

				if e.storageCfg != nil && e.storageCfg.AutoExtract.DeleteArchive {
					log.Printf("auto-extract: deleting archive %q after successful extraction", finalPath)
					if err := os.Remove(finalPath); err != nil {
						log.Printf("auto-extract: failed to remove archive %q: %v", finalPath, err)
					}
				}
			}
		}
	}

	// Update destination_path in DB to the final routed location
	if finalPath != destPath {
		if err := e.queue.UpdateDestinationPath(downloadID, finalPath); err != nil {
			log.Printf("failed to update destination path for download %d: %v", downloadID, err)
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

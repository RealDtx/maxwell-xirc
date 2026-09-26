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
	"github.com/RealDtx/maxwell-irc/fscheck"
	"github.com/RealDtx/maxwell-irc/irc"
	"github.com/RealDtx/maxwell-irc/library"
	"github.com/RealDtx/maxwell-irc/routing"
)

type PendingRequest struct {
	DownloadID int64
	ServerID   int64
	BotNick    string
	CreatedAt  time.Time
}

// pendingRequestTimeout is how long a dispatched XDCC request may wait for a
// DCC SEND before the download is failed and the pending slot freed. Bots
// legitimately queue requests when their slots are full, so this is generous.
// ponytail: fixed 1h; make configurable if a realm's bots queue longer.
const pendingRequestTimeout = time.Hour

type Engine struct {
	mu                 sync.RWMutex
	wg                 sync.WaitGroup
	queue              *Queue
	store              db.Store
	bus                *irc.EventBus
	ircMgr             *irc.Manager
	storageCfg         *config.StorageConfig
	library            *library.Manager
	eventCh            <-chan irc.Event
	stopCh             chan struct{}
	pendingByBot       map[string]*PendingRequest // key: "serverID:botNick"
	maxConcurrent      int
	pumpMu             sync.Mutex // serializes TryDispatchQueued
	transferMu         sync.Mutex
	activeTransfers    map[int64]*dcc.Transfer
	cancelledTransfers map[int64]bool
	activeDestPaths    map[string]bool // base filenames currently being transferred
}

func NewEngine(store db.Store, bus *irc.EventBus, ircMgr *irc.Manager, storageCfg *config.StorageConfig, maxConcurrent int) *Engine {
	return &Engine{
		queue:              New(store, maxConcurrent),
		maxConcurrent:      maxConcurrent,
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

// SetLibrary wires the library manager in — set once at startup, before
// Start(), so no locking is needed to read it from runTransfer.
func (e *Engine) SetLibrary(m *library.Manager) {
	e.library = m
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
		CreatedAt:  time.Now(),
	}
	e.mu.Unlock()
}

// expirePendingRequests fails downloads whose XDCC request never got a DCC
// SEND within pendingRequestTimeout, freeing the per-bot pending slot.
func (e *Engine) expirePendingRequests() {
	now := time.Now()
	var expired []*PendingRequest
	e.mu.Lock()
	for key, pending := range e.pendingByBot {
		if now.Sub(pending.CreatedAt) > pendingRequestTimeout {
			delete(e.pendingByBot, key)
			expired = append(expired, pending)
		}
	}
	e.mu.Unlock()

	for _, pending := range expired {
		msg := fmt.Sprintf("no response from bot %s within %s", pending.BotNick, pendingRequestTimeout)
		log.Printf("expiring pending XDCC request for download %d: %s", pending.DownloadID, msg)
		if err := e.queue.MarkFailed(pending.DownloadID, msg); err != nil {
			log.Printf("failed to mark download %d failed: %v", pending.DownloadID, err)
		}
		e.bus.Publish(irc.Event{
			Type:     irc.EventNotification,
			ServerID: pending.ServerID,
			Data: map[string]string{
				"severity": "warning",
				"message":  fmt.Sprintf("Download timed out: %s", msg),
			},
		})
	}
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

// selectDispatchable returns the queued downloads to dispatch now, oldest
// first. Capacity is maxConcurrent minus inFlight (active transfers plus
// pending XDCC requests), and each bot gets at most one in-flight request.
// queued is expected newest-first (as GetDownloads returns it); busy is
// keyed by pendingKey(serverID, botNick) and is extended in place.
func selectDispatchable(queued []db.Download, busy map[string]bool, inFlight, maxConcurrent int) []*db.Download {
	var out []*db.Download
	for i := len(queued) - 1; i >= 0 && inFlight < maxConcurrent; i-- {
		dl := queued[i]
		key := pendingKey(dl.ServerID, dl.BotNick)
		if busy[key] {
			continue
		}
		busy[key] = true
		inFlight++
		out = append(out, &dl)
	}
	return out
}

// TryDispatchQueued sends XDCC requests for queued downloads while capacity
// allows. It is called whenever a slot may have freed (transfer finished,
// download cancelled/retried/requested) and from the minute sweep as a
// safety net. Downloads whose dispatch fails (e.g. IRC not connected) stay
// "queued" and are retried by the next pump.
func (e *Engine) TryDispatchQueued() {
	if e.ircMgr == nil {
		return
	}
	e.pumpMu.Lock()
	defer e.pumpMu.Unlock()

	e.mu.Lock()
	busy := make(map[string]bool, len(e.pendingByBot))
	for key := range e.pendingByBot {
		busy[key] = true
	}
	e.mu.Unlock()
	inFlight := len(busy)

	active, err := e.store.GetDownloads("downloading")
	if err != nil {
		log.Printf("queue pump: listing active downloads: %v", err)
		return
	}
	for _, dl := range active {
		key := pendingKey(dl.ServerID, dl.BotNick)
		if !busy[key] {
			busy[key] = true
			inFlight++
		}
	}
	if inFlight >= e.maxConcurrent {
		return
	}

	queued, err := e.store.GetDownloads("queued")
	if err != nil {
		log.Printf("queue pump: listing queued downloads: %v", err)
		return
	}

	for _, dl := range selectDispatchable(queued, busy, inFlight, e.maxConcurrent) {
		if err := e.Dispatch(dl); err != nil {
			log.Printf("queue pump: dispatch download %d (%s pack %d): %v",
				dl.ID, dl.BotNick, dl.PackNumber, err)
		}
	}
}

func (e *Engine) loop() {
	sweep := time.NewTicker(time.Minute)
	defer sweep.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-sweep.C:
			e.expirePendingRequests()
			// Safety-net pump: catches slots freed by paths without an
			// explicit pump call and downloads queued while IRC was down.
			e.TryDispatchQueued()
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
	// Reconcile the file index with reality: the bot's offer is authoritative
	// for what this pack actually contains. The upsert also evicts any stale
	// entry that mapped this bot+pack to a different (expected) filename.
	if dl.PackNumber > 0 {
		if err := e.store.UpsertIndexedFile(&db.IndexedFile{
			ServerID:   dl.ServerID,
			Channel:    dl.Channel,
			BotNick:    ev.Nick,
			PackNumber: &dl.PackNumber,
			Filename:   offer.Filename,
			Filesize:   humanSizePtr(offer.Size),
			RawLine:    msg,
		}); err != nil {
			log.Printf("failed to reconcile index for %s pack %d: %v", ev.Nick, dl.PackNumber, err)
		}
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
		// This transfer freed a slot (completed, failed, or cancelled) —
		// start the next queued download if any.
		e.TryDispatchQueued()
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

	// The DCC transfer is done. Mark "processing" (not counted as active, so
	// the freed slot can be reused immediately) and publish it so the UI
	// stops showing "downloading" while the move/extraction run.
	if err := e.queue.MarkProcessing(downloadID, destPath); err != nil {
		log.Printf("failed to mark download %d processing: %v", downloadID, err)
	}
	e.bus.Publish(irc.Event{
		Type: irc.EventDownloadStatus,
		Data: map[string]interface{}{
			"download_id": downloadID,
			"status":      "processing",
			"phase":       "moving",
		},
	})
	e.TryDispatchQueued()

	// Loaded once, up front: destination (target_dir/auto_subdir) and
	// extraction (auto_extract) both key off this row, and the stats-only
	// early-out below needs it too.
	dl, dlErr := e.store.GetDownload(downloadID)
	if dlErr != nil {
		log.Printf("failed to load download %d for post-processing: %v", downloadID, dlErr)
	}

	finalPath := destPath

	// Stats-only downloads never had a real file to keep: remove the temp
	// file and skip the move/extraction entirely rather than running them
	// against a path that no longer exists.
	if dlErr == nil && dl != nil && dl.StatsOnly {
		os.Remove(destPath)
		e.finishTransfer(downloadID, dl, finalPath, "stats_only", tr)
		return
	}

	// Pick the destination directory: an explicit per-download target wins;
	// else the library categorizes it when auto-organize is on; otherwise
	// it drops flat into downloads_dir, unmatched.
	destDir := ""
	var matchedCat *library.Category
	if dlErr == nil && dl != nil && dl.TargetDir != "" {
		destDir = dl.TargetDir
	} else if e.library != nil && dlErr == nil && dl != nil && dl.AutoSubdir {
		libCfg := e.library.Get()
		if libCfg.AutoOrganize {
			if dir, cat, _, ok := library.Resolve(&libCfg, offer.Filename); ok {
				destDir, matchedCat = dir, cat
			}
		}
	}
	if destDir == "" {
		destDir = e.downloadsDir()
	}
	// A failed move must not leave the file silently in the hidden temp dir:
	// fall back to the downloads dir and record why on the download.
	moveNote := ""
	if destDir != "" {
		if moved, err := routing.MoveFile(destPath, destDir); err != nil {
			log.Printf("routing move failed for download %d: %v", downloadID, err)
			moveNote = fmt.Sprintf("could not move to %s: %v", destDir, fscheck.Describe(err, destDir))
			if fallback := e.downloadsDir(); fallback != "" && filepath.Clean(fallback) != filepath.Clean(destDir) {
				if moved, ferr := routing.MoveFile(destPath, fallback); ferr == nil {
					finalPath = moved
					moveNote += "; saved to " + fallback + " instead"
				} else {
					moveNote += fmt.Sprintf("; fallback to %s failed too (%v), file left at %s", fallback, ferr, destPath)
				}
			} else {
				moveNote += ", file left at " + destPath
			}
			e.bus.Publish(irc.Event{
				Type: irc.EventNotification,
				Data: map[string]string{
					"severity": "warning",
					"message":  fmt.Sprintf("%s: %s", offer.Filename, moveNote),
				},
			})
		} else {
			finalPath = moved
		}
	}

	// Auto-extract flattens the archive's files directly into the resolved
	// folder. Only a matched library category can turn this on — an
	// unmatched file (dropped flat into downloads_dir) is never extracted —
	// and the category's auto_extract is further gated by the download's
	// own opt-out.
	autoExtract := matchedCat != nil && matchedCat.AutoExtract && dlErr == nil && dl != nil && dl.AutoExtract
	if autoExtract && routing.IsArchive(offer.Filename) {
		e.bus.Publish(irc.Event{
			Type: irc.EventDownloadStatus,
			Data: map[string]interface{}{
				"download_id": downloadID,
				"status":      "processing",
				"phase":       "extracting",
			},
		})
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
					e.bus.Publish(irc.Event{
						Type: irc.EventNotification,
						Data: map[string]string{
							"severity": "warning",
							"message":  fmt.Sprintf("Extraction skipped for %s: insufficient disk space (need %d MB, have %d MB)", offer.Filename, needed/1024/1024, available/1024/1024),
						},
					})
					skipExtract = true
				}
			}
		}
		if !skipExtract {
			var extracted []string
			var extractErr error
			if library.IsArchive(offer.Filename) {
				extracted, extractErr = library.Extract(finalPath, extractDir)
			} else {
				extracted, extractErr = routing.Extract(finalPath, extractDir)
			}
			if extractErr != nil {
				log.Printf("auto-extract failed for download %d (%s): %v", downloadID, offer.Filename, extractErr)
				e.bus.Publish(irc.Event{
					Type: irc.EventNotification,
					Data: map[string]string{
						"severity": "warning",
						"message":  fmt.Sprintf("Extraction failed for %s: %v", offer.Filename, extractErr),
					},
				})
				// Archive remains untouched. Download stays completed.
			} else {
				// The download is already in its resolved category folder —
				// extracted files are just flattened directly into it.
				for _, ef := range extracted {
					flattenExtracted(ef, extractDir)
				}
				// Remove subdirectories left by extraction — all files have
				// been flattened, so these should now be empty.
				routing.RemoveEmptyDirs(extractDir)

				if matchedCat.DeleteArchive {
					log.Printf("auto-extract: deleting archive %q after successful extraction", finalPath)
					if err := os.Remove(finalPath); err != nil {
						log.Printf("auto-extract: failed to remove archive %q: %v", finalPath, err)
					}
				}
			}
		}
	}

	if moveNote != "" { // before finishTransfer, so the "completed" reload shows it
		if err := e.queue.SetMessage(downloadID, moveNote); err != nil {
			log.Printf("failed to record move note for download %d: %v", downloadID, err)
		}
	}
	e.finishTransfer(downloadID, dl, finalPath, "completed", tr)
}

// finishTransfer marks a download completed at finalPath, records its
// DownloadStat (statStatus is normally "completed", or "stats_only" when the
// temp file was removed instead of routed), and publishes the "completed"
// WebSocket event. dl may be nil if the row failed to load; the stat is then
// skipped since there's nothing to record from.
func (e *Engine) finishTransfer(downloadID int64, dl *db.Download, finalPath, statStatus string, tr *dcc.Transfer) {
	if err := e.queue.MarkCompleted(downloadID, finalPath, tr.PeakSpeed(), tr.AverageSpeed()); err != nil {
		log.Printf("failed to mark download %d completed: %v", downloadID, err)
	}

	if dl != nil {
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
			Status:      statStatus,
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
			"status":      "completed",
			"path":        finalPath,
			"peak_speed":  tr.PeakSpeed(),
			"avg_speed":   tr.AverageSpeed(),
		},
	})
}

func (e *Engine) downloadsDir() string {
	if e.storageCfg == nil {
		return ""
	}
	return e.storageCfg.DownloadsDir
}

// flattenExtracted moves ef (a file pulled out of an archive, possibly still
// inside a subdirectory the archive created) directly into extractDir, so
// nothing from inside the archive lingers in a subfolder.
func flattenExtracted(ef, extractDir string) {
	if filepath.Dir(ef) == extractDir {
		return
	}
	dst := filepath.Join(extractDir, filepath.Base(ef))
	if err := os.Rename(ef, dst); err != nil {
		log.Printf("flattening extracted file %q: %v", ef, err)
	}
}

// humanSizePtr formats a byte count as a short human-readable size string
// (matching the "1.4G" style bots use in advertisements).
func humanSizePtr(bytes int64) *string {
	units := []string{"B", "K", "M", "G", "T"}
	v := float64(bytes)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	s := fmt.Sprintf("%.1f%s", v, units[i])
	return &s
}

func (e *Engine) checkBotHintMessage(serverID int64, botNick, message string) {
	msgLower := strings.ToLower(message)

	// The bot says the requested pack doesn't exist — the index entry that led
	// here is stale. Fail the download and evict the bot+pack from the index.
	for _, hint := range []string{"invalid pack", "no such pack", "pack does not exist"} {
		if !strings.Contains(msgLower, hint) {
			continue
		}
		e.mu.Lock()
		key := pendingKey(serverID, botNick)
		pending := e.pendingByBot[key]
		if pending != nil {
			delete(e.pendingByBot, key)
		}
		e.mu.Unlock()
		if pending == nil {
			return
		}
		e.queue.MarkFailed(pending.DownloadID, fmt.Sprintf("Bot %s: %s", botNick, message))
		if dl, err := e.store.GetDownload(pending.DownloadID); err == nil && dl.PackNumber > 0 {
			if err := e.store.EvictStaleIndexedFiles(dl.ServerID, botNick, dl.PackNumber, ""); err != nil {
				log.Printf("failed to evict stale index entry for %s pack %d: %v", botNick, dl.PackNumber, err)
			}
		}
		e.bus.Publish(irc.Event{
			Type:     irc.EventNotification,
			ServerID: serverID,
			Data: map[string]string{
				"severity": "warning",
				"message":  fmt.Sprintf("Bot %s says: %s — removed stale index entry", botNick, message),
			},
		})
		return
	}

	hints := []string{"passive", "firewall", "can't connect", "dcc rejected", "unable to connect"}
	for _, hint := range hints {
		if strings.Contains(msgLower, hint) {
			// Consume the pending slot — needs_action is terminal until the
			// user intervenes, so don't leave it to the expiry sweeper.
			e.mu.Lock()
			key := pendingKey(serverID, botNick)
			pending := e.pendingByBot[key]
			if pending != nil {
				delete(e.pendingByBot, key)
			}
			e.mu.Unlock()
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

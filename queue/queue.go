package queue

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RealDtx/maxwell-irc/db"
)

type Queue struct {
	mu            sync.Mutex
	store         db.Store
	maxConcurrent int
}

func New(store db.Store, maxConcurrent int) *Queue {
	return &Queue{
		store:         store,
		maxConcurrent: maxConcurrent,
	}
}

func (q *Queue) Add(serverID int64, channel, botNick string, packNumber int, filename string, filesize int64, statsOnly bool, autoExtract bool, autoSubdir bool) (*db.Download, error) {
	dl := &db.Download{
		ServerID:    serverID,
		Channel:     channel,
		BotNick:     botNick,
		PackNumber:  packNumber,
		Filename:    filename,
		Filesize:    filesize,
		Status:      "queued",
		StatsOnly:   statsOnly,
		AutoExtract: autoExtract,
		AutoSubdir:  autoSubdir,
	}

	if err := q.store.CreateDownload(dl); err != nil {
		return nil, fmt.Errorf("creating download: %w", err)
	}

	return dl, nil
}

// NextAndMarkDownloading returns the next queued download to process and atomically
// marks it as downloading under the queue lock. This prevents TOCTOU races where
// two callers could both get the same download from Next() and start it.
// Returns nil if no download is available (max concurrent reached or queue empty).
func (q *Queue) NextAndMarkDownloading() (*db.Download, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	active, err := q.store.GetDownloads("downloading")
	if err != nil {
		return nil, err
	}
	if len(active) >= q.maxConcurrent {
		return nil, nil
	}

	activeBots := make(map[string]bool)
	for _, dl := range active {
		activeBots[dl.BotNick] = true
	}

	queued, err := q.store.GetDownloads("queued")
	if err != nil {
		return nil, err
	}

	for i := len(queued) - 1; i >= 0; i-- {
		dl := queued[i]
		if activeBots[dl.BotNick] {
			continue
		}
		// Atomically mark as downloading
		now := time.Now()
		dl.Status = "downloading"
		dl.StartedAt = &now
		if err := q.store.UpdateDownload(&dl); err != nil {
			return nil, err
		}
		return &dl, nil
	}

	return nil, nil
}

// MarkProcessing marks a download as having finished its DCC transfer and
// entered post-processing (moving/hooks/extraction). It is not counted as
// "downloading" by NextAndMarkDownloading, so the freed slot can be reused
// immediately while post-processing continues in the background.
func (q *Queue) MarkProcessing(id int64, destPath string) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Status = "processing"
	dl.DestinationPath = destPath
	dl.DownloadedBytes = dl.Filesize
	return q.store.UpdateDownload(dl)
}

func (q *Queue) MarkCompleted(id int64, destPath string, peakSpeed, avgSpeed int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	now := time.Now()
	dl.Status = "completed"
	dl.CompletedAt = &now
	dl.DestinationPath = destPath
	dl.DownloadedBytes = dl.Filesize
	dl.PeakSpeed = peakSpeed
	dl.AverageSpeed = avgSpeed
	return q.store.UpdateDownload(dl)
}

func (q *Queue) MarkFailed(id int64, errMsg string) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Status = "failed"
	dl.ErrorMessage = errMsg
	return q.store.UpdateDownload(dl)
}

// SetMessage records a note on a download without changing its status,
// e.g. a completed download whose file could not be moved where intended.
func (q *Queue) SetMessage(id int64, msg string) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.ErrorMessage = msg
	return q.store.UpdateDownload(dl)
}

func (q *Queue) MarkNeedsAction(id int64, msg string) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Status = "needs_action"
	dl.ErrorMessage = msg
	return q.store.UpdateDownload(dl)
}

func (q *Queue) Cancel(id int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Status = "cancelled"
	return q.store.UpdateDownload(dl)
}

func (q *Queue) Retry(id int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.Status = "queued"
	dl.ErrorMessage = ""
	dl.DownloadedBytes = 0
	dl.StartedAt = nil
	dl.CompletedAt = nil
	return q.store.UpdateDownload(dl)
}

func (q *Queue) MoveToFront(id int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}

	floor := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	queued, err := q.store.GetDownloads("queued")
	if err != nil {
		return err
	}

	// Find the minimum created_at among other queued items
	minTime := time.Now()
	hasOthers := false
	for _, item := range queued {
		if item.ID != id {
			if !hasOthers || item.CreatedAt.Before(minTime) {
				minTime = item.CreatedAt
				hasOthers = true
			}
		}
	}

	var target time.Time
	if !hasOthers {
		target = floor
	} else {
		candidate := minTime.Add(-time.Second)
		if candidate.Before(floor) {
			// Note: if multiple items are promoted and both reach the floor,
			// their ordering becomes non-deterministic. In practice this requires
			// as many MoveToFront calls as there are seconds since year 2000.
			target = floor
		} else {
			target = candidate
		}
	}

	dl.CreatedAt = target
	return q.store.UpdateDownload(dl)
}

func (q *Queue) UpdateDestinationPath(id int64, path string) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.DestinationPath = path
	return q.store.UpdateDownload(dl)
}

func (q *Queue) UpdateProgress(id int64, bytesReceived, peakSpeed, avgSpeed int64) error {
	dl, err := q.store.GetDownload(id)
	if err != nil {
		return err
	}
	dl.DownloadedBytes = bytesReceived
	dl.PeakSpeed = peakSpeed
	dl.AverageSpeed = avgSpeed
	return q.store.UpdateDownload(dl)
}

// RequeueInterrupted sets all "downloading" status downloads back to "queued",
// and flips leftover "processing" downloads to "completed": the file itself
// finished transferring (that's what "processing" means), only the
// post-processing step (moving/hooks/extraction) was interrupted by the
// unclean shutdown, and the file is already sitting at its last-known
// destination path on disk. Called on app startup to recover from unclean
// shutdown.
func (q *Queue) RequeueInterrupted() error {
	downloads, err := q.store.GetDownloads("downloading")
	if err != nil {
		return err
	}
	for _, dl := range downloads {
		dl.Status = "queued"
		dl.StartedAt = nil
		if err := q.store.UpdateDownload(&dl); err != nil {
			return err
		}
	}

	processing, err := q.store.GetDownloads("processing")
	if err != nil {
		return err
	}
	for _, dl := range processing {
		log.Printf("download %d was interrupted mid-processing; marking completed at %s", dl.ID, dl.DestinationPath)
		now := time.Now()
		dl.Status = "completed"
		dl.CompletedAt = &now
		if err := q.store.UpdateDownload(&dl); err != nil {
			return err
		}
	}
	return nil
}

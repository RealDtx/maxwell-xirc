package dcc

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

// ErrCancelled is returned by Transfer.Start when the transfer was cancelled via Cancel().
var ErrCancelled = errors.New("transfer cancelled")

type TransferProgress struct {
	BytesReceived int64   `json:"bytes_received"`
	TotalSize     int64   `json:"total_size"`
	Speed         int64   `json:"speed"`
	PeakSpeed     int64   `json:"peak_speed"`
	AverageSpeed  int64   `json:"average_speed"`
	Percentage    float64 `json:"percentage"`
}

type Transfer struct {
	mu            sync.RWMutex
	offer         *DCCOffer
	destPath      string
	partPath      string
	progressCh    chan TransferProgress
	bytesReceived int64
	peakSpeed     int64
	startTime     time.Time
	resumeOffset  int64
	conn          net.Conn
	cancelled     bool
}

// Cancel interrupts an in-progress transfer by closing the underlying TCP connection.
func (t *Transfer) Cancel() {
	t.mu.Lock()
	t.cancelled = true
	conn := t.conn
	t.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

func NewTransfer(offer *DCCOffer, destPath string, progressCh chan TransferProgress) *Transfer {
	return &Transfer{
		offer:      offer,
		destPath:   destPath,
		partPath:   destPath + ".part",
		progressCh: progressCh,
	}
}

func (t *Transfer) SetResumeOffset(offset int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.resumeOffset = offset
}

func (t *Transfer) BytesReceived() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.bytesReceived
}

func (t *Transfer) PeakSpeed() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.peakSpeed
}

func (t *Transfer) AverageSpeed() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	elapsed := time.Since(t.startTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	received := t.bytesReceived - t.resumeOffset
	return int64(float64(received) / elapsed)
}

func (t *Transfer) Start() error {
	addr := fmt.Sprintf("%s:%d", t.offer.IP, t.offer.Port)
	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()
	defer conn.Close()

	// Open or create the .part file
	var file *os.File
	if t.resumeOffset > 0 {
		file, err = os.OpenFile(t.partPath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("opening part file for resume: %w", err)
		}
	} else {
		file, err = os.Create(t.partPath)
		if err != nil {
			return fmt.Errorf("creating part file: %w", err)
		}
	}

	if t.progressCh != nil {
		defer close(t.progressCh)
	}

	t.startTime = time.Now()
	t.bytesReceived = t.resumeOffset

	buf := make([]byte, 64*1024) // 64KB read buffer
	var lastProgressTime time.Time
	var lastProgressBytes int64

	for {
		n, readErr := conn.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				file.Close()
				return fmt.Errorf("writing to file: %w", writeErr)
			}

			t.mu.Lock()
			t.bytesReceived += int64(n)
			received := t.bytesReceived
			t.mu.Unlock()

			// Report progress every 500ms
			now := time.Now()
			if t.progressCh != nil && now.Sub(lastProgressTime) >= 500*time.Millisecond {
				elapsed := now.Sub(lastProgressTime).Seconds()
				var speed int64
				if elapsed > 0 {
					speed = int64(float64(received-lastProgressBytes) / elapsed)
				}

				t.mu.Lock()
				if speed > t.peakSpeed {
					t.peakSpeed = speed
				}
				peak := t.peakSpeed
				t.mu.Unlock()

				var pct float64
				if t.offer.Size > 0 {
					pct = float64(received) / float64(t.offer.Size) * 100
				}

				totalElapsed := now.Sub(t.startTime).Seconds()
				var avgSpeed int64
				if totalElapsed > 0 {
					avgSpeed = int64(float64(received-t.resumeOffset) / totalElapsed)
				}

				select {
				case t.progressCh <- TransferProgress{
					BytesReceived: received,
					TotalSize:     t.offer.Size,
					Speed:         speed,
					PeakSpeed:     peak,
					AverageSpeed:  avgSpeed,
					Percentage:    pct,
				}:
				default:
				}

				lastProgressTime = now
				lastProgressBytes = received
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			file.Close()
			// If the error came from us closing the connection via Cancel(), treat it
			// as a cancellation rather than a failure.
			t.mu.RLock()
			wasCancelled := t.cancelled
			t.mu.RUnlock()
			if wasCancelled {
				return ErrCancelled
			}
			return fmt.Errorf("reading from connection: %w", readErr)
		}
	}

	// Send final progress
	if t.progressCh != nil {
		totalElapsed := time.Since(t.startTime).Seconds()
		var avgSpeed int64
		if totalElapsed > 0 {
			avgSpeed = int64(float64(t.bytesReceived-t.resumeOffset) / totalElapsed)
		}
		select {
		case t.progressCh <- TransferProgress{
			BytesReceived: t.bytesReceived,
			TotalSize:     t.offer.Size,
			Speed:         0,
			PeakSpeed:     t.peakSpeed,
			AverageSpeed:  avgSpeed,
			Percentage:    100,
		}:
		default:
		}
	}

	// Rename .part to final
	if err := file.Close(); err != nil {
		log.Printf("warning: closing part file %s: %v", t.partPath, err)
	}
	if err := os.Rename(t.partPath, t.destPath); err != nil {
		// If the .part file is gone but the final file already exists, a previous
		// transfer completed it (e.g. service restart after successful rename).
		if os.IsNotExist(err) {
			if _, statErr := os.Stat(t.destPath); statErr == nil {
				log.Printf("part file gone but final file exists at %s — treating as complete", t.destPath)
				return nil
			}
		}
		return fmt.Errorf("renaming part file: %w", err)
	}

	return nil
}

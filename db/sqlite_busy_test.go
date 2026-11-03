package db

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// Concurrent writers (index merges from bot ads vs. downloads/sessions) must
// wait for each other instead of failing with SQLITE_BUSY.
func TestSQLiteConcurrentWritesDontBusy(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "busy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	srv := &Server{Name: "srv", Host: "irc.example.net", Port: 6667, Nickname: "nick", Enabled: true}
	if err := s.CreateServer(srv); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8*20*3)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				pack := i
				if _, err := s.BulkMergeIndexedFiles([]IndexedFile{{ServerID: srv.ID, Channel: "#c", BotNick: fmt.Sprintf("bot%d", g), PackNumber: &pack, Filename: fmt.Sprintf("f%d-%d", g, i)}}); err != nil {
					errs <- err
				}
				if err := s.CreateDownload(&Download{ServerID: srv.ID, BotNick: "b", PackNumber: i, Status: "completed"}); err != nil {
					errs <- err
				}
				if _, err := s.DeleteDownloadsByStatus("completed"); err != nil {
					errs <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

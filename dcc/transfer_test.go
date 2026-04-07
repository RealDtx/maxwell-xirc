package dcc

import (
	"io/ioutil"
	"net"
	"os"
	"testing"
)

// mockTCPServer creates a listening TCP server that sends data
func mockTCPServer(port int, data []byte) (net.Listener, error) {
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
	if err != nil {
		return nil, err
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write(data)
	}()
	return ln, nil
}

func TestTransfer_ReceivesFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := ioutil.TempDir("", "transfer-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Find an available port
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Start mock server
	testData := []byte("hello world test data")
	ln, err := mockTCPServer(port, testData)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer ln.Close()

	// Create transfer
	offer := &DCCOffer{
		Filename: "test.txt",
		IP:       "127.0.0.1",
		Port:     port,
		Size:     int64(len(testData)),
	}
	destPath := tmpDir + "/test.txt"
	progressCh := make(chan TransferProgress, 10)
	transfer := NewTransfer(offer, destPath, progressCh)

	// Start transfer
	if err := transfer.Start(); err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	// Verify file contents
	contents, err := ioutil.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(contents) != string(testData) {
		t.Errorf("file contents mismatch: got %q, want %q", string(contents), string(testData))
	}

	// Verify BytesReceived
	if transfer.BytesReceived() != int64(len(testData)) {
		t.Errorf("BytesReceived mismatch: got %d, want %d", transfer.BytesReceived(), int64(len(testData)))
	}

	// Verify .part file is cleaned up
	partPath := destPath + ".part"
	if _, err := os.Stat(partPath); err == nil {
		t.Errorf(".part file should be cleaned up")
	}
}

func TestTransfer_PartFile_UsedDuringTransfer(t *testing.T) {
	// Create temp directory
	tmpDir, err := ioutil.TempDir("", "transfer-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Find an available port
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Start mock server
	testData := []byte("some test data for part file")
	ln, err := mockTCPServer(port, testData)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer ln.Close()

	// Create transfer
	offer := &DCCOffer{
		Filename: "test.txt",
		IP:       "127.0.0.1",
		Port:     port,
		Size:     int64(len(testData)),
	}
	destPath := tmpDir + "/test.txt"
	progressCh := make(chan TransferProgress, 10)
	transfer := NewTransfer(offer, destPath, progressCh)

	// Start transfer
	if err := transfer.Start(); err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	// Verify .part file is cleaned up after success
	partPath := destPath + ".part"
	if _, err := os.Stat(partPath); err == nil {
		t.Errorf(".part file should not exist after successful transfer")
	}

	// Verify final file exists
	if _, err := os.Stat(destPath); err != nil {
		t.Errorf("final file should exist after transfer: %v", err)
	}
}

func TestTransfer_ProgressReporting(t *testing.T) {
	// Create temp directory
	tmpDir, err := ioutil.TempDir("", "transfer-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Find an available port
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Start mock server with data
	testData := []byte("progress test data here")
	ln, err := mockTCPServer(port, testData)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer ln.Close()

	// Create transfer
	offer := &DCCOffer{
		Filename: "test.txt",
		IP:       "127.0.0.1",
		Port:     port,
		Size:     int64(len(testData)),
	}
	destPath := tmpDir + "/test.txt"
	progressCh := make(chan TransferProgress, 100)
	transfer := NewTransfer(offer, destPath, progressCh)

	// Start transfer
	if err := transfer.Start(); err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	// Verify at least one progress update received
	progressReceived := false
	var lastProgress TransferProgress
	for {
		select {
		case p, ok := <-progressCh:
			if !ok {
				goto done
			}
			progressReceived = true
			lastProgress = p
		default:
			goto done
		}
	}
done:
	if !progressReceived {
		t.Errorf("expected progress updates, but received none")
	}
	if lastProgress.BytesReceived != int64(len(testData)) {
		t.Errorf("expected final progress %d, got %d", len(testData), lastProgress.BytesReceived)
	}
}

func TestTransfer_ConnectionRefused(t *testing.T) {
	// Create temp directory
	tmpDir, err := ioutil.TempDir("", "transfer-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use port 1 which should refuse connections (requires root to bind)
	offer := &DCCOffer{
		Filename: "test.txt",
		IP:       "127.0.0.1",
		Port:     1,
		Size:     100,
	}
	destPath := tmpDir + "/test.txt"
	progressCh := make(chan TransferProgress, 10)
	transfer := NewTransfer(offer, destPath, progressCh)

	// Start transfer - should fail
	err = transfer.Start()
	if err == nil {
		t.Errorf("expected connection error, but got none")
	}
}

func TestTransfer_Resume_ExistingPartFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := ioutil.TempDir("", "transfer-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Find an available port
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Full file content
	fullData := []byte("AAAAABBBBBBCCCCCC")
	partialData := fullData[8:] // Simulate resuming from byte 8

	// Start mock server
	ln, err := mockTCPServer(port, partialData)
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer ln.Close()

	destPath := tmpDir + "/test.txt"
	partPath := destPath + ".part"

	// Pre-write partial .part file (first 8 bytes)
	if err := ioutil.WriteFile(partPath, fullData[:8], 0644); err != nil {
		t.Fatalf("failed to write partial file: %v", err)
	}

	// Create transfer with resume offset
	offer := &DCCOffer{
		Filename: "test.txt",
		IP:       "127.0.0.1",
		Port:     port,
		Size:     int64(len(fullData)),
	}
	progressCh := make(chan TransferProgress, 10)
	transfer := NewTransfer(offer, destPath, progressCh)
	transfer.SetResumeOffset(8)

	// Start transfer
	if err := transfer.Start(); err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	// Verify complete file
	contents, err := ioutil.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(contents) != string(fullData) {
		t.Errorf("file contents mismatch: got %q, want %q", string(contents), string(fullData))
	}

	// Verify BytesReceived reflects resumed transfer
	if transfer.BytesReceived() != int64(len(fullData)) {
		t.Errorf("BytesReceived mismatch: got %d, want %d", transfer.BytesReceived(), int64(len(fullData)))
	}

	// Verify .part file is cleaned up
	if _, err := os.Stat(partPath); err == nil {
		t.Errorf(".part file should be cleaned up after resume completion")
	}
}

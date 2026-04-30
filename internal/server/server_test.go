package server

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemoveStaleUnixSocketRefusesRegularFile(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "msgflow.sock")
	if err := os.WriteFile(socketPath, []byte("not a socket"), 0600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	if err := removeStaleUnixSocket(socketPath); err == nil {
		t.Fatal("expected regular file to be rejected")
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("regular file should not be removed: %v", err)
	}
}

func TestRemoveStaleUnixSocketRemovesSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not supported on windows")
	}

	dir, err := os.MkdirTemp("/tmp", "mfsock")
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	socketPath := filepath.Join(dir, "msgflow.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	if unixLn, ok := ln.(*net.UnixListener); ok {
		unixLn.SetUnlinkOnClose(false)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close unix socket listener: %v", err)
	}

	if err := removeStaleUnixSocket(socketPath); err != nil {
		t.Fatalf("remove stale socket: %v", err)
	}
	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected socket path to be removed, got %v", err)
	}
}

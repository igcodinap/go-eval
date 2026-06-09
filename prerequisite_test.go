package eval

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestRequirePassesWhenPrerequisitesPass(t *testing.T) {
	t.Setenv("GOEVAL_TEST_ENV", "ok")
	path := filepath.Join(t.TempDir(), "ready.txt")
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
	}()

	Require(t,
		Env("GOEVAL_TEST_ENV"),
		File(path),
		TCP("test tcp", listener.Addr().String()),
		Func("custom", func(context.Context) error { return nil }),
	)
}

func TestRequireSkipsWhenEnvMissing(t *testing.T) {
	t.Setenv("GOEVAL_TEST_MISSING_ENV", "")
	reached := false

	t.Run("missing", func(t *testing.T) {
		Require(t, Env("GOEVAL_TEST_MISSING_ENV"))
		reached = true
	})

	if reached {
		t.Fatalf("Require returned after missing env")
	}
}

func TestRequireSkipsWhenFileMissing(t *testing.T) {
	reached := false

	t.Run("missing", func(t *testing.T) {
		Require(t, File(filepath.Join(t.TempDir(), "missing.txt")))
		reached = true
	})

	if reached {
		t.Fatalf("Require returned after missing file")
	}
}

func TestRequireSkipsWhenFuncFails(t *testing.T) {
	reached := false

	t.Run("missing", func(t *testing.T) {
		Require(t, Func("custom", func(context.Context) error { return errors.New("not ready") }))
		reached = true
	})

	if reached {
		t.Fatalf("Require returned after failing func")
	}
}

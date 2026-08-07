package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap/zapcore"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestDualSinkLevels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operator.log")

	// Capture stderr while Info is the stdout floor.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })

	lvl := zapcore.InfoLevel
	opts := Options{
		Zap:       crzap.Options{Development: false, Level: lvl},
		FilePath:  path,
		FileLevel: "debug",
	}
	log, closer, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}

	log.Info("essential")
	log.V(1).Info("verbose")
	closer()
	_ = w.Close()

	var stderrBuf bytes.Buffer
	_, _ = stderrBuf.ReadFrom(r)
	stderrBody := stderrBuf.String()
	if !bytes.Contains(stderrBuf.Bytes(), []byte("essential")) {
		t.Fatalf("stderr missing info log: %s", stderrBody)
	}
	if bytes.Contains(stderrBuf.Bytes(), []byte("verbose")) {
		t.Fatalf("stderr should not include V(1) at info level: %s", stderrBody)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("essential")) {
		t.Fatalf("file missing info log: %s", body)
	}
	if !bytes.Contains(body, []byte("verbose")) {
		t.Fatalf("file missing debug/V(1) log: %s", body)
	}
}

func TestParseLevel(t *testing.T) {
	got, err := parseLevel("DEBUG", zapcore.InfoLevel)
	if err != nil || got != zapcore.DebugLevel {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := parseLevel("nope", zapcore.InfoLevel); err == nil {
		t.Fatal("expected error")
	}
	got, err = parseLevel("", zapcore.ErrorLevel)
	if err != nil || got != zapcore.ErrorLevel {
		t.Fatalf("fallback: got %v %v", got, err)
	}
}

func TestStdoutOnlyNoFile(t *testing.T) {
	opts := Options{Zap: crzap.Options{Development: true}}
	log, closer, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	log.Info("ok")
}

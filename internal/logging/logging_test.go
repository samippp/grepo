// logging_test.go checks format selection, the level, and that text output
// leaves out timestamps.

package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, "xml", slog.LevelInfo); err == nil {
		t.Error("New with unknown format: want error")
	}

	var buf bytes.Buffer
	log, err := New(&buf, "text", slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	log.Debug("hidden")
	log.Info("shown", "n", 1)
	got := buf.String()
	if strings.Contains(got, "hidden") {
		t.Errorf("debug line logged at info level: %q", got)
	}
	if !strings.Contains(got, "msg=shown n=1") {
		t.Errorf("info line missing: %q", got)
	}
	if strings.Contains(got, "time=") {
		t.Errorf("text output should not include time: %q", got)
	}

	buf.Reset()
	log, _ = New(&buf, "json", slog.LevelDebug)
	log.Debug("visible")
	if got := buf.String(); !strings.Contains(got, `"msg":"visible"`) || !strings.Contains(got, `"time"`) {
		t.Errorf("json debug line = %q, want msg and time", got)
	}
}

// logging_test.go checks format selection, the verbose flag, and that text
// output leaves out timestamps.

package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, "xml", false); err == nil {
		t.Error("New with unknown format: want error")
	}

	var buf bytes.Buffer
	log, err := New(&buf, "text", false)
	if err != nil {
		t.Fatal(err)
	}
	log.Debug("hidden")
	log.Info("shown", "n", 1)
	got := buf.String()
	if strings.Contains(got, "hidden") {
		t.Errorf("debug line logged without verbose: %q", got)
	}
	if !strings.Contains(got, "msg=shown n=1") {
		t.Errorf("info line missing: %q", got)
	}
	if strings.Contains(got, "time=") {
		t.Errorf("text output should not include time: %q", got)
	}

	buf.Reset()
	log, _ = New(&buf, "json", true)
	log.Debug("visible")
	if got := buf.String(); !strings.Contains(got, `"msg":"visible"`) || !strings.Contains(got, `"time"`) {
		t.Errorf("json debug line = %q, want msg and time", got)
	}
}

// Package logging builds grepo's *slog.Logger.
//
// Logs go to stderr; stdout is for results. Pipeline packages take a logger
// argument rather than using slog's default; pure packages (graph) don't log.
//
// Levels: Debug for diagnosis (shown with -v), Info for what each stage did
// and how long it took (hidden for query commands unless -v), Warn for a
// failure the run survives, Error for a failed run. Never log LLM prompts,
// responses or API keys above Debug.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"time"
)

// New returns a logger writing to w from level up, as "text" or "json".
func New(w io.Writer, format string, level slog.Level) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{Level: level}

	switch format {
	case "text":
		// No timestamps in a terminal; JSON keeps them.
		opts.ReplaceAttr = dropTime
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("unknown log format %q (want text or json)", format)
	}
}

// Since is the time since t, rounded to the millisecond, for "took" fields.
func Since(t time.Time) time.Duration {
	return time.Since(t).Round(time.Millisecond)
}

func dropTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

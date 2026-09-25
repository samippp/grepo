// Package logging builds the *slog.Logger that grepo passes through its
// pipeline. The CLI uses it now, and the web server will use it too.
//
// Conventions:
//   - Logs go to stderr, never stdout. stdout is reserved for results, so
//     `grepo analyze <link> > graph.json` always produces clean JSON.
//   - Pipeline packages take a *slog.Logger argument instead of using slog's
//     global default. That lets the web server give each analysis job its own
//     logger with fields such as job ID and repo attached to every line.
//   - Pure packages (graph) don't log. Their callers time and log them.
//
// Levels:
//   - Debug: details for diagnosing problems (git commands, directories). Shown with -v.
//   - Info: what each stage did and how long it took.
//   - Warn: something failed but the run continues (e.g. a package didn't load).
//   - Error: the run failed.
//
// Never log LLM prompts, responses or API keys above Debug.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"time"
)

// New returns a logger that writes to w. format is "text" (readable, for
// terminals) or "json" (for the server and log tools). verbose enables Debug.
func New(w io.Writer, format string, verbose bool) (*slog.Logger, error) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}

	switch format {
	case "text":
		// Timestamps are clutter in a terminal. JSON output keeps them.
		opts.ReplaceAttr = dropTime
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	default:
		return nil, fmt.Errorf("unknown log format %q (want text or json)", format)
	}
}

// Since returns the time elapsed since t, rounded to the millisecond, for
// "took" fields.
func Since(t time.Time) time.Duration {
	return time.Since(t).Round(time.Millisecond)
}

func dropTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

// Package source turns what the user gives grepo (a GitHub link or a local
// path) into a directory on disk that the load package can read.
//
// It is the first stage of the pipeline and is shared by every front end: the
// CLI calls it today, and the web app will call it with the link a user pastes.
//
// GitHub repos are shallow-cloned (latest commit only) into the user cache
// directory, e.g. %LocalAppData%\grepo\repos\github.com\owner\repo on Windows.
// Later runs on the same repo update the existing clone instead of starting
// over. Requires git on PATH. Only public repos are supported for now.
package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/samippp/grepo/internal/logging"
)

// Repo is a repository ready to be loaded.
type Repo struct {
	Dir    string `json:"-"`                // local directory to analyze
	URL    string `json:"url,omitempty"`    // canonical GitHub URL; empty for local paths
	Commit string `json:"commit,omitempty"` // analyzed commit SHA; empty for local paths
}

// Resolve returns the repository arg refers to. An existing directory is used
// as is. Anything else is treated as a GitHub link and cloned, or updated if it
// is already cached.
func Resolve(ctx context.Context, arg string, log *slog.Logger) (*Repo, error) {
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		log.Debug("using local directory", "dir", arg)
		return &Repo{Dir: arg}, nil
	}

	owner, name, err := ParseGitHubURL(arg)
	if err != nil {
		return nil, fmt.Errorf("%q is not a directory or a GitHub repo link: %w", arg, err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git must be installed to analyze GitHub links")
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	// GitHub names are case-insensitive, so lowercase the cache path to avoid
	// keeping two clones of the same repo.
	dir := filepath.Join(cache, "grepo", "repos", "github.com", strings.ToLower(owner), strings.ToLower(name))
	repoURL := "https://github.com/" + owner + "/" + name

	start := time.Now()
	if err := syncClone(ctx, log, repoURL, dir); err != nil {
		return nil, err
	}
	commit, err := git(ctx, log, dir, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	log.Info("repo ready", "repo", repoURL, "commit", commit, "took", logging.Since(start))
	return &Repo{Dir: dir, URL: repoURL, Commit: commit}, nil
}

// Owner and repo names may only contain these characters on GitHub. Checking
// them also stops input like "../.." from escaping the cache directory.
var namePart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ParseGitHubURL extracts owner and repo from links such as
//
//	https://github.com/owner/repo
//	https://github.com/owner/repo.git
//	https://github.com/owner/repo/tree/main/some/dir  (extra path is ignored)
//	github.com/owner/repo
func ParseGitHubURL(s string) (owner, name string, err error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if host := strings.ToLower(u.Hostname()); host != "github.com" && host != "www.github.com" {
		return "", "", errors.New("only github.com links are supported")
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", errors.New("link must include owner and repo, like https://github.com/owner/repo")
	}
	owner, name = parts[0], strings.TrimSuffix(parts[1], ".git")
	for _, p := range []string{owner, name} {
		if !namePart.MatchString(p) || p == "." || p == ".." {
			return "", "", fmt.Errorf("invalid owner or repo name %q", p)
		}
	}
	return owner, name, nil
}

// syncClone makes dir a shallow clone of the latest commit on the repo's
// default branch.
func syncClone(ctx context.Context, log *slog.Logger, repoURL, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		log.Info("updating cached clone", "repo", repoURL)
		log.Debug("cache location", "dir", dir)
		if _, err := git(ctx, log, dir, "fetch", "--depth", "1", "origin", "HEAD"); err != nil {
			return err
		}
		_, err := git(ctx, log, dir, "reset", "--hard", "FETCH_HEAD")
		return err
	}

	// Clone into a temp dir and rename it into place, so a failed or
	// interrupted clone never leaves a half-finished repo in the cache.
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), filepath.Base(dir)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) // no-op once the rename has succeeded

	log.Info("cloning", "repo", repoURL)
	log.Debug("cache location", "dir", dir)
	if _, err := git(ctx, log, "", "clone", "--depth", "1", "--single-branch", "--no-tags", repoURL+".git", tmp); err != nil {
		return err
	}
	return os.Rename(tmp, dir)
}

// git runs a git command in dir and returns its trimmed stdout.
func git(ctx context.Context, log *slog.Logger, dir string, args ...string) (string, error) {
	log.Debug("running git", "args", args, "dir", dir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Fail instead of asking for credentials when a repo is private or does
	// not exist. The second variable stops Git Credential Manager's login popup
	// on Windows.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

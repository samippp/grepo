// source_test.go checks GitHub link parsing. It needs no network access; the
// clone path is exercised by running `grepo analyze <link>` by hand.

package source

import "testing"

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		in          string
		owner, name string
		wantErr     bool
	}{
		{in: "https://github.com/spf13/cobra", owner: "spf13", name: "cobra"},
		{in: "https://github.com/spf13/cobra.git", owner: "spf13", name: "cobra"},
		{in: "https://github.com/spf13/cobra/", owner: "spf13", name: "cobra"},
		{in: "https://github.com/spf13/cobra/tree/main/doc", owner: "spf13", name: "cobra"},
		{in: "github.com/spf13/cobra", owner: "spf13", name: "cobra"},
		{in: "https://www.github.com/spf13/cobra", owner: "spf13", name: "cobra"},
		{in: "  https://github.com/spf13/cobra  ", owner: "spf13", name: "cobra"},

		{in: "https://gitlab.com/foo/bar", wantErr: true},
		{in: "https://github.com/spf13", wantErr: true},
		{in: "https://github.com/../etc", wantErr: true},
		{in: "https://github.com/foo/bar%20baz", wantErr: true},
		{in: "ftp://github.com/foo/bar", wantErr: true},
		{in: "not-a-dir", wantErr: true},
	}
	for _, tt := range tests {
		owner, name, err := ParseGitHubURL(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseGitHubURL(%q) = %s/%s, want error", tt.in, owner, name)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseGitHubURL(%q) error: %v", tt.in, err)
			continue
		}
		if owner != tt.owner || name != tt.name {
			t.Errorf("ParseGitHubURL(%q) = %s/%s, want %s/%s", tt.in, owner, name, tt.owner, tt.name)
		}
	}
}

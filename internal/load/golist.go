// grepo's own loader: `go list` for metadata, then parsing and type-checking
// here, dependencies without function bodies. Replaces go/packages in Load.

package load

import (
	"context"
	"errors"
	"log/slog"
)

// fromGoList loads like GoPackages, without go/packages.
func fromGoList(ctx context.Context, dir string, log *slog.Logger) (*Result, error) {
	return nil, errors.New("not written yet")
}

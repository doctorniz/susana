//go:build !linux

package host

import (
	"fmt"

	"github.com/doctorniz/susana/internal/integration"
)

func init() {
	integration.Register("host", func(o integration.Options) (integration.Integration, error) {
		return nil, fmt.Errorf("host %q: host checks are only supported on Linux", o.Name)
	})
}

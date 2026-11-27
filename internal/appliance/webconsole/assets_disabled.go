//go:build !webconsole

package webconsole

import "io/fs"

// Assets returns nil when the web console was not
// included in this build.
func Assets() (fs.FS, error) {
	return nil, nil
}

//go:build webconsole

package webconsole

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embeddedAssets embed.FS

// Assets returns the compiled web console filesystem.
func Assets() (fs.FS, error) {
	return fs.Sub(embeddedAssets, "dist")
}

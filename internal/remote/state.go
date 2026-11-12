package remote

import (
	"net/http"
	"path/filepath"
)

// serveAssets serves the app's assets directory to the player, which
// loads ui.Image's asset paths from it.
func serveAssets(addr, dir string) {
	abs, _ := filepath.Abs(dir)
	http.ListenAndServe(addr, http.FileServer(http.Dir(abs)))
}

package remote

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// serveAssets serves the app's assets directory to the player, which
// loads ui.Image's asset paths from it.
func serveAssets(ln net.Listener, dir string) {
	abs, _ := filepath.Abs(dir)
	http.Serve(ln, http.FileServer(http.Dir(abs)))
}

// listen listens on addr, or takes the listener asuka dev passed, on the
// descriptor the environment variable fdEnv names. asuka dev keeps its
// listeners across reloads, so that a player reconnecting while one run
// stops and the next starts waits in its queue, never refused, and the
// next run can start while the last one still listens.
func listen(addr, fdEnv string) (net.Listener, error) {
	if v := os.Getenv(fdEnv); v != "" {
		fd, err := strconv.Atoi(v)
		if err != nil {
			return nil, err
		}
		f := os.NewFile(uintptr(fd), fdEnv)
		ln, err := net.FileListener(f)
		f.Close() // FileListener has its own copy
		return ln, err
	}
	return net.Listen("tcp", addr)
}

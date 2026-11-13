// Command devrun is an app's run under asuka dev, for the tests of its
// restarts (devstate.Wait): it logs to DEVRUN_LOG as it starts, as it's
// let go on and as it's stopped. With DEVRUN_QUIT set it quits as it
// starts, as a build that panics in init does.
package main

import (
	"fmt"
	"os"

	"github.com/egoist/asuka/internal/devstate"
)

func log(what string) {
	f, err := os.OpenFile(os.Getenv("DEVRUN_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(f, "%s %s\n", what, os.Getenv("DEVRUN_NAME"))
	f.Close()
}

func main() {
	log("start")
	if os.Getenv("DEVRUN_QUIT") != "" {
		os.Exit(2)
	}
	stopped := devstate.Wait()
	log("go")
	<-stopped
	log("stop")
}

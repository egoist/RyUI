//go:build android

package mobile

/*
#include <android/log.h>
#include <stdlib.h>

static void nui_log(int prio, const char *msg) { __android_log_write(prio, "nui", msg); }
*/
import "C"

import (
	"bufio"
	"os"
	"unsafe"
)

// Android drops a process's stdout and stderr: send the app's (fmt.Print,
// panics' messages before the crash) to logcat, tagged nui.
func init() {
	for _, f := range []struct {
		file **os.File
		prio C.int
	}{{&os.Stdout, C.ANDROID_LOG_INFO}, {&os.Stderr, C.ANDROID_LOG_WARN}} {
		r, w, err := os.Pipe()
		if err != nil {
			return
		}
		*f.file = w
		go func(prio C.int) {
			s := bufio.NewScanner(r)
			s.Buffer(make([]byte, 64<<10), 1<<20)
			for s.Scan() {
				m := C.CString(s.Text())
				C.nui_log(prio, m)
				C.free(unsafe.Pointer(m))
			}
		}(f.prio)
	}
}

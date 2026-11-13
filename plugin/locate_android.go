//go:build android

package plugin

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// bundled is the library beside the app's Go code (libasuka.so), where
// asuka dev puts both (the app's files/asuka), and where a release's are
// when Android extracts them. The executable is Android's app_process:
// its directory isn't the app's.
func bundled(_ string, l Library, f File) []string {
	dir := codeDir()
	if dir == "" {
		return nil
	}
	return []string{filepath.Join(dir, f.Name)}
}

// systemName is the library asuka build ships in the APK's lib/<abi>:
// the app's own native libraries are on the linker's search path for its
// code.
func systemName(f File) string { return f.Name }

// codeDir is the directory of the file this code was loaded from: the
// mapping in /proc/self/maps that holds one of its functions. A library
// loaded straight from the APK (uncompressed, as a release's are) is
// mapped from the .apk, beside which there's no library.
func codeDir() string {
	here, _, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	pc := uint64(here)
	f, err := os.Open("/proc/self/maps")
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		// 7f0e2a000-7f0e3b000 r-xp 00000000 fd:05 1234  /data/.../libasuka.so
		fields := strings.Fields(s.Text())
		if len(fields) < 6 {
			continue
		}
		lo, hi, ok := strings.Cut(fields[0], "-")
		if !ok {
			continue
		}
		start, err1 := strconv.ParseUint(lo, 16, 64)
		end, err2 := strconv.ParseUint(hi, 16, 64)
		if err1 != nil || err2 != nil || pc < start || pc >= end {
			continue
		}
		path := strings.Join(fields[5:], " ")
		if !strings.HasPrefix(path, "/") || strings.HasSuffix(path, ".apk") {
			return ""
		}
		return filepath.Dir(path)
	}
	return ""
}

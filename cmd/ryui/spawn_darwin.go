package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"

	"github.com/ebitengine/purego"
)

// spawnOwn starts the app's executable as its own responsible process,
// as Xcode does: macOS asks the user for a privacy permission (camera,
// location...) on behalf of the process responsible for the one asking,
// which for a child of ryui is the terminal; disclaiming it makes the
// app answer for itself, with its own Info.plist's purpose strings and
// its own entry in System Settings. It inherits only the standard input,
// output and error; its signals start at their defaults.
func spawnOwn(path, dir string, env []string) (*os.Process, error) {
	if err := loadSpawn(); err != nil {
		return nil, err
	}
	var attr, actions uintptr
	if r := spawnattrInit(&attr); r != 0 {
		return nil, fmt.Errorf("posix_spawnattr_init: %v", syscall.Errno(r))
	}
	defer spawnattrDestroy(&attr)
	if r := fileActionsInit(&actions); r != 0 {
		return nil, fmt.Errorf("posix_spawn_file_actions_init: %v", syscall.Errno(r))
	}
	defer fileActionsDestroy(&actions)

	const (
		setSigDef      = 0x04
		setSigMask     = 0x08
		cloexecDefault = 0x4000
	)
	var none, all uint32 = 0, ^uint32(0)
	spawnattrSetSigmask(&attr, &none)
	spawnattrSetSigdefault(&attr, &all)
	spawnattrSetFlags(&attr, setSigDef|setSigMask|cloexecDefault)
	if r := setDisclaim(&attr, 1); r != 0 {
		return nil, fmt.Errorf("responsibility_spawnattrs_setdisclaim: %v", syscall.Errno(r))
	}
	for fd := range 3 {
		fileActionsAddInherit(&actions, int32(fd))
	}
	cdir := cstr(dir)
	if r := fileActionsAddChdir(&actions, &cdir[0]); r != 0 {
		return nil, fmt.Errorf("posix_spawn_file_actions_addchdir_np: %v", syscall.Errno(r))
	}

	cpath := cstr(path)
	argv, keepArgv := cstrs([]string{path})
	envp, keepEnv := cstrs(env)
	var pid int32
	r := posixSpawn(&pid, &cpath[0], &actions, &attr, &argv[0], &envp[0])
	runtime.KeepAlive(cdir)
	runtime.KeepAlive(cpath)
	runtime.KeepAlive(keepArgv)
	runtime.KeepAlive(keepEnv)
	if r != 0 {
		return nil, fmt.Errorf("starting %s: %v", path, syscall.Errno(r))
	}
	return os.FindProcess(int(pid))
}

var (
	spawnOnce sync.Once
	spawnErr  error

	posixSpawn             func(pid *int32, path *byte, actions, attr *uintptr, argv, envp **byte) int32
	spawnattrInit          func(attr *uintptr) int32
	spawnattrDestroy       func(attr *uintptr) int32
	spawnattrSetFlags      func(attr *uintptr, flags int16) int32
	spawnattrSetSigmask    func(attr *uintptr, set *uint32) int32
	spawnattrSetSigdefault func(attr *uintptr, set *uint32) int32
	setDisclaim            func(attr *uintptr, disclaim int32) int32
	fileActionsInit        func(actions *uintptr) int32
	fileActionsDestroy     func(actions *uintptr) int32
	fileActionsAddInherit  func(actions *uintptr, fd int32) int32
	fileActionsAddChdir    func(actions *uintptr, path *byte) int32
)

func loadSpawn() error {
	spawnOnce.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			spawnErr = err
			return
		}
		for name, fn := range map[string]any{
			"posix_spawn":                            &posixSpawn,
			"posix_spawnattr_init":                   &spawnattrInit,
			"posix_spawnattr_destroy":                &spawnattrDestroy,
			"posix_spawnattr_setflags":               &spawnattrSetFlags,
			"posix_spawnattr_setsigmask":             &spawnattrSetSigmask,
			"posix_spawnattr_setsigdefault":          &spawnattrSetSigdefault,
			"responsibility_spawnattrs_setdisclaim":  &setDisclaim,
			"posix_spawn_file_actions_init":          &fileActionsInit,
			"posix_spawn_file_actions_destroy":       &fileActionsDestroy,
			"posix_spawn_file_actions_addinherit_np": &fileActionsAddInherit,
			"posix_spawn_file_actions_addchdir_np":   &fileActionsAddChdir,
		} {
			sym, err := purego.Dlsym(lib, name)
			if err != nil {
				spawnErr = fmt.Errorf("%s: %v", name, err)
				return
			}
			purego.RegisterFunc(fn, sym)
		}
	})
	return spawnErr
}

// cstr is s as a C string.
func cstr(s string) []byte { return append([]byte(s), 0) }

// cstrs is ss as a C array of C strings, ending in NULL, and the strings
// it points to, to keep alive while it's used.
func cstrs(ss []string) ([]*byte, [][]byte) {
	keep := make([][]byte, len(ss))
	arr := make([]*byte, len(ss)+1)
	for i, s := range ss {
		keep[i] = cstr(s)
		arr[i] = &keep[i][0]
	}
	return arr, keep
}

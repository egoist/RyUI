# 0003: In-process code patching and an interpreted app package

- **Status**: Rejected (for now)
- **Created**: 2026-11-14
- **Related**: #8

## Summary

Two ways to reload without restarting the app's process, both
considered and rejected for now: patching the running process's
functions with newly compiled code (as Dioxus's Subsecond does for
Rust), and running the app's own package in a Go interpreter while
Asuka stays compiled (as Flutter runs Dart on its VM in development).
This records why, and what would make them worth another look.

## What was considered

### Patching functions in the running process

Subsecond (Dioxus 0.7, 2025) compiles only the functions that changed,
links them against the running binary's symbols ("ThinLink", with the
address of `main` to account for ASLR), loads the patch as a library,
and sends calls through a jump table it updates. InjectionNext does
the like for Swift on the simulator and macOS, through the dynamic
linker's interposing. Neither restarts the process, so all state
survives, native views included.

For Go:

- **Loading code needs `plugin`, which needs cgo.** go-hotfix (2026),
  the one Go library doing this, builds a fix with
  `-buildmode=plugin` and redirects the old functions with gomonkey.
  Asuka's rule is no cgo outside `mobile/`, so `asuka dev`'s process
  (`CGO_ENABLED=0`) can't load plugins. Loading object files without
  `plugin` (as goloader did) means registering their module data with
  the runtime through `linkname`s that Go has been closing since 1.23,
  and breaks with each release.
- **Closures can't be patched.** go-hotfix can't, and Asuka's UI is
  closures: components, handlers, `Show`'s and `For`'s bodies. The
  code a developer edits most is the code it can't reach.
- **It needs `-gcflags=all=-N -l`**: no inlining or optimization, so
  calls go through the function that's patched. That's a slower app to
  develop against, and a build unlike the one shipped.
- **Changed types can't be patched** (struct layouts, signatures):
  every approach (Subsecond, InjectionNext, go-hotfix) leaves those to a
  restart, so the restart path is needed anyway.

### An interpreted app package

ixgo (goplus, the former igop) interprets Go 1.25 to 1.27 with
generics; yaegi interprets most of Go. `asuka dev` could build Asuka
compiled and run the app's own package interpreted, swapping its
functions on a change.

- Two semantics: an interpreter isn't the compiler, so a bug could be
  the interpreter's, and the app runs differently in development than
  shipped.
- It's slow where Asuka needs speed: the core's hot paths call app
  closures (signals' computations, list cell builders), and the
  interpreter's reflection is what DESIGN.md's hot path rules exist to
  avoid.
- Native packages' generated glue, `go:embed` and build constraints
  would all need the interpreter's support.

## Why not now

A reload, measured as #8 left it: an incremental `go build` of the
largest example (`examples/usage`, 2,200 lines, 259 packages) takes
0.44s without DWARF. The new run starts behind the old one, and its
signals come back by name. On macOS, the system's check of each new
program adds 0.2s to 0.5s, which Developer Tools turns off. A restart
is under a second, and keeps the state that matters most.

What patching would add is the state a restart loses, and 0001 (keep
the player's views) and 0002 (restore navigation) get most of that
without it, keeping one compiler and no cgo.

## What would change this

- Go gains a supported way to load code without cgo, or `plugin`
  without cgo.
- Builds become the bottleneck: apps whose incremental build takes
  seconds, where patching a function would be much faster than
  linking.
- 0001 and 0002 turn out not to be enough: users keep losing state
  that only a process that never restarts would keep.

## References

- [Subsecond](https://docs.rs/subsecond/): Dioxus's hot patching.
- [go-hotfix](https://pkg.go.dev/github.com/go-hotfix/hotfix).
- [ixgo](https://github.com/goplus/ixgo/wiki).
- [InjectionIII](https://github.com/johnno1962/InjectionIII), and
  InjectionNext.
- [Faster Rust builds on Mac](https://nnethercote.github.io/2025/09/04/faster-rust-builds-on-mac.html):
  macOS's check of new programs, and Developer Tools.

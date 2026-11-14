# 0002: Restore navigation across reloads

- **Status**: Draft
- **Created**: 2026-11-14
- **Related**: #8, 0001

## Summary

After a reload, `asuka dev` brings back the screens that were pushed on
each navigation stack, by replaying the user's actions that pushed
them, in order, against the new run, and stopping at the first one
that doesn't push the same screen. Only actions whose handler did
nothing but navigate are replayed, so a reload never sends a message
or saves a form twice. A later phase adds typed, serializable routes,
which apps can use to have their screens restored exactly, in
development and by the platform's state restoration in a built app.

## Motivation

Editing a screen three pushes deep means, on every save, tapping back
down to it. Kept state (`signal.KeepPlaces`) brings back the app's
signals, and with them tabs (`TabsProps.Selected`) and sheets
(`SheetProps.Open`), which are signals. But a stack's screens are
closures, `Navigator.Push(func() Node, ...)`, and a closure can't be
saved: what it captured lives in the run that ended. So a reload starts
at the root screen.

## Design

### Phase 1: replay

**Recording.** In a development build (keeping on), the core logs, for
each push on a stack:

- the stack: its place (as 0001 names elements: the call path that
  made it, by the source's names) and ordinal there;
- the screen: the stable name of the closure pushed (the closure's
  place, `Screen>row.OnPress>nav.Push#0`) and its depth on the stack;
- the cause: the event being handled when `Push` was called (an
  element's event, from the host), with the target element's place and
  ordinal and the event's payload (`ops.AppendEvent`'s bytes).

A pop (by code, or the user's back gesture, which the host reports)
truncates that stack's log to the new depth. A push made outside any
event (a timer, a goroutine's `ui.Post`, a deep link) has no cause and
ends that stack's log there: it can't be replayed.

**Purity.** The core also notes whether the event's handler did
anything but navigate:

- wrote an app signal (other than through navigation): `signal`'s
  `Set` in a development build checks a flag that the event dispatch
  sets;
- called a native package (`native`'s backend) or a store write
  (`asuka/sdk/store`, `asuka/sdk/sqlite`): hooks in those, in
  development builds;
- posted work to the main thread (`ui.Post`) from the handler:
  likewise.

A handler that did any of these is marked impure. It can't be
replayed, and the stack's log stops before it. Handlers that call
`net/http` or start plain goroutines can't be seen. The purity check
catches the common side effects, not every one, and the docs say so.
`//asuka:noreplay` on a handler's function marks it impure by hand.

**Saving.** The log is saved with the kept state (`devstate`, as JSON,
keyed by the stack's place), when the run stops.

**Replaying.** In the next run, after the root mounts and the kept
signals are back, the core replays each stack's log, entry by entry:

1. Find the element at the recorded place and ordinal. None, or of
   another kind: stop.
2. Dispatch the recorded event to it, as if the host had sent it.
3. Check that the handler pushed a screen whose closure has the
   recorded name, at the recorded depth. No push, or another screen:
   pop what it pushed and stop.

Replayed pushes are made without animation: the stack is built in one
batch, as the platform's state restoration does. With 0001, the screens
that come back reuse the views the player still has.

A replay that stops early leaves the stack as deep as it got, which is
never worse than today's return to the root.

### Phase 2: routes

Replay can't bring back a screen pushed by an impure handler (a
"Create" button that saves, then shows what it made), a timer or a deep
link. For those, an app can push a route instead of a closure:

```go
type Note struct{ ID int64 }

func (r Note) Screen() ui.Node { return NoteScreen(r.ID) }

nav.PushRoute(Note{ID: id}, ui.ScreenOptions{})
```

- A route is a value whose type implements `Screen() ui.Node`, and is
  registered once (`ui.Routes(Note{}, Settings{})`), so a stack's routes
  can be encoded (the type's registered name and its JSON) and decoded.
- Under `asuka dev`, a stack of routes is saved with the kept state
  and pushed again as it was, with no replay.
- In a built app, the same encoding gives the platform's state
  restoration (UIKit's state restoration, Android's saved instance
  state) the stack to bring back after the system ended the app, and
  `ui.OnOpenURL` handlers a stack to push from a link.

Phase 2 is an API addition, and needs its own review against
DESIGN.md's navigation section; it's sketched here so phase 1 doesn't
make choices it would have to undo.

Phase 2 has since been built another way, as URL routes (`ui.Router`,
DESIGN.md's "Routes"): a screen is a route's pattern and parameters, not
a registered type, so its state serializes to an href and JSON, and it
comes back after `asuka dev`'s reloads, a browser's reload, a desktop
window's next launch and from a link. Replay remains for screens pushed
with a Navigator.

## Testing

With the fake host (ui's and internal/remote's tests):

- Push two screens from buttons, reload: both are back, without
  animation, and the log is the same.
- A handler that writes a signal and then pushes: not replayed. The
  stack stops before it.
- Change the source so a button pushes another screen: replay stops
  there.
- Remove the button: replay stops there.
- Pop by the back gesture, reload: the popped screen doesn't come back.
- e2e on the simulator and the emulator: a screen two pushes deep,
  edited, is still shown after the reload.

## Open questions

- Events whose meaning depends on the host's state at the time (a
  list's row index after the list changed, a gesture's coordinates):
  replay the same payload, or re-target by the row's item ID, which
  `CmdListUpdate` already carries?
- Purity: is "wrote no app signal, called no native or store API"
  enough of a guard to replay by default, or should replay be opt-in
  (`asuka dev --restore-navigation`) until it's proven?
- Several stacks (tabs each with a stack): replayed tab by tab, which
  needs the selected tab restored first. It's a signal, so it is, but
  the order needs stating.
- Phase 2's route registry versus generics: can `PushRoute[R Route]`
  avoid registration by recording the type at the call?

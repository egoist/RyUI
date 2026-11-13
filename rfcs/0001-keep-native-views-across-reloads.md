# 0001: Keep the player's native views across reloads

- **Status**: Draft
- **Created**: 2026-11-14
- **Related**: #8 (faster reloads, kept state named from the source),
  0002

## Summary

When `asuka dev` reloads, the player keeps the native views it has and
matches the new run's tree to them, instead of destroying every view
and building the new tree from nothing. A view the new tree still has
keeps everything the platform holds in it: its scroll offset, its
focus, caret and selection, a video's playback, a web view's page, a
map's camera, an open keyboard. Only what changed is created,
destroyed, moved or set. It's done in the player's proxy
(`internal/remote`), in Go, so every platform's player gets it without
changes to its host.

## Motivation

Today a reload keeps the app's signals but not the platform's state.
`Proxy.Attach` destroys every view of the last session, and the new
run's mount creates them again with new node IDs, so:

- a long list scrolls back to the top;
- the text field being typed in loses focus and the keyboard drops;
- a video starts over, a web view loads its page again;
- the whole screen flashes as it's torn down and built again.

React Fast Refresh, Flutter and Compose Hot Reload all keep the
existing UI tree and update it, which is what makes their reloads feel
like nothing happened but the edit. Asuka restarts the process, so the
core's tree can't be kept, but the player's can: the player outlives
the run, and its views are still there when the new run connects.

## Design

### Where it happens

In `Proxy` (internal/remote/player.go), between the core and the
platform host. The host keeps knowing its views by the IDs it was told
when it made them; the proxy translates the new run's IDs to the ones
the host already has. Hosts don't change.

### What the proxy keeps

For the session that ends (its connection drops), instead of destroying
its views at the next `Attach`, the proxy keeps a shadow of its tree,
built from the ops it forwarded:

- each node: its ID, kind, parent, place among its parent's children,
  the props it was set (IDs and values) and the events it listened to;
- the root.

The proxy already tracks the live IDs (`x.live`) and the root; this
extends that to the tree's shape and props. Memory is per view, freed
as views are destroyed: small next to the views themselves.

### Telling nodes apart: places

Matching by position alone (the third child of the root's first
child) breaks as soon as a view is added before others. So, in a
development build, the core tells the proxy where each node was made:
its **place**, the call path of the code that made it, named as kept
state names its signals (`signal.KeepPlaces`: closures by what they are
in their function), and its ordinal among the nodes made at that place
under the same parent.

- The core sends it as a development-only op or prop on `OpCreate`,
  only when keeping is on (`asuka dev`, a places table). The proxy
  takes it out of the batch: hosts never see it, and a built app never
  sends it.
- `ui`'s element constructors take the call path the same way
  `keepSignal` does, skipping Asuka's own frames. The cost is a
  `runtime.Callers` per created element, in development builds only.

### Matching

When the new run's first batch arrives (the mount, up to its
`OpSetRoot`), the proxy matches the new tree against the shadow, top
down:

1. The new root matches the old root if their kinds are equal.
2. Under a matched pair, each new child matches the old child of the
   same kind and place, and the same ordinal among that place's
   children. When no places came (an older core), the fallback is
   kind and ordinal among siblings of that kind.
3. A new node matches nothing if its parent didn't match.

A node also doesn't match when a prop that's fixed at creation differs
(a view whose kind of content can't change after it's made). The
schema (`ops/schema.go`) has no such mark today; it would need one,
generated with the rest, rather than a list kept by hand in the proxy.

### Rewriting the batch

With the matches made, the proxy rewrites the new run's ops before the
host sees them:

- **Creates** of matched nodes are dropped. From then on, every op's
  node ID for the session goes through a translation table, new ID to
  old. Events and measure requests from the host go back the other way.
- **Props**: the new batch sets every prop the new tree wants. For a
  reused view, the proxy also resets the props the old one had set and
  the new one doesn't, to the schema's default, so nothing stale
  survives. A prop set to the value it already has is dropped:
  setting a text field's value to its current value mustn't move its
  caret.
- **Children**: the proxy compares each reused parent's old and new
  child lists, and emits the fewest `OpRemove`/`OpInsert` moves (a
  longest increasing subsequence, as keyed list diffing does). Moving a
  focused field out of its parent would resign it, so children that
  keep their order aren't touched.
- **Listen/Unlisten**: the events the old view listened to and the new
  one doesn't are unlistened.
- **Destroys**: old views that matched nothing are destroyed after the
  batch, children first, as the core does.
- **Lists** (`ui.List`): the list view is reused, so its scroll offset
  is kept. The new run's mount sends its `CmdListUpdate` with the
  items' IDs, which are the app's and likely the same as before, so a
  host's diffable data may see nothing changed. But the cells on
  screen hold views bound (`ListBind`) to the old run's nodes, and
  must be bound again to the new run's. How the proxy asks the host to
  rebind its visible cells is open (below).

Frames come as they do now, in the frames batch after layout: a reused
view that keeps its size doesn't move.

If anything is inconsistent (an op names a node the proxy can't
place, the root's kind changed), the proxy falls back to what it does
today: destroy everything and apply the new batch as is. A reload is
never worse than now.

### Desktop and web

The desktop hosts run in the app's own process, which a reload ends,
so their windows can't be kept this way. That could change if
`asuka dev` on a desktop used the player's split as phones do: a
long-lived host process (the player for this machine) and the app's Go
code in the process that reloads, over the same protocol. That would be
a separate RFC; this one is written so the proxy's matching is reusable
there.

The web host could do the same in JavaScript: keep the DOM and the
host, start the new build's WebAssembly, and have the page translate
IDs as the proxy does. Also a later RFC.

## What it gives

| Kept across a reload | Today | With this |
|---|---|---|
| Signals' values | yes | yes |
| Scroll offsets (scroll views, lists) | no | yes |
| Focus, caret, selection, keyboard | no | yes |
| Playing video and audio views | no | yes |
| A web view's page and its JavaScript | no | yes |
| A map's camera | no | yes |
| Screens pushed | no | with 0002 |

## Testing

In Go, with the fake host as the player, as the protocol's tests are
(internal/remote):

- Reloading an unchanged app sends the host no `OpCreate` and no
  `OpDestroy`: counted in the fake host's op log.
- Adding a view before others creates exactly that view, and moves
  none that kept their order.
- A view whose kind changed is destroyed and a new one created.
- A prop the new tree doesn't set is reset to its default; one set to
  its current value isn't sent.
- Events from the host reach the new run's handlers through the
  translation, and measure requests come back the right way.
- A forced inconsistency falls back to clearing everything.
- On the simulator and the emulator (e2e): a list scrolled down stays
  scrolled down after an edit; a focused field keeps focus and the
  keyboard stays up.

## Open questions

- Should places for elements reuse the signal places table (closures
  and call sites), or does `ui` need its own table of element
  constructor sites? Elements are mostly made by `ui` calls in app
  code, so the call path and the closures' names are likely enough.
- Which props are fixed at creation, per kind? This needs a pass over
  `ops/schema.go` and a flag there.
- Rebinding a reused list's visible cells: a command to the host
  (every host's list would need it), or the proxy unbinding and
  binding them itself through the core's list requests?
- Animations in flight during a reload: let them finish, or cancel
  them?
- Native packages' views (`native` views from an app's Swift or
  Kotlin): reused as any view? Their own state is theirs, so likely
  yes, but their props may not have schema defaults.

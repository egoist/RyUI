# RFCs

Proposals for changes big enough to want a design before code: what
they'd change, why, how, and what's still open. DESIGN.md stays the
source of truth for what Asuka *is*; an RFC that's accepted and built
moves its design there, and the RFC keeps the reasoning.

Each RFC is a file `NNNN-short-name.md` with a header:

- **Status**: Draft, Accepted, Built (with the commit or PR), Rejected,
  or Withdrawn.
- **Created**, and the RFCs or PRs it relates to.

## Index

| RFC | Title | Status |
|---|---|---|
| [0001](0001-keep-native-views-across-reloads.md) | Keep the player's native views across reloads | Draft |
| [0002](0002-restore-navigation-across-reloads.md) | Restore navigation across reloads | Draft |
| [0003](0003-in-process-code-patching.md) | In-process code patching and an interpreted app package | Rejected |
| [0004](0004-asuka-test.md) | asuka test: the same UI tests on the platforms' real views | Draft |
| [0005](0005-devtools.md) | Devtools: debugging, inspecting and profiling an app | Draft |

## Background: hot reload today

`asuka dev` (DESIGN.md, docs/development.md) rebuilds the app's Go code
on a save and runs it again: in a player on a phone, in its own window
on a desktop, in the page on the web. The next run starts behind the
last (`internal/devstate`'s `Wait`), and the app's signals come back by
names read from the source (`signal.KeepPlaces`). What a reload still
loses:

- Native view state: scroll positions, focus and the caret, a playing
  video, a web view's page, a map's camera, the keyboard. The player
  clears the last session's views and the new run mounts from scratch
  (0001).
- Navigation: screens are closures, so a reload starts at the root
  screen (0002).

Both came out of a survey of recent hot reload work (React Fast
Refresh, Flutter's stateful hot reload on the web, Compose Hot Reload,
Dioxus's Subsecond, InjectionNext), in which keeping the existing UI
tree, rather than tearing it down, is what makes a reload feel
seamless. 0003 records why patching the running process, the other
direction that survey points to, doesn't fit Asuka.

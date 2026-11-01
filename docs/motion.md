# Motion and gestures

RyUI animates with each platform's own machinery: UIView animations on
iOS, animators on Android. You change state inside `ui.Animate`, and
what that change moves on screen animates. This page covers animating
changes, transitions in and out, shared elements, frame-driven
animation, gestures, haptics and Reduce Motion.

## Animate a change

Wrap the write in `ui.Animate`. Whatever the next flush changes then
animates: frames that layout moves, and the visual props listed below.

```go
func Details() ui.Node {
	expanded := signal.New(false)
	return ui.Column(ui.Style{Gap: 12, Padding: ui.All(20)},
		ui.Button(ui.ButtonProps{Title: ui.Val("More"), OnPress: func() {
			ui.Animate(func() { expanded.Set(!expanded.Peek()) })
		}}),
		ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			h := float32(80)
			if expanded.Get() {
				h = 240
			}
			return ui.Style{Height: ui.Pt(h), CornerRadius: 16, Background: ui.SecondaryBackground}
		})}),
	)
}
```

The box grows, and everything below it slides down with it: layout runs
once, and each frame that changed animates to its new place. You don't
animate positions yourself.

The same write without `ui.Animate` is applied at once. `ui.Animate`
runs on the main thread, as every signal write does; from a goroutine,
call it inside `ui.Post`.

## Duration, curve and repeat

`ui.AnimateWith` takes a `ui.Animation`:

- `Duration` is in seconds; 0 means 0.25.
- `Curve` is `ui.EaseInOut` (the default), `ui.Linear`, `ui.EaseIn`,
  `ui.EaseOut` or `ui.Spring`. A spring settles with a little overshoot:
  UIKit's spring animation on iOS, an overshoot interpolator on Android.
  It still takes `Duration`.
- `Repeat` runs it again this many times more; `ui.Forever` repeats it
  until the props change again. With `Autoreverses`, each run goes back
  before the next.

```go
func Pulse() ui.Node {
	on := signal.New(false)
	dot := ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
		s := ui.Style{Width: ui.Pt(24), Height: ui.Pt(24), CornerRadius: 12, Background: ui.Red}
		if on.Get() {
			s.Transform = ui.Transform{Scale: 1.6}
			s.Opacity = 0.35
		}
		return s
	})})
	start := func() {
		ui.AnimateWith(ui.Animation{Duration: 0.8, Curve: ui.EaseInOut, Repeat: ui.Forever, Autoreverses: true},
			func() { on.Set(true) })
	}
	stop := func() { on.Set(false) } // set without an animation: the pulse stops
	return ui.Row(ui.Style{Gap: 16},
		dot,
		ui.Button(ui.ButtonProps{Title: ui.Val("Pulse"), OnPress: start}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Stop"), OnPress: stop}),
	)
}
```

A repeating animation stops when its props are set again without an
animation. On Android, only opacity and transforms repeat; on iOS,
everything that animates does.

## What animates

| Change | iOS | Android |
| --- | --- | --- |
| Frames (position and size, from layout) | yes | yes |
| `Opacity` | yes | yes |
| `Transform` | yes | yes |
| `Background` (a color) | yes | yes |
| `CornerRadius` | yes | yes |
| A `Shape`'s `TrimStart` and `TrimEnd` | yes | yes |
| A `Shape`'s path | yes | no |

Other props (`BorderColor`, `Shadow`, `Gradient`, text color and fonts)
change at once. A view only animates if it's on screen; views that
appear during `ui.Animate` don't animate in, unless they have a
[transition](#enter-and-exit).

`Opacity` 0 is the zero value and means opaque, so a view can't animate
to fully transparent by its `Opacity`. To fade a view out, remove it
with a `Fade` transition.

## Transforms

`Style.Transform` moves, scales and rotates a view around its center as
it's drawn. Layout doesn't see it: its neighbors stay where they are.

- `TranslateX`, `TranslateY`: an offset, in points.
- `Scale`: both axes (0 means 1); `ScaleX`, `ScaleY` set one axis.
- `Rotate`: degrees, clockwise.

```go
func Flip() ui.Node {
	flipped := signal.New(false)
	return ui.View(ui.ViewProps{
		OnPress: func() {
			ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.5}, func() { flipped.Set(!flipped.Peek()) })
		},
		Style: ui.StyleFunc(func() ui.Style {
			t := ui.Transform{}
			if flipped.Get() {
				t = ui.Transform{Rotate: 6, Scale: 1.04}
			}
			return ui.Style{Height: ui.Pt(120), CornerRadius: 20, Background: ui.Accent, Transform: t}
		}),
	})
}
```

## Enter and exit

`Style.Transition` is the state a view comes in from when it's added
during `ui.Animate` (a `Show` turning true, an item added to a `For`),
and goes out to when it's removed during one. A removed view stays on
screen until it has gone out, and takes no touches meanwhile.

- `ui.Fade` fades in and out.
- `ui.Pop` fades and grows in from 85% (a popover, a badge).
- `ui.SlideUp` fades in from 24 points below (a toast, a new row).
- `ui.Transition{...}` is your own: `Fade`, `Scale` (0: none) and an
  offset `X`, `Y` in points. They combine: `ui.Transition{Fade: true,
  Scale: 0.5, Y: -8}`.

```go
func Toast() ui.Node {
	shown := signal.New(false)
	save := func() {
		ui.Animate(func() { shown.Set(true) })
		ui.Announce("Saved")
		go func() {
			time.Sleep(1500 * time.Millisecond)
			ui.Post(func() { ui.Animate(func() { shown.Set(false) }) })
		}()
	}
	return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}},
		ui.Button(ui.ButtonProps{Title: ui.Val("Save"), OnPress: save}),
		ui.Show(shown, func() ui.Node {
			return ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Left: ui.Pt(20), Right: ui.Pt(20), Bottom: ui.Pt(20),
				Background: ui.SecondaryBackground, CornerRadius: 16, Padding: ui.Symmetric(14, 18),
				Transition: ui.SlideUp}},
				ui.StaticText("Saved"))
		}),
	)
}
```

Transitions run only inside `ui.Animate`, with its duration and curve.
The same `shown.Set(true)` without it shows the toast at once. A view
only transitions in if its parent is already on screen.

## Shared elements

`Style.Match` is a key a view shares with another, as SwiftUI's
`matchedGeometryEffect`. When, during `ui.Animate`, a view with the key
goes and another with it comes, the new one grows from the old one's
place and size into its own, while the old one moves along with it and
fades. Here a row opens into a card, its artwork and title moving with
it (from [`examples/gallery`](../examples/gallery)):

```go
func Matched() ui.Node {
	open := signal.New(false)
	toggle := func() {
		ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.5}, func() { open.Set(!open.Peek()) })
	}
	art := func(size float32, radius float32) ui.Style {
		return ui.Style{Width: ui.Pt(size), Height: ui.Pt(size), CornerRadius: radius, Background: ui.Orange, Match: "album"}
	}
	return ui.Show(open, func() ui.Node {
		return ui.View(ui.ViewProps{OnPress: toggle, Style: ui.Style{Gap: 12, Align: ui.AlignCenter}},
			ui.View(ui.ViewProps{Style: art(240, 24)}),
			ui.StaticText("Night Drive", ui.Style{Font: ui.Title2, Match: "album-title"}))
	}, func() ui.Node {
		return ui.View(ui.ViewProps{OnPress: toggle, Style: ui.Style{Direction: ui.RowDir, Gap: 12, Align: ui.AlignCenter}},
			ui.View(ui.ViewProps{Style: art(56, 10)}),
			ui.StaticText("Night Drive", ui.Style{Font: ui.Headline, Match: "album-title"}))
	})
}
```

- Both views must change in the same `ui.Animate`: one removed, one
  added.
- Keys belong to the root: one view at a time has each.
- A view that goes with a key and has no partner fades out.

## Zoom into a pushed screen

`ScreenOptions.ZoomFrom` names the `Match` key of a view on the screen
below. The pushed screen grows out of that view, and shrinks back into
it when it's popped. It works for a `List`'s cells, as in this grid of
photos:

```go
func Photos() ui.Node {
	ids := []int{10, 11, 12, 13, 14, 15}
	return ui.List(ui.ListProps{Columns: 3, Gap: 2}, ui.Val(ids), func(id int) int { return id },
		func(id signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
			nav := ui.UseNavigator()
			return ui.View(ui.ViewProps{
				Label: "Photo",
				OnPress: func() {
					i := signal.Untrack(id.Get)
					nav.Push(func() ui.Node { return Photo(i) },
						ui.ScreenOptions{Title: ui.Val("Photo"), ZoomFrom: fmt.Sprintf("photo-%d", i)})
				},
				Style: ui.StyleFunc(func() ui.Style {
					return ui.Style{AspectRatio: 1, Match: fmt.Sprintf("photo-%d", id.Get())}
				}),
			}, ui.Image(ui.ImageProps{
				Source: signal.Func(func() string { return fmt.Sprintf("https://picsum.photos/id/%d/300/300", id.Get()) }),
				Mode:   ui.ImageFill, Style: ui.Style{Grow: 1},
			}))
		})
}
```

- **iOS 18 and later** use UIKit's zoom transition; the interactive
  pop gesture shrinks the screen back into the view. Earlier versions
  push as usual.
- **Android** runs Material's container transform: the screen grows
  from the view into its bounds, rounded, the two cross-fading near the
  view. A predictive back gesture shrinks it back.
- If no view with the key is on the screen below, it pushes as usual.

You don't need `ui.Animate` for this: pushes animate on their own. See
[navigation](navigation.md#stacks).

## Shapes that draw themselves

A `Shape`'s `TrimStart` and `TrimEnd` are the part of its outline
drawn, from 0 to 1. Change them inside `ui.Animate` and the stroke
draws itself: a progress ring, a check mark.

```go
func ring(w, h float32) *ui.Path {
	r := min(w, h)/2 - 4
	return new(ui.Path).Arc(w/2, h/2, r, -math.Pi/2, 3*math.Pi/2, true)
}

func Ring() ui.Node {
	done := signal.New(float32(0.3))
	return ui.Column(ui.Style{Gap: 12},
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: ui.Accent, LineWidth: 8, LineCap: ui.CapRound, TrimEnd: done,
			Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Finish"), OnPress: func() {
			ui.AnimateWith(ui.Animation{Duration: 0.8}, func() { done.Set(1) })
		}}),
	)
}
```

## Frame by frame

When the motion is computed rather than declared (physics, a game, a
gauge following a value), `ui.OnFrame` calls a function at each frame
the display draws, 60 or 120 times a second, with the frame's time.
What it writes shows in that frame. It runs until you call the `stop`
it returns, or until the component's owner is disposed. While no
callback is registered, the host asks for no frames.

```go
func Spinner() ui.Node {
	env := ui.UseEnvironment()
	angle := signal.New(float32(0))
	var last time.Duration
	ui.OnFrame(func(t time.Duration) {
		if last != 0 && !env.Get().ReduceMotion {
			a := angle.Peek() + float32((t-last).Seconds()*180) // half a turn a second
			angle.Set(float32(math.Mod(float64(a), 360)))
		}
		last = t
	})
	return ui.View(ui.ViewProps{Decorative: true, Style: ui.StyleFunc(func() ui.Style {
		return ui.Style{Transform: ui.Transform{Rotate: angle.Get()}}
	})}, ui.Symbol(ui.IconSettings, ui.Style{Font: ui.LargeTitle, Color: ui.Accent}))
}
```

Base the motion on the time between frames, not on the count of
frames: the rate differs between devices.

## Gestures

A `View` takes gestures, recognized by the platform's recognizers:

- `OnPress` and `OnLongPress`: a tap, and a touch held.
- `OnPan`: a drag.
- `OnPinch`: two fingers spreading or closing.
- `OnRotate`: two fingers turning.
- `OnSwipe`: a quick flick, with its `ui.SwipeDirection`: `ui.SwipeLeft`,
  `ui.SwipeRight`, `ui.SwipeUp` or `ui.SwipeDown`.

Pan, pinch and rotate call their function with a `ui.Gesture` as it goes:

- `State`: `ui.GestureBegan`, `ui.GestureChanged`, then
  `ui.GestureEnded`, or `ui.GestureCancelled` if the system took the
  touch away.
- `X`, `Y`: where the touch is, in the view, in points.
- `TranslationX`, `TranslationY`: how far a pan has moved since the finger
  came down, so `X - TranslationX` is where it came down.
- `VelocityX`, `VelocityY`: a pan's speed, in points per second.
- `Scale`: a pinch's scale since it began (1 for a pan).
- `Rotation`: a rotation's angle since it began, in radians, clockwise.

A dragged view follows the finger through its transform, and springs
back when it's let go:

```go
func Puck() ui.Node {
	drag := signal.New(ui.Gesture{})
	return ui.View(ui.ViewProps{
		OnPan: func(g ui.Gesture) {
			if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
				ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.45}, func() { drag.Set(ui.Gesture{}) })
				return
			}
			drag.Set(g)
		},
		Style: ui.StyleFunc(func() ui.Style {
			g := drag.Get()
			return ui.Style{Width: ui.Pt(64), Height: ui.Pt(64), CornerRadius: 32, Background: ui.Accent,
				Transform: ui.Transform{TranslateX: g.TranslationX, TranslateY: g.TranslationY}}
		}),
	})
}
```

Scale and rotation are since the gesture began, so keep where the last
one left off to add the next to it. Gestures on the same view work
together: pinch and turn at once.

```go
angle, base := signal.New(float32(0)), float32(0)
scale, baseScale := signal.New(float32(1)), float32(1)
return ui.View(ui.ViewProps{
	Label: "A square to turn",
	OnRotate: func(g ui.Gesture) {
		angle.Set(base + g.Rotation*180/math.Pi)
		if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
			base = angle.Peek()
		}
	},
	OnPinch: func(g ui.Gesture) {
		scale.Set(min(max(baseScale*g.Scale, 0.5), 2))
		if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
			baseScale = scale.Peek()
		}
	},
	Style: ui.StyleFunc(func() ui.Style {
		return ui.Style{Width: ui.Pt(160), Height: ui.Pt(160), CornerRadius: 24, Background: ui.Purple,
			Transform: ui.Transform{Rotate: angle.Get(), Scale: scale.Get()}}
	}),
})
```

Once a pan, pinch or rotation has begun, it has the touch: a scroll
view around it doesn't take it over, and a pressable view inside it
gets no press, as a drag that starts on a button doesn't press it.

A swipe or a pan can't be done with a screen reader on. Offer what it
does as an [accessibility action](accessibility.md#custom-actions) too.

## Pressed and hover states

A `View` reports its interaction states as they change, and a style
can follow them in three ways. Start with the first one that does the
job:

1. **Variants**, for the view's own look. `Style.Pressed`, `Hovered`,
   `Dragging` and `DragOver` are laid over the rest of the style while
   the view is in that state. They are CSS's `:active` and `:hover`, or
   Tailwind's `active:` and `hover:`.
2. **An `Interaction`**, for other views and anything else that follows
   the state. Give the view one, then read `it.Pressed()` or
   `it.Hovered()` wherever you need them, such as `Style.When`, a
   `StyleFunc`, a `Text` or a `Show`. This is how a child follows its
   parent (a group's hover) and a sibling follows its neighbor (a peer's).
3. **Handlers**, for what isn't a style, such as starting a recording:
   `OnPressedChange` and `OnHoverChange`.

```go
func Card(title string, open func()) ui.Node {
	card := ui.NewInteraction()
	return ui.View(ui.ViewProps{
		OnPress:     open,
		Interaction: card,
		Style: ui.Style{Direction: ui.RowDir, Padding: ui.All(16), CornerRadius: 12,
			Background: ui.SecondaryGroupedBackground,
			Hovered:    &ui.Style{Background: ui.SecondaryBackground},
			Pressed:    &ui.Style{Background: ui.Separator, Transform: ui.Transform{Scale: 0.97}}},
	},
		// The card's hover, followed by its title: a group's hover.
		ui.StaticText(title, ui.Style{Grow: 1}.When(card.Hovered, ui.Style{Color: ui.Accent})),
	)
}
```

The states:

- **Pressed** is true while a finger, or the mouse's primary button, is
  down on the view.
  - In a scrolling container it comes on a moment late, so a scroll
    doesn't flash it.
  - It goes off when the finger lifts, moves off the view (moving back
    on presses it again), or a scroll, a long press or another gesture
    takes over the touch.
  - `OnPress` fires only for a release while pressed.
- **Hovered** is true while a pointer is over the view or its children:
  a mouse, a trackpad, an iPad's pointer, or a hovering Apple Pencil or
  stylus. A touch screen alone never hovers.
- **Dragging** is true while the view is being dragged (its `Drag`).
- **DragOver** is true while a drag that its `Drop` takes is over it.

How variants and `When` combine:

- A variant sets only the fields it names. The rest stay as the style
  has them.
- `Font` is merged field by field, so `Font{Weight: ui.Bold}` keeps the
  text style. Every other field is replaced whole.
- Variants apply in the order Hovered, Pressed, Dragging, DragOver, so
  pressed wins over hovered.
- `When` calls chain, and later ones win.
- A zero field means "not set", so a state can't clear a field. To show
  something only in a state, set the field in the other state with
  `Unless`:

```go
row := ui.NewInteraction()
ui.View(ui.ViewProps{Interaction: row},
	ui.Text(name),
	// The delete button shows while the row is hovered.
	ui.View(ui.ViewProps{OnPress: remove, Style: ui.Style{}.Unless(row.Hovered, ui.Style{Hidden: true})},
		ui.Symbol(ui.IconTrash)),
)
```

`When` and `Unless` take any `func() bool`, so a style can follow any
signal too: `base.When(selected.Get, ui.Style{Background: ui.Accent})`.

A view tracks only the states that something reads: its variants, or
reads of its `Interaction`.

- A view with no `Pressed` variant and no reader of `Pressed` doesn't
  report its pressed state, and keeps the platform's highlight.
- Once a view reports its pressed state, it draws its own. The
  platform's highlight (Android's ripple, Windows' hover and pressed
  fills) is left off.

Each state is reported once per change. A view that goes away while in
a state leaves it, so nothing that follows it stays stuck. Give the same
`Interaction` to several views and a state is true while any of them is
in it.

Without `OnPress`, a press held is a hold, as in hold to record. The
recording is a side effect, so it goes in the handler:

```go
ui.View(ui.ViewProps{
	OnPressedChange: func(on bool) {
		if on {
			recorder.Start()
		} else {
			recorder.Stop()
		}
	},
	Label: "Hold to record",
	Style:           ui.Style{Padding: ui.All(16), Pressed: &ui.Style{Background: ui.Red}},
}, ...)
```

## Drag and drop

`ViewProps.Drag` makes a view draggable. On a touch screen, a long
press then a move lifts it, and that long press doesn't also fire
`OnLongPress`. With a mouse, a move with the button held drags it. The
view's image follows the finger or pointer, within the app and into
other apps. `ViewProps.Drop` makes a view take drops.

A `ui.DragItem` is a text, a link (`Kind: ui.SharedLink`), a file
(`Path`), or a value of the app's (`Value`):

- Inside the app, a drop gets each item as it is, Go value and all.
- Other apps get the item's text, link or file. An item that's only a
  value goes nowhere else.
- `MIME` is the item's type, which targets accept it by. Texts, links
  and files get theirs filled in. Give a `Value` a type of the app's
  own, so only its targets take it.

```go
type Task struct{ Title string; Done bool }

func TaskCard(t *Task) ui.Node {
	return ui.View(ui.ViewProps{
		Drag: &ui.DragSource{
			Items: func() []ui.DragItem {
				return []ui.DragItem{{Value: t, MIME: "application/x-myapp-task", Text: t.Title}}
			},
		},
		// Faded while it's dragged.
		Style: ui.Style{Padding: ui.All(12), Dragging: &ui.Style{Opacity: 0.4}},
	}, ui.StaticText(t.Title))
}

func DoneColumn(move func(*Task), cards ...ui.Node) ui.Node {
	return ui.View(ui.ViewProps{
		Drop: &ui.DropTarget{
			Accept: []string{"application/x-myapp-task"},
			OnDrop: func(d ui.Drop) {
				for _, it := range d.Items {
					if t, ok := it.Value.(*Task); ok {
						move(t)
					}
				}
			},
		},
		// Outlined while a task is dragged over it.
		Style: ui.Style{Gap: 8, Padding: ui.All(8), BorderWidth: 2, BorderColor: ui.Clear,
			DragOver: &ui.Style{BorderColor: ui.Accent}},
	}, cards...)
}
```

The drag source:

- `Items` runs in an effect, so what it reads keeps it up to date. A
  drag carries what the items were when it began.
- `OnPhase` reports `ui.DragBegan` as the view lifts. Then it reports
  either `ui.DragDropped`, when a target took it (for a move, remove
  what was dragged), or `ui.DragCancelled`. For the dragged look, use
  the `Dragging` variant or `Interaction.Dragging`.

The drop target:

- `Accept` lists MIME types: exact (`"text/plain"`), a family
  (`"image/*"`) or anything (`"*/*"`).
- A drag with no item of those types never comes over the view.
- Of nested targets, the innermost one that takes a drag gets it.
- `OnOverChange` reports a drag coming over the view and leaving it,
  the same state as the `DragOver` variant.
- `OnMove` reports where the drag is over the view, as an insertion
  point.
- `OnDrop` gets the items it takes and where they were dropped.
  `Drop.Local` says the drag came from the app.

Drops from other apps (files from Finder or Files, a photo from Photos,
a link from a browser) come once their items are loaded:

- On iOS and Android, files are copied into the app's caches first and
  `Drop.Copied` is true. Move them where the app keeps them, or remove
  them when done.
- On macOS and Windows, `Path` is the user's file itself. Don't remove
  it.

## Haptics

`ui.Haptic` plays feedback: `ui.HapticSelection` (a value ticked over),
`ui.HapticLight`, `ui.HapticMedium` and `ui.HapticHeavy` (impacts), and
`ui.HapticSuccess`, `ui.HapticWarning` and `ui.HapticError` (an
outcome).

```go
OnSwipe: func(d ui.SwipeDirection) {
	switch d {
	case ui.SwipeLeft:
		page.Set(page.Peek() + 1)
	case ui.SwipeRight:
		page.Set(max(page.Peek()-1, 0))
	}
	ui.Haptic(ui.HapticSelection)
},
OnLongPress: func() { ui.Haptic(ui.HapticMedium) },
```

On iOS these are `UIFeedbackGenerator`'s. Android has no exact
equivalents: RyUI plays the closest of `performHapticFeedback`'s
constants, and success, warning and error are confirm and reject from
Android 11; before that, a key press or a long press.

## Reduce Motion

`ui.UseEnvironment().Get().ReduceMotion` is true when the user asked for
less motion: iOS's Reduce Motion, or Android's animations turned off
(Developer options, or Accessibility, Remove animations). It changes as
the user changes the setting.

What RyUI does about it on its own:

- **iOS**: transitions dissolve in place instead of moving or scaling,
  as UIKit's own do then, and shared elements (`Match`) don't fly: the
  views come and go with their transitions. Other animations run.
- **Android**: the system's animator scale is 0, so every animation,
  RyUI's and the platform's, ends at once.

What's left to you is motion RyUI can't tell is decorative: an `OnFrame`
loop (as the spinner above stops), a repeating pulse, an animation that
moves something far. Read `ReduceMotion` and do less.

# モーションとジェスチャー

Asuka は、プラットフォームにアニメーションの仕組みがあればそれを使います。iOS では UIView のアニメーション、Android ではアニメーター、macOS では AppKit の暗黙的アニメーションと Core Animation、ブラウザーでは Web Animations API です。Windows と GTK にはビューのフレームをアニメーションさせる仕組みがないため、Windows と Linux のホストは同じカーブを使って、フレームごとに自前でアニメーションを進めます。`ui.Animate` の中で状態を変えると、その変更によって画面上で動くものがアニメーションします。このページでは、変更のアニメーション、表示と非表示のトランジション、共有要素、フレーム単位のアニメーション、タッチ・マウス・トラックパッドによるジェスチャー、触覚フィードバック、「視差効果を減らす」（Reduce Motion）について説明します。

## 変更のアニメーション

書き込みを `ui.Animate` で囲みます。すると、次のフラッシュで変わるものがアニメーションします。対象は、レイアウトによって動くフレームと、後述の見た目に関するプロパティです。

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
			return ui.Style{Height: ui.Pt(h), CornerRadius: 16, Background: colors.SecondaryBackground}
		})}),
	)
}
```

ボックスが大きくなり、その下にあるものもすべて一緒に下へずれていきます。レイアウトは一度だけ実行され、変わったフレームがそれぞれ新しい位置へアニメーションします。位置を自分でアニメーションさせる必要はありません。

同じ書き込みを `ui.Animate` なしで行うと、変更はすぐに反映されます。`ui.Animate` は、ほかのシグナルの書き込みと同じくメインスレッドで実行します。ゴルーチンから呼ぶときは `ui.Post` の中で呼んでください。

## 再生時間、カーブ、繰り返し

`ui.AnimateWith` は `ui.Animation` を受け取ります。

- `Duration` は秒単位です。0 は 0.25 秒を意味します。
- `Curve` は `ui.EaseInOut`（デフォルト）、`ui.Linear`、`ui.EaseIn`、`ui.EaseOut`、`ui.Spring` のいずれかです。スプリングは減衰するばねで、`Duration` が終わるまでに静止します。iOS では UIKit のスプリングアニメーションを、Android、Windows、Linux、Web では同じばねの補間関数を使います。
- `Damping` はスプリングの減衰比です。1 なら終点を越えずに静止し、それより小さいと行き過ぎてから戻ってきます（0 は 0.85 を意味し、少しだけ行き過ぎます）。`Velocity` は開始時の速さで、1 秒あたりに変化量の何割進むかで表します（UIKit の `initialSpringVelocity`）。フリックの勢いを引き継ぐのに使います。
- `Delay` は、開始までに待つ秒数です。
- `OnDone` は、アニメーションが論理的に完了したとき（`Delay` と `Duration` の後、繰り返すならその回ごと）に呼ばれます。次のステップはここからつなげます。
- macOS の AppKit の暗黙的アニメーションには、スプリングも遅延もありません。弾むスプリングはカーブで行き過ぎる動きになり、遅延は無視されます。
- `Repeat` は、指定した回数だけアニメーションを追加で繰り返します。`ui.Forever` なら、プロパティがまた変わるまで繰り返し続けます。`Autoreverses` を指定すると、毎回、次の回の前に元へ戻ります。macOS と Windows は繰り返さず、アニメーションは一度だけ実行されます。

```go
func Pulse() ui.Node {
	on := signal.New(false)
	dot := ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
		s := ui.Style{Width: ui.Pt(24), Height: ui.Pt(24), CornerRadius: 12, Background: colors.Red}
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
	stop := func() { on.Set(false) } // アニメーションなしで設定すると、パルスが止まる
	return ui.Row(ui.Style{Gap: 16},
		dot,
		ui.Button(ui.ButtonProps{Title: ui.Val("Pulse"), OnPress: start}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Stop"), OnPress: stop}),
	)
}
```

ジェスチャーの終わりをスプリングに引き継ぐことはよくあります。ジェスチャーの速度を残りの距離で割ったものをスプリングの `Velocity` にすると、指が与えた速さのままビューが動き続けます。

```go
OnPan: func(g ui.Gesture) {
	if g.State != ui.GestureEnded {
		drag.Set(g) // 指に追従する
		return
	}
	d := math.Hypot(float64(g.TranslationX), float64(g.TranslationY))
	v := 0.0
	if d > 1 { // 元の位置へ向かう速さ（戻る方向に沿った成分）
		v = -(float64(g.VelocityX*g.TranslationX) + float64(g.VelocityY*g.TranslationY)) / d / d
	}
	ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.6, Damping: 0.55, Velocity: float32(v)},
		func() { drag.Set(ui.Gesture{}) })
},
```

連続するステップは `OnDone` でつなげます。次の例は、現れて、しばらく表示され、消えていくトーストです。

```go
ui.AnimateWith(ui.Animation{OnDone: func() {
	ui.AnimateWith(ui.Animation{Delay: 1.5}, func() { shown.Set(false) })
}}, func() { shown.Set(true) })
```

繰り返すアニメーションは、そのプロパティがアニメーションなしで再び設定されると止まります。Android で繰り返すのは不透明度と変形だけです。iOS、Linux、Web ではアニメーションするものすべてが繰り返します。macOS と Windows では一度だけ実行されます。

## アニメーションするもの

| 変更 | iOS | Android | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- | --- | --- |
| フレーム（レイアウトによる位置とサイズ） | ○ | ○ | ○ | ○ | ○ | ○ |
| `Opacity` | ○ | ○ | ○ | × | ○ | ○ |
| `Transform` | ○ | ○ | ○ | ○ | ○ | ○ |
| `Background`（色） | ○ | ○ | × | × | × | ○ |
| `CornerRadius` | ○ | ○ | ○ | × | × | × |
| `Shape` の `TrimStart` と `TrimEnd` | ○ | ○ | ○ | × | ○ | × |
| `Shape` のパス | ○ | × | ○ | × | × | × |

ほかのプロパティ（`BorderColor`、`Shadow`、`Gradient`、文字色やフォント）はすぐに切り替わります。アニメーションするのは画面上にあるビューだけです。`ui.Animate` の間に現れたビューは、[トランジション](#表示と非表示のトランジション)を持っていない限り、アニメーションしながら現れることはありません。

`Opacity` の 0 はゼロ値で、不透明を意味します。そのため、`Opacity` でビューを完全な透明までアニメーションさせることはできません。ビューをフェードアウトさせたいときは、`Fade` トランジションを付けてビューを取り除きます。

## 変形

`Style.Transform` は、ビューを描画するときに、その中心を基準に移動・拡大縮小・回転させます。レイアウトはこれを考慮しないので、隣のビューは元の位置のままです。

- `TranslateX`、`TranslateY`：オフセット（ポイント単位）。
- `Scale`：両方の軸の拡大率（0 は 1 を意味します）。`ScaleX`、`ScaleY` は片方の軸だけを設定します。
- `Rotate`：角度（度単位、時計回り）。

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
			return ui.Style{Height: ui.Pt(120), CornerRadius: 20, Background: colors.Accent, Transform: t}
		}),
	})
}
```

## 表示と非表示のトランジション

`Style.Transition` は、`ui.Animate` の間にビューが追加されたとき（`Show` が true になる、`For` に項目が加わるなど）にどの状態から現れるか、そして `ui.Animate` の間に取り除かれたときにどの状態へ消えていくかを表します。取り除かれたビューは消え終わるまで画面に残り、その間はタッチを受け付けません。

- `ui.TransitionFade` はフェードイン・フェードアウトします。
- `ui.TransitionPop` はフェードしながら 85% の大きさから拡大して現れます（ポップオーバーやバッジ向け）。
- `ui.TransitionSlideUp` は 24 ポイント下からフェードインします（トーストや新しい行向け）。
- `ui.Transition{...}` は独自のトランジションです。`Fade`、`Scale`（0 なら拡大縮小なし）、ポイント単位のオフセット `X`、`Y` を指定でき、組み合わせることもできます（`ui.Transition{Fade: true, Scale: 0.5, Y: -8}`）。

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
				Background: colors.SecondaryBackground, CornerRadius: 16, Padding: ui.Symmetric(14, 18),
				Transition: ui.TransitionSlideUp}},
				ui.StaticText("Saved"))
		}),
	)
}
```

トランジションは `ui.Animate` の中でだけ、その長さとカーブで実行されます。同じ `shown.Set(true)` でも、`ui.Animate` の外ではトーストがすぐに表示されます。また、ビューがトランジションで現れるのは、親がすでに画面上にあるときだけです。

## 共有要素

`Style.Match` は、ビューがほかのビューと共有するキーで、SwiftUI の `matchedGeometryEffect` にあたります。`ui.Animate` の間に、あるキーを持つビューが消えて同じキーを持つ別のビューが現れると、新しいビューは古いビューの位置と大きさから自分の位置と大きさへと広がります。その間、古いビューも一緒に動きながらフェードアウトします。次の例では、行がカードに開き、アートワークとタイトルがそれに合わせて移動します（[`examples/gallery`](../../examples/gallery) より）。

```go
func Matched() ui.Node {
	open := signal.New(false)
	toggle := func() {
		ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.5}, func() { open.Set(!open.Peek()) })
	}
	art := func(size float32, radius float32) ui.Style {
		return ui.Style{Width: ui.Pt(size), Height: ui.Pt(size), CornerRadius: radius, Background: colors.Orange, Match: "album"}
	}
	return ui.Show(open, func() ui.Node {
		return ui.View(ui.ViewProps{OnPress: toggle, Style: ui.Style{Gap: 12, Align: ui.AlignCenter}},
			ui.View(ui.ViewProps{Style: art(240, 24)}),
			ui.StaticText("Night Drive", ui.Style{Font: fonts.Title2, Match: "album-title"}))
	}, func() ui.Node {
		return ui.View(ui.ViewProps{OnPress: toggle, Style: ui.Style{Direction: ui.RowDir, Gap: 12, Align: ui.AlignCenter}},
			ui.View(ui.ViewProps{Style: art(56, 10)}),
			ui.StaticText("Night Drive", ui.Style{Font: fonts.Headline, Match: "album-title"}))
	})
}
```

- デスクトップと Web を含む、すべてのプラットフォームで動作します。
- 2 つのビューは同じ `ui.Animate` の中で変わる必要があります。一方が取り除かれ、もう一方が追加されるということです。
- キーはルートに属します。一つのキーを持てるのは、一度に一つのビューだけです。
- キーを持ったまま消えるビューに相手がいなければ、そのビューはフェードアウトします。

## プッシュする画面へのズーム

`ScreenOptions.ZoomFrom` には、下にある画面のビューの `Match` キーを指定します。プッシュされた画面はそのビューから広がって現れ、ポップされるとそのビューへ縮んで戻ります。次の写真のグリッドのように、`List` のセルでも使えます。

```go
func Photos() ui.Node {
	ids := []int{10, 11, 12, 13, 14, 15}
	return ui.List(ui.ListProps{Columns: 3, Gap: 2}, ui.Val(ids), func(id int) int { return id },
		func(id signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
			nav := ui.UseNavigator()
			return ui.View(ui.ViewProps{
				Label: ui.Val("Photo"),
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

- **iOS 18 以降**では UIKit のズームトランジションを使います。インタラクティブなポップのジェスチャーで、画面はビューへ縮んで戻ります。それより前のバージョンでは通常どおりプッシュします。
- **Android** では Material のコンテナートランスフォームを使います。画面はビューから角を丸めたまま自分の領域まで広がり、ビューの近くでは両者がクロスフェードします。予測型「戻る」ジェスチャーで、画面は縮んで戻ります。
- **macOS、Windows、Linux、Web** では通常どおりプッシュします。
- 下の画面にそのキーを持つビューがなければ、通常どおりプッシュします。

この場合 `ui.Animate` は必要ありません。プッシュはそれ自体がアニメーションするからです。詳しくは[ナビゲーション](navigation.md)を参照してください。

## 線が描かれていくシェイプ

`Shape` の `TrimStart` と `TrimEnd` は、輪郭のうち描画する範囲を 0 から 1 で表します。これらを `ui.Animate` の中で変えると、線が描かれていくようにアニメーションします。進捗リングやチェックマークに使えます。

```go
func ring(w, h float32) *ui.Path {
	r := min(w, h)/2 - 4
	return new(ui.Path).Arc(w/2, h/2, r, -math.Pi/2, 3*math.Pi/2, true)
}

func Ring() ui.Node {
	done := signal.New(float32(0.3))
	return ui.Column(ui.Style{Gap: 12},
		ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.Accent, LineWidth: 8, LineCap: ui.CapRound, TrimEnd: done,
			Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Finish"), OnPress: func() {
			ui.AnimateWith(ui.Animation{Duration: 0.8}, func() { done.Set(1) })
		}}),
	)
}
```

## フレーム単位のアニメーション

動きを宣言するのではなく計算で求める場合（物理演算、ゲーム、値に追従するゲージなど）は、`ui.OnFrame` を使います。`ui.OnFrame` は、ディスプレイが描画するフレームごと（1 秒に 60 回または 120 回）に、そのフレームの時刻を渡して関数を呼びます。関数の中で書き込んだ内容は、そのフレームに反映されます。この関数は、戻り値の `stop` を呼ぶか、コンポーネントのオーナーが破棄されるまで呼ばれ続けます。コールバックが一つも登録されていない間は、ホストはフレームを要求しません。

```go
func Spinner() ui.Node {
	env := ui.UseEnvironment()
	angle := signal.New(float32(0))
	var last time.Duration
	ui.OnFrame(func(t time.Duration) {
		if last != 0 && !env.Get().ReduceMotion {
			a := angle.Peek() + float32((t-last).Seconds()*180) // 1 秒に半回転
			angle.Set(float32(math.Mod(float64(a), 360)))
		}
		last = t
	})
	return ui.View(ui.ViewProps{Decorative: true, Style: ui.StyleFunc(func() ui.Style {
		return ui.Style{Transform: ui.Transform{Rotate: angle.Get()}}
	})}, ui.Symbol(ui.IconSettings, ui.Style{Font: fonts.LargeTitle, Color: colors.Accent}))
}
```

動きは、フレームの数ではなくフレーム間の経過時間をもとに計算してください。フレームレートはデバイスによって異なります。

## ジェスチャー

`View` は、プラットフォームのジェスチャー認識器が認識したジェスチャーを受け取ります。

- `OnPress` と `OnLongPress`：タップと長押し。
- `OnPan`：ドラッグ。
- `OnPinch`：2 本の指を広げる・狭める操作。
- `OnRotate`：2 本の指をひねる操作。
- `OnSwipe`：すばやく払う操作。方向は `ui.SwipeDirection`（`ui.SwipeLeft`、`ui.SwipeRight`、`ui.SwipeUp`、`ui.SwipeDown`）で受け取ります。

マウスやトラックパッドでは、同じハンドラーが、各デスクトップでそれぞれに相当する操作を受け取ります。

| ジェスチャー | macOS | Windows | Linux | Web |
| --- | --- | --- | --- | --- |
| `OnPress` | クリック | クリック、またはフォーカスのあるビューでの Enter か Space | クリック、またはフォーカスのあるビューでの Enter か Space | クリック、またはフォーカスのあるビューでの Enter か Space |
| `OnLongPress` | 副ボタンのクリック、またはボタンの押し続け | 右クリック、またはボタンの押し続け | 副ボタンのクリック、またはボタンの押し続け | 右クリック、またはボタンやタッチの押し続け |
| `OnPan` | ボタンを押したままのドラッグ | ボタンを押したままのドラッグ | ボタンを押したままのドラッグ、タッチ、またはタッチパッド | ポインターを押したままのドラッグ |
| `OnSwipe` | 最後にすばやく動かすドラッグ、トラックパッドの 2 本指で払う操作、システムがアプリに任せている場合は 3 本指のスワイプ | 最後にすばやく動かすドラッグ | 最後にすばやく動かすドラッグ、タッチスクリーンで払う操作 | 最後にすばやく動かすドラッグ |
| `OnPinch` | トラックパッドのピンチ | タッチパッドのピンチ、Ctrl を押しながらのホイール、またはタッチスクリーン上の 2 本指 | タッチパッドかタッチスクリーンのピンチ、または Ctrl を押しながらのホイール | トラックパッドのピンチ、Ctrl を押しながらのホイール、またはタッチスクリーン上の 2 本指 |
| `OnRotate` | トラックパッドの回転 | タッチスクリーン上で 2 本指をひねる操作 | タッチパッドかタッチスクリーンの回転 | タッチスクリーン上で 2 本指をひねる操作 |

マウスのボタンを押し続けたら何が起こるかを試す人はあまりいません。そのため、重要な場面では、`OnLongPress` でできることをメニュー項目やボタンなど別の方法でもできるようにしてください。Windows と Linux では、Ctrl を押しながらホイールを 1 段回すごとに 10% ピンチし、ホイールが止まるとピンチが終わります。Mac のトラックパッドで 2 本指で払う操作は、引き続きビューの周りにあるものをスクロールします。上下のスワイプになるのは、周りにその方向へスクロールするものがない場合だけです。

パン、ピンチ、回転は、ジェスチャーの進行に合わせて `ui.Gesture` を渡して関数を呼びます。

- `State`：`ui.GestureBegan`、`ui.GestureChanged`、そして `ui.GestureEnded` の順に変わります。システムがタッチを奪った場合は `ui.GestureCancelled` になります。
- `X`、`Y`：タッチの位置（ビュー内の座標、ポイント単位）。
- `TranslationX`、`TranslationY`：指が触れてからパンで移動した距離。つまり `X - TranslationX` が、指が触れた位置です。
- `VelocityX`、`VelocityY`：パンの速さ（ポイント毎秒）。
- `Scale`：ピンチ開始時からの拡大率（パンでは 1）。
- `Rotation`：回転開始時からの角度（ラジアン、時計回り）。

ドラッグしたビューは変形によって指に追従し、指を離すとスプリングで元の位置へ戻ります。

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
			return ui.Style{Width: ui.Pt(64), Height: ui.Pt(64), CornerRadius: 32, Background: colors.Accent,
				Transform: ui.Transform{TranslateX: g.TranslationX, TranslateY: g.TranslationY}}
		}),
	})
}
```

拡大率と回転角はそのジェスチャーの開始時からの値です。そのため、前回のジェスチャーが終わったときの値を保存しておき、次のジェスチャーの値をそれに加えます。同じビューに付けたジェスチャーは同時に働くので、ピンチしながら回転させることもできます。

```go
angle, base := signal.New(float32(0)), float32(0)
scale, baseScale := signal.New(float32(1)), float32(1)
return ui.View(ui.ViewProps{
	Label: ui.Val("A square to turn"),
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
		return ui.Style{Width: ui.Pt(160), Height: ui.Pt(160), CornerRadius: 24, Background: colors.Purple,
			Transform: ui.Transform{Rotate: angle.Get(), Scale: scale.Get()}}
	}),
})
```

パン、ピンチ、回転がいったん始まると、タッチはそのジェスチャーのものになります。周りのスクロールビューがタッチを横取りすることはなく、中にある押せるビューも押されません。ボタンの上から始めたドラッグでボタンが押されないのと同じです。

スワイプやパンは、スクリーンリーダーがオンの状態では行えません。同じことを[アクセシビリティアクション](accessibility.md)としても提供してください。

## 押下とホバーの状態

`View` は操作の状態が変わるたびにそれを通知し、スタイルは 3 つの方法でその状態に追従できます。まずは目的を果たせる最初の方法から使ってください。

1. **バリアント**：ビュー自身の見た目を変えるためのものです。`Style.Pressed`、`Hovered`、`Dragging`、`DragOver` は、ビューがその状態にある間、スタイルのほかの部分の上に重ねて適用されます。CSS の `:active` や `:hover`、Tailwind の `active:` や `hover:` にあたります。
2. **`Interaction`**：ほかのビューや、その状態に追従するあらゆるもののためのものです。ビューに `Interaction` を渡し、必要な場所で `it.Pressed()` や `it.Hovered()` を読みます。たとえば `Style.When`、`StyleFunc`、`Text`、`Show` の中です。子が親に追従する（グループのホバー）のも、兄弟が隣のビューに追従する（ピアのホバー）のも、この方法で実現します。
3. **ハンドラー**：録音の開始など、スタイル以外のことのためのものです。`OnPressedChange` と `OnHoverChange` を使います。

```go
func Card(title string, open func()) ui.Node {
	card := ui.NewInteraction()
	return ui.View(ui.ViewProps{
		OnPress:     open,
		Interaction: card,
		Style: ui.Style{Direction: ui.RowDir, Padding: ui.All(16), CornerRadius: 12,
			Background: colors.SecondaryGroupedBackground,
			Hovered:    &ui.Style{Background: colors.SecondaryBackground},
			Pressed:    &ui.Style{Background: colors.Separator, Transform: ui.Transform{Scale: 0.97}}},
	},
		// カードのホバーにタイトルが追従する（グループのホバー）
		ui.StaticText(title, ui.Style{Grow: 1}.When(card.Hovered, ui.Style{Color: colors.Accent})),
	)
}
```

状態は次のとおりです。

- **Pressed** は、指またはマウスの主ボタンがビューの上で押されている間 true になります。
  - スクロールするコンテナーの中では少し遅れて true になるので、スクロールしただけで押下表示が一瞬出ることはありません。
  - 指を離したとき、指がビューの外へ出たとき（ビューの上へ戻ると再び押下状態になります）、またはスクロールや長押し、ほかのジェスチャーがタッチを引き継いだときに false に戻ります。
  - `OnPress` が呼ばれるのは、押下状態のまま指やボタンを離したときだけです。
- **Hovered** は、ポインターがビューまたはその子の上にある間 true になります。ポインターとは、マウス、トラックパッド、iPad のポインター、ホバー中の Apple Pencil やスタイラスのことです。タッチスクリーンだけではホバーは起きません。
- **Dragging** は、ビューがドラッグされている間（その `Drag`）true になります。
- **DragOver** は、そのビューの `Drop` が受け付けるドラッグがビューの上にある間 true になります。

バリアントと `When` の組み合わせ方は次のとおりです。

- バリアントは、自分が指定したフィールドだけを設定します。それ以外はスタイルの値のままです。
- `Font` はフィールドごとにマージされます。そのため `Font{Weight: fonts.Bold}` を指定してもテキストスタイルは保たれます。ほかのフィールドはすべて丸ごと置き換えられます。
- バリアントは Hovered、Pressed、Dragging、DragOver の順に適用されます。したがって、押下状態はホバー状態より優先されます。
- `When` は連鎖させることができ、後のものが優先されます。
- ゼロ値のフィールドは「未設定」を意味するため、状態によってフィールドを消すことはできません。ある状態のときだけ何かを表示したい場合は、`Unless` を使って、もう一方の状態のときにフィールドを設定します。

```go
row := ui.NewInteraction()
ui.View(ui.ViewProps{Interaction: row},
	ui.Text(name),
	// 削除ボタンは行がホバーされている間だけ表示される
	ui.View(ui.ViewProps{OnPress: remove, Style: ui.Style{}.Unless(row.Hovered, ui.Style{Hidden: true})},
		ui.Symbol(ui.IconTrash)),
)
```

`When` と `Unless` は任意の `func() bool` を受け取るので、スタイルをどんなシグナルにも追従させられます（`base.When(selected.Get, ui.Style{Background: colors.Accent})`）。

ビューが追跡するのは、何かが読んでいる状態だけです。読んでいるものとは、そのビューのバリアントか、その `Interaction` の読み取りです。

- `Pressed` バリアントがなく、`Pressed` を読むものもないビューは押下状態を通知せず、プラットフォームのハイライトがそのまま使われます。
- 押下状態を通知するビューは、押下表示を自分で描きます。プラットフォームのハイライト（Android のリップル、Windows のホバー時や押下時の塗り）は表示されません。

各状態は、変わるたびに一度だけ通知されます。ある状態のまま消えたビューはその状態を抜けるので、それに追従しているものが元に戻らなくなることはありません。同じ `Interaction` を複数のビューに渡すと、そのうちどれか一つでもその状態にあれば、状態は true になります。

`OnPress` がない場合、押し続ける操作は「ホールド」になります。「押している間だけ録音する」がその例です。録音は副作用なので、ハンドラーの中で行います。

```go
ui.View(ui.ViewProps{
	OnPressedChange: func(on bool) {
		if on {
			recorder.Start()
		} else {
			recorder.Stop()
		}
	},
	Label: ui.Val("Hold to record"),
	Style:           ui.Style{Padding: ui.All(16), Pressed: &ui.Style{Background: colors.Red}},
}, ...)
```

## ドラッグ＆ドロップ

`ViewProps.Drag` を指定すると、ビューをドラッグできるようになります。タッチスクリーンでは、長押ししてから指を動かすとビューが持ち上がります。このときの長押しで `OnLongPress` が呼ばれることはありません。マウスでは、ボタンを押したまま動かすとドラッグが始まります。ビューの画像は指やポインターに追従し、アプリ内でも、ほかのアプリへでもドラッグできます。`ViewProps.Drop` を指定すると、ビューがドロップを受け付けるようになります。

`ui.DragItem` は、テキスト、リンク（`Kind: ui.SharedLink`）、ファイル（`Path`）、アプリ独自の値（`Value`）のいずれかです。

- アプリ内では、ドロップ先は各項目を Go の値も含めてそのまま受け取ります。
- ほかのアプリが受け取るのは、項目のテキスト、リンク、ファイルです。値しか持たない項目は、アプリの外へは渡りません。
- `MIME` は項目の型で、ドロップ先はこれを見て受け付けるかどうかを決めます。テキスト、リンク、ファイルには自動的に設定されます。`Value` にはアプリ独自の型を指定して、自分のアプリのドロップ先だけが受け取るようにしてください。

```go
type Task struct{ Title string; Done bool }

func TaskCard(t *Task) ui.Node {
	return ui.View(ui.ViewProps{
		Drag: &ui.DragSource{
			Items: func() []ui.DragItem {
				return []ui.DragItem{{Value: t, MIME: "application/x-myapp-task", Text: t.Title}}
			},
		},
		// ドラッグ中は薄く表示する
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
		// タスクがドラッグされて上にある間は枠線を表示する
		Style: ui.Style{Gap: 8, Padding: ui.All(8), BorderWidth: 2, BorderColor: colors.Clear,
			DragOver: &ui.Style{BorderColor: colors.Accent}},
	}, cards...)
}
```

ドラッグ元：

- `Items` はエフェクトの中で実行されるので、読んでいる値が変われば最新の状態に保たれます。ドラッグが運ぶのは、ドラッグ開始時点の項目です。
- `OnPhase` は、ビューが持ち上がると `ui.DragBegan` を通知します。その後、ドロップ先が受け取った場合は `ui.DragDropped` を（移動の場合は、ドラッグしたものをここで取り除きます）、そうでなければ `ui.DragCancelled` を通知します。ドラッグ中の見た目には、`Dragging` バリアントか `Interaction.Dragging` を使います。

ドロップ先：

- `Accept` には MIME タイプを列挙します。完全一致（`"text/plain"`）、ファミリー（`"image/*"`）、すべて（`"*/*"`）のいずれかで指定します。
- これらの型の項目を一つも含まないドラッグは、ビューの上に来ても反応しません。
- ドロップ先が入れ子になっている場合、そのドラッグを受け付ける最も内側のドロップ先が受け取ります。
- `OnOverChange` は、ドラッグがビューの上に来たときと離れたときに通知します。`DragOver` バリアントと同じ状態です。
- `OnMove` は、ドラッグがビュー上のどこにあるかを挿入位置として通知します。
- `OnDrop` は、受け付けた項目とドロップされた位置を受け取ります。`Drop.Local` は、ドラッグがアプリ内から来たかどうかを示します。

ほかのアプリからのドロップ（Finder や「ファイル」アプリからのファイル、「写真」アプリからの写真、ブラウザーからのリンクなど）は、項目の読み込みが終わってから届きます。

- iOS と Android では、ファイルはまずアプリのキャッシュにコピーされ、`Drop.Copied` が true になります。アプリがファイルを保存する場所へ移動するか、使い終わったら削除してください。
- macOS、Windows、Linux では、`Path` はユーザーのファイルそのものです。削除しないでください。
- ブラウザーでは、ドロップされたファイルはアプリのファイル（ページのメモリ上）にコピーされ、`Drop.Copied` が true になります。

## キー

ビューは、キーボードのフォーカスが自分か自分の中のビューにある間、`Keys` でキーを受け取ります。`Keys` は `KeyMap` で、`Shortcut` からその処理への対応表です。キーは、フォーカスのあるビューから外側へたどって最初にそのキーを持つビューへ届きます。これはフォーカスのあるコントロールやメニューバーより先なので、たとえば検索フィールドの上下キーで、その下の検索結果を移動できます。

```go
sel := signal.New(0)
move := func(by int) func() {
	return func() {
		n := len(signal.Untrack(results.Get))
		sel.Set(max(0, min(sel.Peek()+by, n-1)))
	}
}
ui.TextField(ui.TextFieldProps{
	Value:    query,
	Keys:     ui.KeyMap{{Key: "down"}: move(1), {Key: "up"}: move(-1)},
	OnSubmit: func() { open(signal.Untrack(results.Get)[sel.Peek()]) },
})
```

キーは文字か名前付きのキー（`enter`、`escape`、`tab`、`space`、`delete`、`backspace`、`up`、`down`、`left`、`right`、`home`、`end`、`pageup`、`pagedown`、`f1` から `f12`）で、指定した修飾キーと完全に一致したときだけ反応します（`ui.Cmd("d")`、`{Key: "up", Mods: ui.Alt}`）。押し続けるとリピートします。修飾キーなしの文字を登録すると、ビューの中にあるフィールドへの入力を横取りしてしまいます。そのため修飾キーを付けるか、フィールドのない場所に置いてください。

`ui.UseKeys(ui.KeyMap{...})` は、ウィンドウ独自のキーを登録します。キーが最後に届く先であり、何にもフォーカスがないときは最初に届く先になります。ゲームの矢印キーやビューアーの Space キーなどに使います。登録したキーは、それを呼んだコンポーネントと一緒に破棄されます。

キーはハードウェアキーボードから届きます。デスクトップ、iPad、Chromebook のキーボードや、ブラウザーでのキーボードです。スマートフォンには普通キーボードがないので、キーを唯一の操作手段にしないでください。テキストを変換中の入力メソッド（日本語や中国語のキーボード）は、そのキーを自分で処理します。`examples/launcher` では、上下キーで検索結果を移動できます。

## 触覚フィードバック

`ui.Haptic` はフィードバックを再生します。`ui.HapticSelection`（値が切り替わったとき）、`ui.HapticLight`、`ui.HapticMedium`、`ui.HapticHeavy`（衝撃）、`ui.HapticSuccess`、`ui.HapticWarning`、`ui.HapticError`（結果）があります。

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

iOS では `UIFeedbackGenerator` のフィードバックを使います。Android には完全に対応するものがないため、Asuka は `performHapticFeedback` の定数のうち最も近いものを再生します。成功、警告、エラーは、Android 11 以降では confirm と reject、それより前では key press と long press になります。

Force Touch トラックパッドを備えた Mac では、`ui.HapticSelection` は `NSHapticFeedbackManager` の alignment に、それ以外は level change になります。トラックパッドは、指が触れている間にこれらを再生します。Windows と Linux には触覚フィードバックがないため、`ui.Haptic` は何もしません。ブラウザーでは、`navigator.vibrate` があれば（Android の Chrome）短く振動し、なければ何もしません。

## 視差効果を減らす

ユーザーが動きを減らすよう設定していると、`ui.UseEnvironment().Get().ReduceMotion` が true になります。この値は、ユーザーが設定を変えると変わります。

- **iOS**：「設定」>「アクセシビリティ」>「動作」>「視差効果を減らす」。
- **Android**：アニメーションをオフにしたとき（開発者向けオプション、または「ユーザー補助」>「アニメーションを削除」）。
- **macOS**：「システム設定」>「アクセシビリティ」>「ディスプレイ」>「視差効果を減らす」。
- **Windows**：「設定」>「アクセシビリティ」>「視覚効果」>「アニメーション効果」をオフ。
- **Linux**：GNOME のアニメーションをオフにしたとき（「設定」>「アクセシビリティ」>「アニメーションを減らす」（Reduce Animation）、GTK の `gtk-enable-animations`）。
- **Web**：ブラウザーの `prefers-reduced-motion`。システムの設定に従います。

Asuka がこれに対して自動で行うことは次のとおりです。

- **iOS**：トランジションは移動や拡大縮小をせず、その場でディゾルブします。UIKit 自身のトランジションもこの設定では同じ動きになります。共有要素（`Match`）も飛んでいかず、各ビューはそれぞれのトランジションで現れたり消えたりします。それ以外のアニメーションは実行されます。
- **Android**：システムのアニメーター再生時間スケールが 0 になるため、Asuka のものもプラットフォームのものも、すべてのアニメーションが即座に終わります。
- **macOS、Windows、Linux、Web**：`ui.Animate` の変更は、トランジションや共有要素も含めてすぐに反映されます。スタックのプッシュとポップは、macOS ではスライドせず、ブラウザーではクロスフェードし、Linux ではアニメーションをオフにしたときの libadwaita のナビゲーションと同じ動きになります。

残りは、装飾的かどうかを Asuka が判断できない動きです。たとえば `OnFrame` のループ（上のスピナーが止まるように）、繰り返すパルス、何かを遠くまで動かすアニメーションなどです。こうした動きは `ReduceMotion` を読んで控えめにしてください。

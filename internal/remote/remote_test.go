package remote

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/egoist/nui/host"
	"github.com/egoist/nui/host/fake"
	"github.com/egoist/nui/internal/remote/testapp"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

// player is a core serving app over a pipe, and a player (the fake
// host behind a proxy) on the test's goroutine, its main thread.
type player struct {
	t     *testing.T
	h     *fake.Host
	x     *Proxy
	posts chan func()
	conns chan net.Conn
	conn  net.Conn
}

var env = host.Environment{Width: 390, Height: 844, Scale: 3, FontScale: 1}

func start(t *testing.T, app func() ui.Node) *player {
	h := fake.New()
	h.Env = env
	x := NewProxy(h)
	h.Connect(x)
	// The platform host reports its environment before there's a
	// connection; the proxy keeps it for the core.
	h.SetEnvironment(env)
	s := &player{t: t, h: h, x: x, posts: make(chan func(), 16), conns: make(chan net.Conn)}
	done := make(chan struct{})
	srv := NewServer(app, s.posts)
	go func() {
		srv.Loop(s.conns)
		close(done)
	}()
	s.connect()
	// The core disposes the app's roots when it stops.
	t.Cleanup(func() {
		s.conn.Close() // the player goes: a call the core is in fails at once
		close(s.conns)
		<-done
	})
	return s
}

func (s *player) connect() {
	a, b := net.Pipe()
	s.conn = a
	s.conns <- b
	s.x.Attach(a)
}

// until runs the player's main thread until cond holds.
func (s *player) until(what string, cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second) // a hang guard, not a timing assertion
	for !cond() {
		if time.Now().After(deadline) {
			s.t.Fatalf("waiting for %s\n%s", what, s.h.Log())
		}
		s.h.RunPosted()
		time.Sleep(time.Millisecond)
	}
}

func hasText(h *fake.Host, text string) bool {
	for _, v := range h.OfKind(ops.KindText) {
		if v.Label() == text {
			return true
		}
	}
	return false
}

func TestPlayerShowsTheAppAndForwardsEvents(t *testing.T) {
	var count *signal.Signal[int]
	app := func() ui.Node {
		count = signal.New(0)
		return ui.Column(ui.Style{},
			ui.Text(signal.Func(func() string { return fmt.Sprint("Count: ", count.Get()) })),
			ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() { count.Set(count.Peek() + 1) }}),
		)
	}
	s := start(t, app)
	// Attaching sent the environment, which mounted the app: its views
	// are here when Attach returns, laid out with the player's size.
	if s.h.One(ops.KindText, "Count: 0") == nil || s.h.Root().Frame[2] != 390 {
		t.Fatalf("\n%s", s.h.Log())
	}
	// A tap goes to the core and comes back as ops before Tap returns, as
	// in process.
	s.h.Tap(s.h.One(ops.KindButton, "Add"))
	if s.h.One(ops.KindText, "Count: 1") == nil {
		t.Fatalf("\n%s", s.h.Log())
	}

	// A goroutine's ui.Post: the core applies while the player is idle;
	// the proxy's posted drain applies it.
	go ui.Post(func() { count.Set(41) })
	s.until("Count: 41", func() bool { return hasText(s.h, "Count: 41") })
}

func TestListBindsWhileTheCoreWaits(t *testing.T) {
	items := make([]int, 1000)
	for i := range items {
		items[i] = i
	}
	app := func() ui.Node {
		return ui.List(ui.ListProps{}, signal.Static(items), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.Text(signal.Func(func() string { return fmt.Sprint("row ", item.Get()) }))
			})
	}
	s := start(t, app)
	list := s.h.OfKind(ops.KindList)[0]
	if n := s.h.ListCount(list); n != 1000 {
		t.Fatal(n)
	}
	// The player binds a cell: the core makes its row and applies it, the
	// player measuring it meanwhile, all inside ListBind.
	cell := s.h.NewCell()
	s.h.Bind(list, 7, cell)
	if s.h.One(ops.KindText, "row 7") == nil {
		t.Fatalf("\n%s", s.h.Log())
	}
}

func TestReconnectingStartsAFreshSession(t *testing.T) {
	app := func() ui.Node { return ui.StaticText("hello") }
	s := start(t, app)
	if s.h.One(ops.KindText, "hello") == nil {
		t.Fatal("not mounted")
	}
	// The connection drops (the core restarted): the proxy notices.
	s.conn.Close()
	s.until("the proxy to notice", func() bool { return s.x.peer() == nil })
	// A new connection: the player clears the last session's views first.
	s.connect()
	if got := len(s.h.OfKind(ops.KindText)); got != 1 {
		t.Fatalf("%d texts after reconnecting\n%s", got, s.h.Log())
	}
}

func TestTwoPlayersAtOnce(t *testing.T) {
	app := func() ui.Node {
		n := signal.New(0)
		return ui.Button(ui.ButtonProps{
			Title:   signal.Func(func() string { return fmt.Sprint("tapped ", n.Get()) }),
			OnPress: func() { n.Set(n.Peek() + 1) },
		})
	}
	a := start(t, app)
	// A second player joins the same core: it gets its own root.
	h := fake.New()
	h.Env = env
	x := NewProxy(h)
	h.Connect(x)
	h.SetEnvironment(env)
	b := &player{t: t, h: h, x: x, posts: a.posts, conns: a.conns}
	b.connect()
	defer b.conn.Close()

	a.h.Tap(a.h.One(ops.KindButton, "tapped 0"))
	if a.h.One(ops.KindButton, "tapped 1") == nil || b.h.One(ops.KindButton, "tapped 0") == nil {
		t.Fatalf("a:\n%s\nb:\n%s", a.h.Log(), b.h.Log())
	}
}

func TestStateSurvivesARestart(t *testing.T) {
	signal.KeepState(nil)
	defer signal.KeepState(nil)
	s := start(t, testapp.Counter)
	for i := 0; i < 3; i++ {
		s.h.Tap(s.h.Root().Children[0])
	}
	// The app's code restarts: the old run saves, the new one restores.
	saved := make(chan map[string][]byte)
	s.posts <- func() { saved <- signal.KeptState() }
	state := <-saved
	s.conn.Close()
	s.until("the proxy to notice", func() bool { return s.x.peer() == nil })

	signal.KeepState(state)
	s.connect() // the same player, a new core
	if s.h.One(ops.KindButton, "count 3") == nil {
		t.Fatalf("\n%s", s.h.Log())
	}
}

func TestManyRoundTripsInOneCall(t *testing.T) {
	// Mounting measures each text: hundreds of round trips while the core
	// is inside one call. The core's reader must keep reading meanwhile.
	app := func() ui.Node {
		var kids []ui.Node
		for i := 0; i < 300; i++ {
			kids = append(kids, ui.StaticText(fmt.Sprint("text ", i)))
		}
		return ui.Column(ui.Style{}, kids...)
	}
	s := start(t, app)
	if got := len(s.h.OfKind(ops.KindText)); got != 300 || s.h.Measures < 300 {
		t.Fatalf("%d texts, %d measures", got, s.h.Measures)
	}
}

// A player cut off (Android cuts a backgrounded app's network) resumes
// its session: the app isn't mounted again, its views stay, and what the
// player sent while cut off arrives.
func TestACutSessionResumes(t *testing.T) {
	var count *signal.Signal[int]
	mounts := 0
	app := func() ui.Node {
		mounts++
		count = signal.New(0)
		return ui.Column(ui.Style{},
			ui.Text(signal.Func(func() string { return fmt.Sprint("Count: ", count.Get()) })),
			ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() { count.Set(count.Peek() + 1) }}),
		)
	}
	s := start(t, app)
	s.h.Tap(s.h.One(ops.KindButton, "Add"))
	if s.h.One(ops.KindText, "Count: 1") == nil {
		t.Fatalf("\n%s", s.h.Log())
	}
	root := s.h.Root().ID
	s.conn.Close()
	s.until("the proxy to notice", func() bool { return s.x.peer() == nil })
	// A tap while cut off waits.
	s.h.Tap(s.h.One(ops.KindButton, "Add"))
	if s.h.One(ops.KindText, "Count: 1") == nil {
		t.Fatalf("changed while cut off\n%s", s.h.Log())
	}
	s.h.Reset()
	s.connect()
	s.until("Count: 2", func() bool { return hasText(s.h, "Count: 2") })
	if mounts != 1 || s.h.Root().ID != root || s.h.Count(ops.OpDestroy) != 0 || s.h.Count(ops.OpCreate) != 0 {
		t.Fatalf("mounted %d times; root %d (was %d); ops:\n%s", mounts, s.h.Root().ID, root, s.h.Log())
	}
}

// What the core changes while its player is cut off waits, batch by
// batch, and reaches the player as it comes back: a large batch, then
// small ones (which mustn't write over it).
func TestChangesWhileCutOffArrive(t *testing.T) {
	var count *signal.Signal[int]
	var many *signal.Signal[bool]
	app := func() ui.Node {
		count = signal.New(0)
		many = signal.New(false)
		return ui.Column(ui.Style{},
			ui.Text(signal.Func(func() string { return fmt.Sprint("Count: ", count.Get()) })),
			ui.Show(many, func() ui.Node {
				var rows []ui.Node
				for i := range 20 {
					rows = append(rows, ui.StaticText(fmt.Sprint("row ", i)))
				}
				return ui.Column(ui.Style{}, rows...)
			}))
	}
	s := start(t, app)
	s.until("Count: 0", func() bool { return hasText(s.h, "Count: 0") })
	s.conn.Close()
	s.until("the proxy to notice", func() bool { return s.x.peer() == nil })
	// Changes on the core's loop, each flushed: a batch of its own.
	post := func(f func()) {
		ui.Post(f)
		done := make(chan struct{})
		s.posts <- func() { close(done) } // after the flush
		<-done
	}
	post(func() { many.Set(true) })
	for i := 1; i <= 3; i++ {
		post(func() { count.Set(i) })
	}
	s.connect()
	s.until("Count: 3 and the rows", func() bool { return hasText(s.h, "Count: 3") && hasText(s.h, "row 19") })
}

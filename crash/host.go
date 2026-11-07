package crash

import "github.com/egoist/ryui/ui"

// postMain runs f on the main thread, with the app's host (tests replace
// it).
var postMain = ui.Post

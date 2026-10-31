// Package review asks the user to rate the app without leaving it
// (expo-store-review): the App Store's rating prompt (SKStoreReview), and
// Google Play's in-app review. The stores decide whether it shows: iOS
// shows it at most three times a year, Play on its own quota, and neither
// says whether it did. Ask after a success (a task done, an order
// placed), never from a button: for a button, open AppStoreURL or
// PlayStoreURL.
package review

import "github.com/egoist/ryui/ui"

// Request asks the store to show its rating prompt, and calls done (if
// not nil) on the main thread once it's been handled: shown, rated,
// dismissed, or not shown. err is only for a failure (Play's library
// missing, no Play Store). Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Request(done func(err error)) {
	ui.Call("review", "request", nil, func(_ []byte, err error) {
		if done != nil {
			done(err)
		}
	})
}

// AppStoreURL is the app's App Store page, open on writing a review, for
// ui.OpenURL (a button's "Rate us"): id is its Apple ID, the number in
// its App Store link.
func AppStoreURL(id string) string {
	return "https://apps.apple.com/app/id" + id + "?action=write-review"
}

// PlayStoreURL is the app's Google Play page, for ui.OpenURL: id is its
// application ID (ryui.json's id).
func PlayStoreURL(id string) string {
	return "https://play.google.com/store/apps/details?id=" + id + "&showAllReviews=true"
}

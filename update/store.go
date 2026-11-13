//go:build ios || android || (!darwin && !windows && !linux && !js)

package update

import "context"

func defaultSource() Source { return feedSource(buildFeed) }

// Supported is ErrUnsupported on a phone: the App Store and Google Play
// update apps (an app can ask for a review, not for its update).
func Supported() error { return ErrUnsupported }

// Install is ErrUnsupported on a phone.
func (r *Release) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	return ErrUnsupported
}

// Relaunch does nothing on a phone: the system starts and ends apps.
func Relaunch() {}

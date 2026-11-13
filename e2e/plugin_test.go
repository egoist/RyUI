//go:build e2e

package e2e

import "testing"

// Native packages (asuka native) are built into the app on each platform:
// the greeter's module answers the app's Go (on iOS through its Swift
// package's own dependency, GreeterKit: a resource and C, "kit 42"), the
// kitchen's module answers with its structs, enums and slices, and the
// greeter's typed view, set as it's made, sends the app an event.
func nativePackages(t *testing.T, s screen, platform, greeting string) {
	waitFor(t, s, []string{greeting, "kitchen home: salt 1.5g, 2 tags", "shown: " + platform + " badge for Asuka"})
}

func TestNativePackagesIOS(t *testing.T) {
	nativePackages(t, iosApp(t, "e2e/testdata/pluginapp", "dev.asuka.pluginapp"), "iOS", "hello Asuka from iOS, kit 42")
}

func TestNativePackagesAndroid(t *testing.T) {
	nativePackages(t, androidApp(t, "e2e/testdata/pluginapp", "dev.asuka.pluginapp"), "Android", "hello Asuka from Android")
}

func TestNativePackagesWeb(t *testing.T) {
	nativePackages(t, webApp(t, "e2e/testdata/pluginapp"), "web", "hello Asuka from the web")
}

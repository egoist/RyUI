//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/asuka/conformance"
)

// The conformance sheet (package conformance) on each platform: the app
// that shows it is launched, the screen captured until the sheet is on
// it, and the sheet measured.

// sheetShot captures the screen with shot until the sheet is found in
// it (bounded), and returns that capture.
func sheetShot(t *testing.T, shot func() ([]byte, error)) image.Image {
	t.Helper()
	deadline := time.Now().Add(within)
	var last error
	for time.Now().Before(deadline) {
		b, err := shot()
		if err == nil {
			img, err := png.Decode(bytes.NewReader(b))
			if err == nil {
				if _, err = conformance.Measure(img, ""); err == nil {
					return img
				}
			}
			last = err
		} else {
			last = err
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("the sheet never showed: %v", last)
	return nil
}

func TestConformanceIOS(t *testing.T) {
	s := iosApp(t, "conformance/app", "dev.asuka.conformance")
	shot := filepath.Join(t.TempDir(), "screen.png")
	img := sheetShot(t, func() ([]byte, error) {
		if out, err := exec.Command("xcrun", "simctl", "io", s.udid, "screenshot", "--type=png", shot).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("%v: %s", err, out)
		}
		return os.ReadFile(shot)
	})
	conformance.Check(t, "ios", img, "ios")
}

func TestConformanceAndroid(t *testing.T) {
	s := androidApp(t, "conformance/app", "dev.asuka.conformance")
	img := sheetShot(t, func() ([]byte, error) {
		return exec.Command(adb(), "-s", s.serial, "exec-out", "screencap", "-p").Output()
	})
	conformance.Check(t, "android", img, "android")
}

// TestConformanceWeb measures the sheet in headless Chrome at three
// device pixel ratios: the web host places frames on the device's
// pixels, and sizes shadows and borders in CSS pixels.
func TestConformanceWeb(t *testing.T) {
	browser := chrome()
	if browser == "" {
		t.Skip("no Chrome (ASUKA_CHROME)")
	}
	url := webServer(t, "conformance/app")
	for _, scale := range []string{"1", "1.5", "2"} {
		t.Run(scale+"x", func(t *testing.T) {
			page := devtoolsPage(t, browser, url, scale)
			img := sheetShot(t, func() ([]byte, error) { return page.screenshot() })
			conformance.Check(t, "web@"+scale+"x", img, "web")
		})
	}
}

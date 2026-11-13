package update

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// CheckBundle checks, as Sparkle does, that the update's bundle may take
// the place of the app's beyond its update signature: its code signature
// is valid (nothing changed since it was signed) and of the same signer
// (team ID) and identifier as the running app's, when that one is signed.
// It also takes the quarantine attribute off the new bundle, which macOS
// gives files only apps that ask download, so that Gatekeeper doesn't ask
// the user again about a version of an app they opened.
func CheckBundle(bundle, running string) error {
	_ = exec.Command("/usr/bin/xattr", "-dr", "com.apple.quarantine", bundle).Run()
	old, ok := signer(running)
	if !ok {
		return nil // not signed, or changed since: nothing to compare
	}
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", bundle).CombinedOutput(); err != nil {
		return fmt.Errorf("the update's code signature isn't valid: %s", bytes.TrimSpace(out))
	}
	now, ok := signer(bundle)
	switch {
	case !ok:
		return fmt.Errorf("the update isn't signed")
	case now.team != old.team:
		return fmt.Errorf("the update is signed by team %s, not the app's %s", now.team, old.team)
	case now.id != old.id:
		return fmt.Errorf("the update is the app %s, not %s", now.id, old.id)
	}
	return nil
}

type codeSigner struct{ team, id string }

// signer returns the team ID ("not set" for ad hoc signatures) and the
// identifier of a bundle whose code signature is valid.
func signer(bundle string) (codeSigner, bool) {
	if exec.Command("/usr/bin/codesign", "--verify", "--strict", bundle).Run() != nil {
		return codeSigner{}, false
	}
	out, err := exec.Command("/usr/bin/codesign", "-dv", "--verbose=2", bundle).CombinedOutput()
	if err != nil {
		return codeSigner{}, false
	}
	var s codeSigner
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "TeamIdentifier="); ok {
			s.team = v
		} else if v, ok := strings.CutPrefix(line, "Identifier="); ok {
			s.id = v
		}
	}
	return s, s.id != ""
}

//go:build !js && (ios || android || asuka_bundled || !(darwin || linux || windows))

package plugin

// devPath: a shipped app (asuka_bundled) and a phone's have their
// libraries with them, and never download one.
func devPath(string, *Manifest, Library, string) (string, error) { return "", nil }

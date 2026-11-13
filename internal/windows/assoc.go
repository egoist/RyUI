//go:build windows

package windows

import "fmt"

// File associations, recent documents and the jump list's Recent, and the
// registry's values of the user's that they need: the host's file types
// (registered for the user as the app starts), asuka/recent and
// asuka/login.

var (
	procSHChangeNotify     = shell32.NewProc("SHChangeNotify")
	procSHAddToRecentDocs  = shell32.NewProc("SHAddToRecentDocs")
	procRegDeleteKeyValueW = advapi32.NewProc("RegDeleteKeyValueW")

	clsidApplicationDestinations = NewGUID("86c14003-4d6b-4ef3-a7b4-0506663b2e68")
	iidApplicationDestinations   = NewGUID("12337d35-94c6-48a0-bce7-6a9c69d4d600")
)

// AssociationsChanged tells the shell the file associations changed
// (SHChangeNotify's SHCNE_ASSOCCHANGED), so Explorer's icons and Open
// With follow them.
func AssociationsChanged() {
	procSHChangeNotify.Call(0x08000000, 0, 0, 0) // SHCNE_ASSOCCHANGED, SHCNF_IDLIST
}

// AddRecentDocument adds path to the shell's recent items for the app
// (SHAddToRecentDocs, SHARD_PATHW): its jump list's Recent shows it, for
// the file types the app is registered to open.
func AddRecentDocument(path string) {
	procSHAddToRecentDocs.Call(3, Str(path)) // SHARD_PATHW
}

// ClearJumpListRecent removes the documents of the app's jump list
// (IApplicationDestinations.RemoveAllDestinations), not the user's other
// recent items. COM must be initialized on the thread.
func ClearJumpListRecent() error {
	d := CoCreate(&clsidApplicationDestinations, &iidApplicationDestinations)
	if d == 0 {
		return fmt.Errorf("the jump list's destinations aren't available")
	}
	defer Release(d)
	// IUnknown (3), SetAppID, RemoveDestination, RemoveAllDestinations.
	if hr := ComCall(d, 5); Failed(hr) {
		return fmt.Errorf("clearing the jump list: %#x", uint32(hr))
	}
	return nil
}

// RegDeleteUserValue deletes a value of a key under HKEY_CURRENT_USER;
// one that isn't there is no error.
func RegDeleteUserValue(path, name string) error {
	r, _, _ := procRegDeleteKeyValueW.Call(hkeyCurrentUser, Str(path), Str(name))
	if r != 0 && r != 2 && r != 3 { // ERROR_FILE_NOT_FOUND, ERROR_PATH_NOT_FOUND
		return fmt.Errorf("the registry's %s: %d", path, r)
	}
	return nil
}

// RegUserHasValue reports whether a key under HKEY_CURRENT_USER has the
// value ("": its default), whatever its type.
func RegUserHasValue(path, name string) bool {
	_, _, ok := regQuery(path, name)
	return ok
}

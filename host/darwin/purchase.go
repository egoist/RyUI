//go:build darwin && !ios

package darwin

import (
	"errors"
	"unsafe"

	"github.com/ebitengine/purego"
)

// asuka/sdk/purchase on a Mac: StoreKit 2, a Swift API the Objective-C
// runtime can't reach, so it's the app's Swift library's
// (platform/macos's Purchase.swift, the iOS host's, built into
// AsukaNatives.dylib for an app that imports the package: the CLI's
// featurePermissions). The module hands each call to the library's
// asuka_purchase_call, and its answer comes back through a callback, on
// the main thread. Without the library's (an app run with go run, not
// built), there's no store.

var swiftPurchase struct {
	call    func(id uint64, method *byte, args unsafe.Pointer, n int)
	ok      bool
	next    uint64
	replies map[uint64]func([]byte, error)
}

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["purchase"] = purchaseModule()
	})
}

// loadSwiftPurchase finds the library's purchase functions, if it has
// them, and boots them (StoreKit's listener starts, as the app does).
func loadSwiftPurchase(lib uintptr) {
	if _, err := purego.Dlsym(lib, "asuka_purchase_call"); err != nil {
		return
	}
	var boot func(reply uintptr)
	purego.RegisterLibFunc(&boot, lib, "asuka_purchase_boot")
	purego.RegisterLibFunc(&swiftPurchase.call, lib, "asuka_purchase_call")
	swiftPurchase.ok = true
	swiftPurchase.replies = map[uint64]func([]byte, error){}
	boot(purego.NewCallback(swiftPurchaseReply))
}

// swiftPurchaseReply answers a call (a purego callback, on the main
// thread).
func swiftPurchaseReply(id uint64, ok int32, p *byte, n int) {
	reply := swiftPurchase.replies[id]
	if reply == nil {
		return
	}
	delete(swiftPurchase.replies, id)
	var b []byte
	if n > 0 && p != nil {
		b = append([]byte(nil), unsafe.Slice(p, n)...)
	}
	if ok == 0 {
		reply(nil, errors.New(string(b)))
		return
	}
	reply(b, nil)
}

// purchaseMethods are the module's (internal/purchasespec).
var purchaseMethods = []string{"available", "products", "buy", "finish", "entitlements", "restore", "status",
	"manage", "refund", "redeem", "storefront", "next", "stop"}

func purchaseModule() map[string]method {
	m := map[string]method{}
	for _, name := range purchaseMethods {
		cname := append([]byte(name), 0)
		m[name] = func(args []byte, reply func([]byte, error)) {
			if !swiftPurchase.ok {
				switch name {
				case "available":
					reply([]byte{0}, nil)
				case "stop":
					reply(nil, nil)
				default:
					reply(nil, errors.New("not available: the App Store needs the app built (asuka dev or asuka build), whose Swift has StoreKit"))
				}
				return
			}
			swiftPurchase.next++
			id := swiftPurchase.next
			swiftPurchase.replies[id] = reply
			var p unsafe.Pointer
			if len(args) > 0 {
				p = unsafe.Pointer(unsafe.SliceData(args))
			}
			swiftPurchase.call(id, unsafe.SliceData(cname), p, len(args))
		}
	}
	return m
}

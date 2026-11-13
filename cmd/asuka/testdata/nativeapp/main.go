// An app with a native package (internal/nativegen's kitchen), without a
// window: the Mac build's tests build its Swift.
package main

import (
	"fmt"

	"github.com/egoist/asuka/internal/nativegen/testdata/kitchen"
)

func main() {
	k, err := kitchen.NewKitchen()
	fmt.Println(k, err)
}

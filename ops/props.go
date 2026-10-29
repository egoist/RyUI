package ops

import "fmt"

type propInfo struct {
	name string
	typ  Type
}

func propOf(k Kind, p PropID) (propInfo, bool) {
	if p > 0 && int(p) < len(commonProps) {
		return commonProps[p], true
	}
	if int(k) < len(kindProps) && p >= FirstKindProp && int(p-FirstKindProp) < len(kindProps[k]) {
		return kindProps[k][p-FirstKindProp], true
	}
	return propInfo{}, false
}

// PropName names a common prop, or a kind's prop by ID.
func PropName(p PropID) string {
	if info, ok := propOf(0, p); ok {
		return info.name
	}
	return fmt.Sprintf("#%d", p)
}

// KindPropName names prop p of kind k.
func KindPropName(k Kind, p PropID) string {
	if info, ok := propOf(k, p); ok {
		return info.name
	}
	return fmt.Sprintf("#%d", p)
}

// PropType returns the wire type of prop p of kind k, if it exists.
func PropType(k Kind, p PropID) (Type, bool) {
	info, ok := propOf(k, p)
	return info.typ, ok
}

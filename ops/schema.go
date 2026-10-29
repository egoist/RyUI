package ops

//go:generate go run ./internal/gen

// The schema: every kind of native view, its props, the events hosts
// send, the commands the core sends and the enums they use. go generate
// writes the Go constants (schema_gen.go) and the Swift decoder
// (hosts/ios/Sources/NuiOps/Ops.swift) from it.
//
// Adding a component: add its kind and props here, run go generate, add
// the component in ui, and its view in every host.

// Type is the wire type of a prop's value.
type Type uint8

const (
	TBool Type = iota + 1
	TInt
	TFloat
	TString
	TColor
	TEdges
	TEnum
	TBytes
)

// Payload is what an event carries.
type Payload uint8

const (
	PNone Payload = iota
	PBool
	PInt
	PFloat
	PString
	PPoint
	PEnvironment
)

type PropDef struct {
	Name string
	Type Type
	Enum string // for TEnum
	Doc  string
}

type KindDef struct {
	Name  string
	Props []PropDef
	Doc   string
}

type EventDef struct {
	Name    string
	Payload Payload
	Doc     string
}

type CommandDef struct {
	Name string
	Doc  string
}

type EnumDef struct {
	Name   string
	Values []string // the first is the default (0)
}

type SchemaDef struct {
	// Common props apply to every kind; IDs 1..63.
	Common []PropDef
	// Kind props have IDs from 64, per kind. Kind IDs start at 1.
	Kinds    []KindDef
	Events   []EventDef // IDs from 1
	Commands []CommandDef
	Enums    []EnumDef
}

// FirstKindProp is the ID of each kind's first own prop.
const FirstKindProp = 64

var Schema = SchemaDef{
	Common: []PropDef{
		{Name: "Background", Type: TColor},
		{Name: "CornerRadius", Type: TFloat},
		{Name: "BorderWidth", Type: TFloat},
		{Name: "BorderColor", Type: TColor},
		{Name: "Opacity", Type: TFloat},
		{Name: "Hidden", Type: TBool},
		{Name: "Clip", Type: TBool},
		{Name: "A11yLabel", Type: TString},
		{Name: "A11yHint", Type: TString},
		{Name: "A11yRole", Type: TEnum, Enum: "Role"},
		{Name: "Color", Type: TColor, Doc: "text color, or tint for controls"},
		{Name: "TextStyle", Type: TEnum, Enum: "TextStyle"},
		{Name: "FontSize", Type: TFloat},
		{Name: "FontWeight", Type: TEnum, Enum: "FontWeight"},
		{Name: "TextAlign", Type: TEnum, Enum: "TextAlign"},
		{Name: "LineLimit", Type: TInt, Doc: "0: no limit"},
		{Name: "Disabled", Type: TBool},
		{Name: "Menu", Type: TBytes, Doc: "a menu: u32 count, then per item u8 role (AlertButtonRole) and a u32-length title; Action events carry the item's index"},
		{Name: "MenuTrigger", Type: TEnum, Enum: "MenuTrigger"},
	},
	Kinds: []KindDef{
		{Name: "View", Doc: "a plain container"},
		{Name: "Text", Props: []PropDef{
			{Name: "Text", Type: TString},
		}},
		{Name: "Button", Props: []PropDef{
			{Name: "Title", Type: TString},
			{Name: "Variant", Type: TEnum, Enum: "ButtonVariant"},
		}},
		{Name: "TextField", Props: []PropDef{
			{Name: "Value", Type: TString},
			{Name: "Placeholder", Type: TString},
			{Name: "Keyboard", Type: TEnum, Enum: "Keyboard"},
			{Name: "ReturnKey", Type: TEnum, Enum: "ReturnKey"},
			{Name: "Secure", Type: TBool},
			{Name: "Autocapitalize", Type: TEnum, Enum: "Autocapitalize"},
			{Name: "Autocorrect", Type: TEnum, Enum: "Autocorrect"},
			{Name: "ContentType", Type: TString, Doc: "AutoFill hint, e.g. username"},
		}},
		{Name: "Toggle", Props: []PropDef{
			{Name: "On", Type: TBool},
		}},
		{Name: "Slider", Props: []PropDef{
			{Name: "Value", Type: TFloat},
			{Name: "Min", Type: TFloat},
			{Name: "Max", Type: TFloat},
		}},
		{Name: "Scroll", Props: []PropDef{
			{Name: "Horizontal", Type: TBool},
			{Name: "Refreshable", Type: TBool, Doc: "pull to refresh sends Refresh"},
			{Name: "Refreshing", Type: TBool},
		}, Doc: "its single child is the content; the content's frame is the content size"},
		{Name: "List", Props: []PropDef{
			{Name: "Refreshable", Type: TBool},
			{Name: "Refreshing", Type: TBool},
		}, Doc: "rows live in host-made cells, bound through ListBind"},
		{Name: "Activity", Props: []PropDef{
			{Name: "Animating", Type: TBool},
		}},
		{Name: "Stack", Doc: "a navigation stack; its children are Screens: an Insert pushes, a Destroy pops (animated when it's the top). A pop the user makes (back gesture, back button) is reported with Popped, the screen's ID"},
		{Name: "Screen", Props: []PropDef{
			{Name: "Title", Type: TString},
			{Name: "Actions", Type: TBytes, Doc: "bar buttons, encoded as Menu; Action events carry the index"},
			{Name: "BackGesture", Type: TBool, Doc: "false: no interactive back"},
			{Name: "Icon", Type: TString, Doc: "a tab's icon: an SF Symbol name (Android maps the common ones)"},
		}, Doc: "a screen of a Stack, or a page of Tabs: sized by the host, which reports its Environment"},
		{Name: "Tabs", Props: []PropDef{
			{Name: "Selected", Type: TInt},
		}, Doc: "its children are Screens (pages); the user's choice is reported with Selected"},
		{Name: "Sheet", Props: []PropDef{
			{Name: "Detents", Type: TInt, Doc: "bits: 1 medium, 2 large (0: large)"},
			{Name: "Grabber", Type: TBool},
		}, Doc: "a modal sheet with one child, its content: Present shows it, Destroy dismisses it; the user's dismissal is reported with Dismissed. Sized by the host, which reports its Environment"},
		{Name: "Alert", Props: []PropDef{
			{Name: "Title", Type: TString},
			{Name: "Message", Type: TString},
			{Name: "Buttons", Type: TBytes, Doc: "encoded as Menu"},
			{Name: "Style", Type: TEnum, Enum: "AlertStyle"},
		}, Doc: "an alert or action sheet: Present shows it; a button reports Action with its index and dismisses it; dismissing otherwise reports Dismissed"},
	},
	Events: []EventDef{
		{Name: "Press", Payload: PNone},
		{Name: "LongPress", Payload: PNone},
		{Name: "TextChanged", Payload: PString},
		{Name: "Submit", Payload: PNone},
		{Name: "Focus", Payload: PNone},
		{Name: "Blur", Payload: PNone},
		{Name: "Toggled", Payload: PBool},
		{Name: "ValueChanged", Payload: PFloat},
		{Name: "Scrolled", Payload: PPoint},
		{Name: "Environment", Payload: PEnvironment, Doc: "sent for the root; the ID is the root's"},
		{Name: "Lifecycle", Payload: PInt, Doc: "a Lifecycle value"},
		{Name: "Popped", Payload: PInt, Doc: "the user popped a Stack's top screen (its ID)"},
		{Name: "Selected", Payload: PInt, Doc: "the user selected a tab (its index)"},
		{Name: "Action", Payload: PInt, Doc: "a menu item, alert button or bar button (its index)"},
		{Name: "Dismissed", Payload: PNone},
		{Name: "Refresh", Payload: PNone, Doc: "the user pulled to refresh"},
	},
	Commands: []CommandDef{
		{Name: "Focus"},
		{Name: "Blur"},
		{Name: "ListUpdate", Doc: "args: u32 count, then count i64 item IDs in order"},
		{Name: "Present", Doc: "show a Sheet or Alert"},
		{Name: "Haptic", Doc: "args: u8 Haptic; sent to the root"},
	},
	Enums: []EnumDef{
		{Name: "Role", Values: []string{"None", "Button", "Header", "Image", "Link", "Text", "Search"}},
		{Name: "TextStyle", Values: []string{"Default", "LargeTitle", "Title", "Title2", "Title3", "Headline", "Subheadline", "Body", "Callout", "Footnote", "Caption", "Caption2"}},
		{Name: "FontWeight", Values: []string{"Default", "Ultralight", "Thin", "Light", "Regular", "Medium", "Semibold", "Bold", "Heavy", "Black"}},
		{Name: "TextAlign", Values: []string{"Start", "Center", "End", "Justify"}},
		{Name: "ButtonVariant", Values: []string{"Plain", "Filled", "Tinted", "Gray", "Bordered"}},
		{Name: "Keyboard", Values: []string{"Default", "Number", "Decimal", "Email", "URL", "Phone"}},
		{Name: "ReturnKey", Values: []string{"Default", "Done", "Go", "Next", "Search", "Send"}},
		{Name: "Autocapitalize", Values: []string{"Sentences", "None", "Words", "Characters"}},
		{Name: "Autocorrect", Values: []string{"Default", "Yes", "No"}},
		{Name: "Lifecycle", Values: []string{"Foreground", "Background", "MemoryWarning"}},
		{Name: "AlertStyle", Values: []string{"Alert", "ActionSheet"}},
		{Name: "AlertButtonRole", Values: []string{"Default", "Cancel", "Destructive"}},
		{Name: "MenuTrigger", Values: []string{"Tap", "LongPress"}},
		{Name: "Haptic", Values: []string{"Selection", "Light", "Medium", "Heavy", "Success", "Warning", "Error"}},
		{Name: "SemanticColor", Values: []string{"None", "Label", "SecondaryLabel", "TertiaryLabel", "Background", "SecondaryBackground", "GroupedBackground", "Separator", "Accent", "Red", "Orange", "Yellow", "Green", "Blue", "Purple", "Pink", "Gray", "Clear"}},
	},
}

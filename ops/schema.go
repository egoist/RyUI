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
	PGesture // a pan or pinch: state, location, translation, velocity, scale
	PBytes
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
		{Name: "Menu", Type: TBytes, Doc: "a menu: u32 count, then per item u8 role (AlertButtonRole), a u32-length title and a u32-length icon (Symbol.Name; empty: none); Action events carry the item's index"},
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
			{Name: "Icon", Type: TString, Doc: "a symbol (Symbol.Name) before the title"},
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
			{Name: "Multiline", Type: TBool, Doc: "lines of text: return is a new line (no Submit), and the field grows with its text up to LineLimit lines (none: no limit), then scrolls; sent before it's inserted"},
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
		{Name: "Image", Props: []PropDef{
			{Name: "Source", Type: TString, Doc: "an http(s) URL, or a file in the app's assets"},
			{Name: "Mode", Type: TEnum, Enum: "ImageMode"},
		}, Doc: "loaded and decoded off the main thread, cached; reports Loaded with its natural size (one point per pixel), or Failed. Color tints it"},
		{Name: "Native", Props: []PropDef{
			{Name: "Type", Type: TString, Doc: "the registered native kind; sent right after Create"},
			{Name: "Prop", Type: TBytes, Doc: "one named value: string name, u8 type (1 bool, 2 int, 3 float, 4 string), value"},
		}, Doc: "a view the app registers with each host; its events are Native"},
		{Name: "RichText", Props: []PropDef{
			{Name: "Content", Type: TBytes, Doc: "u32 style count, per style: u8 TextStyle (0: the view's), f32 size (0: the view's), u8 FontWeight (0: the view's), u8 flags (1 italic, 2 monospaced, 4 underline, 8 strikethrough, 16 inline: the run is U+FFFC, the next inline component), u64 color (0: the view's), u64 background (0: none), string link; then u32 paragraph count, per paragraph u32 run count, per run u32 style index and a string"},
			{Name: "Inlines", Type: TBytes, Doc: "the inline components, in the order of their runs: u32 count, per component u32 node id (a child of the view), f32 width, f32 height. Each sits on the baseline at its run's place; the host positions the child there (its frame's origin is 0, 0)"},
		}, Doc: "styled paragraphs typeset and drawn by nui's own view (CoreText, StaticLayout); the common text props are its defaults. A tapped link reports Link. Its children are its inline components"},
		{Name: "Selectable", Doc: "a container whose RichText descendants share one selection, across texts: the platform's handles, loupe and edit menu; Copy copies the selected text"},
		{Name: "Symbol", Props: []PropDef{
			{Name: "Name", Type: TString, Doc: "an SF Symbol name: iOS draws any; Android the common ones, as Material Symbols"},
		}, Doc: "a symbol sized by the text props (a text style, scaled as text is) and colored by Color (the label color by default)"},
		{Name: "Segmented", Props: []PropDef{
			{Name: "Segments", Type: TBytes, Doc: "u32 count, then each segment's u32-length title"},
			{Name: "Selected", Type: TInt, Doc: "the selected segment's index"},
		}, Doc: "one of a few options, side by side (UISegmentedControl; a Material segmented button group). The user's choice is reported with Selected"},
		{Name: "Progress", Props: []PropDef{
			{Name: "Value", Type: TFloat, Doc: "done, from 0 to 1"},
			{Name: "Indeterminate", Type: TBool, Doc: "under way, how far unknown (Android animates; iOS shows the track)"},
		}, Doc: "a progress bar (UIProgressView, a Material linear progress indicator); Color is its fill"},
		{Name: "DatePicker", Props: []PropDef{
			{Name: "Value", Type: TInt, Doc: "the instant, in Unix milliseconds"},
			{Name: "Mode", Type: TEnum, Enum: "DateMode"},
			{Name: "Min", Type: TInt, Doc: "the earliest choice, in Unix milliseconds (0: none)"},
			{Name: "Max", Type: TInt, Doc: "the latest choice, in Unix milliseconds (0: none)"},
		}, Doc: "a date or a time, chosen the platform's way (a compact UIDatePicker; Material's date and time pickers from buttons that show it), in the device's time zone and format. The user's choice is reported with ValueChanged, in Unix milliseconds"},
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
		{Name: "Selected", Payload: PInt, Doc: "the user selected a tab or a segment (its index)"},
		{Name: "Action", Payload: PInt, Doc: "a menu item, alert button or bar button (its index)"},
		{Name: "Dismissed", Payload: PNone},
		{Name: "Refresh", Payload: PNone, Doc: "the user pulled to refresh"},
		{Name: "Pan", Payload: PGesture},
		{Name: "Pinch", Payload: PGesture},
		{Name: "Swipe", Payload: PInt, Doc: "a SwipeDirection bit"},
		{Name: "Loaded", Payload: PPoint, Doc: "an image loaded: its natural size"},
		{Name: "Failed", Payload: PString},
		{Name: "Native", Payload: PBytes, Doc: "a native view's event: string name, u32 count, then named values as in Native's Prop"},
		{Name: "Link", Payload: PString, Doc: "a rich text's link was tapped (its URL)"},
	},
	Commands: []CommandDef{
		{Name: "Focus"},
		{Name: "Blur"},
		{Name: "ListUpdate", Doc: "args: u32 count, then count i64 item IDs in order"},
		{Name: "Present", Doc: "show a Sheet or Alert"},
		{Name: "Haptic", Doc: "args: u8 Haptic; sent to the root"},
		{Name: "AnimateBegin", Doc: "args: f32 duration (seconds), u8 AnimationCurve; sent to the root: the frames and visual props up to AnimateEnd animate"},
		{Name: "AnimateEnd"},
		{Name: "OpenURL", Doc: "args: the URL; sent to the root: the system opens it (a browser, another app)"},
		{Name: "Accent", Doc: "args: u64 Color (0: the platform's), u8 flags (1: on Android, the wallpaper's colors where the system has them); sent to the root when it mounts: the app's accent color, which the platform's controls and Accent take (iOS: the tint color; Android: a Material 3 color scheme made from it)"},
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
		{Name: "ImageMode", Values: []string{"Fit", "Fill", "Stretch", "Center"}},
		{Name: "DateMode", Values: []string{"Date", "Time", "DateTime"}},
		{Name: "GestureState", Values: []string{"Began", "Changed", "Ended", "Cancelled"}},
		{Name: "AnimationCurve", Values: []string{"EaseInOut", "Linear", "EaseIn", "EaseOut", "Spring"}},
		{Name: "SemanticColor", Values: []string{"None", "Label", "SecondaryLabel", "TertiaryLabel", "Background", "SecondaryBackground", "GroupedBackground", "Separator", "Accent", "Red", "Orange", "Yellow", "Green", "Blue", "Purple", "Pink", "Gray", "Clear"}},
	},
}

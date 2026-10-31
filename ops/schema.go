package ops

//go:generate go run ./internal/gen

// The schema: every kind of native view, its props, the events hosts
// send, the commands the core sends and the enums they use. go generate
// writes the Go constants (schema_gen.go) and the Swift decoder
// (platform/ios/Sources/RyUIOps/Ops.swift) from it.
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
		{Name: "A11yActions", Type: TBytes, Doc: "a screen reader's actions on the view: u32 count, then each name (u32-length string); taking one sends A11yAction with its index"},
		{Name: "A11yAdjustable", Type: TBool, Doc: "the view is adjustable (a screen reader's swipe up and down, TalkBack's volume keys): Adjust reports each step"},
		{Name: "A11yHidden", Type: TBool, Doc: "hidden from screen readers, with everything in it: a decoration"},
		{Name: "Color", Type: TColor, Doc: "text color, or tint for controls"},
		{Name: "TextStyle", Type: TEnum, Enum: "TextStyle"},
		{Name: "FontSize", Type: TFloat},
		{Name: "FontWeight", Type: TEnum, Enum: "FontWeight"},
		{Name: "TextAlign", Type: TEnum, Enum: "TextAlign"},
		{Name: "LineLimit", Type: TInt, Doc: "0: no limit"},
		{Name: "Disabled", Type: TBool},
		{Name: "Menu", Type: TBytes, Doc: "a menu: u32 count, then per item u8 role (AlertButtonRole), a u32-length title and a u32-length icon (Symbol.Name; empty: none); Action events carry the item's index"},
		{Name: "MenuTrigger", Type: TEnum, Enum: "MenuTrigger"},
		{Name: "Transform", Type: TBytes, Doc: "f32 translate x, y (points), scale x, y, rotation (degrees, clockwise), around the view's center; layout doesn't see it"},
		{Name: "Shadow", Type: TBytes, Doc: "u64 color (0: none), f32 blur radius, offset x, y (points), from the view's shape"},
		{Name: "Gradient", Type: TBytes, Doc: "a linear background gradient: u32 count, per stop u64 color and f32 location (0 to 1); f32 start x, y, end x, y (unit points of the view)"},
		{Name: "Transition", Type: TBytes, Doc: "u8 fade, f32 scale (0: none), offset x, y (points): the state a view comes in from when it's inserted during an animation, and goes out to when it's destroyed during one (the host keeps it until it has)"},
		{Name: "Material", Type: TEnum, Enum: "Material", Doc: "a translucent background that blurs what's behind (iOS's materials); Android, which can't, shows its surface color, translucent"},
		{Name: "FontFamily", Type: TString, Doc: "a font file's name in the app's assets/fonts (Inter-Bold.ttf: Inter-Bold), a family of them (Inter: Inter-Regular, Inter-Bold...), a generic family (serif, monospace, rounded) or a system family; scaled as the text style is. Under ryui dev, a font file's URL, which the host loads, then sends FontLoaded"},
		{Name: "SwipeActions", Type: TBytes, Doc: "a List cell's swipe actions, encoded as Menu (iOS: its trailing actions; Android: a full swipe runs the first); Action events on the cell carry the index"},
		{Name: "Match", Type: TString, Doc: "a key it shares with another view: when one with the key is destroyed during an animation and another inserted, the new one comes in from the old one's place and size, and the old one goes out to the new one's, fading (SwiftUI's matchedGeometryEffect). The host notes where the views with keys are as an animated batch starts destroying, and matches them once the batch's frames are applied"},
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
			{Name: "Columns", Type: TInt, Doc: "a grid of this many columns (0, 1: a list, with separators); each cell (width - gap * (columns - 1)) / columns wide, a row as tall as its tallest"},
			{Name: "Gap", Type: TFloat, Doc: "a grid's spacing between cells, across and down"},
			{Name: "Appearance", Type: TEnum, Enum: "ListAppearance", Doc: "a list's look (not a grid's): inset grouped, its rows the list's width less Inset on each side"},
			{Name: "Inset", Type: TFloat, Doc: "an inset grouped list's rows' inset from each side, in points (the core's, from the platform's margins)"},
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
			{Name: "Search", Type: TString, Doc: "a search field in the bar, with this placeholder (UISearchController; a toolbar search action expanding to a SearchView): typing is reported with TextChanged, the search key with Submit"},
			{Name: "SearchText", Type: TString, Doc: "the search field's text, set from code"},
			{Name: "ZoomFrom", Type: TString, Doc: "a Match key: the view with it on the screen below, which the screen zooms out of as it's pushed and into as it's popped (iOS 18's zoom transition, whose back swipe shrinks it into the view; on Android, Material's container transform)"},
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
			{Name: "Placeholder", Type: TString, Doc: "an image (usually an asset) shown until Source loads, and if it fails"},
		}, Doc: "loaded and decoded off the main thread, cached (in memory; one fetched over http(s) on disk too, unless its response says no-store); reports Loaded with its natural size (one point per pixel), or Failed. Color tints it"},
		{Name: "Native", Props: []PropDef{
			{Name: "Type", Type: TString, Doc: "the registered native kind; sent right after Create"},
			{Name: "Prop", Type: TBytes, Doc: "one named value: string name, u8 type (1 bool, 2 int, 3 float, 4 string), value"},
		}, Doc: "a view the app registers with each host; its events are Native"},
		{Name: "RichText", Props: []PropDef{
			{Name: "Content", Type: TBytes, Doc: "u32 style count, per style: u8 TextStyle (0: the view's), f32 size (0: the view's), u8 FontWeight (0: the view's), u8 flags (1 italic, 2 monospaced, 4 underline, 8 strikethrough, 16 inline: the run is U+FFFC, the next inline component), u64 color (0: the view's), u64 background (0: none), string link; then u32 paragraph count, per paragraph u32 run count, per run u32 style index and a string"},
			{Name: "Inlines", Type: TBytes, Doc: "the inline components, in the order of their runs: u32 count, per component u32 node id (a child of the view), f32 width, f32 height. Each sits on the baseline at its run's place; the host positions the child there (its frame's origin is 0, 0)"},
		}, Doc: "styled paragraphs typeset and drawn by ryui's own view (CoreText, StaticLayout); the common text props are its defaults. A tapped link reports Link. Its children are its inline components"},
		{Name: "Selectable", Doc: "a container whose RichText descendants share one selection, across texts: the platform's handles, loupe and edit menu; Copy copies the selected text"},
		{Name: "Symbol", Props: []PropDef{
			{Name: "Name", Type: TString, Doc: "an SF Symbol name: iOS draws any; Android the common ones, as Material Symbols"},
		}, Doc: "a symbol sized by the text props (a text style, scaled as text is) and colored by Color (the label color by default)"},
		{Name: "Segmented", Props: []PropDef{
			{Name: "Segments", Type: TBytes, Doc: "u32 count, then each segment's u32-length title"},
			{Name: "Selected", Type: TInt, Doc: "the selected segment's index"},
		}, Doc: "one of a few options, side by side (UISegmentedControl; a Material segmented button group). The user's choice is reported with Selected"},
		{Name: "Wheel", Props: []PropDef{
			{Name: "Items", Type: TBytes, Doc: "u32 count, then each item's u32-length title"},
			{Name: "Selected", Type: TInt, Doc: "the selected item's index"},
		}, Doc: "one of many options on a spinning wheel (UIPickerView; Android's NumberPicker). The user's choice is reported with Selected"},
		{Name: "Stepper", Props: []PropDef{
			{Name: "Value", Type: TFloat},
			{Name: "Min", Type: TFloat},
			{Name: "Max", Type: TFloat},
			{Name: "Step", Type: TFloat},
		}, Doc: "minus and plus buttons that step a value (UIStepper; on Android, which has none, two Material icon buttons). The user's steps are reported with ValueChanged"},
		{Name: "ColorPicker", Props: []PropDef{
			{Name: "Value", Type: TColor},
			{Name: "Alpha", Type: TBool, Doc: "the user can choose an opacity too"},
			{Name: "Title", Type: TString, Doc: "the picker's title"},
		}, Doc: "a swatch of a color that opens the platform's picker (UIColorWell and UIColorPickerViewController; on Android, which has none, a Material dialog of the palette Google's apps offer). The user's choice is reported with ColorChanged"},
		{Name: "Progress", Props: []PropDef{
			{Name: "Value", Type: TFloat, Doc: "done, from 0 to 1"},
			{Name: "Indeterminate", Type: TBool, Doc: "under way, how far unknown (Android animates; iOS shows the track)"},
		}, Doc: "a progress bar (UIProgressView, a Material linear progress indicator); Color is its fill"},
		{Name: "Shape", Props: []PropDef{
			{Name: "Path", Type: TBytes, Doc: "the outline, in points from the view's top left: per segment a u8 PathOp and its f32 arguments"},
			{Name: "Fill", Type: TColor},
			{Name: "Stroke", Type: TColor},
			{Name: "LineWidth", Type: TFloat},
			{Name: "LineCap", Type: TEnum, Enum: "LineCap"},
			{Name: "LineJoin", Type: TEnum, Enum: "LineJoin"},
			{Name: "TrimStart", Type: TFloat, Doc: "the part of the outline drawn: from 0 (its start) to 1 (its end); animatable"},
			{Name: "TrimEnd", Type: TFloat},
			{Name: "Dash", Type: TBytes, Doc: "a dashed stroke: f32 phase, then f32 lengths, drawn and skipped in turn (points); empty: solid"},
		}, Doc: "a drawn outline (CAShapeLayer; a Path on Android's canvas), filled and/or stroked"},
		{Name: "Video", Props: []PropDef{
			{Name: "Source", Type: TString, Doc: "an http(s) URL (a file, an HLS stream), or a file in the app's assets"},
			{Name: "Mode", Type: TEnum, Enum: "ImageMode", Doc: "fit or fill (Android: stretch and center fit)"},
			{Name: "HideControls", Type: TBool, Doc: "no playback controls (the platform's are shown by default)"},
			{Name: "AutoPlay", Type: TBool},
			{Name: "Loop", Type: TBool},
			{Name: "Muted", Type: TBool},
		}, Doc: "a video (AVPlayerViewController, Media3's PlayerView). Loaded reports its natural size once known, Ended its end (each time, looping), Failed an error; the Play, Pause and Seek commands drive it"},
		{Name: "WebView", Props: []PropDef{
			{Name: "URL", Type: TString, Doc: "the page: an http(s) URL, or a file in the app's assets"},
			{Name: "HTML", Type: TString, Doc: "markup to show instead of URL"},
			{Name: "Preload", Type: TString, Doc: "a script run before each page's own, in every frame (WKUserScript at document start; WebViewCompat.addDocumentStartJavaScript); set before URL"},
			{Name: "UserAgent", Type: TString, Doc: "the User-Agent the page is fetched with, and navigator.userAgent (empty: the platform's)"},
			{Name: "Block", Type: TBytes, Doc: "requests to block: u32 count, then u32-length patterns, each a host (which matches its subdomains too) or, with a /, a part of the URL (iOS: a content rule list; Android: shouldInterceptRequest)"},
			{Name: "Requests", Type: TBool, Doc: "report each request the page makes with Request (Android; iOS can't see them)"},
		}, Doc: "a web page (WKWebView, android.webkit.WebView). Navigated reports each page loaded (its URL), Failed a page that couldn't load, Message what the page posts with window.ryui.postMessage(text), WebState its URL, title, progress and history, Download a response the page can't show (a file to save), NewWindow a page it would open in a new window (window.open, target=_blank: without a listener, it loads in this view), Request each request (with Requests); the Eval command runs script in it, WebNavigate goes back, forward, reloads or stops"},
		{Name: "DatePicker", Props: []PropDef{
			{Name: "Value", Type: TInt, Doc: "the instant, in Unix milliseconds"},
			{Name: "Mode", Type: TEnum, Enum: "DateMode"},
			{Name: "Min", Type: TInt, Doc: "the earliest choice, in Unix milliseconds (0: none)"},
			{Name: "Max", Type: TInt, Doc: "the latest choice, in Unix milliseconds (0: none)"},
		}, Doc: "a date or a time, chosen the platform's way (a compact UIDatePicker; Material's date and time pickers from buttons that show it), in the device's time zone and format. The user's choice is reported with ValueChanged, in Unix milliseconds"},
		{Name: "Window", Props: []PropDef{
			{Name: "Title", Type: TString},
			{Name: "Width", Type: TFloat, Doc: "its content's width when it opens, in points (0: the host's default)"},
			{Name: "Height", Type: TFloat},
			{Name: "MinWidth", Type: TFloat, Doc: "the least the user can resize its content to (0: the host's)"},
			{Name: "MinHeight", Type: TFloat},
		}, Doc: "a desktop window of the app's (macOS, Windows; mobile hosts can't show one), with one child, its content: Present opens it, Destroy closes it; the user closing it is reported with Dismissed. Sized by the host (and the user), which reports its Environment"},
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
		{Name: "Scrolled", Payload: PPoint, Doc: "a Scroll's or List's content offset, in points from its content's top left, as it scrolls"},
		{Name: "Environment", Payload: PEnvironment, Doc: "sent for the root; the ID is the root's"},
		{Name: "Lifecycle", Payload: PInt, Doc: "a Lifecycle value"},
		{Name: "Popped", Payload: PInt, Doc: "the user popped a Stack's top screen (its ID)"},
		{Name: "Selected", Payload: PInt, Doc: "the user selected a tab or a segment (its index)"},
		{Name: "Action", Payload: PInt, Doc: "a menu item, alert button or bar button (its index)"},
		{Name: "Dismissed", Payload: PNone},
		{Name: "Refresh", Payload: PNone, Doc: "the user pulled to refresh"},
		{Name: "Pan", Payload: PGesture},
		{Name: "Pinch", Payload: PGesture},
		{Name: "Rotate", Payload: PGesture, Doc: "two fingers turning: Rotation is the angle since they began"},
		{Name: "Swipe", Payload: PInt, Doc: "a SwipeDirection bit"},
		{Name: "Loaded", Payload: PPoint, Doc: "an image loaded: its natural size"},
		{Name: "Failed", Payload: PString},
		{Name: "Native", Payload: PBytes, Doc: "a native view's event: string name, u32 count, then named values as in Native's Prop"},
		{Name: "Link", Payload: PString, Doc: "a rich text's link was tapped (its URL)"},
		{Name: "A11yAction", Payload: PInt, Doc: "a screen reader took one of the view's A11yActions (its index)"},
		{Name: "Adjust", Payload: PInt, Doc: "a screen reader adjusted the view: 1 up, -1 down"},
		{Name: "Ended", Payload: PNone, Doc: "a Video played to its end"},
		{Name: "Navigated", Payload: PString, Doc: "a WebView loaded a page (its URL)"},
		{Name: "Message", Payload: PString, Doc: "a WebView's page posted a message: window.ryui.postMessage(text)"},
		{Name: "IncomingURL", Payload: PString, Doc: "the app was opened with a URL (its scheme, or a universal or app link), at launch or since; to the root"},
		{Name: "FontLoaded", Payload: PString, Doc: "a font a FontFamily named by URL (under ryui dev) loaded, to the root: text in it is measured again"},
		{Name: "Frame", Payload: PFloat, Doc: "a frame the display draws (vsync), while Frames is on: its time in seconds (monotonic); to the root"},
		{Name: "Result", Payload: PBytes, Doc: "a Call's answer, to the root: u32 call id, u8 ok, then the result's bytes (an error's message when not ok)"},
		{Name: "ColorChanged", Payload: PInt, Doc: "the user chose a ColorPicker's color: a Color (RGBA)"},
		{Name: "WebState", Payload: PBytes, Doc: "a WebView's page changed: string URL, string title, u8 flags (1 loading, 2 can go back, 4 can go forward), f32 progress (0 to 1)"},
		{Name: "Request", Payload: PString, Doc: "a WebView's page made a request (its URL), with Requests"},
		{Name: "Download", Payload: PBytes, Doc: "a WebView's page led to a file it can't show: string URL, string MIME type, string file name, i64 size (-1: unknown)"},
		{Name: "NewWindow", Payload: PString, Doc: "a WebView's page would open a page in a new window (its URL)"},
		{Name: "Shared", Payload: PString, Doc: "another app shared text with the app (a link from its share sheet: iOS's share extension, Android's ACTION_SEND, with ryui.json's shareTarget), at launch or since; to the root"},
	},
	Commands: []CommandDef{
		{Name: "Focus"},
		{Name: "Blur"},
		{Name: "ListUpdate", Doc: "args: u32 count, then count i64 item IDs in order"},
		{Name: "Present", Doc: "show a Sheet or Alert"},
		{Name: "Haptic", Doc: "args: u8 Haptic; sent to the root"},
		{Name: "AnimateBegin", Doc: "args: f32 duration (seconds), u8 AnimationCurve, i32 repeat (times more; -1: forever), u8 autoreverses; sent to the root: the frames and visual props up to AnimateEnd animate. A repeating animation stops when its prop is set again"},
		{Name: "AnimateEnd"},
		{Name: "OpenURL", Doc: "args: the URL; sent to the root: the system opens it (a browser, another app)"},
		{Name: "Call", Doc: "args: u32 call id, string module, string method, then the call's bytes; sent to the root: the host runs the module's method (its own, or one an app registers) and answers with Result"},
		{Name: "Copy", Doc: "args: the text; sent to the root: it's put on the clipboard"},
		{Name: "Play", Doc: "sent to a Video"},
		{Name: "Pause", Doc: "sent to a Video"},
		{Name: "Seek", Doc: "args: f64 seconds from the start; sent to a Video"},
		{Name: "Eval", Doc: "args: JavaScript; sent to a WebView: run in its page"},
		{Name: "Frames", Doc: "args: u8 on; sent to the root: the host sends Frame at each vsync (CADisplayLink, Choreographer) while it's on"},
		{Name: "Announce", Doc: "args: the text; sent to the root: a screen reader, if one is on, reads it out"},
		{Name: "Share", Doc: "args: the text (a URL is shared as one); sent to the root: the system's share sheet"},
		{Name: "ScrollTo", Doc: "args: u32 item index, u8 animated; sent to a List: it scrolls the item into view"},
		{Name: "ScrollToOffset", Doc: "args: f32 x, f32 y (points from the content's top left; clamped, so a large one is the end), u8 animated; sent to a Scroll or List"},
		{Name: "Accent", Doc: "args: u64 Color (0: the platform's), u8 flags (1: on Android, the wallpaper's colors where the system has them); sent to the root when it mounts: the app's accent color, which the platform's controls and Accent take (iOS: the tint color; Android: a Material 3 color scheme made from it)"},
		{Name: "WebNavigate", Doc: "args: u8 0 back, 1 forward, 2 reload, 3 stop; sent to a WebView"},
		{Name: "MenuBar", Doc: "args: the app's menus (desktop): u32 count, per menu a u32-length title and u32 item count, per item u8 flags (1 separator, 2 disabled, 4 checked, 8 destructive), a u32-length title, a u32-length key (a character, or enter, escape, tab, space, delete, backspace, up, down, left, right, home, end, pageup, pagedown, f1-f12; empty: no shortcut) and u8 modifiers (1 primary: ⌘ on macOS, Ctrl on Windows; 2 shift; 4 alt, a Mac's option; 8 control); sent to the root, again when they change: the host shows them with its own (a Mac's app, Edit and Window menus), and reports a chosen item, or its shortcut, with Action on the root carrying the item's index among all the menus' items"},
		{Name: "WindowOptions", Doc: "args: f32 width, height, min width, min height (points, 0: the host's), then the title (rest of the args; empty: the app's name); sent to the root when it mounts: the main window's"},
		{Name: "ColorScheme", Doc: "args: u8 ColorScheme; sent to the root: the app shows light or dark whatever the system's setting, or follows it again (iOS: the windows' overrideUserInterfaceStyle; Android: AppCompat's night mode)"},
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
		{Name: "ListAppearance", Values: []string{"Plain", "InsetGrouped"}},
		{Name: "DateMode", Values: []string{"Date", "Time", "DateTime"}},
		{Name: "Material", Values: []string{"None", "UltraThin", "Thin", "Regular", "Thick", "Bar"}},
		{Name: "GestureState", Values: []string{"Began", "Changed", "Ended", "Cancelled"}},
		{Name: "PathOp", Values: []string{"End", "Move", "Line", "Quad", "Cubic", "Close", "Rect", "RoundedRect", "Ellipse", "Arc"}},
		{Name: "LineCap", Values: []string{"Butt", "Round", "Square"}},
		{Name: "LineJoin", Values: []string{"Miter", "Round", "Bevel"}},
		{Name: "AnimationCurve", Values: []string{"EaseInOut", "Linear", "EaseIn", "EaseOut", "Spring"}},
		{Name: "ColorScheme", Values: []string{"System", "Light", "Dark"}},
		{Name: "SemanticColor", Values: []string{"None", "Label", "SecondaryLabel", "TertiaryLabel", "Background", "SecondaryBackground", "GroupedBackground", "Separator", "Accent", "Red", "Orange", "Yellow", "Green", "Blue", "Purple", "Pink", "Gray", "Clear", "SecondaryGroupedBackground"}},
	},
}

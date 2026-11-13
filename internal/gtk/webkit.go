//go:build linux && !android

package gtk

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

// WebKitGTK 6.0 (the GTK 4 port) and the JavaScriptCore and libsoup it
// links, opened the first time a web view is made (LoadWebKit): an app
// that shows none doesn't need them installed.

var (
	libWebKit, libJSC, libSoup uintptr
	webKitOnce                 sync.Once
	webKitErr                  error
)

var webKitSonames = []struct {
	lib   *uintptr
	names []string
}{
	{&libJSC, []string{"libjavascriptcoregtk-6.0.so.1", "libjavascriptcoregtk-6.0.so"}},
	{&libSoup, []string{"libsoup-3.0.so.0", "libsoup-3.0.so"}},
	{&libWebKit, []string{"libwebkitgtk-6.0.so.4", "libwebkitgtk-6.0.so"}},
}

// Bound by LoadWebKit.
var (
	WebKitWebViewNew                         func() Ptr
	WebKitWebViewLoadURI                     func(v Ptr, uri string)
	WebKitWebViewLoadHTML                    func(v Ptr, html string, base Ptr)
	WebKitWebViewGoBack                      func(v Ptr)
	WebKitWebViewGoForward                   func(v Ptr)
	WebKitWebViewReload                      func(v Ptr)
	WebKitWebViewStopLoading                 func(v Ptr)
	WebKitWebViewGetURI                      func(v Ptr) Ptr
	WebKitWebViewGetTitle                    func(v Ptr) Ptr
	WebKitWebViewGetEstimatedLoadProgress    func(v Ptr) float64
	WebKitWebViewIsLoading                   func(v Ptr) bool
	WebKitWebViewCanGoBack                   func(v Ptr) bool
	WebKitWebViewCanGoForward                func(v Ptr) bool
	WebKitWebViewGetSettings                 func(v Ptr) Ptr
	WebKitWebViewGetUserContentManager       func(v Ptr) Ptr
	WebKitWebViewGetBackForwardList          func(v Ptr) Ptr
	WebKitWebViewEvaluateJavascript          func(v Ptr, script string, length int, world, sourceURI, cancellable, cb, data Ptr)
	WebKitWebViewCallAsyncJavascriptFunction func(v Ptr, body string, length int, args, world, sourceURI, cancellable, cb, data Ptr)
	WebKitWebViewCallAsyncJavascriptFinish   func(v, res Ptr, err *Ptr) Ptr
	WebKitSettingsSetUserAgent               func(s Ptr, ua Ptr)
	WebKitSettingsSetEnableDeveloperExtras   func(s Ptr, on bool)

	WebKitUserContentManagerRegisterScriptMessageHandler func(m Ptr, name string, world Ptr) bool
	WebKitUserContentManagerAddScript                    func(m, script Ptr)
	WebKitUserContentManagerRemoveAllScripts             func(m Ptr)
	WebKitUserContentManagerAddFilter                    func(m, filter Ptr)
	WebKitUserContentManagerRemoveAllFilters             func(m Ptr)
	WebKitUserScriptNew                                  func(source string, frames, when int32, allow, block Ptr) Ptr
	WebKitUserScriptUnref                                func(s Ptr)
	WebKitUserContentFilterStoreNew                      func(path string) Ptr
	WebKitUserContentFilterStoreSave                     func(store Ptr, id string, source, cancellable, cb, data Ptr)
	WebKitUserContentFilterStoreSaveFinish               func(store, res Ptr, err *Ptr) Ptr
	WebKitUserContentFilterUnref                         func(f Ptr)

	WebKitPolicyDecisionUse                         func(d Ptr)
	WebKitPolicyDecisionIgnore                      func(d Ptr)
	WebKitResponsePolicyDecisionGetResponse         func(d Ptr) Ptr
	WebKitResponsePolicyDecisionIsMimeTypeSupported func(d Ptr) bool
	WebKitResponsePolicyDecisionIsMainFrameMain     func(d Ptr) bool
	WebKitURIResponseGetURI                         func(r Ptr) Ptr
	WebKitURIResponseGetMimeType                    func(r Ptr) Ptr
	WebKitURIResponseGetSuggestedFilename           func(r Ptr) Ptr
	WebKitURIResponseGetContentLength               func(r Ptr) uint64
	WebKitURIResponseGetHTTPHeaders                 func(r Ptr) Ptr
	WebKitURIRequestGetURI                          func(r Ptr) Ptr
	WebKitNavigationActionGetRequest                func(a Ptr) Ptr
	WebKitPolicyErrorQuark                          func() uint32
	WebKitNetworkErrorQuark                         func() uint32

	WebKitNetworkSessionGetDefault       func() Ptr
	WebKitNetworkSessionGetCookieManager func(s Ptr) Ptr
	WebKitCookieManagerGetCookies        func(m Ptr, uri string, cancellable, cb, data Ptr)
	WebKitCookieManagerGetCookiesFinish  func(m, res Ptr, err *Ptr) Ptr // a GList of SoupCookie
	SoupCookieGetName                    func(c Ptr) Ptr
	SoupCookieGetValue                   func(c Ptr) Ptr
	SoupCookieFree                       func(c Ptr)
	SoupMessageHeadersGetOne             func(h Ptr, name string) Ptr
	JSCValueToString                     func(v Ptr) Ptr
	webKitWebViewGetTypeForCheck         func() GType
)

// The values of WebKit's enums the host uses.
const (
	WebKitLoadStarted    = 0
	WebKitLoadRedirected = 1
	WebKitLoadCommitted  = 2
	WebKitLoadFinished   = 3

	WebKitPolicyNavigation = 0
	WebKitPolicyNewWindow  = 1
	WebKitPolicyResponse   = 2

	WebKitInjectAllFrames                 = 0
	WebKitInjectAtDocStart                = 0
	WebKitInjectAtDocEnd                  = 1
	WebKitPolicyErrorFrameLoadInterrupted = 102 // WEBKIT_POLICY_ERROR_FRAME_LOAD_INTERRUPTED_BY_POLICY_CHANGE
	WebKitNetworkErrorCancelled           = 302 // WEBKIT_NETWORK_ERROR_CANCELLED
)

var webKitFuncs = []struct {
	lib  *uintptr
	p    any
	name string
}{
	{&libWebKit, &WebKitWebViewNew, "webkit_web_view_new"},
	{&libWebKit, &WebKitWebViewLoadURI, "webkit_web_view_load_uri"},
	{&libWebKit, &WebKitWebViewLoadHTML, "webkit_web_view_load_html"},
	{&libWebKit, &WebKitWebViewGoBack, "webkit_web_view_go_back"},
	{&libWebKit, &WebKitWebViewGoForward, "webkit_web_view_go_forward"},
	{&libWebKit, &WebKitWebViewReload, "webkit_web_view_reload"},
	{&libWebKit, &WebKitWebViewStopLoading, "webkit_web_view_stop_loading"},
	{&libWebKit, &WebKitWebViewGetURI, "webkit_web_view_get_uri"},
	{&libWebKit, &WebKitWebViewGetTitle, "webkit_web_view_get_title"},
	{&libWebKit, &WebKitWebViewGetEstimatedLoadProgress, "webkit_web_view_get_estimated_load_progress"},
	{&libWebKit, &WebKitWebViewIsLoading, "webkit_web_view_is_loading"},
	{&libWebKit, &WebKitWebViewCanGoBack, "webkit_web_view_can_go_back"},
	{&libWebKit, &WebKitWebViewCanGoForward, "webkit_web_view_can_go_forward"},
	{&libWebKit, &WebKitWebViewGetSettings, "webkit_web_view_get_settings"},
	{&libWebKit, &WebKitWebViewGetUserContentManager, "webkit_web_view_get_user_content_manager"},
	{&libWebKit, &WebKitWebViewGetBackForwardList, "webkit_web_view_get_back_forward_list"},
	{&libWebKit, &WebKitWebViewEvaluateJavascript, "webkit_web_view_evaluate_javascript"},
	{&libWebKit, &WebKitWebViewCallAsyncJavascriptFunction, "webkit_web_view_call_async_javascript_function"},
	{&libWebKit, &WebKitWebViewCallAsyncJavascriptFinish, "webkit_web_view_call_async_javascript_function_finish"},
	{&libWebKit, &WebKitSettingsSetUserAgent, "webkit_settings_set_user_agent"},
	{&libWebKit, &WebKitSettingsSetEnableDeveloperExtras, "webkit_settings_set_enable_developer_extras"},
	{&libWebKit, &WebKitUserContentManagerRegisterScriptMessageHandler, "webkit_user_content_manager_register_script_message_handler"},
	{&libWebKit, &WebKitUserContentManagerAddScript, "webkit_user_content_manager_add_script"},
	{&libWebKit, &WebKitUserContentManagerRemoveAllScripts, "webkit_user_content_manager_remove_all_scripts"},
	{&libWebKit, &WebKitUserContentManagerAddFilter, "webkit_user_content_manager_add_filter"},
	{&libWebKit, &WebKitUserContentManagerRemoveAllFilters, "webkit_user_content_manager_remove_all_filters"},
	{&libWebKit, &WebKitUserScriptNew, "webkit_user_script_new"},
	{&libWebKit, &WebKitUserScriptUnref, "webkit_user_script_unref"},
	{&libWebKit, &WebKitUserContentFilterStoreNew, "webkit_user_content_filter_store_new"},
	{&libWebKit, &WebKitUserContentFilterStoreSave, "webkit_user_content_filter_store_save"},
	{&libWebKit, &WebKitUserContentFilterStoreSaveFinish, "webkit_user_content_filter_store_save_finish"},
	{&libWebKit, &WebKitUserContentFilterUnref, "webkit_user_content_filter_unref"},
	{&libWebKit, &WebKitPolicyDecisionUse, "webkit_policy_decision_use"},
	{&libWebKit, &WebKitPolicyDecisionIgnore, "webkit_policy_decision_ignore"},
	{&libWebKit, &WebKitResponsePolicyDecisionGetResponse, "webkit_response_policy_decision_get_response"},
	{&libWebKit, &WebKitResponsePolicyDecisionIsMimeTypeSupported, "webkit_response_policy_decision_is_mime_type_supported"},
	{&libWebKit, &WebKitResponsePolicyDecisionIsMainFrameMain, "webkit_response_policy_decision_is_main_frame_main_resource"},
	{&libWebKit, &WebKitURIResponseGetURI, "webkit_uri_response_get_uri"},
	{&libWebKit, &WebKitURIResponseGetMimeType, "webkit_uri_response_get_mime_type"},
	{&libWebKit, &WebKitURIResponseGetSuggestedFilename, "webkit_uri_response_get_suggested_filename"},
	{&libWebKit, &WebKitURIResponseGetContentLength, "webkit_uri_response_get_content_length"},
	{&libWebKit, &WebKitURIResponseGetHTTPHeaders, "webkit_uri_response_get_http_headers"},
	{&libWebKit, &WebKitURIRequestGetURI, "webkit_uri_request_get_uri"},
	{&libWebKit, &WebKitNavigationActionGetRequest, "webkit_navigation_action_get_request"},
	{&libWebKit, &WebKitPolicyErrorQuark, "webkit_policy_error_quark"},
	{&libWebKit, &WebKitNetworkErrorQuark, "webkit_network_error_quark"},
	{&libWebKit, &WebKitNetworkSessionGetDefault, "webkit_network_session_get_default"},
	{&libWebKit, &WebKitNetworkSessionGetCookieManager, "webkit_network_session_get_cookie_manager"},
	{&libWebKit, &WebKitCookieManagerGetCookies, "webkit_cookie_manager_get_cookies"},
	{&libWebKit, &WebKitCookieManagerGetCookiesFinish, "webkit_cookie_manager_get_cookies_finish"},
	{&libWebKit, &webKitWebViewGetTypeForCheck, "webkit_web_view_get_type"},
	{&libSoup, &SoupCookieGetName, "soup_cookie_get_name"},
	{&libSoup, &SoupCookieGetValue, "soup_cookie_get_value"},
	{&libSoup, &SoupCookieFree, "soup_cookie_free"},
	{&libSoup, &SoupMessageHeadersGetOne, "soup_message_headers_get_one"},
	{&libJSC, &JSCValueToString, "jsc_value_to_string"},
}

// LoadWebKit opens WebKitGTK 6.0, once. It fails, saying what to
// install, when it isn't installed. Main thread, after Load.
func LoadWebKit() error {
	webKitOnce.Do(func() {
		for _, l := range webKitSonames {
			var err error
			for _, name := range l.names {
				*l.lib, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
				if err == nil {
					break
				}
			}
			if *l.lib == 0 {
				webKitErr = fmt.Errorf("WebKitGTK 6.0 isn't installed (%s: %v): install it to show web pages (Ubuntu, Debian: libwebkitgtk-6.0-4; Fedora: webkitgtk6.0; Arch: webkitgtk-6.0)", l.names[0], err)
				return
			}
		}
		for _, f := range webKitFuncs {
			p, err := purego.Dlsym(*f.lib, f.name)
			if err != nil {
				webKitErr = fmt.Errorf("WebKitGTK 6.0 is too old (no %s): update it to show web pages", f.name)
				return
			}
			purego.RegisterFunc(f.p, p)
		}
	})
	return webKitErr
}

// IsWebView reports whether w is a WebKitWebView.
func IsWebView(w Ptr) bool {
	return webKitErr == nil && libWebKit != 0 && TypeCheckInstanceIsA(w, webKitWebViewGetTypeForCheck())
}

// WebKitError is a GError's domain (a quark), code and message.
func WebKitError(e Ptr) (domain uint32, code int32, msg string) {
	if e == 0 {
		return 0, 0, ""
	}
	return *At[uint32](e), *At[int32](e + 4), GoString(*At[Ptr](e + 8))
}

// WebGLib is what web.go and video.go call of GLib and GIO: lists,
// variant dictionaries, files. Bound by Load, in a namespace of its own.
var WebGLib struct {
	ListFree          func(l Ptr)
	VariantDictNew    func(from Ptr) Ptr
	VariantDictInsert func(d Ptr, key string, v Ptr)
	VariantDictEnd    func(d Ptr) Ptr
	VariantDictUnref  func(d Ptr)
	VariantRefSink    func(v Ptr) Ptr
	VariantUnref      func(v Ptr)
	FileNewForPath    func(path string) Ptr
	FileNewForURI     func(uri string) Ptr
}

func init() {
	g := &WebGLib
	bindings = append(bindings,
		binding{&g.ListFree, "g_list_free"},
		binding{&g.VariantDictNew, "g_variant_dict_new"},
		binding{&g.VariantDictInsert, "g_variant_dict_insert_value"},
		binding{&g.VariantDictEnd, "g_variant_dict_end"},
		binding{&g.VariantDictUnref, "g_variant_dict_unref"},
		binding{&g.VariantRefSink, "g_variant_ref_sink"},
		binding{&g.VariantUnref, "g_variant_unref"},
		binding{&g.FileNewForPath, "g_file_new_for_path"},
		binding{&g.FileNewForURI, "g_file_new_for_uri"},
	)
}

// WebKitListItems is a GList's data pointers.
func WebKitListItems(l Ptr) []Ptr {
	var out []Ptr
	for ; l != 0; l = *At[Ptr](l + 8) {
		out = append(out, *At[Ptr](l))
	}
	return out
}

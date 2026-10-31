# The Go core calls these through JNI.
-keep class dev.ryui.RyUIRuntime { *; }
-keep class dev.ryui.RyUINative { *; }
# The host finds the app's native views by name.
-keep class dev.ryui.app.RyUIAppNatives { *; }
-keep class * implements dev.ryui.RyUINativeModule { *; }
# The optional modules' plugins, found by name.
-keep class * implements dev.ryui.RyUIPlugin { <init>(); }

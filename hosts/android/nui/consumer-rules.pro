# The Go core calls these through JNI.
-keep class dev.nui.NuiRuntime { *; }
-keep class dev.nui.NuiNative { *; }
# The host finds the app's native views by name.
-keep class dev.nui.app.NuiAppNatives { *; }
-keep class * implements dev.nui.NuiNativeModule { *; }

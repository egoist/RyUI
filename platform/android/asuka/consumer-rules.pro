# The Go core calls these through JNI.
-keep class dev.asuka.AsukaRuntime { *; }
-keep class dev.asuka.AsukaNative { *; }
# The host finds the app's native views by name.
-keep class dev.asuka.app.AsukaAppNatives { *; }
-keep class * implements dev.asuka.AsukaNativeModule { *; }
# The optional modules' plugins, found by name.
-keep class * implements dev.asuka.AsukaPlugin { <init>(); }
# asuka/sdk/print's PDF writer subclasses the print callbacks in their package.
-keep class android.print.AsukaPdfWriter** { *; }

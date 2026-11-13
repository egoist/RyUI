# The Go core calls these through JNI.
-keep class dev.asuka.AsukaRuntime { *; }
-keep class dev.asuka.AsukaNative { *; }
# Packages' typed modules (asuka native's AsukaModule objects), which the
# host finds by name, and the runtime JNI calls.
-keep class * implements dev.asuka.AsukaModuleProvider { *; }
-keep class dev.asuka.AsukaModules { *; }
# The optional modules' plugins, found by name.
-keep class * implements dev.asuka.AsukaPlugin { <init>(); }
# asuka/sdk/print's PDF writer subclasses the print callbacks in their package.
-keep class android.print.AsukaPdfWriter** { *; }

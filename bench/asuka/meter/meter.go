// Package meter is the benchmarks' meter (bench/README.md), a native
// package: the same Swift and Kotlin the SwiftUI and Compose apps measure
// with (swift/Meter.swift, android/src/main/kotlin/dev/asuka/app/Meter.kt),
// for Go to mark its scenarios.
package meter

// Meter measures the main thread's work, frame by frame, and reports it
// (the log: bench/run reads it). Making one starts watching.
//
//asuka:native
//asuka:mainthread
type Meter interface {
	// Begin starts recording a scenario's frames.
	Begin(scenario string)
	// End stops recording and reports the scenario's frames.
	End()
	// Startup reports the time from the process's start to now.
	Startup()
	// Memory reports the memory the app uses, after a scenario.
	Memory(scenario string)
	// Done reports that the benchmarks are over.
	Done()
}

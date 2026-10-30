## iOS

iPhone 17 Pro simulator, 2026-10-30; release builds; the median of 5 runs (after one to warm up).

Memory is the footprint iOS counts against the app (phys_footprint).

| scenario | | nui | SwiftUI |
|---|---|---:|---:|
| **startup** | from the process's start to the first frame, ms | 307 | 356 |
| **memory-idle** | at the first frame, MB | 14.8 | 15.1 |
|  | peak since launch, MB | 14.8 | 15.1 |
| **mount** | 1,000 rows appear: the main thread's time, ms | 88.1 | 163 |
| **memory-mount** | with the 1,000 rows, MB | 36.1 | 93.2 |
|  | peak since launch, MB | 36.7 | 99.1 |
| **update-one** | mean ms a frame | 2.15 | 4.91 |
|  | 95th percentile | 3.02 | 7.03 |
|  | dropped frames | 0.00 | 0.00 |
| **update-all** | mean ms a frame | 6.39 | 62.4 |
|  | 95th percentile | 7.64 | 68.5 |
|  | dropped frames | 2.00 | 120 |
| **scroll** | mean ms a frame | 1.02 | 1.14 |
|  | 95th percentile | 3.72 | 5.71 |
|  | dropped frames | 0.00 | 0.00 |
| **memory-list** | after scrolling the list, MB | 35.0 | 21.4 |
|  | peak since launch, MB | 45.2 | 125 |

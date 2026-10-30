## iOS

iPhone 17 Pro simulator, 2026-10-30; release builds; the median of 5 runs (after one to warm up).

Memory is the footprint iOS counts against the app (phys_footprint).

| scenario | | nui | SwiftUI |
|---|---|---:|---:|
| **startup** | from the process's start to the first frame, ms | 315 | 337 |
| **memory-idle** | at the first frame, MB | 15.5 | 15.0 |
|  | peak since launch, MB | 15.5 | 15.0 |
| **mount** | 1,000 rows appear: the main thread's time, ms | 124 | 164 |
| **memory-mount** | with the 1,000 rows, MB | 113 | 92.9 |
|  | peak since launch, MB | 119 | 98.8 |
| **update-one** | mean ms a frame | 3.43 | 4.80 |
|  | 95th percentile | 4.15 | 6.90 |
|  | dropped frames | 0.00 | 0.00 |
| **update-all** | mean ms a frame | 33.7 | 64.9 |
|  | 95th percentile | 41.8 | 71.7 |
|  | dropped frames | 107 | 120 |
| **scroll** | mean ms a frame | 1.05 | 1.23 |
|  | 95th percentile | 3.69 | 5.84 |
|  | dropped frames | 0.00 | 0.00 |
| **memory-list** | after scrolling the list, MB | 35.7 | 21.8 |
|  | peak since launch, MB | 242 | 125 |

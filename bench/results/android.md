## Android

sdk_gphone16k_arm64 emulator, 2026-10-30; release builds; the median of 5 runs (after one to warm up).

Memory is the proportional set size (PSS); its peak, the resident set's high-water mark (VmHWM).

| scenario | | RyUI | Compose |
|---|---|---:|---:|
| **startup** | from the process's start to the first frame, ms | 319 | 327 |
| **memory-idle** | at the first frame, MB | 43.3 | 32.3 |
|  | peak since launch, MB | 132 | 120 |
| **mount** | 1,000 rows appear: the main thread's time, ms | 539 | 444 |
| **memory-mount** | with the 1,000 rows, MB | 72.8 | 66.7 |
|  | peak since launch, MB | 162 | 156 |
| **update-one** | mean ms a frame | 2.74 | 26.8 |
|  | 95th percentile | 7.49 | 36.3 |
|  | dropped frames | 26.0 | 128 |
|  | frame's whole time, ms (with the GPU) | 134 | 75.2 |
| **update-all** | mean ms a frame | 14.6 | 125 |
|  | 95th percentile | 24.0 | 162 |
|  | dropped frames | 50.0 | 119 |
|  | frame's whole time, ms (with the GPU) | 81.8 | 267 |
| **scroll** | mean ms a frame | 4.03 | 4.19 |
|  | 95th percentile | 9.49 | 8.70 |
|  | dropped frames | 165 | 170 |
|  | frame's whole time, ms (with the GPU) | 127 | 135 |
| **memory-list** | after scrolling the list, MB | 74.9 | 74.5 |
|  | peak since launch, MB | 169 | 182 |

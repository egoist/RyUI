## Android

sdk_gphone16k_arm64 emulator, 2026-10-30; release builds; the median of 3 runs (after one to warm up).

Memory is the proportional set size (PSS); its peak, the resident set's high-water mark (VmHWM).

| scenario | | nui | Compose |
|---|---|---:|---:|
| **startup** | from the process's start to the first frame, ms | 367 | 405 |
| **memory-idle** | at the first frame, MB | 43.1 | 32.2 |
|  | peak since launch, MB | 131 | 119 |
| **mount** | 1,000 rows appear: the main thread's time, ms | 595 | 201 |
| **memory-mount** | with the 1,000 rows, MB | 84.5 | 66.5 |
|  | peak since launch, MB | 174 | 155 |
| **update-one** | mean ms a frame | 7.85 | 24.6 |
|  | 95th percentile | 13.3 | 33.4 |
|  | dropped frames | 122 | 137 |
|  | frame's whole time, ms (with the GPU) | 118 | 75.1 |
| **update-all** | mean ms a frame | 32.1 | 121 |
|  | 95th percentile | 44.2 | 151 |
|  | dropped frames | 87.0 | 118 |
|  | frame's whole time, ms (with the GPU) | 73.6 | 262 |
| **scroll** | mean ms a frame | 1.27 | 1.18 |
|  | 95th percentile | 3.22 | 3.22 |
|  | dropped frames | 163 | 163 |
|  | frame's whole time, ms (with the GPU) | 132 | 130 |
| **memory-list** | after scrolling the list, MB | 86.6 | 74.2 |
|  | peak since launch, MB | 188 | 179 |

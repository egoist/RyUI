// The C ABI of nui's Go core (mobile/mobile.go). Every call comes from
// the main thread.
#ifndef NUI_H
#define NUI_H

#include <stdint.h>
#include <stddef.h>

typedef struct {
	// Applies a batch of encoded ops (ops/ops.go). The buffer is valid
	// during the call only.
	void (*apply)(const uint8_t *buf, size_t len);
	// Measures view id: width/height are known sizes or NaN; avail* are
	// the space available: a definite size, +inf (max-content) or -inf
	// (min-content).
	void (*measure)(uint32_t id, float width, float height, float availWidth, float availHeight, float *outWidth, float *outHeight);
	// Asks for nui_run_posted to be called soon on the main thread. Any
	// thread.
	void (*post)(void);
} nui_host_callbacks;

void nui_start(nui_host_callbacks *callbacks);
void nui_attach(void);
void nui_event(const uint8_t *buf, size_t len);
int64_t nui_list_count(uint32_t list);
void nui_list_bind(uint32_t list, int64_t index, uint32_t cell);
void nui_list_unbind(uint32_t list, uint32_t cell);
void nui_run_posted(void);

#endif

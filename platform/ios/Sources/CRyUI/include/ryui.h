// The C ABI of RyUI's Go core (mobile/mobile.go). Every call comes from
// the main thread.
#ifndef RYUI_H
#define RYUI_H

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
	// Asks for ryui_run_posted to be called soon on the main thread. Any
	// thread.
	void (*post)(void);
} ryui_host_callbacks;

void ryui_start(ryui_host_callbacks *callbacks);
void ryui_attach(void);
void ryui_event(const uint8_t *buf, size_t len);
int64_t ryui_list_count(uint32_t list);
void ryui_list_bind(uint32_t list, int64_t index, uint32_t cell);
void ryui_list_unbind(uint32_t list, uint32_t cell);
void ryui_run_posted(void);
// Sets an environment variable for the Go code (os.Setenv), which copied
// the process's as it loaded.
void ryui_setenv(const char *key, const char *value);

#endif

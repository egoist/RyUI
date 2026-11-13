// The C ABI of Asuka's Go core (mobile/mobile.go). Every call comes from
// the main thread.
#ifndef ASUKA_H
#define ASUKA_H

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
	// Asks for asuka_run_posted to be called soon on the main thread. Any
	// thread.
	void (*post)(void);
	// Typed native modules (package native's Backend), from any thread:
	// make an object, call its method (answers into out, cap bytes, else
	// into *big, which native_free frees; the answer's length returned),
	// start a method that takes a context (asuka_native_done answers it),
	// cancel it, let an object go.
	size_t (*native_new)(const uint8_t *cls, size_t clsLen, uint32_t iface, uint8_t *out, size_t cap, uint8_t **big);
	size_t (*native_call)(uint64_t handle, uint32_t method, const uint8_t *in, size_t len, uint8_t *out, size_t cap, uint8_t **big);
	void (*native_start)(uint64_t handle, uint32_t method, const uint8_t *in, size_t len, uint64_t call);
	void (*native_cancel)(uint64_t call);
	void (*native_release)(uint64_t handle);
	void (*native_free)(void *p);
} asuka_host_callbacks;

void asuka_start(asuka_host_callbacks *callbacks);
void asuka_attach(void);
void asuka_event(const uint8_t *buf, size_t len);
int64_t asuka_list_count(uint32_t list);
void asuka_list_bind(uint32_t list, int64_t index, uint32_t cell);
void asuka_list_unbind(uint32_t list, uint32_t cell);
void asuka_run_posted(void);
// Sets an environment variable for the Go code (os.Setenv), which copied
// the process's as it loaded.
void asuka_setenv(const char *key, const char *value);
// Begins the run's crash record (asuka/sdk/crash), with info, JSON of the
// device and the app: the directory native reports go in, "" if the app
// doesn't report crashes. Any thread, before asuka_start; free the result.
char *asuka_crash_start(const char *info);
// Says the host wrote a report in the directory's inbox. Any thread.
void asuka_crash_notify(void);
// Typed native modules' way back to Go, from any thread: a started
// call's answer; a Go callback's call, its answer (none: 0) into *out,
// which the caller frees.
void asuka_native_done(uint64_t call, const uint8_t *p, size_t len);
size_t asuka_native_invoke(uint64_t id, const uint8_t *p, size_t len, uint8_t **out);

#endif

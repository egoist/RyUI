// The callbacks a platform host gives the core (see platform/ios/Sources/CAsuka/include/asuka.h).
#ifndef ASUKA_HOST_H
#define ASUKA_HOST_H

#include <stdint.h>
#include <stddef.h>

typedef struct {
	void (*apply)(const uint8_t *buf, size_t len);
	void (*measure)(uint32_t id, float width, float height, float availWidth, float availHeight, float *outWidth, float *outHeight);
	void (*post)(void);
	size_t (*native_new)(const uint8_t *cls, size_t clsLen, uint32_t iface, uint8_t *out, size_t cap, uint8_t **big);
	size_t (*native_call)(uint64_t handle, uint32_t method, const uint8_t *in, size_t len, uint8_t *out, size_t cap, uint8_t **big);
	void (*native_start)(uint64_t handle, uint32_t method, const uint8_t *in, size_t len, uint64_t call);
	void (*native_cancel)(uint64_t call);
	void (*native_release)(uint64_t handle);
	void (*native_free)(void *p);
} asuka_host_callbacks;

#endif

// The callbacks a platform host gives the core (see platform/ios/Sources/CAsuka/include/asuka.h).
#ifndef ASUKA_HOST_H
#define ASUKA_HOST_H

#include <stdint.h>
#include <stddef.h>

typedef struct {
	void (*apply)(const uint8_t *buf, size_t len);
	void (*measure)(uint32_t id, float width, float height, float availWidth, float availHeight, float *outWidth, float *outHeight);
	void (*post)(void);
} asuka_host_callbacks;

#endif

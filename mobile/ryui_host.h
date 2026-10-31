// The callbacks a platform host gives the core (see hosts/ios/Sources/CRyUI/include/ryui.h).
#ifndef RYUI_HOST_H
#define RYUI_HOST_H

#include <stdint.h>
#include <stddef.h>

typedef struct {
	void (*apply)(const uint8_t *buf, size_t len);
	void (*measure)(uint32_t id, float width, float height, float availWidth, float availHeight, float *outWidth, float *outHeight);
	void (*post)(void);
} ryui_host_callbacks;

#endif

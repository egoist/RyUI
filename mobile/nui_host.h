// The callbacks a platform host gives the core (see hosts/ios/Sources/CNui/include/nui.h).
#ifndef NUI_HOST_H
#define NUI_HOST_H

#include <stdint.h>
#include <stddef.h>

typedef struct {
	void (*apply)(const uint8_t *buf, size_t len);
	void (*measure)(uint32_t id, float width, float height, float availWidth, float availHeight, float *outWidth, float *outHeight);
	void (*post)(void);
} nui_host_callbacks;

#endif

// JNI entry points for the Android host (hosts/android, dev.ryui.RyUINative).
// Kotlin calls in on the main thread; the core calls back into
// dev.ryui.RyUIRuntime: apply(ByteBuffer) once per batch, measure(...) and
// post() (from any thread).

#include <jni.h>
#include <string.h>
#include "_cgo_export.h"

static JavaVM *vm;
static jobject runtime;
static jmethodID mApply, mMeasure, mPost;

JNIEXPORT jint JNI_OnLoad(JavaVM *v, void *reserved) {
	vm = v;
	return JNI_VERSION_1_6;
}

// env returns the calling thread's JNIEnv, attaching it if needed.
static JNIEnv *env(int *attached) {
	JNIEnv *e = NULL;
	*attached = 0;
	if ((*vm)->GetEnv(vm, (void **)&e, JNI_VERSION_1_6) == JNI_EDETACHED) {
		(*vm)->AttachCurrentThread(vm, &e, NULL);
		*attached = 1;
	}
	return e;
}

static void check(JNIEnv *e) {
	if ((*e)->ExceptionCheck(e)) {
		(*e)->ExceptionDescribe(e);
		(*e)->FatalError(e, "ryui: the Android host threw");
	}
}

static void cb_apply(const uint8_t *buf, size_t len) {
	int attached;
	JNIEnv *e = env(&attached);
	jobject bb = (*e)->NewDirectByteBuffer(e, (void *)buf, (jlong)len);
	(*e)->CallVoidMethod(e, runtime, mApply, bb);
	(*e)->DeleteLocalRef(e, bb);
	check(e);
}

static void cb_measure(uint32_t id, float w, float h, float aw, float ah, float *ow, float *oh) {
	int attached;
	JNIEnv *e = env(&attached);
	jlong r = (*e)->CallLongMethod(e, runtime, mMeasure, (jint)id, w, h, aw, ah);
	check(e);
	uint32_t hi = (uint32_t)((uint64_t)r >> 32), lo = (uint32_t)r;
	memcpy(ow, &hi, 4);
	memcpy(oh, &lo, 4);
}

static void cb_post(void) {
	int attached;
	JNIEnv *e = env(&attached);
	(*e)->CallVoidMethod(e, runtime, mPost);
	check(e);
	if (attached) {
		(*vm)->DetachCurrentThread(vm);
	}
}

static ryui_host_callbacks callbacks = {cb_apply, cb_measure, cb_post};

JNIEXPORT void JNICALL Java_dev_ryui_RyUINative_start(JNIEnv *e, jclass cls, jobject rt) {
	runtime = (*e)->NewGlobalRef(e, rt);
	jclass k = (*e)->GetObjectClass(e, rt);
	mApply = (*e)->GetMethodID(e, k, "apply", "(Ljava/nio/ByteBuffer;)V");
	mMeasure = (*e)->GetMethodID(e, k, "measure", "(IFFFF)J");
	mPost = (*e)->GetMethodID(e, k, "post", "()V");
	ryui_start(&callbacks);
}

JNIEXPORT void JNICALL Java_dev_ryui_RyUINative_attach(JNIEnv *e, jclass cls) { ryui_attach(); }

JNIEXPORT void JNICALL Java_dev_ryui_RyUINative_event(JNIEnv *e, jclass cls, jbyteArray a) {
	jsize n = (*e)->GetArrayLength(e, a);
	jbyte *p = (*e)->GetByteArrayElements(e, a, NULL);
	ryui_event((uint8_t *)p, (size_t)n);
	(*e)->ReleaseByteArrayElements(e, a, p, JNI_ABORT);
}

JNIEXPORT jlong JNICALL Java_dev_ryui_RyUINative_listCount(JNIEnv *e, jclass cls, jint list) {
	return ryui_list_count((uint32_t)list);
}

JNIEXPORT void JNICALL Java_dev_ryui_RyUINative_listBind(JNIEnv *e, jclass cls, jint list, jlong index, jint cell) {
	ryui_list_bind((uint32_t)list, index, (uint32_t)cell);
}

JNIEXPORT void JNICALL Java_dev_ryui_RyUINative_listUnbind(JNIEnv *e, jclass cls, jint list, jint cell) {
	ryui_list_unbind((uint32_t)list, (uint32_t)cell);
}

JNIEXPORT void JNICALL Java_dev_ryui_RyUINative_runPosted(JNIEnv *e, jclass cls) { ryui_run_posted(); }

JNIEXPORT void JNICALL Java_dev_ryui_RyUINative_setenv(JNIEnv *e, jclass cls, jstring key, jstring value) {
	const char *k = (*e)->GetStringUTFChars(e, key, NULL);
	const char *v = (*e)->GetStringUTFChars(e, value, NULL);
	ryui_setenv((char *)k, (char *)v);
	(*e)->ReleaseStringUTFChars(e, key, k);
	(*e)->ReleaseStringUTFChars(e, value, v);
}

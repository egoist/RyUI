// JNI entry points for the Android host (platform/android, dev.asuka.AsukaNative).
// Kotlin calls in on the main thread; the core calls back into
// dev.asuka.AsukaRuntime: apply(ByteBuffer) once per batch, measure(...) and
// post() (from any thread).

#include <jni.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include "_cgo_export.h"

static JavaVM *vm;
static jobject runtime;
static jmethodID mApply, mMeasure, mPost;
// Typed native modules (dev.asuka.AsukaModules): its class, found on the
// main thread (FindClass on another can't see the app's classes).
static jclass modules;
static jmethodID mNew, mCall, mStart, mCancel, mRelease;
static pthread_key_t detachKey;

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
		(*e)->FatalError(e, "asuka: the Android host threw");
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

// Typed native modules. Go calls from its own threads, many: each is
// attached once, and detached as it ends.
static void detach(void *unused) { (*vm)->DetachCurrentThread(vm); }

static JNIEnv *modulesEnv(void) {
	JNIEnv *e = NULL;
	if ((*vm)->GetEnv(vm, (void **)&e, JNI_VERSION_1_6) == JNI_EDETACHED) {
		(*vm)->AttachCurrentThread(vm, &e, NULL);
		pthread_setspecific(detachKey, (void *)1);
	}
	return e;
}

// answerOf copies a Kotlin answer where Go reads it: into out when it
// fits, else into memory of its own (*big, which cb_native_free frees).
static size_t answerOf(JNIEnv *e, jbyteArray a, uint8_t *out, size_t cap, uint8_t **big) {
	if ((*e)->ExceptionCheck(e) || a == NULL) {
		(*e)->ExceptionDescribe(e);
		(*e)->ExceptionClear(e);
		static const char msg[] = "\x03\x13\x00\x00\x00asuka: Kotlin threw";
		size_t n = sizeof msg - 1;
		if (n <= cap) {
			memcpy(out, msg, n);
		} else {
			*big = malloc(n);
			memcpy(*big, msg, n);
		}
		return n;
	}
	size_t n = (size_t)(*e)->GetArrayLength(e, a);
	if (n <= cap) {
		(*e)->GetByteArrayRegion(e, a, 0, (jsize)n, (jbyte *)out);
	} else {
		*big = malloc(n);
		(*e)->GetByteArrayRegion(e, a, 0, (jsize)n, (jbyte *)*big);
	}
	(*e)->DeleteLocalRef(e, a);
	return n;
}

static jbyteArray bytes(JNIEnv *e, const uint8_t *p, size_t n) {
	jbyteArray a = (*e)->NewByteArray(e, (jsize)n);
	if (n > 0) (*e)->SetByteArrayRegion(e, a, 0, (jsize)n, (const jbyte *)p);
	return a;
}

static size_t cb_native_new(const uint8_t *cls, size_t clsLen, uint32_t iface, uint8_t *out, size_t cap, uint8_t **big) {
	JNIEnv *e = modulesEnv();
	jbyteArray c = bytes(e, cls, clsLen);
	jbyteArray a = (*e)->CallStaticObjectMethod(e, modules, mNew, c, (jint)iface);
	(*e)->DeleteLocalRef(e, c);
	return answerOf(e, a, out, cap, big);
}

static size_t cb_native_call(uint64_t h, uint32_t method, const uint8_t *in, size_t len, uint8_t *out, size_t cap, uint8_t **big) {
	JNIEnv *e = modulesEnv();
	jbyteArray i = bytes(e, in, len);
	jbyteArray a = (*e)->CallStaticObjectMethod(e, modules, mCall, (jlong)h, (jint)method, i);
	(*e)->DeleteLocalRef(e, i);
	return answerOf(e, a, out, cap, big);
}

static void cb_native_start(uint64_t h, uint32_t method, const uint8_t *in, size_t len, uint64_t call) {
	JNIEnv *e = modulesEnv();
	jbyteArray i = bytes(e, in, len);
	(*e)->CallStaticVoidMethod(e, modules, mStart, (jlong)h, (jint)method, i, (jlong)call);
	(*e)->DeleteLocalRef(e, i);
	check(e);
}

static void cb_native_cancel(uint64_t call) {
	JNIEnv *e = modulesEnv();
	(*e)->CallStaticVoidMethod(e, modules, mCancel, (jlong)call);
	check(e);
}

static void cb_native_release(uint64_t h) {
	JNIEnv *e = modulesEnv();
	(*e)->CallStaticVoidMethod(e, modules, mRelease, (jlong)h);
	check(e);
}

static void cb_native_free(void *p) { free(p); }

static asuka_host_callbacks callbacks = {cb_apply, cb_measure, cb_post,
	cb_native_new, cb_native_call, cb_native_start, cb_native_cancel, cb_native_release, cb_native_free};

// Kotlin's way back to Go: a started call's answer; a Go callback's call.
JNIEXPORT void JNICALL Java_dev_asuka_AsukaModules_done(JNIEnv *e, jclass cls, jlong call, jbyteArray a) {
	jsize n = (*e)->GetArrayLength(e, a);
	jbyte *p = (*e)->GetByteArrayElements(e, a, NULL);
	asuka_native_done((uint64_t)call, (uint8_t *)p, (size_t)n);
	(*e)->ReleaseByteArrayElements(e, a, p, JNI_ABORT);
}

JNIEXPORT jbyteArray JNICALL Java_dev_asuka_AsukaModules_invoke(JNIEnv *e, jclass cls, jlong id, jbyteArray a) {
	jsize n = (*e)->GetArrayLength(e, a);
	jbyte *p = (*e)->GetByteArrayElements(e, a, NULL);
	uint8_t *out = NULL;
	size_t m = asuka_native_invoke((uint64_t)id, (uint8_t *)p, (size_t)n, &out);
	(*e)->ReleaseByteArrayElements(e, a, p, JNI_ABORT);
	if (out == NULL) return NULL;
	jbyteArray r = bytes(e, out, m);
	free(out);
	return r;
}

JNIEXPORT void JNICALL Java_dev_asuka_AsukaNative_start(JNIEnv *e, jclass cls, jobject rt) {
	runtime = (*e)->NewGlobalRef(e, rt);
	jclass k = (*e)->GetObjectClass(e, rt);
	mApply = (*e)->GetMethodID(e, k, "apply", "(Ljava/nio/ByteBuffer;)V");
	mMeasure = (*e)->GetMethodID(e, k, "measure", "(IFFFF)J");
	mPost = (*e)->GetMethodID(e, k, "post", "()V");
	pthread_key_create(&detachKey, detach);
	modules = (*e)->NewGlobalRef(e, (*e)->FindClass(e, "dev/asuka/AsukaModules"));
	mNew = (*e)->GetStaticMethodID(e, modules, "new", "([BI)[B");
	mCall = (*e)->GetStaticMethodID(e, modules, "call", "(JI[B)[B");
	mStart = (*e)->GetStaticMethodID(e, modules, "start", "(JI[BJ)V");
	mCancel = (*e)->GetStaticMethodID(e, modules, "cancel", "(J)V");
	mRelease = (*e)->GetStaticMethodID(e, modules, "release", "(J)V");
	asuka_start(&callbacks);
}

JNIEXPORT void JNICALL Java_dev_asuka_AsukaNative_attach(JNIEnv *e, jclass cls) { asuka_attach(); }

JNIEXPORT void JNICALL Java_dev_asuka_AsukaNative_event(JNIEnv *e, jclass cls, jbyteArray a) {
	jsize n = (*e)->GetArrayLength(e, a);
	jbyte *p = (*e)->GetByteArrayElements(e, a, NULL);
	asuka_event((uint8_t *)p, (size_t)n);
	(*e)->ReleaseByteArrayElements(e, a, p, JNI_ABORT);
}

JNIEXPORT jlong JNICALL Java_dev_asuka_AsukaNative_listCount(JNIEnv *e, jclass cls, jint list) {
	return asuka_list_count((uint32_t)list);
}

JNIEXPORT void JNICALL Java_dev_asuka_AsukaNative_listBind(JNIEnv *e, jclass cls, jint list, jlong index, jint cell) {
	asuka_list_bind((uint32_t)list, index, (uint32_t)cell);
}

JNIEXPORT void JNICALL Java_dev_asuka_AsukaNative_listUnbind(JNIEnv *e, jclass cls, jint list, jint cell) {
	asuka_list_unbind((uint32_t)list, (uint32_t)cell);
}

JNIEXPORT void JNICALL Java_dev_asuka_AsukaNative_runPosted(JNIEnv *e, jclass cls) { asuka_run_posted(); }

JNIEXPORT void JNICALL Java_dev_asuka_AsukaNative_setenv(JNIEnv *e, jclass cls, jstring key, jstring value) {
	const char *k = (*e)->GetStringUTFChars(e, key, NULL);
	const char *v = (*e)->GetStringUTFChars(e, value, NULL);
	asuka_setenv((char *)k, (char *)v);
	(*e)->ReleaseStringUTFChars(e, key, k);
	(*e)->ReleaseStringUTFChars(e, value, v);
}

JNIEXPORT jstring JNICALL Java_dev_asuka_AsukaNative_crashStart(JNIEnv *e, jclass cls, jstring info) {
	const char *i = (*e)->GetStringUTFChars(e, info, NULL);
	char *dir = asuka_crash_start((char *)i);
	(*e)->ReleaseStringUTFChars(e, info, i);
	jstring out = (*e)->NewStringUTF(e, dir);
	free(dir);
	return out;
}

JNIEXPORT void JNICALL Java_dev_asuka_AsukaNative_crashNotify(JNIEnv *e, jclass cls) { asuka_crash_notify(); }

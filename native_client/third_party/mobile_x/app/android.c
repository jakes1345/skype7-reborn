// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build android
// +build android

#include <android/log.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <string.h>
#include "_cgo_export.h"

#define LOG_INFO(...) __android_log_print(ANDROID_LOG_INFO, "Go", __VA_ARGS__)
#define LOG_FATAL(...) __android_log_print(ANDROID_LOG_FATAL, "Go", __VA_ARGS__)

static jclass current_class_X;

static jclass find_class(JNIEnv *env, const char *class_name) {
	jclass clazz = (*env)->FindClass(env, class_name);
	if (clazz == NULL) {
		(*env)->ExceptionClear(env);
		LOG_FATAL("cannot find %s", class_name);
		return NULL;
	}
	return clazz;
}

static jmethodID find_method(JNIEnv *env, jclass clazz, const char *name, const char *sig) {
	jmethodID m = (*env)->GetMethodID(env, clazz, name, sig);
	if (m == 0) {
		(*env)->ExceptionClear(env);
		LOG_FATAL("cannot find method %s %s", name, sig);
		return 0;
	}
	return m;
}

static jmethodID find_static_method(JNIEnv *env, jclass clazz, const char *name, const char *sig) {
	jmethodID m = (*env)->GetStaticMethodID(env, clazz, name, sig);
	if (m == 0) {
		(*env)->ExceptionClear(env);
		LOG_FATAL("cannot find method %s %s", name, sig);
		return 0;
	}
	return m;
}

static jmethodID key_rune_method_X;

jint JNI_OnLoad_X(JavaVM* vm, void* reserved) {
	JNIEnv* env;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		return -1;
	}

	return JNI_VERSION_1_6;
}

static int main_running = 0;

// Entry point from our subclassed NativeActivity.
//
// By here, the Go runtime has been initialized (as we are running in
// -buildmode=c-shared) but the first time it is called, Go's main.main
// hasn't been called yet.
//
// The Activity may be created and destroyed multiple times throughout
// the life of a single process. Each time, onCreate is called.
void ANativeActivity_onCreate_X(ANativeActivity *activity, void* savedState, size_t savedStateSize) {
	if (!main_running) {
		JNIEnv* env = activity->env;

		// Note that activity->clazz is mis-named.
		current_class_X = (*env)->GetObjectClass(env, activity->clazz);
		current_class_X = (*env)->NewGlobalRef(env, current_class_X);
		key_rune_method_X = find_static_method(env, current_class_X, "getRune", "(III)I");

		setCurrentContext_X(activity->vm, (*env)->NewGlobalRef(env, activity->clazz));

		// Set TMPDIR.
		jmethodID gettmpdir = find_method(env, current_class_X, "getTmpdir", "()Ljava/lang/String;");
		jstring jpath = (jstring)(*env)->CallObjectMethod(env, activity->clazz, gettmpdir, NULL);
		const char* tmpdir = (*env)->GetStringUTFChars(env, jpath, NULL);
		if (setenv("TMPDIR", tmpdir, 1) != 0) {
			LOG_INFO("setenv(\"TMPDIR\", \"%s\", 1) failed: %d", tmpdir, errno);
		}
		(*env)->ReleaseStringUTFChars(env, jpath, tmpdir);

		// Call the Go main.main.
		uintptr_t mainPC = (uintptr_t)dlsym(RTLD_DEFAULT, "main.main");
		if (!mainPC) {
			LOG_FATAL("missing main.main");
		}
		callMain_X(mainPC);
		main_running = 1;
	}

	// These functions match the methods on Activity, described at
	// http://developer.android.com/reference/android/app/Activity.html
	//
	// Note that onNativeWindowResized is not called on resize. Avoid it.
	// https://code.google.com/p/android/issues/detail?id=180645
	activity->callbacks->onStart = onStart_X;
	activity->callbacks->onResume = onResume_X;
	activity->callbacks->onSaveInstanceState = onSaveInstanceState_X;
	activity->callbacks->onPause = onPause_X;
	activity->callbacks->onStop = onStop_X;
	activity->callbacks->onDestroy = onDestroy_X;
	activity->callbacks->onWindowFocusChanged = onWindowFocusChanged_X;
	activity->callbacks->onNativeWindowCreated = onNativeWindowCreated_X;
	activity->callbacks->onNativeWindowRedrawNeeded = onNativeWindowRedrawNeeded_X;
	activity->callbacks->onNativeWindowDestroyed = onNativeWindowDestroyed_X;
	activity->callbacks->onInputQueueCreated = onInputQueueCreated_X;
	activity->callbacks->onInputQueueDestroyed = onInputQueueDestroyed_X;
	activity->callbacks->onConfigurationChanged = onConfigurationChanged_X;
// activity->callbacks->onLowMemory = onLowMemory_X;

	onCreate_X(activity);
}

// TODO(crawshaw): Test configuration on more devices.
static const EGLint RGB_888[] = {
	EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT,
	EGL_SURFACE_TYPE, EGL_WINDOW_BIT,
	EGL_BLUE_SIZE, 8,
	EGL_GREEN_SIZE, 8,
	EGL_RED_SIZE, 8,
	EGL_DEPTH_SIZE, 16,
	EGL_CONFIG_CAVEAT, EGL_NONE,
	EGL_NONE
};

EGLDisplay display_X = NULL;
EGLSurface surface_X = NULL;

static char* initEGLDisplay() {
	display_X = eglGetDisplay(EGL_DEFAULT_DISPLAY);
	if (!eglInitialize(display_X, 0, 0)) {
		return "EGL initialize failed";
	}
	return NULL;
}

char* createEGLSurface_X(ANativeWindow* window) {
	char* err;
	EGLint numConfigs, format;
	EGLConfig config;
	EGLContext context;

	if (display_X == 0) {
		if ((err = initEGLDisplay()) != NULL) {
			return err;
		}
	}

	if (!eglChooseConfig(display_X, RGB_888, &config, 1, &numConfigs)) {
		return "EGL choose RGB_888 config failed";
	}
	if (numConfigs <= 0) {
		return "EGL no config found";
	}

	eglGetConfigAttrib(display_X, config, EGL_NATIVE_VISUAL_ID, &format);
	if (ANativeWindow_setBuffersGeometry(window, 0, 0, format) != 0) {
		return "EGL set buffers geometry failed";
	}

	surface_X = eglCreateWindowSurface(display_X, config, window, NULL);
	if (surface_X == EGL_NO_SURFACE) {
		return "EGL create surface failed";
	}

	const EGLint contextAttribs[] = { EGL_CONTEXT_CLIENT_VERSION, 2, EGL_NONE };
	context = eglCreateContext(display_X, config, EGL_NO_CONTEXT, contextAttribs);

	if (eglMakeCurrent(display_X, surface_X, surface_X, context) == EGL_FALSE) {
		return "eglMakeCurrent failed";
	}
	return NULL;
}

char* destroyEGLSurface_X() {
	if (!eglDestroySurface(display_X, surface_X)) {
		return "EGL destroy surface failed";
	}
	return NULL;
}

int32_t getKeyRune_X(JNIEnv* env, AInputEvent* e) {
	return (int32_t)(*env)->CallStaticIntMethod(
		env,
		current_class_X,
		key_rune_method_X,
		AInputEvent_getDeviceId(e),
		AKeyEvent_getKeyCode(e),
		AKeyEvent_getMetaState(e)
	);
}

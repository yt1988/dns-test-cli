#include <jni.h>
#include <dlfcn.h>
#include <mutex>

namespace {
using RunFn = char* (*)(char*, char*);
using FreeFn = void (*)(char*);

std::mutex bridgeMutex;
void* goLibrary = nullptr;
RunFn runTest = nullptr;
FreeFn freeString = nullptr;

bool loadBridge() {
    std::lock_guard<std::mutex> lock(bridgeMutex);
    if (runTest && freeString) return true;

    goLibrary = dlopen("libdnstest.so", RTLD_NOW);
    if (!goLibrary) return false;

    runTest = reinterpret_cast<RunFn>(dlsym(goLibrary, "GoRunDNSTest"));
    freeString = reinterpret_cast<FreeFn>(dlsym(goLibrary, "GoFreeString"));
    return runTest && freeString;
}
}

extern "C"
JNIEXPORT jstring JNICALL
Java_cn_dnstest_android_NativeDns_run(JNIEnv* env, jobject /* thiz */, jstring args, jstring dns) {
    if (!loadBridge()) {
        return env->NewStringUTF("{\"error\":\"无法加载 DNS 测试引擎\"}");
    }

    const char* argsChars = env->GetStringUTFChars(args, nullptr);
    const char* dnsChars = env->GetStringUTFChars(dns, nullptr);
    if (!argsChars || !dnsChars) {
        if (argsChars) env->ReleaseStringUTFChars(args, argsChars);
        if (dnsChars) env->ReleaseStringUTFChars(dns, dnsChars);
        return env->NewStringUTF("{\"error\":\"读取测试参数失败\"}");
    }

    char* result = runTest(const_cast<char*>(argsChars), const_cast<char*>(dnsChars));
    jstring output = env->NewStringUTF(result ? result : "{\"error\":\"测试引擎没有返回结果\"}");
    if (result) freeString(result);

    env->ReleaseStringUTFChars(args, argsChars);
    env->ReleaseStringUTFChars(dns, dnsChars);
    return output;
}

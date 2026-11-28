//go:build android

package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"unsafe"
)

// GoRunDNSTest is called by the Android JNI layer. DNS addresses come from Android LinkProperties.
//
//export GoRunDNSTest
func GoRunDNSTest(cargs *C.char, cdns *C.char) *C.char {
	dns := C.GoString(cdns)
	if dns == "" {
		_ = os.Unsetenv("DNSTEST_SYSTEM_DNS")
	} else {
		_ = os.Setenv("DNSTEST_SYSTEM_DNS", dns)
	}

	args := strings.Fields(C.GoString(cargs))
	args = append(args, "--lang", "zh-CN", "--json", "--quiet", "--no-color")

	var stdout, stderr bytes.Buffer
	code := cli(args, &stdout, &stderr)
	if code != 0 {
		payload, _ := json.Marshal(map[string]string{"error": strings.TrimSpace(stderr.String())})
		return C.CString(string(payload))
	}
	return C.CString(stdout.String())
}

// GoFreeString releases a string returned by GoRunDNSTest.
//
//export GoFreeString
func GoFreeString(s *C.char) {
	C.free(unsafe.Pointer(s))
}

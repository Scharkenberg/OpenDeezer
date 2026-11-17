// MSVC-target cgo toolchains (Visual Studio's clang): start the Go runtime of
// the c-shared DLL.
//
// The Go linker (1.27) starts a c-shared DLL's runtime through a single
// GNU-style .ctors entry pointing at _rt0_amd64_windows_lib. Only the MinGW C
// runtime runs .ctors; the MSVC C runtime runs .CRT$XCU initializers instead,
// so without this the runtime never initializes and every exported DZ* call
// blocks forever. Register the same entry point there.
//
// In a normal executable (go test, go vet) the Go runtime starts itself and
// _rt0_amd64_windows_lib does not exist: /alternatename then resolves it to a
// no-op. Compiled only for MSVC targets (MinGW gcc does not define _MSC_VER),
// so the MinGW build is unchanged.
#if defined(_MSC_VER) && defined(_WIN64)
extern void _rt0_amd64_windows_lib(void);
void odz_go_rt0_absent(void) {}
#pragma comment(linker, "/alternatename:_rt0_amd64_windows_lib=odz_go_rt0_absent")
#pragma section(".CRT$XCU", read)
__declspec(allocate(".CRT$XCU")) void (*const odz_go_rt0_init)(void) = _rt0_amd64_windows_lib;
#endif

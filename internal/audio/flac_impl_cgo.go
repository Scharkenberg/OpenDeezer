//go:build cgo

package audio

// dr_flac (public domain or MIT-0), compiled here; flac_cgo.go holds the Go side. The
// callbacks reach the Go reader through the handle in pUserData.

/*
#cgo CFLAGS: -O2 -I${SRCDIR}/cdec
#define DR_FLAC_IMPLEMENTATION
#define DR_FLAC_NO_STDIO
#define DR_FLAC_NO_OGG
#define DR_FLAC_NO_WCHAR
#include <stdint.h>
#include "dr_flac.h"

extern size_t pdFlacRead(uintptr_t h, void* buf, size_t n);
extern int pdFlacSeek(uintptr_t h, int offset, int origin);
extern int pdFlacTell(uintptr_t h, int64_t* cursor);

static size_t pd_flac_read(void* u, void* buf, size_t n) { return pdFlacRead((uintptr_t)u, buf, n); }
static drflac_bool32 pd_flac_seek(void* u, int offset, drflac_seek_origin origin) { return pdFlacSeek((uintptr_t)u, offset, (int)origin) != 0; }
static drflac_bool32 pd_flac_tell(void* u, drflac_int64* cursor) {
	int64_t c = 0;
	int ok = pdFlacTell((uintptr_t)u, &c);
	*cursor = (drflac_int64)c;
	return ok != 0;
}

drflac* pd_flac_open(uintptr_t h) { return drflac_open(pd_flac_read, pd_flac_seek, pd_flac_tell, (void*)h, NULL); }
*/
import "C"

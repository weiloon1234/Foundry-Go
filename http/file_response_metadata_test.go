package http_test

import (
	"bytes"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestFileTransferBudgetIncludesNativeRangeFraming(t *testing.T) {
	limits := foundryhttp.FileResponseLimits{Bytes: 16, Ranges: 4, RangeBytes: 4096}
	info := foundryhttp.FileResponseInfo{Seekable: true, MediaTypes: []foundryhttp.MediaType{"text/plain"}}
	bound, err := info.TransferBytes(limits)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/sample", nil)
	request.Header.Set("Range", "bytes=0-0,2-2,4-4,6-6")
	response := httptest.NewRecorder()
	response.Header().Set("Content-Type", "text/plain")
	stdhttp.ServeContent(response, request, "sample", time.Time{}, bytes.NewReader([]byte("0123456789abcdef")))
	if response.Code != 206 || int64(response.Body.Len()) <= limits.Bytes || int64(response.Body.Len()) > bound {
		t.Fatal("native range body is not bounded by exported transfer budget")
	}
	info.Seekable = false
	if bound, err := info.TransferBytes(limits); err != nil || bound != limits.Bytes {
		t.Fatal("stream acquired range overhead", err)
	}
	if _, err := info.TransferBytes(foundryhttp.FileResponseLimits{}); err == nil {
		t.Fatal("invalid limits accepted")
	}
	info.MediaTypes = nil
	if _, err := info.TransferBytes(limits); err == nil {
		t.Fatal("invalid response accepted")
	}
}

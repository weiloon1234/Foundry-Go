package datatable

import (
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"testing"
	"time"
)

func TestManagerDownloadTimeoutKeepsNilAndRouteBounds(t *testing.T) {
	var missing *Manager
	if missing.DownloadTimeout() != 0 {
		t.Fatal("nil manager timeout changed")
	}
	ordinary := &Manager{config: DefaultConfig()}
	if ordinary.DownloadTimeout() != 20*time.Minute {
		t.Fatal("default timeout changed")
	}
	maximum := &Manager{config: Config{ExportTimeout: foundryhttp.MaxRouteTimeout}}
	if maximum.DownloadTimeout() != foundryhttp.MaxRouteTimeout {
		t.Fatal("timeout exceeded route maximum")
	}
}

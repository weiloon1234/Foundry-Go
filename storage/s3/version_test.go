package s3_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

func TestProviderWriteVersionsRespectHistoricalReadCapabilities(t *testing.T) {
	for _, provider := range []s3.Provider{s3.AWS, s3.R2} {
		for _, size := range []int64{3, s3.MinPartBytes + 17} {
			t.Run(fmt.Sprintf("provider-%d/size-%d", provider, size), func(t *testing.T) {
				peer := &uploadPeer{}
				want := storage.VersionID("fixture-version")
				if provider == s3.R2 {
					want = ""
				}
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == "HEAD" && (r.URL.Query().Get("versionId") != string(want) || r.Header.Get("If-Match") != `"published"`) {
						t.Error("publication metadata read lost its supported pin")
					}
					w.Header().Set("X-Amz-Version-Id", "fixture-version")
					peer.ServeHTTP(w, r)
				})
				var disk *storage.Disk
				if provider == s3.R2 {
					disk = r2WireDisk(t, handler)
				} else {
					disk, _ = wireDisk(t, handler, nil)
				}
				stored, err := disk.Put(t.Context(), objectKey(t), &zeroSource{remaining: size}, storage.PutOptions{})
				if err != nil || stored.Object.Size != size || stored.Object.Version != want {
					t.Fatal("write response misrepresented retained versions", err)
				}
			})
		}
	}
}

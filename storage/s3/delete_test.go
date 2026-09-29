package s3_test

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
)

func TestVersionedDeleteSelectsTheVersionWithoutACondition(t *testing.T) {
	var calls atomic.Int32
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "DELETE" || r.URL.Query().Get("versionId") != "fixture-version" || r.Header.Get("If-Match") != "" {
			t.Error("version delete carried a condition or lost its version", r.Method, r.URL.RawQuery)
		}
		w.Header().Set("X-Amz-Version-Id", "fixture-version")
		w.WriteHeader(204)
	}), nil)
	if !disk.Capabilities().Versions || disk.Capabilities().ConditionalVersionDelete {
		t.Fatal("AWS capability combination misdeclared")
	}
	if err := disk.Delete(t.Context(), objectKey(t), storage.DeleteOptions{Version: "fixture-version"}); err != nil {
		t.Fatal("versioned bucket deletion by version failed", err)
	}
	// AWS evaluates If-Match against the current object, never a selected version.
	err := disk.Delete(t.Context(), objectKey(t), storage.DeleteOptions{IfMatch: `"one"`, Version: "fixture-version"})
	var failure *storage.Error
	if !errors.As(err, &failure) || failure.Code() != storage.Unsupported || failure.Outcome() != storage.Unchanged {
		t.Fatal("unsupported selector combination was not rejected", err)
	}
	if calls.Load() != 1 {
		t.Fatal("rejected combination reached the provider")
	}
}

type deleteRequest struct {
	Objects []struct {
		Key string `xml:"Key"`
	} `xml:"Object"`
}

func TestDeleteManyUsesOneBatchRequestAndReportsEachKey(t *testing.T) {
	var calls atomic.Int32
	disk, _ := wireDisk(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || !r.URL.Query().Has("delete") {
			t.Error("batch delete did not use DeleteObjects", r.Method, r.URL.RawQuery)
			wireError(w, 400, "UnexpectedFixtureOperation")
			return
		}
		body, _ := io.ReadAll(r.Body)
		var request deleteRequest
		if err := xml.Unmarshal(body, &request); err != nil || len(request.Objects) != 4 || request.Objects[0].Key != "isolated/batch/a" {
			t.Error("batch request changed keys", err, string(body))
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<DeleteResult><Deleted><Key>isolated/batch/a</Key></Deleted>`+
			`<Error><Key>isolated/batch/b</Key><Code>AccessDenied</Code><Message>private</Message></Error>`+
			`<Error><Key>isolated/batch/c</Key><Code>NoSuchKey</Code><Message>private</Message></Error></DeleteResult>`)
	}), func(c *s3.Config) { c.Namespace, _ = storage.ParsePrefix("isolated/") })
	var keys []storage.ObjectKey
	for _, name := range []string{"batch/a", "batch/b", "batch/c", "batch/d"} {
		key, err := storage.ParseKey(name)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	result, err := disk.DeleteMany(t.Context(), keys)
	if err == nil || calls.Load() != 1 {
		t.Fatal("partial batch failure was reported as success", err)
	}
	if len(result.Deleted) != 2 || result.Deleted[0] != keys[0] || result.Deleted[1] != keys[2] || len(result.Failed) != 2 {
		t.Fatal("batch results lost per-key outcomes", result)
	}
	var denied, unreported *storage.Error
	if !errors.As(result.Failed[0].Err, &denied) || result.Failed[0].Key != keys[1] || denied.Code() != storage.Forbidden || denied.Outcome() != storage.Unchanged {
		t.Fatal("denied key misclassified", result.Failed[0].Err)
	}
	if !errors.As(result.Failed[1].Err, &unreported) || result.Failed[1].Key != keys[3] || unreported.Outcome() != storage.Unknown {
		t.Fatal("unreported key was not uncertain", result.Failed[1].Err)
	}
	if _, err := disk.DeleteMany(t.Context(), []storage.ObjectKey{keys[0], keys[0]}); !errors.Is(err, storage.Invalid) || calls.Load() != 1 {
		t.Fatal("duplicate keys reached the provider", err)
	}
}

package deployment

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/value"
)

func rehearsalDocument() string {
	return strings.Replace(bindingDocument(), `"schema":2`, `"schema":2,"rehearsal":{"run_id":"`+strings.Repeat("a", 32)+`","disks":[{"disk":"uploads","runtime_cap_bytes":8388608}]}`, 1)
}

type descriptorInfo struct {
	uid  uint32
	mode fs.FileMode
}

func (d descriptorInfo) Name() string       { return "descriptor.json" }
func (d descriptorInfo) Size() int64        { return 0 }
func (d descriptorInfo) Mode() fs.FileMode  { return d.mode }
func (d descriptorInfo) ModTime() time.Time { return time.Time{} }
func (d descriptorInfo) IsDir() bool        { return d.mode.IsDir() }
func (d descriptorInfo) Sys() any           { return &syscall.Stat_t{Uid: d.uid} }

func TestRehearsalDescriptorOwnership(t *testing.T) {
	for _, info := range []descriptorInfo{{0, 0644}, {501, 0644}, {0, 0664}, {0, 0666}} {
		want := info.uid == 0 && info.mode == 0644
		if rootOwnedDescriptor(info) != want {
			t.Fatal("incorrect ownership decision", info)
		}
		_, err := readFeatureDescriptorOwned(writeBindingDocument(t, rehearsalDocument()), func(os.FileInfo) bool { return rootOwnedDescriptor(info) })
		if (err == nil) != want {
			t.Fatal("descriptor ownership bypass", err)
		}
	}
	if os.Geteuid() != 0 {
		t.Setenv(RehearsalRunEnvironment, strings.Repeat("a", 32))
		if _, err := ReadBindings(writeBindingDocument(t, rehearsalDocument())); !errors.Is(err, fault.Invalid) {
			t.Fatal("operator-owned descriptor accepted", err)
		}
	}
}

func TestRehearsalDescriptorGateAndAssembly(t *testing.T) {
	for _, tc := range []struct {
		name, document, run string
		granted             bool
	}{
		{"no block", bindingDocument(), strings.Repeat("a", 32), false},
		{"no identity", rehearsalDocument(), "", false},
		{"mismatch", rehearsalDocument(), strings.Repeat("b", 32), false},
		{"full rehearsal", rehearsalDocument(), strings.Repeat("a", 32), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Model a deploy-owned file without requiring tests to run as root.
			// The real filesystem ownership predicate is tested separately above.
			t.Setenv(RehearsalRunEnvironment, tc.run)
			bindings, err := readBindingsOwned(writeBindingDocument(t, tc.document), func(os.FileInfo) bool { return true })
			if err != nil {
				t.Fatal(err)
			}
			grant := bindings.Rehearsal()
			if _, ok := grant.Cap("uploads"); ok != tc.granted {
				t.Fatal("incorrect grant")
			}
			if _, ok := grant.Cap("other"); ok {
				t.Fatal("unlisted disk granted")
			}
			snapshot := grant.Disks()
			if snapshot != nil {
				snapshot["uploads"] = 1
			}
			if cap, ok := grant.Cap("uploads"); ok && cap != 8<<20 {
				t.Fatal("grant was mutable")
			}
			s := infrastructure.DefaultSettings()
			disk := infrastructure.DefaultDiskSettings()
			disk.Driver = infrastructure.CompatibleDisk
			disk.Cloud.Bucket, disk.Cloud.Region, disk.Cloud.Endpoint = "test-bucket", "us-east-1", "https://unused.example.test"
			disk.Cloud.Credentials = "rehearsal"
			disk.Cloud.Compatible.ConditionalCreate = true
			disk.Cloud.Compatible.SingleRequestConditions = true
			s.Storage.Default = "uploads"
			s.Storage.Disks = infrastructure.Disks{"uploads": disk, "other": disk}
			s.Credentials = infrastructure.CredentialSources{"rehearsal": {Mode: credentials.Static, AccessKey: secret.New("test"), SecretKey: secret.New("test")}}
			plan, err := infrastructure.ConfigureStorage(s, infrastructure.WithRehearsal(grant))
			if err != nil {
				t.Fatal(err)
			}
			app, err := foundation.NewBuilder().Register(plan.Providers()...).Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer app.Shutdown(context.Background())
			if err := app.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			services, err := foundation.Resolve(app.Services(), infrastructure.ServicesKey)
			if err != nil {
				t.Fatal(err)
			}
			runtimeDisk, err := services.Storage.Disk("uploads")
			if err != nil {
				t.Fatal(err)
			}
			other, _ := services.Storage.Disk("other")
			if other.Capabilities().ConditionalWriteMode != "" {
				t.Fatal("unlisted disk modified")
			}
			owners, err := extensions.NewRegistry(extensiontest.Members.Registration())
			if err != nil {
				t.Fatal(err)
			}
			store, err := extensions.New(&database.DB{}, owners, extensions.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close(t.Context())
			collection := attachments.Define(extensiontest.Members, "documents", attachments.Policy{Disk: storage.DefineDisk("uploads"), Cardinality: attachments.Single, AnyMedia: true, MaxBytes: 500 << 20, MaxStoredBytes: 500 << 20})
			manager, err := attachments.New(attachments.Dependencies{Store: store, Disks: services.Storage}, attachments.DefaultConfig(), collection.Registration())
			if !tc.granted {
				if !errors.Is(err, storage.Unsupported) {
					t.Fatal("production policy bypass", err)
				}
				return
			}
			if err != nil {
				t.Fatal("500 MiB startup refused", err)
			}
			defer manager.Close(t.Context())
			key, _ := storage.ParseKey("private-key")
			_, err = runtimeDisk.Put(t.Context(), key, strings.NewReader("private-content"), storage.PutOptions{Condition: storage.IfAbsent(), Size: value.Set[int64](9 << 20)})
			if !errors.Is(err, storage.RehearsalLimitExceeded) {
				t.Fatal("cap lost", err)
			}
		})
	}
}

func TestRehearsalDescriptorRejectsMalformedGrants(t *testing.T) {
	for _, invalid := range []string{
		strings.Replace(rehearsalDocument(), `"runtime_cap_bytes":8388608`, `"runtime_cap_bytes":0`, 1),
		strings.Replace(rehearsalDocument(), `"disks":[`, `"disks":[{"disk":"uploads","runtime_cap_bytes":8388608},`, 1),
		strings.Replace(rehearsalDocument(), `"disk":"uploads"`, `"disk":"invalid/path"`, 1),
		strings.Replace(rehearsalDocument(), strings.Repeat("a", 32), "invalid", 1),
		strings.Replace(rehearsalDocument(), `"disks":[{"disk":"uploads","runtime_cap_bytes":8388608}]`, `"disks":[]`, 1),
	} {
		if _, err := readFeatureDescriptorOwned(writeBindingDocument(t, invalid), func(os.FileInfo) bool { return true }); err == nil {
			t.Fatal("malformed grant accepted")
		}
	}
}

func TestRunEnvironmentWithoutDescriptorGrantsNothing(t *testing.T) {
	t.Setenv(RehearsalRunEnvironment, strings.Repeat("a", 32))
	t.Setenv(BindingsEnvironment, "")
	if err := os.Unsetenv(BindingsEnvironment); err != nil {
		t.Fatal(err)
	}
	bindings, present, err := LoadBindings()
	if err != nil || present || len(bindings.Rehearsal().Disks()) != 0 {
		t.Fatal("identity without descriptor granted rehearsal", err)
	}
	t.Setenv(BindingsEnvironment, writeBindingDocument(t, bindingDocument()))
	bindings, present, err = LoadBindings()
	if err != nil || !present || len(bindings.Rehearsal().Disks()) != 0 {
		t.Fatal("descriptor without block granted rehearsal", err)
	}
}

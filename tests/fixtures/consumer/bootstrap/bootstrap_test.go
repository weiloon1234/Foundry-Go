package bootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	stdhttp "net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIndependentBootstrapHTTPActorsAndUpload(t *testing.T) {
	settings := Defaults()
	settings.Schema = "bootstrap_" + strings.ToLower(rand.Text())
	settings.App.HTTP.Server.Address = "127.0.0.1:0"
	connection := infrastructure.DefaultConnectionSettings()
	connection.Primary = infrastructure.PostgreSQLSettingsFromConfig(pgtest.Config(t))
	settings.App.Services.Database.Connections = infrastructure.DatabaseConnections{"default": connection}
	root := t.TempDir()
	disk := infrastructure.DefaultDiskSettings()
	disk.Local.Root = root
	settings.App.Services.Storage.Disks = infrastructure.Disks{"default": disk}
	var err error
	settings.MemberID, err = model.NewID[Member]()
	if err != nil {
		t.Fatal(err)
	}
	settings.OperatorID, err = model.NewID[Operator]()
	if err != nil {
		t.Fatal(err)
	}
	settings.MemberToken = secret.New("fixture-member")
	settings.OperatorToken = secret.New("fixture-operator")
	app, err := Build(t.Context(), settings, application.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("Build acquired storage", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	db, err := app.Resources().Database()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(t.Context(), `CREATE SCHEMA "`+settings.Schema+`"`); err != nil {
		t.Fatal(err)
	}
	if err := withinSchema(t.Context(), db, settings.Schema, func(tx *database.Tx) error {
		for _, ddl := range []string{"CREATE TABLE bootstrap_members(id uuid PRIMARY KEY,name text NOT NULL)", "CREATE TABLE bootstrap_operators(id uuid PRIMARY KEY,department text NOT NULL)"} {
			if _, err := tx.Exec(t.Context(), ddl); err != nil {
				return err
			}
		}
		if _, err := QueryBootstrapMembers().Create(t.Context(), tx, MemberDraft{}.SetID(settings.MemberID).SetName("Ada")); err != nil {
			return err
		}
		_, err := QueryBootstrapOperators().Create(t.Context(), tx, OperatorDraft{}.SetID(settings.OperatorID).SetDepartment("Operations"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- app.Run(t.Context(), foundation.HTTP) }()
	address, err := app.HTTPReady(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + address
	client := stdhttp.Client{Timeout: 5 * time.Second}
	request := func(method, path, actor string, body io.Reader, content string, origin bool) *stdhttp.Response {
		t.Helper()
		r, err := stdhttp.NewRequestWithContext(t.Context(), method, base+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if actor == "member" {
			r.AddCookie(&stdhttp.Cookie{Name: "bootstrap_member", Value: settings.MemberToken.Reveal()})
		}
		if actor == "operator" {
			r.Header.Set("Authorization", "Bearer "+settings.OperatorToken.Reveal())
		}
		if content != "" {
			r.Header.Set("Content-Type", content)
		}
		if origin {
			r.Header.Set("Origin", base)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	check := func(r *stdhttp.Response, status int) {
		t.Helper()
		defer r.Body.Close()
		if r.StatusCode != status {
			data, _ := io.ReadAll(r.Body)
			t.Fatalf("status %d want %d: %s", r.StatusCode, status, data)
		}
		_, _ = io.Copy(io.Discard, r.Body)
	}
	profile := request("GET", "/api/member", "member", nil, "", false)
	var result Profile
	if err := json.NewDecoder(profile.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	_ = profile.Body.Close()
	if result.Name != "Welcome Ada" || result.Kind != "member" {
		t.Fatal(result)
	}
	check(request("GET", "/api/member", "operator", nil, "", false), 401)
	check(request("POST", "/api/operator", "operator", nil, "", false), 200)
	check(request("POST", "/api/avatar", "member", nil, "", false), 403)
	var upload bytes.Buffer
	form := multipart.NewWriter(&upload)
	part, err := form.CreateFormFile("image", "avatar.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(part, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	response := request("POST", "/api/avatar", "member", &upload, form.FormDataContentType(), true)
	if response.StatusCode != 201 {
		data, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		t.Fatalf("upload: %d %s", response.StatusCode, data)
	}
	var saved UploadResult
	if err := json.NewDecoder(response.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if saved.Width != 32 || saved.Bytes <= 0 {
		t.Fatal(saved)
	}
	selected, err := app.Resources().Disk()
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := selected.Open(t.Context(), saved.Key, storage.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(reader)
	_ = reader.Close()
	if err != nil || decoded.Bounds().Dx() != 32 {
		t.Fatal("stored image incorrect", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("HTTP did not stop")
	}
}
func TestGeneratedApplicationConfigurationIsTyped(t *testing.T) {
	schema, err := SettingsConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	keys := SettingsConfigKeys()
	settings, _, err := schema.Load(Defaults(), config.Inputs[Settings]{Overrides: []config.Override[Settings]{keys.App.HTTP.Server.RequestTimeout.Set(3 * time.Second), keys.Greeting.Set("Hello")}})
	if err != nil || settings.App.HTTP.Server.RequestTimeout != 3*time.Second || settings.Greeting != "Hello" {
		t.Fatal("typed application configuration failed", err)
	}
}

package s3_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cloudcredentials "github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/s3"
	"github.com/weiloon1234/Foundry-Go/value"
)

type postPolicy struct {
	Expiration string `json:"expiration"`
	Conditions []any  `json:"conditions"`
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

// formPeer emulates S3 POST-object enforcement: the SigV4 policy signature,
// every exact-match condition and the content-length-range.
type formPeer struct {
	t        *testing.T
	accepted [][]byte
}

func (p *formPeer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reject := func(code string) {
		w.WriteHeader(403)
		fmt.Fprintf(w, "<Error><Code>%s</Code></Error>", code)
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		reject("MalformedPOSTRequest")
		return
	}
	value := func(name string) string {
		for field, values := range r.MultipartForm.Value {
			if strings.EqualFold(field, name) && len(values) == 1 {
				return values[0]
			}
		}
		return ""
	}
	encoded := value("policy")
	credential := strings.Split(value("x-amz-credential"), "/")
	if len(credential) != 5 {
		reject("InvalidPolicyDocument")
		return
	}
	key := hmacSHA256([]byte("AWS4fixture-secret"), credential[1])
	for _, part := range credential[2:] {
		key = hmacSHA256(key, part)
	}
	if hex.EncodeToString(hmacSHA256(key, encoded)) != value("x-amz-signature") {
		reject("SignatureDoesNotMatch")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	var policy postPolicy
	if err != nil || json.Unmarshal(raw, &policy) != nil {
		reject("InvalidPolicyDocument")
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) != 1 {
		reject("InvalidArgument")
		return
	}
	file, _ := files[0].Open()
	data, _ := io.ReadAll(file)
	for _, condition := range policy.Conditions {
		switch typed := condition.(type) {
		case map[string]any:
			for name, expected := range typed {
				if name != "bucket" && value(name) != expected {
					reject("AccessDenied")
					return
				}
			}
		case []any:
			if typed[0] == "content-length-range" && (float64(len(data)) < typed[1].(float64) || float64(len(data)) > typed[2].(float64)) {
				reject("EntityTooLarge")
				return
			}
		}
	}
	p.accepted = append(p.accepted, data)
	w.WriteHeader(204)
}

func submitForm(t *testing.T, client *http.Client, form storage.UploadForm, fields map[string]string, content []byte) int {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range form.Fields() {
		text := field.Value
		if override, ok := fields[field.Name]; ok {
			text = override
		}
		if err := writer.WriteField(field.Name, text); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile(form.FileField(), "upload.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(content)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	response, err := client.Post(form.URL(), writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	return response.StatusCode
}

func TestPresignedFormUploadPinsKeyTypeSizeAndChecksum(t *testing.T) {
	peer := &formPeer{t: t}
	server := httptest.NewTLSServer(peer)
	t.Cleanup(server.Close)
	config := s3.DefaultConfig("fixture-bucket", "us-east-1")
	config.Endpoint, config.PathStyle = server.URL, true
	config.HTTPClient = &noNetwork{}
	config.Namespace, _ = storage.ParsePrefix("isolated/")
	config = config.WithCredentials(cloudcredentials.ProviderFunc(func(context.Context) (cloudcredentials.Value, error) {
		return cloudcredentials.Value{AccessKey: secret.New("fixture-key"), SecretKey: secret.New("fixture-secret")}, nil
	}))
	backend, err := s3.Open(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := storage.NewDisk("uploads", backend, storage.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = disk.Close(context.Background())
		_ = backend.Close()
	})
	content := []byte("\x89PNG form upload")
	digest := storage.SHA256(sha256.Sum256(content))
	form, err := disk.TemporaryUploadForm(t.Context(), objectKey(t), storage.UploadFormOptions{ExpiresIn: 10 * time.Minute, ContentType: "image/png", MinSize: 1, MaxSize: 64, Checksum: value.Set(digest)})
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{}
	signature := ""
	for _, field := range form.Fields() {
		fields[field.Name] = field.Value
		if strings.EqualFold(field.Name, "x-amz-signature") {
			signature = field.Value
		}
	}
	if form.URL() != server.URL+"/fixture-bucket" || form.FileField() != "file" || fields["key"] != "isolated/object" || fields["Content-Type"] != "image/png" || fields["x-amz-meta-foundry-sha256"] != digest.String() {
		t.Fatal("form fields changed", form.URL(), fields)
	}
	raw, err := base64.StdEncoding.DecodeString(fields["policy"])
	var policy postPolicy
	if err != nil || json.Unmarshal(raw, &policy) != nil || !strings.Contains(string(raw), `["content-length-range",1,64]`) || !strings.Contains(string(raw), `{"key":"isolated/object"}`) || strings.Contains(string(raw), "starts-with") {
		t.Fatal("policy does not pin the key and size range", string(raw))
	}
	if expiration, err := time.Parse(time.RFC3339, policy.Expiration); err != nil || expiration.After(time.Now().Add(11*time.Minute)) || !form.ExpiresAt().Equal(expiration) && form.ExpiresAt().Sub(expiration).Abs() > time.Second {
		t.Fatal("policy expiry changed", policy.Expiration, err)
	}
	if signature == "" || strings.Contains(fmt.Sprintf("%v %+v %#v", form, form, form), signature) {
		t.Fatal("upload form was not redacted")
	}
	if _, err := json.Marshal(form); err == nil {
		t.Fatal("bearer upload form implicitly serialized")
	}
	// The fake provider enforces the signed policy as S3 does.
	client := server.Client()
	if status := submitForm(t, client, form, nil, content); status != 204 || len(peer.accepted) != 1 {
		t.Fatal("valid form upload rejected", status)
	}
	for _, change := range []struct {
		fields  map[string]string
		content []byte
	}{
		{map[string]string{"key": "isolated/other"}, content},
		{map[string]string{"Content-Type": "text/html"}, content},
		{nil, bytes.Repeat([]byte("x"), 65)},
		{nil, nil},
		{map[string]string{"policy": base64.StdEncoding.EncodeToString([]byte(`{"conditions":[]}`))}, content},
	} {
		if status := submitForm(t, client, form, change.fields, change.content); status != 403 {
			t.Fatal("form upload escaped its policy", change.fields, len(change.content), status)
		}
	}
	for _, options := range []storage.UploadFormOptions{
		{ExpiresIn: time.Minute, ContentType: "image/png", MinSize: 10, MaxSize: 5},
		{ExpiresIn: time.Minute, ContentType: "image/png", MaxSize: 0},
		{ExpiresIn: time.Minute, MaxSize: 10},
		{ExpiresIn: time.Minute, ContentType: "image/png", MaxSize: 2 << 30},
	} {
		if _, err := disk.TemporaryUploadForm(t.Context(), objectKey(t), options); err == nil {
			t.Fatal("unbounded form upload accepted", options.MinSize, options.MaxSize)
		}
	}
	r2 := s3.R2WithCredentials("fixture-bucket", "https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com", cloudcredentials.ProviderFunc(func(context.Context) (cloudcredentials.Value, error) {
		return cloudcredentials.Value{AccessKey: secret.New("fixture-key"), SecretKey: secret.New("fixture-secret")}, nil
	}))
	r2.HTTPClient = &noNetwork{}
	r2Backend, err := s3.Open(t.Context(), r2)
	if err != nil {
		t.Fatal(err)
	}
	defer r2Backend.Close()
	if _, err := r2Backend.TemporaryUploadForm(t.Context(), objectKey(t), storage.UploadFormOptions{ExpiresIn: time.Minute, ContentType: "image/png", MaxSize: 10}); !errors.Is(err, storage.Unsupported) {
		t.Fatal("R2 form uploads were not rejected", err)
	}
}

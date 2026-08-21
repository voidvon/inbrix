package web

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestNewerVersion(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"1.17.0", "1.16.0", true},
		{"v2.0.0", "1.99.99", true},
		{"1.16.0", "1.16.0", false},
		{"1.15.9", "1.16.0", false},
		{"1.17", "1.16.0", false},
		{"1.17.0", "dev", false},
	}
	for _, test := range tests {
		if got := newerVersion(test.candidate, test.current); got != test.want {
			t.Errorf("newerVersion(%q, %q) = %t, want %t", test.candidate, test.current, got, test.want)
		}
	}
}

func TestManifestDigestRequiresExactAsset(t *testing.T) {
	digest := sha256.Sum256([]byte("archive"))
	manifest := fmt.Sprintf("%x  other.zip\n%x  inbrix_1.17.0_%s_%s.zip.sig\n%x  inbrix_1.17.0_%s_%s.zip\n", digest, digest, runtime.GOOS, runtime.GOARCH, digest, runtime.GOOS, runtime.GOARCH)
	name := releaseAssetName("1.17.0")
	got, err := manifestDigest([]byte(manifest), name)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(digest[:]) {
		t.Fatal("manifest digest did not match")
	}
	if _, err := manifestDigest([]byte(manifest), "missing.zip"); err == nil {
		t.Fatal("missing asset was accepted")
	}
}

func TestLatestReleaseFallsBackWhenGitHubAPIIsRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/latest":
			http.Error(w, "rate limited", http.StatusForbidden)
		case "/releases/latest":
			http.Redirect(w, r, "/releases/tag/v1.18.0", http.StatusFound)
		case "/releases/tag/v1.18.0":
			_, _ = w.Write([]byte("release"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	handler := NewUpdateHandler("1.16.0", nil)
	handler.httpClient = server.Client()
	handler.latestURL = server.URL + "/api/latest"
	handler.fallbackURL = server.URL + "/releases/latest"
	release, err := handler.latestRelease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if release.TagName != "v1.18.0" || len(release.Assets) != 2 {
		t.Fatalf("unexpected fallback release: %+v", release)
	}
}

func TestUpdateHandlerChecksAndInstallsVerifiedRelease(t *testing.T) {
	archive := updateTestArchive(t, []byte("new binary"))
	digest := sha256.Sum256(archive)
	assetName := releaseAssetName("1.17.0")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v1.17.0",
				HTMLURL: "https://github.com/voidvon/inbrix/releases/tag/v1.17.0",
				Assets: []githubAsset{
					{Name: assetName, BrowserDownloadURL: "http://" + r.Host + "/asset"},
					{Name: "SHA256SUMS", BrowserDownloadURL: "http://" + r.Host + "/sums"},
				},
			})
		case "/asset":
			_, _ = w.Write(archive)
		case "/sums":
			_, _ = fmt.Fprintf(w, "%x  %s\n", digest, assetName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	executable := filepath.Join(dir, "inbrix")
	if err := os.WriteFile(executable, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	handler := NewUpdateHandler("1.16.0", nil)
	handler.latestURL = server.URL + "/latest"
	handler.httpClient = server.Client()
	handler.executable = func() (string, error) { return executable, nil }
	app := fiber.New()
	app.Post("/check", handler.HandleCheck)
	app.Post("/install", handler.HandleInstall)

	response, err := app.Test(httptest.NewRequest(http.MethodPost, "/check", nil))
	if err != nil {
		t.Fatal(err)
	}
	var status updateStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !status.UpdateAvailable || status.LatestVersion != "1.17.0" {
		t.Fatalf("unexpected check response: status=%d body=%+v", response.StatusCode, status)
	}

	response, err = app.Test(httptest.NewRequest(http.MethodPost, "/install", nil), 10000)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("install status = %d", response.StatusCode)
	}
	installed, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(executable + ".previous")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(installed, []byte("new binary")) || !bytes.Equal(previous, []byte("old binary")) {
		t.Fatalf("unexpected installed=%q previous=%q", installed, previous)
	}
}

func updateTestArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	file, err := writer.Create("inbrix/inbrix")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

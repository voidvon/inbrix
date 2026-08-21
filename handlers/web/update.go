package web

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	updateRepository    = "voidvon/inbrix"
	updateRepositoryURL = "https://github.com/" + updateRepository
	maxReleaseFileSize  = 256 << 20
)

type UpdateHandler struct {
	version     string
	httpClient  *http.Client
	latestURL   string
	fallbackURL string
	executable  func() (string, error)
	restart     func() error
}

type updateStatus struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	RepositoryURL   string `json:"repositoryUrl"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	CanAutoUpdate   bool   `json:"canAutoUpdate"`
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	HTMLURL string        `json:"html_url"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func NewUpdateHandler(version string, restart func() error) *UpdateHandler {
	return &UpdateHandler{
		version:     strings.TrimSpace(version),
		httpClient:  &http.Client{Timeout: 45 * time.Second},
		latestURL:   "https://api.github.com/repos/" + updateRepository + "/releases/latest",
		fallbackURL: updateRepositoryURL + "/releases/latest",
		executable:  os.Executable,
		restart:     restart,
	}
}

func (h *UpdateHandler) HandleInfo(c *fiber.Ctx) error {
	return c.JSON(h.status(githubRelease{}))
}

func (h *UpdateHandler) HandleCheck(c *fiber.Ctx) error {
	release, err := h.latestRelease(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "Could not check GitHub for updates: " + err.Error()})
	}
	return c.JSON(h.status(release))
}

func (h *UpdateHandler) HandleInstall(c *fiber.Ctx) error {
	release, err := h.latestRelease(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "Could not check GitHub for updates: " + err.Error()})
	}
	status := h.status(release)
	if !status.UpdateAvailable {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "No newer release is available"})
	}
	if !status.CanAutoUpdate {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Automatic updates are unavailable for this build or platform"})
	}
	assetName := releaseAssetName(status.LatestVersion)
	assetURL := releaseAssetURL(release, assetName)
	manifestURL := releaseAssetURL(release, "SHA256SUMS")
	if assetURL == "" || manifestURL == "" {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "The GitHub release does not contain the required update files"})
	}
	manifest, err := h.download(c.UserContext(), manifestURL, 1<<20)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "Could not download the release checksum: " + err.Error()})
	}
	wantDigest, err := manifestDigest(manifest, assetName)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	archive, err := h.download(c.UserContext(), assetURL, maxReleaseFileSize)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "Could not download the update: " + err.Error()})
	}
	digest := sha256.Sum256(archive)
	if !bytes.Equal(digest[:], wantDigest) {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "The downloaded update failed checksum verification"})
	}
	binary, err := binaryFromArchive(archive)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "The update archive is invalid: " + err.Error()})
	}
	executable, err := h.executable()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not locate the running executable"})
	}
	if err := replaceExecutable(executable, binary); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Could not install the update: " + err.Error()})
	}
	if h.restart != nil {
		if err := h.restart(); err != nil {
			_ = restoreExecutable(executable)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The update was downloaded but restart could not be scheduled: " + err.Error()})
		}
	}
	return c.JSON(fiber.Map{"ok": true, "version": status.LatestVersion, "restarting": h.restart != nil})
}

func (h *UpdateHandler) status(release githubRelease) updateStatus {
	latest := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	available := latest != "" && newerVersion(latest, h.version)
	return updateStatus{
		CurrentVersion:  h.version,
		LatestVersion:   latest,
		UpdateAvailable: available,
		RepositoryURL:   updateRepositoryURL,
		ReleaseURL:      release.HTMLURL,
		CanAutoUpdate:   h.version != "" && h.version != "dev" && (runtime.GOOS == "linux" || runtime.GOOS == "darwin") && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"),
	}
}

func (h *UpdateHandler) latestRelease(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.latestURL, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "inbrix-update/"+h.version)
	response, err := h.httpClient.Do(req)
	if err != nil {
		return h.latestReleaseFromRedirect(ctx)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return h.latestReleaseFromRedirect(ctx)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, errors.New("the latest release has no version tag")
	}
	return release, nil
}

func (h *UpdateHandler) latestReleaseFromRedirect(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.fallbackURL, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("User-Agent", "inbrix-update/"+h.version)
	response, err := h.httpClient.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub returned HTTP %d", response.StatusCode)
	}
	const marker = "/releases/tag/"
	index := strings.LastIndex(response.Request.URL.Path, marker)
	if index < 0 {
		return githubRelease{}, errors.New("GitHub did not identify a latest release")
	}
	tag := strings.TrimSpace(response.Request.URL.Path[index+len(marker):])
	if tag == "" || strings.Contains(tag, "/") {
		return githubRelease{}, errors.New("GitHub returned an invalid release tag")
	}
	version := strings.TrimPrefix(tag, "v")
	base := updateRepositoryURL + "/releases/download/" + tag + "/"
	release := githubRelease{TagName: tag, HTMLURL: updateRepositoryURL + "/releases/tag/" + tag}
	release.Assets = append(release.Assets,
		githubAsset{Name: releaseAssetName(version), BrowserDownloadURL: base + releaseAssetName(version)},
		githubAsset{Name: "SHA256SUMS", BrowserDownloadURL: base + "SHA256SUMS"},
	)
	return release, nil
}

func (h *UpdateHandler) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "inbrix-update/"+h.version)
	response, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, errors.New("download is larger than the allowed limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download is larger than the allowed limit")
	}
	return data, nil
}

func releaseAssetName(version string) string {
	return fmt.Sprintf("inbrix_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)
}

func releaseAssetURL(release githubRelease, name string) string {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func manifestDigest(manifest []byte, assetName string) ([]byte, error) {
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != assetName {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			return nil, errors.New("The release checksum is malformed")
		}
		return digest, nil
	}
	return nil, errors.New("The release checksum does not cover this platform's update")
}

func binaryFromArchive(data []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, file := range reader.File {
		if filepath.ToSlash(file.Name) != "inbrix/inbrix" {
			continue
		}
		if file.UncompressedSize64 > maxReleaseFileSize {
			return nil, errors.New("binary is larger than the allowed limit")
		}
		opened, err := file.Open()
		if err != nil {
			return nil, err
		}
		binary, readErr := io.ReadAll(io.LimitReader(opened, maxReleaseFileSize+1))
		closeErr := opened.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(binary) == 0 || len(binary) > maxReleaseFileSize {
			return nil, errors.New("binary is empty or too large")
		}
		return binary, nil
	}
	return nil, errors.New("archive does not contain inbrix/inbrix")
}

func replaceExecutable(path string, binary []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".inbrix-update-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err = temp.Write(binary); err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tempName, info.Mode().Perm()); err != nil {
		return err
	}
	backup := path + ".previous"
	_ = os.Remove(backup)
	if err := os.Link(path, backup); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(backup)
		return err
	}
	return nil
}

func restoreExecutable(path string) error {
	return os.Rename(path+".previous", path)
}

func newerVersion(candidate, current string) bool {
	a, okA := parseVersion(candidate)
	b, okB := parseVersion(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parseVersion(value string) ([3]int, bool) {
	var parsed [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return parsed, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsed, false
		}
		parsed[i] = n
	}
	return parsed, true
}

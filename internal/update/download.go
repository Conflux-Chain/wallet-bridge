package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type releaseAsset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type taggedRelease struct {
	release
	Assets []releaseAsset `json:"assets"`
}

func (r taggedRelease) asset(name string) (releaseAsset, error) {
	var found releaseAsset
	for _, asset := range r.Assets {
		if asset.Name != name {
			continue
		}
		if found.ID != 0 || asset.ID <= 0 || asset.State != "uploaded" || asset.Size <= 0 ||
			!strings.HasPrefix(asset.Digest, "sha256:") || !validSHA256(strings.TrimPrefix(asset.Digest, "sha256:")) {
			return releaseAsset{}, fmt.Errorf("invalid or duplicate release asset %s", name)
		}
		found = asset
	}
	if found.ID == 0 {
		return found, fmt.Errorf("missing release asset %s", name)
	}
	return found, nil
}

func validSHA256(value string) bool {
	if len(value) != 2*sha256.Size {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func assetRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > 0 && len(via) < 5 && via[0].Header.Get("Accept") == "application/octet-stream" &&
		req.URL.Scheme == "https" && req.URL.User == nil {
		switch req.URL.Host {
		case "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
			return nil
		}
	}
	return errors.New("unexpected GitHub redirect")
}

// DownloadProgram streams into a staging writer. Discard its contents on any error.
// A successful result is the verified SHA-256 of the requested program.
func DownloadProgram(ctx context.Context, tag, name string, dst io.Writer) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return downloadProgram(ctx, &http.Client{CheckRedirect: assetRedirect}, tag, name, dst)
}

func downloadProgram(ctx context.Context, client *http.Client, tag, name string, dst io.Writer) (string, error) {
	if !validReleaseTag(tag) {
		return "", errors.New("invalid release tag")
	}
	var r taggedRelease
	if _, err := githubJSON(ctx, client, releasesURL+"/tags/"+tag, &r); err != nil {
		return "", err
	}
	if r.Tag != tag || r.Draft {
		return "", errors.New("release is not the requested published tag")
	}
	manifest, err := r.asset("SHA256SUMS")
	if err != nil {
		return "", err
	}
	program, err := r.asset(name)
	if err != nil {
		return "", err
	}
	// The manifest contains one checksum per release target and is held in memory.
	if manifest.Size > 64<<10 {
		return "", errors.New("release checksum list is too large")
	}
	var list bytes.Buffer
	if err := manifest.copyVerified(ctx, client, &list); err != nil {
		return "", err
	}
	digest, err := manifestDigest(list.String(), name)
	if err != nil {
		return "", err
	}
	if program.Digest != "sha256:"+digest {
		return "", errors.New("program digest differs between GitHub and SHA256SUMS")
	}
	if err := program.copyVerified(ctx, client, dst); err != nil {
		return "", err
	}
	return digest, nil
}

func manifestDigest(list, name string) (string, error) {
	var found string
	for line := range strings.Lines(list) {
		digest, filename, ok := strings.Cut(strings.TrimRight(line, "\r\n"), "  ")
		if !ok || !validSHA256(digest) || filename == "" {
			return "", errors.New("invalid SHA256SUMS entry")
		}
		if filename == name {
			if found != "" {
				return "", fmt.Errorf("duplicate checksum for %s", name)
			}
			found = digest
		}
	}
	if found == "" {
		return "", fmt.Errorf("missing checksum for %s", name)
	}
	return found, nil
}

func (a releaseAsset) copyVerified(ctx context.Context, client *http.Client, dst io.Writer) error {
	endpoint := fmt.Sprintf("%s/assets/%d", releasesURL, a.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		// CDN request URLs can include temporary download credentials.
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			err = requestErr.Err
		}
		return fmt.Errorf("download %s: %w", a.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", a.Name, resp.StatusCode)
	}
	hash := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(dst, hash), resp.Body, a.Size); err != nil {
		return fmt.Errorf("read or stage %s: %w", a.Name, err)
	}
	var extra [1]byte
	if _, err := io.ReadFull(resp.Body, extra[:]); err != io.EOF {
		return fmt.Errorf("unexpected end or extra bytes in %s", a.Name)
	}
	if a.Digest != "sha256:"+hex.EncodeToString(hash.Sum(nil)) {
		return fmt.Errorf("SHA-256 mismatch for %s", a.Name)
	}
	return nil
}

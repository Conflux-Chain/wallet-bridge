// Package update checks the published Wallet Bridge releases.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const releasesURL = "https://api.github.com/repos/Conflux-Chain/wallet-bridge/releases"

type Result struct {
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	State   string `json:"state"`
}

type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func Check(ctx context.Context, current string, includePreview bool) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return check(ctx, client, releasesURL, current, includePreview)
}

func check(ctx context.Context, client *http.Client, url, current string, includePreview bool) (Result, error) {
	result := Result{Current: current}
	for page := 1; ; page++ {
		releases, more, err := readPage(ctx, client, fmt.Sprintf("%s?per_page=30&page=%d", url, page))
		if err != nil {
			return Result{}, err
		}
		for _, r := range releases {
			// x/mod also accepts v1 and v1.0, while our release tags use all three components.
			if r.Draft || !semver.IsValid(r.Tag) || semver.Canonical(r.Tag)+semver.Build(r.Tag) != r.Tag {
				continue
			}
			if !includePreview && (r.Prerelease || semver.Prerelease(r.Tag) != "") {
				continue
			}
			if semver.Compare(r.Tag, result.Latest) > 0 {
				result.Latest = r.Tag
			}
		}
		if !more {
			break
		}
	}
	// Development builds may use "dev" or embed a numeric version without a v prefix.
	local := "v" + strings.TrimPrefix(current, "v")
	switch {
	case result.Latest == "":
		result.State = "no_release"
	case !semver.IsValid(local):
		result.State = "current_unversioned"
	case semver.Compare(result.Latest, local) > 0:
		result.State = "update_available"
	default:
		result.State = "up_to_date"
	}
	return result, nil
}

func readPage(ctx context.Context, client *http.Client, url string) ([]release, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("GitHub release query returned HTTP %d", resp.StatusCode)
	}
	var entries []release
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&entries); err != nil || entries == nil {
		return nil, false, errors.New("GitHub release query returned an invalid list")
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return nil, false, errors.New("Unexpected data after GitHub release list")
	}
	return entries, strings.Contains(resp.Header.Get("Link"), `rel="next"`), nil
}

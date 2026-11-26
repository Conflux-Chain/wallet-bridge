//go:build windows || darwin

package update

import (
	"bytes"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/mod/semver"
)

const stagedVersion = "v1.0.0-rc.1"

func stageSource(t *testing.T, program []byte, requests *atomic.Int32) *http.Client {
	return localReleaseSource(t, stagedVersion, program, requests)
}

func localReleaseSource(t *testing.T, tag string, program []byte, requests *atomic.Int32) *http.Client {
	t.Helper()
	name := "wallet-bridge-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(program))
	list := []byte(digest + "  " + name + "\n")
	releaseJSON, err := json.Marshal(taggedRelease{release: release{Tag: tag, Prerelease: semver.Prerelease(tag) != ""}, Assets: []releaseAsset{
		{ID: 1, Name: "SHA256SUMS", Size: int64(len(list)), State: "uploaded", Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(list))},
		{ID: 2, Name: name, Size: int64(len(program)), State: "uploaded", Digest: "sha256:" + digest},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: downloadTransport(func(r *http.Request) (*http.Response, error) {
		var data []byte
		switch r.URL.String() {
		case releasesURL + "/tags/" + tag:
			data = releaseJSON
		case releasesURL + "/assets/1":
			data = list
		case releasesURL + "/assets/2":
			requests.Add(1)
			data = program
		default:
			return nil, fmt.Errorf("unexpected path: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})}
}

func TestStageConcurrentAndRepeat(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "private")
	var requests atomic.Int32
	client := stageSource(t, program, &requests)
	var group sync.WaitGroup
	var results [2]Candidate
	var errs [2]error
	for i := range results {
		group.Go(func() { results[i], errs[i] = stage(t.Context(), client, stagedVersion, dir) })
	}
	group.Wait()
	if errs[0] != nil || errs[1] != nil || results[0] != results[1] {
		t.Fatal(results, errs)
	}
	before := requests.Load()
	got, err := stage(t.Context(), client, stagedVersion, dir)
	if err != nil || got != results[0] || requests.Load() != before {
		t.Fatal("verified candidate not reused", got, err)
	}
	if got.SHA256 != fmt.Sprintf("%x", sha256.Sum256(program)) || filepath.Dir(got.Path) != filepath.Join(dir, "versions", stagedVersion) {
		t.Fatal(got)
	}
	if err := os.WriteFile(got.Path, []byte("conflict"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := stage(t.Context(), client, stagedVersion, dir); err == nil {
		t.Fatal("conflicting candidate accepted")
	}
	data, err := os.ReadFile(got.Path)
	if err != nil || string(data) != "conflict" || requests.Load() != before {
		t.Fatal("existing file replaced", err)
	}
	files, err := os.ReadDir(filepath.Join(dir, "versions"))
	if err != nil || len(files) != 1 || files[0].Name() != stagedVersion {
		t.Fatal("unexpected staging files", files, err)
	}
}

func TestStageWrongPlatform(t *testing.T) {
	// A thin Mach-O header for a CPU different from either native macOS target.
	var header bytes.Buffer
	if err := binary.Write(&header, binary.LittleEndian, []uint32{macho.Magic64, uint32(macho.CpuPpc64), 0, uint32(macho.TypeExec), 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "private")
	var requests atomic.Int32
	_, err := stage(t.Context(), stageSource(t, header.Bytes(), &requests), stagedVersion, dir)
	if err == nil || requests.Load() != 1 {
		t.Fatal("expected platform rejection", err)
	}
	files, err := os.ReadDir(filepath.Join(dir, "versions"))
	if err != nil || len(files) != 0 {
		t.Fatal("failed download left output", files, err)
	}
}

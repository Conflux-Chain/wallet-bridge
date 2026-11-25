package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type downloadTransport func(*http.Request) (*http.Response, error)

func (f downloadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadProgram(t *testing.T) {
	const tag, name = "v1.0.0-rc.1+build", "wallet-bridge-darwin-arm64"
	const binary = "sample program bytes"
	sum := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	for _, tc := range []struct {
		fault       string
		wantProgram bool
	}{
		{"", true}, {"draft", false}, {"wrong tag", false}, {"missing asset", false},
		{"duplicate asset", false}, {"missing digest", false}, {"bad manifest", false},
		{"conflicting digests", false}, {"duplicate checksum", false}, {"missing checksum", false},
		{"replaced program", true}, {"short program", true}, {"extra bytes", true}, {"HTTP error", true},
	} {
		t.Run("asset/"+tc.fault, func(t *testing.T) {
			list := sum(binary) + "  " + name + "\n"
			switch tc.fault {
			case "duplicate checksum":
				list += list
			case "missing checksum":
				list = sum(binary) + "  another-platform\n"
			case "conflicting digests":
				list = sum("other") + "  " + name + "\n"
			}
			r := taggedRelease{release: release{Tag: tag, Prerelease: true}, Assets: []releaseAsset{
				{ID: 1, Name: "SHA256SUMS", Size: int64(len(list)), State: "uploaded", Digest: "sha256:" + sum(list)},
				{ID: 2, Name: name, Size: int64(len(binary)), State: "uploaded", Digest: "sha256:" + sum(binary)},
			}}
			data := binary
			switch tc.fault {
			case "draft":
				r.Draft = true
			case "wrong tag":
				r.Tag = "v2.0.0"
			case "missing asset":
				r.Assets = r.Assets[:1]
			case "duplicate asset":
				r.Assets = append(r.Assets, r.Assets[1])
			case "missing digest":
				r.Assets[1].Digest = ""
			case "bad manifest":
				list = strings.Repeat("x", len(list))
			case "replaced program":
				data = strings.Repeat("x", len(data))
			case "short program":
				data = data[:len(data)-1]
			case "extra bytes":
				data += "x"
			}
			var gotProgram bool
			transport := downloadTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" || req.Header.Get("Authorization") != "" {
					t.Fatal("expected a public GET")
				}
				status := http.StatusOK
				var body string
				switch req.URL.String() {
				case releasesURL + "/tags/" + tag:
					b, err := json.Marshal(r)
					if err != nil {
						t.Fatal(err)
					}
					body = string(b)
				case releasesURL + "/assets/1":
					body = list
				case releasesURL + "/assets/2":
					body = data
					gotProgram = true
					if tc.fault == "HTTP error" {
						status = http.StatusNotFound
					}
				default:
					t.Fatalf("unexpected URL %s", req.URL)
				}
				if strings.Contains(req.URL.Path, "/assets/") && req.Header.Get("Accept") != "application/octet-stream" {
					t.Fatal("missing binary Accept header")
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
			})
			var staged bytes.Buffer
			digest, err := downloadProgram(t.Context(), &http.Client{Transport: transport, CheckRedirect: assetRedirect}, tag, name, &staged)
			if gotProgram != tc.wantProgram {
				t.Fatalf("program requested = %t, error = %v", gotProgram, err)
			}
			if tc.fault == "" {
				if err != nil || digest != sum(binary) || staged.String() != binary {
					t.Fatal(digest, err)
				}
			} else if err == nil || digest != "" {
				t.Fatal("failed verification returned success", digest)
			}
		})
	}
}

func TestDownloadRedirect(t *testing.T) {
	first, _ := http.NewRequestWithContext(context.Background(), "GET", releasesURL+"/assets/1", nil)
	for _, tc := range []struct {
		url, accept string
		allowed     bool
	}{
		{"https://release-assets.githubusercontent.com/a", "application/octet-stream", true},
		{"https://release-assets.githubusercontent.com/a", "application/vnd.github+json", false},
		{"http://release-assets.githubusercontent.com/a", "application/octet-stream", false},
		{"https://example.org/a", "application/octet-stream", false},
	} {
		first.Header.Set("Accept", tc.accept)
		next, _ := http.NewRequest("GET", tc.url, nil)
		if err := assetRedirect(next, []*http.Request{first}); (err == nil) != tc.allowed {
			t.Fatal(tc, err)
		}
	}
}

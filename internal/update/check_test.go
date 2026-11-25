package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleaseSelection(t *testing.T) {
	for _, tc := range []struct {
		name, current string
		preview       bool
		tags          []release
		latest, state string
	}{
		{"newer stable wins with previews", "v1.0.0", true, []release{{Tag: "v1.1.0-rc.1", Prerelease: true}, {Tag: "v1.2.0"}}, "v1.2.0", "update_available"},
		{"preview opt in", "1.9.0", true, []release{{Tag: "v1.10.0-rc.10", Prerelease: true}, {Tag: "v1.10.0-rc.2", Prerelease: true}, {Tag: "v1.9.0"}}, "v1.10.0-rc.10", "update_available"},
		{"default excludes previews", "v1.9.0", false, []release{{Tag: "v1.10.0-rc.1", Prerelease: true}, {Tag: "v1.9.0+other"}}, "v1.9.0+other", "up_to_date"},
		{"no stable release", "v1.0.0", false, []release{{Tag: "v2.0.0", Draft: true}, {Tag: "v1.5.0", Prerelease: true}, {Tag: "v2.0.0-rc.1"}, {Tag: "v1"}, {Tag: "invalid"}}, "", "no_release"},
		{"development build", "dev", false, []release{{Tag: "v2.0.0"}}, "v2.0.0", "current_unversioned"},
		{"ahead of available release", "v3.0.0", false, []release{{Tag: "v2.0.0"}}, "v2.0.0", "up_to_date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("Authorization") != "" {
					t.Error("expected a public GET")
				}
				switch r.URL.Query().Get("page") {
				case "1":
					w.Header().Set("Link", `<https://api.github.com/example?page=2>; rel="next"`)
					json.NewEncoder(w).Encode(tc.tags[:1])
				case "2":
					json.NewEncoder(w).Encode(tc.tags[1:])
				default:
					t.Error("unexpected page")
					http.Error(w, "unexpected page", 400)
				}
			}))
			defer server.Close()
			got, err := check(t.Context(), server.Client(), server.URL, tc.current, tc.preview)
			if err != nil || got != (Result{Current: tc.current, Latest: tc.latest, State: tc.state}) {
				t.Fatalf("result = %+v, err = %v", got, err)
			}
		})
	}
}

func TestReleaseQueryFailure(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{403, "rate limited"}, {200, "null"}, {200, "[] trailing"}, {200, "{"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<next>; rel="next"`)
				w.Write([]byte(`[{"tag_name":"v1.0.1"}]`))
				return
			}
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		}))
		got, err := check(t.Context(), server.Client(), server.URL, "v1.0.0", false)
		server.Close()
		if err == nil || got != (Result{}) {
			t.Fatalf("partial result on failure: %+v, %v", got, err)
		}
	}
}

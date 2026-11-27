package api

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenRequestInDefaultBrowser(t *testing.T) {
	const endpoint = Origin + "/api/v1/browser/open-request"
	const id = "0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, kind, id, origin string
		launchFails            bool
		status                 int
	}{
		{"connection", "connection", id, Origin, false, 204},
		{"transaction", "transaction", id, Origin, false, 204},
		{"missing origin", "connection", id, "", false, 403},
		{"unsupported kind", "session", id, Origin, false, 400},
		{"invalid ID", "transaction", "invalid", Origin, false, 400},
		{"launch failure", "connection", id, Origin, true, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testHandler(t, func() {}).(*handler)
			var opened []string
			h.config.OpenBrowser = func(url string) error {
				opened = append(opened, url)
				if tc.launchFails {
					return errors.New("unavailable")
				}
				return nil
			}
			r := httptest.NewRequest("POST", endpoint, strings.NewReader(`{"kind":"`+tc.kind+`","id":"`+tc.id+`"}`))
			r.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || len(w.Result().Cookies()) != 0 {
				t.Fatalf("status %d, cookies %v", w.Code, w.Result().Cookies())
			}
			if tc.status == 204 || tc.launchFails {
				if len(opened) != 1 || opened[0] != Origin+"/?"+tc.kind+"="+id {
					t.Fatalf("opened %v", opened)
				}
			} else if len(opened) != 0 {
				t.Fatalf("unexpected launch: %v", opened)
			}
		})
	}
}

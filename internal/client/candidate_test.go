//go:build windows || darwin

package client

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
)

func TestCandidateProbe(t *testing.T) {
	id := strings.Repeat("3", 32)
	for _, tc := range []struct {
		name  string
		alter func(*api.InfoResponse)
		want  bool
	}{
		{"prepared", func(*api.InfoResponse) {}, true},
		{"open", func(i *api.InfoResponse) { i.Ready = true }, false},
		{"other upgrade", func(i *api.InfoResponse) { i.VerifyingUpgrade = "" }, false},
		{"other version", func(i *api.InfoResponse) { i.Version = "dev" }, false},
		{"missing capability", func(i *api.InfoResponse) { i.Capabilities = i.Capabilities[1:] }, false},
		{"old API", func(i *api.InfoResponse) { i.APIVersion.Minor-- }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			if err := credentials.Ensure(c.dir); err != nil {
				t.Fatal(err)
			}
			useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/info" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected authenticated request or endpoint %s", r.URL.Path)
					return
				}
				info := signedInfo(t, c, r.URL.Query().Get("challenge"))
				info.Ready, info.VerifyingUpgrade = false, id
				info.APIVersion.Minor = 16
				info.Capabilities = api.ServiceCapabilities()
				tc.alter(&info)
				json.NewEncoder(w).Encode(sign(info))
			}))
			if ok, err := c.ProbeCandidate(t.Context(), id, "test"); ok != tc.want || (err == nil) != tc.want {
				t.Fatal(ok, err)
			}
		})
	}
}

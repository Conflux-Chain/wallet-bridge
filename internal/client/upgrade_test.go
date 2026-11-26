//go:build windows || darwin

package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestUpgradeStopRequiresWalletCheckAndShutdown(t *testing.T) {
	id := strings.Repeat("a", 32)
	busy := `{"code":"wallet_busy","activeWalletRequest":{"kind":"transaction","id":"` + id + `","phase":"calling"}}`
	for _, tc := range []struct {
		name, body, code string
		minor, status    int
		capable, locked  bool
	}{
		{"old API", "", "incompatible_service", 14, 0, true, false},
		{"missing capability", "", "incompatible_service", 15, 0, false, false},
		{"wallet occupied", busy, "wallet_busy", 15, 409, true, false},
		{"invalid request ID", strings.Replace(busy, id, "unknown", 1), "request_failed", 15, 409, true, false},
		{"another upgrader", `{"code":"upgrade_in_progress"}`, "upgrade_in_progress", 15, 503, true, false},
		{"invalid acknowledgement", `{}`, "request_failed", 15, 202, true, false},
		{"lock held", `{"state":"stopping"}`, "stop_timeout", 15, 202, true, true},
		{"idle and stopped", `{"state":"stopping"}`, "", 15, 202, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			if err := credentials.Ensure(c.dir); err != nil {
				t.Fatal(err)
			}
			if tc.locked {
				lock, err := platform.AcquireServiceLock(c.dir)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			}
			var stops atomic.Int32
			useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/info":
					info := signedInfo(t, c, r.URL.Query().Get("challenge"))
					info.APIVersion.Minor = tc.minor
					if tc.capable {
						info.Capabilities = append(info.Capabilities, upgradeStopCapability)
					}
					json.NewEncoder(w).Encode(sign(info))
				case "/api/v1/status":
					json.NewEncoder(w).Encode(map[string]any{"state": "running", "startedAt": time.Now().UTC()})
				case "/api/v1/control/stop-for-upgrade":
					stops.Add(1)
					if tc.status == 0 {
						t.Error("sent upgrade stop to an unsupported service")
						return
					}
					w.WriteHeader(tc.status)
					w.Write([]byte(tc.body))
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
			}))
			result, err := c.StopForUpgrade(t.Context())
			if tc.code == "" {
				if err != nil || result.State != "stopped" {
					t.Fatal(result, err)
				}
			} else if codeOf(t, err) != tc.code || result.State != "" {
				t.Fatal(result, err)
			}
			if tc.code == "wallet_busy" {
				var blocked *UpgradeBusyError
				if !errors.As(err, &blocked) || blocked.Request.ID != id || !strings.Contains(err.Error(), "/?transaction="+id) {
					t.Fatal(err)
				}
			}
			if tc.status == 0 && stops.Load() != 0 || tc.status != 0 && stops.Load() != 1 {
				t.Fatal("unexpected number of stop requests", stops.Load())
			}
		})
	}
}

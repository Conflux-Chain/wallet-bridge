//go:build windows || darwin

package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func authorizationCore(t *testing.T, s *Store) *app.BrowserAuthorization {
	t.Helper()
	a, err := app.NewBrowserAuthorization(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func authorizeBrowser(t *testing.T, a *app.BrowserAuthorization) app.BrowserAuthorizationResult {
	t.Helper()
	ticket, err := a.Issue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.Authorize(context.Background(), ticket.Token)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBrowserAuthorizationSurvivesRestartAndRevocation(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	a := authorizationCore(t, s)
	first, second, third := authorizeBrowser(t, a), authorizeBrowser(t, a), authorizeBrowser(t, a)
	pending, err := a.Issue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTestStore(t, dir)
	a = authorizationCore(t, s)
	if _, err := a.Authorize(ctx, pending.Token); !errors.Is(err, app.ErrInvalidBrowserTicket) {
		t.Fatal("restart restored a one-time ticket", err)
	}
	got, err := a.Authenticate(ctx, first.Token)
	if err != nil || got.ID != first.Grant.ID || !got.ExpiresAt.Equal(first.Grant.ExpiresAt) {
		t.Fatal("restart lost or extended persistent authorization", err)
	}
	if err := a.Revoke(ctx, first.Grant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, first.Token); !errors.Is(err, app.ErrInvalidBrowserCredential) {
		t.Fatal("revoked browser authenticated", err)
	}
	if err := a.Logout(ctx, second.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, second.Token); !errors.Is(err, app.ErrInvalidBrowserCredential) {
		t.Fatal("logout did not revoke the current credential", err)
	}
	if _, err := a.Authenticate(ctx, third.Token); err != nil {
		t.Fatal("individual revocation affected another browser", err)
	}
	count, err := a.RevokeAll(ctx)
	if err != nil || count != 1 {
		t.Fatal("revoke-all did not revoke the remaining grant", count, err)
	}
	s.Close()
	for _, name := range []string{DatabaseName, DatabaseName + "-journal"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{pending.Token, first.Token, second.Token, third.Token} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatal("authorization secret reached persistent storage")
			}
		}
	}
	s = openTestStore(t, dir)
	a = authorizationCore(t, s)
	for _, secret := range []string{first.Token, second.Token, third.Token} {
		if _, err := a.Authenticate(ctx, secret); !errors.Is(err, app.ErrInvalidBrowserCredential) {
			t.Fatal("restart restored a revoked credential", err)
		}
	}
	grants, err := a.List(ctx)
	if err != nil || len(grants) != 3 {
		t.Fatal("revocation deleted authorization history", err)
	}
}

func TestRevokeAllPreservesRevocationsAfterClockRollback(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	for i := range 3 {
		grant, digest := fixture(i)
		if err := s.CreateBrowserGrant(ctx, grant, digest); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := fixture(0)
	revoked := first.IssuedAt.Add(time.Hour)
	if err := s.RevokeBrowserGrant(ctx, first.ID, revoked); err != nil {
		t.Fatal(err)
	}
	count, err := s.RevokeAllBrowserGrants(ctx, first.IssuedAt.Add(-time.Hour))
	if err != nil || count != 2 {
		t.Fatal("clock rollback prevented revoking all grants", count, err)
	}
	if count, err := s.RevokeAllBrowserGrants(ctx, revoked); err != nil || count != 0 {
		t.Fatal("repeated revoke-all changed existing revocations", count, err)
	}
	grants, err := s.ListBrowserGrants(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if grant.RevokedAt == nil || (grant.ID == first.ID && !grant.RevokedAt.Equal(revoked)) ||
			(grant.ID != first.ID && !grant.RevokedAt.Equal(grant.IssuedAt)) {
			t.Fatal("revocation time was overwritten or invalid", grant.ID)
		}
	}
}

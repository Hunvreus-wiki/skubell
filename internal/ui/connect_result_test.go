package ui

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Hunvreus-wiki/skubell/internal/api"
)

// TestClassifyConnectResult locks down the "never fail silently" guarantee: a genuine connection failure must be
// classified as connectFailed (shown to the user), and only a real user cancellation may be silent. It guards the
// regression where hiding the progress dialog canceled the context and every failure was swallowed.
func TestClassifyConnectResult(t *testing.T) {
	t.Parallel()

	authFailure := fmt.Errorf(
		"connect/login failed: %w: Incorrect username or password entered. Please try again.",
		api.ErrAuthenticationFailed,
	)

	cases := []struct {
		name       string
		connectErr error
		ctxErr     error
		want       connectOutcome
	}{
		{
			name:       "success proceeds",
			connectErr: nil,
			ctxErr:     nil,
			want:       connectProceed,
		},
		{
			name:       "success proceeds even if context later canceled",
			connectErr: nil,
			ctxErr:     context.Canceled,
			want:       connectProceed,
		},
		{
			name:       "auth failure is shown, not swallowed",
			connectErr: authFailure,
			ctxErr:     nil,
			want:       connectFailed,
		},
		{
			name:       "generic failure is shown",
			connectErr: errors.New("connect/siteinfo failed: boom"),
			ctxErr:     nil,
			want:       connectFailed,
		},
		{
			name:       "failure is NOT swallowed when ctxErr is nil (the fixed bug)",
			connectErr: api.ErrAuthenticationFailed,
			ctxErr:     nil,
			want:       connectFailed,
		},
		{
			name:       "user cancellation via context error is silent",
			connectErr: errors.New("connect/login failed: request canceled"),
			ctxErr:     context.Canceled,
			want:       connectCanceled,
		},
		{
			name:       "error wrapping context.Canceled is silent even if ctxErr not yet observed",
			connectErr: fmt.Errorf("execute login request: %w", context.Canceled),
			ctxErr:     nil,
			want:       connectCanceled,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyConnectResult(tc.connectErr, tc.ctxErr); got != tc.want {
				t.Fatalf("classifyConnectResult(%v, %v) = %d, want %d", tc.connectErr, tc.ctxErr, got, tc.want)
			}
		})
	}
}

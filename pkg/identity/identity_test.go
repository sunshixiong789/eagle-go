package identity

import (
	"context"
	"testing"
)

func TestPrincipalContext(t *testing.T) {
	ctx := context.Background()
	if principal, ok := FromContext(ctx); ok || principal != nil || Subject(ctx) != "" {
		t.Fatalf("anonymous context = %+v, %v, %q", principal, ok, Subject(ctx))
	}

	want := &Principal{Subject: "user-1", SessionID: "session-1", Roles: []string{"viewer"}}
	ctx = NewContext(ctx, want)
	got, ok := FromContext(ctx)
	if !ok || got != want || Subject(ctx) != "user-1" {
		t.Fatalf("principal = %+v, %v, subject=%q", got, ok, Subject(ctx))
	}

	nilCtx := NewContext(context.Background(), nil)
	if principal, ok := FromContext(nilCtx); ok || principal != nil {
		t.Fatalf("nil principal = %+v, %v", principal, ok)
	}
}

package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestListOpenDependencyPRsPerRequestTimeout(t *testing.T) {
	t.Run("slow request times out", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()
		client, err := NewClientWithBaseURLAndTimeout(context.Background(), "", server.URL+"/", 50*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ListOpenDependencyPRs(context.Background(), "owner", "repo", ListOptions{Limit: 1})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want context deadline exceeded", err)
		}
	})

	t.Run("larger timeout permits slow request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()
		client, err := NewClientWithBaseURLAndTimeout(context.Background(), "", server.URL+"/", 500*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.ListOpenDependencyPRs(context.Background(), "owner", "repo", ListOptions{Limit: 1}); err != nil {
			t.Fatalf("ListOpenDependencyPRs(): %v", err)
		}
	})
}

func TestSequentialCallsGetFreshPerRequestTimeout(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(100 * time.Millisecond)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client, err := NewClientWithBaseURLAndTimeout(context.Background(), "", server.URL+"/", 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	parent := context.Background()
	if _, err := client.ListOpenDependencyPRs(parent, "owner", "repo", ListOptions{Limit: 1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first call error = %v, want context deadline exceeded", err)
	}
	if _, err := client.ListOpenDependencyPRs(parent, "owner", "repo", ListOptions{Limit: 1}); err != nil {
		t.Fatalf("second call did not receive a fresh timeout budget: %v", err)
	}
}

package debounce

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWindow = 50 * time.Millisecond

func waitForCallback(t *testing.T, ch <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for callback")
	}
}

func TestBuffer_SinglePush_FiresAfterWindow(t *testing.T) {
	fired := make(chan struct{}, 1)
	var gotKey Key
	var gotData any

	buf := NewBuffer(testWindow, func(k Key, d any) {
		gotKey = k
		gotData = d
		fired <- struct{}{}
	})
	defer buf.Stop()

	key := Key{Owner: "owner", Repo: "repo", Branch: "feature"}
	buf.Push(key, "sha-abc")

	waitForCallback(t, fired, 5*testWindow)
	assert.Equal(t, key, gotKey)
	assert.Equal(t, "sha-abc", gotData)
}

func TestBuffer_MultiplePushes_OnlyLastFires(t *testing.T) {
	var callCount atomic.Int32
	fired := make(chan struct{}, 1)

	buf := NewBuffer(testWindow, func(_ Key, _ any) {
		callCount.Add(1)
		fired <- struct{}{}
	})
	defer buf.Stop()

	key := Key{Owner: "owner", Repo: "repo", Branch: "feature"}
	buf.Push(key, "sha-1")
	buf.Push(key, "sha-2")
	buf.Push(key, "sha-3")

	waitForCallback(t, fired, 5*testWindow)
	// Give extra time to confirm no more callbacks fire.
	time.Sleep(2 * testWindow)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestBuffer_MultiplePushes_UsesLatestData(t *testing.T) {
	fired := make(chan struct{}, 1)
	var gotData any

	buf := NewBuffer(testWindow, func(_ Key, d any) {
		gotData = d
		fired <- struct{}{}
	})
	defer buf.Stop()

	key := Key{Owner: "owner", Repo: "repo", Branch: "feature"}
	buf.Push(key, "sha-1")
	buf.Push(key, "sha-2")
	buf.Push(key, "sha-3")

	waitForCallback(t, fired, 5*testWindow)
	assert.Equal(t, "sha-3", gotData)
}

func TestBuffer_DifferentKeys_Independent(t *testing.T) {
	var mu sync.Mutex
	results := make(map[Key]any)
	fired := make(chan struct{}, 2)

	buf := NewBuffer(testWindow, func(k Key, d any) {
		mu.Lock()
		results[k] = d
		mu.Unlock()
		fired <- struct{}{}
	})
	defer buf.Stop()

	key1 := Key{Owner: "owner", Repo: "repo", Branch: "branch-1"}
	key2 := Key{Owner: "owner", Repo: "repo", Branch: "branch-2"}
	buf.Push(key1, "data-1")
	buf.Push(key2, "data-2")

	waitForCallback(t, fired, 5*testWindow)
	waitForCallback(t, fired, 5*testWindow)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, results, 2)
	assert.Equal(t, "data-1", results[key1])
	assert.Equal(t, "data-2", results[key2])
}

func TestBuffer_Cancel_PreventsCallback(t *testing.T) {
	var callCount atomic.Int32

	buf := NewBuffer(testWindow, func(_ Key, _ any) {
		callCount.Add(1)
	})
	defer buf.Stop()

	key := Key{Owner: "owner", Repo: "repo", Branch: "feature"}
	buf.Push(key, "sha-1")
	buf.Cancel(key)

	// Wait well past the window.
	time.Sleep(3 * testWindow)
	assert.Equal(t, int32(0), callCount.Load())
}

func TestBuffer_Stop_CancelsAll(t *testing.T) {
	var callCount atomic.Int32

	buf := NewBuffer(testWindow, func(_ Key, _ any) {
		callCount.Add(1)
	})

	key1 := Key{Owner: "owner", Repo: "repo", Branch: "branch-1"}
	key2 := Key{Owner: "owner", Repo: "repo", Branch: "branch-2"}
	buf.Push(key1, "data-1")
	buf.Push(key2, "data-2")
	buf.Stop()

	// Wait well past the window.
	time.Sleep(3 * testWindow)
	assert.Equal(t, int32(0), callCount.Load())
}

func TestBuffer_ConcurrentPushes_Safe(t *testing.T) {
	var callCount atomic.Int32
	done := make(chan struct{})

	buf := NewBuffer(testWindow, func(_ Key, _ any) {
		callCount.Add(1)
	})
	defer buf.Stop()

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := Key{Owner: "owner", Repo: "repo", Branch: "feature"}
			buf.Push(key, n)
		}(i)
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	<-done
	// Wait for debounce to fire.
	time.Sleep(3 * testWindow)

	// With all pushes to the same key, exactly 1 callback should fire.
	assert.Equal(t, int32(1), callCount.Load())
}

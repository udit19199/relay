package kvstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func resetControllerForTest(t *testing.T) {
	t.Helper()
	if errShutdown := ShutdownLocal(); errShutdown != nil {
		t.Fatalf("ShutdownLocal() error = %v", errShutdown)
	}
	t.Cleanup(func() {
		if errShutdown := ShutdownLocal(); errShutdown != nil {
			t.Errorf("ShutdownLocal() cleanup error = %v", errShutdown)
		}
	})
}

func TestControllerDefaultDisabledReturnsNilClient(t *testing.T) {
	resetControllerForTest(t)

	client, kvMode, errClient := CurrentKVClient()
	if errClient != nil {
		t.Fatalf("CurrentKVClient() error = %v, want nil", errClient)
	}
	if kvMode {
		t.Fatalf("CurrentKVClient() kvMode = true, want false")
	}
	if client != nil {
		t.Fatalf("CurrentKVClient() client = %#v, want nil", client)
	}
}

func TestControllerConfigureLocalLifecycleAndReuse(t *testing.T) {
	resetControllerForTest(t)

	dir := t.TempDir()
	dbPath1 := filepath.Join(dir, "kv1.db")
	dbPath2 := filepath.Join(dir, "kv2.db")

	if errCfg := ConfigureLocal(true, dbPath1); errCfg != nil {
		t.Fatalf("ConfigureLocal(true, dbPath1) error = %v", errCfg)
	}

	client1, kvMode1, errClient1 := CurrentKVClient()
	if errClient1 != nil || !kvMode1 || client1 == nil {
		t.Fatalf("CurrentKVClient() = (%v, %v, %v), want (*BoltStore, true, nil)", client1, kvMode1, errClient1)
	}
	bolt1, ok := client1.(*BoltStore)
	if !ok {
		t.Fatalf("CurrentKVClient() type = %T, want *BoltStore", client1)
	}

	// Reconfiguring with the same resolved path must reuse the same *BoltStore instance.
	if errCfgAgain := ConfigureLocal(true, dbPath1); errCfgAgain != nil {
		t.Fatalf("ConfigureLocal(true, dbPath1) second call error = %v", errCfgAgain)
	}
	client1Again, kvMode1Again, errClient1Again := CurrentKVClient()
	if errClient1Again != nil || !kvMode1Again {
		t.Fatalf("CurrentKVClient() after idempotent ConfigureLocal = (%v, %v, %v)", client1Again, kvMode1Again, errClient1Again)
	}
	if client1Again != bolt1 {
		t.Fatalf("ConfigureLocal with identical path did not reuse *BoltStore instance: got %p, want %p", client1Again, bolt1)
	}

	// Switching to a new path must close the old store and open the new one.
	if errCfg2 := ConfigureLocal(true, dbPath2); errCfg2 != nil {
		t.Fatalf("ConfigureLocal(true, dbPath2) error = %v", errCfg2)
	}
	client2, kvMode2, errClient2 := CurrentKVClient()
	if errClient2 != nil || !kvMode2 || client2 == nil {
		t.Fatalf("CurrentKVClient() after path switch = (%v, %v, %v)", client2, kvMode2, errClient2)
	}
	bolt2, ok := client2.(*BoltStore)
	if !ok {
		t.Fatalf("CurrentKVClient() type = %T, want *BoltStore", client2)
	}
	if bolt2 == bolt1 {
		t.Fatalf("ConfigureLocal with new path returned old *BoltStore instance")
	}
	if bolt2.Path() != dbPath2 {
		t.Fatalf("bolt2.Path() = %q, want %q", bolt2.Path(), dbPath2)
	}
	// Old store should be closed; operations on bolt1 must fail.
	if _, _, errOld := bolt1.KVGet(context.Background(), "key"); errOld == nil {
		t.Fatalf("expected old BoltStore to be closed after switching path")
	}

	// Disabling local KV closes the store and resets state to memory mode.
	if errDisable := ConfigureLocal(false, ""); errDisable != nil {
		t.Fatalf("ConfigureLocal(false, \"\") error = %v", errDisable)
	}
	clientDisabled, kvModeDisabled, errDisabled := CurrentKVClient()
	if errDisabled != nil || kvModeDisabled || clientDisabled != nil {
		t.Fatalf("CurrentKVClient() after disable = (%v, %v, %v), want (nil, false, nil)", clientDisabled, kvModeDisabled, errDisabled)
	}
}

func TestControllerConfigureLocalInvalidPathReportsUnavailableError(t *testing.T) {
	resetControllerForTest(t)

	// Passing an existing directory as the database file path causes bbolt.Open to fail.
	invalidPath := t.TempDir()
	errCfg := ConfigureLocal(true, invalidPath)
	if errCfg == nil {
		t.Fatalf("ConfigureLocal(true, directory) error = nil, want error")
	}

	client, kvMode, errClient := CurrentKVClient()
	if !kvMode {
		t.Fatalf("CurrentKVClient() kvMode = false, want true when enabled but open failed")
	}
	if client != nil {
		t.Fatalf("CurrentKVClient() client = %#v, want nil on open failure", client)
	}
	if errClient == nil {
		t.Fatalf("CurrentKVClient() error = nil, want unavailable error")
	}
}

func TestControllerHelpersWithLocalBoltStore(t *testing.T) {
	resetControllerForTest(t)

	dbPath := filepath.Join(t.TempDir(), "helpers.db")
	if errCfg := ConfigureLocal(true, dbPath); errCfg != nil {
		t.Fatalf("ConfigureLocal(true, dbPath) error = %v", errCfg)
	}

	ctx := context.Background()
	type payload struct {
		Name string `json:"name"`
	}

	kvMode, errSet := KVSetJSONRequired(ctx, "cpa:test:json", payload{Name: "alice"}, 10*time.Minute)
	if errSet != nil || !kvMode {
		t.Fatalf("KVSetJSONRequired() = (%v, %v), want (true, nil)", kvMode, errSet)
	}

	var got payload
	kvMode, found, errGet := KVGetJSONRequired(ctx, "cpa:test:json", &got)
	if errGet != nil || !kvMode || !found || got.Name != "alice" {
		t.Fatalf("KVGetJSONRequired() = (%v, %v, %v, %+v), want (true, true, nil, alice)", kvMode, found, errGet, got)
	}

	if !KVSetNXBestEffort(ctx, "cpa:test:nx", []byte("1"), 5*time.Minute) {
		t.Fatalf("first KVSetNXBestEffort() = false, want true")
	}
	if KVSetNXBestEffort(ctx, "cpa:test:nx", []byte("2"), 5*time.Minute) {
		t.Fatalf("second KVSetNXBestEffort() = true, want false")
	}

	if !KVExpireBestEffort(ctx, "cpa:test:json", 20*time.Minute) {
		t.Fatalf("KVExpireBestEffort() = false, want true")
	}
	if !KVDelBestEffort(ctx, "cpa:test:json", "cpa:test:nx") {
		t.Fatalf("KVDelBestEffort() = false, want true")
	}
	var afterDel payload
	if _, foundAfterDel := KVGetJSONBestEffort(ctx, "cpa:test:json", &afterDel); foundAfterDel {
		t.Fatalf("KVGetJSONBestEffort() found deleted key")
	}
}

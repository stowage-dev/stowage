// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package backend_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/memory"
)

func TestRegisterDefaultsToConfigSource(t *testing.T) {
	reg := backend.NewRegistry()
	if err := reg.Register(memory.New("alpha", "Alpha")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	src, ok := reg.Source("alpha")
	if !ok {
		t.Fatal("Source(alpha) not found")
	}
	if src != backend.SourceConfig {
		t.Fatalf("source=%q want %q", src, backend.SourceConfig)
	}
}

func TestRegisterWithSource(t *testing.T) {
	reg := backend.NewRegistry()
	if err := reg.RegisterWithSource(memory.New("alpha", "Alpha"), backend.SourceDB); err != nil {
		t.Fatalf("RegisterWithSource: %v", err)
	}
	entries := reg.List()
	if len(entries) != 1 || entries[0].Source != backend.SourceDB {
		t.Fatalf("entries=%+v", entries)
	}
}

func TestRegisterRejectsDuplicate(t *testing.T) {
	reg := backend.NewRegistry()
	_ = reg.Register(memory.New("alpha", "Alpha"))
	if err := reg.Register(memory.New("alpha", "Alpha-2")); err == nil {
		t.Fatal("second Register should have failed")
	}
}

func TestUnregisterRemovesEntry(t *testing.T) {
	reg := backend.NewRegistry()
	_ = reg.Register(memory.New("alpha", "Alpha"))

	if err := reg.Unregister("alpha"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if _, ok := reg.Get("alpha"); ok {
		t.Fatal("Get(alpha) should be missing after Unregister")
	}
	if len(reg.List()) != 0 {
		t.Fatalf("List=%d want 0", len(reg.List()))
	}
}

func TestUnregisterUnknownErrors(t *testing.T) {
	reg := backend.NewRegistry()
	err := reg.Unregister("ghost")
	if !errors.Is(err, backend.ErrNotRegistered) {
		t.Fatalf("err=%v want ErrNotRegistered", err)
	}
}

func TestUnregisterFreesIDForReuse(t *testing.T) {
	reg := backend.NewRegistry()
	_ = reg.Register(memory.New("alpha", "Alpha"))
	_ = reg.Unregister("alpha")
	// Same id can be reused after unregister.
	if err := reg.Register(memory.New("alpha", "Alpha-2")); err != nil {
		t.Fatalf("re-Register after Unregister: %v", err)
	}
}

func TestReplaceSwapsBackend(t *testing.T) {
	reg := backend.NewRegistry()
	_ = reg.RegisterWithSource(memory.New("alpha", "Alpha"), backend.SourceDB)

	fresh := memory.New("alpha", "Alpha-renamed")
	if err := reg.Replace("alpha", fresh); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, ok := reg.Get("alpha")
	if !ok || got != fresh {
		t.Fatalf("Get returned %#v ok=%v; expected fresh pointer", got, ok)
	}
}

func TestReplacePreservesSource(t *testing.T) {
	reg := backend.NewRegistry()
	_ = reg.RegisterWithSource(memory.New("alpha", "Alpha"), backend.SourceDB)
	if err := reg.Replace("alpha", memory.New("alpha", "Alpha-2")); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	src, _ := reg.Source("alpha")
	if src != backend.SourceDB {
		t.Fatalf("source=%q want %q", src, backend.SourceDB)
	}
}

func TestReplaceResetsStatusAndHistory(t *testing.T) {
	reg := backend.NewRegistry()
	_ = reg.Register(memory.New("alpha", "Alpha"))

	// Seed a non-zero status; the only public way to do this is via the
	// probe path, which records both status and history. Drive a probe
	// against the live backend so it succeeds and produces a record.
	reg.ProbeAll(t.Context(), time.Second)

	st, _ := reg.Status("alpha")
	if !st.Healthy {
		t.Fatalf("seed probe: status=%+v", st)
	}
	if len(reg.History("alpha")) == 0 {
		t.Fatal("seed probe should have produced a history record")
	}

	if err := reg.Replace("alpha", memory.New("alpha", "Alpha-2")); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	st, _ = reg.Status("alpha")
	if st.Healthy || !st.LastProbeAt.IsZero() {
		t.Fatalf("status not reset after Replace: %+v", st)
	}
	if h := reg.History("alpha"); len(h) != 0 {
		t.Fatalf("history not cleared after Replace: %d records", len(h))
	}
}

func TestReplaceRejectsIDMismatch(t *testing.T) {
	reg := backend.NewRegistry()
	_ = reg.Register(memory.New("alpha", "Alpha"))
	if err := reg.Replace("alpha", memory.New("beta", "Beta")); err == nil {
		t.Fatal("Replace with mismatched backend ID should error")
	}
}

func TestReplaceRejectsNil(t *testing.T) {
	reg := backend.NewRegistry()
	_ = reg.Register(memory.New("alpha", "Alpha"))
	if err := reg.Replace("alpha", nil); err == nil {
		t.Fatal("Replace(nil) should error")
	}
}

func TestReplaceUnknownErrors(t *testing.T) {
	reg := backend.NewRegistry()
	err := reg.Replace("ghost", memory.New("ghost", "Ghost"))
	if !errors.Is(err, backend.ErrNotRegistered) {
		t.Fatalf("err=%v want ErrNotRegistered", err)
	}
}

// TestConcurrentMutationsRace stresses the registry under -race so we catch
// any missing locking on the new mutators.
func TestConcurrentMutationsRace(t *testing.T) {
	reg := backend.NewRegistry()
	const n = 20
	for i := 0; i < n; i++ {
		_ = reg.Register(memory.New(idFor(i), "B"))
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(3)
		// Reader: List + Get + Source
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = reg.List()
				reg.Get(idFor(i))
				reg.Source(idFor(i))
			}
		}()
		// Replacer: swap with a fresh instance
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = reg.Replace(idFor(i), memory.New(idFor(i), "B"))
			}
		}()
		// Cycler: unregister then re-register to free/reclaim the slot
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := reg.Unregister(idFor(i)); err == nil {
					_ = reg.Register(memory.New(idFor(i), "B"))
				}
			}
		}()
	}
	wg.Wait()
}

func idFor(i int) string {
	return "be-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26))
}

package tracker

import (
	"testing"
	"time"

	"github.com/onllm-dev/onwatch/v2/internal/api"
	"github.com/onllm-dev/onwatch/v2/internal/store"
)

func makeAntigravitySnapshot(capturedAt time.Time, models []api.AntigravityModelQuota) *api.AntigravitySnapshot {
	return &api.AntigravitySnapshot{
		CapturedAt: capturedAt,
		Models:     models,
	}
}

func makeModel(id, label string, remaining float64, resetTime *time.Time) api.AntigravityModelQuota {
	return api.AntigravityModelQuota{
		ModelID:           id,
		Label:             label,
		RemainingFraction: remaining,
		IsExhausted:       remaining <= 0,
		ResetTime:         resetTime,
	}
}

func TestAntigravityTracker_FirstSnapshot_CreatesCycle(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	resetTime := time.Now().Add(24 * time.Hour)
	snapshot := makeAntigravitySnapshot(time.Now(), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.8, &resetTime),
	})

	err := tr.Process(snapshot)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	cycle, err := s.QueryActiveAntigravityCycle("model-a")
	if err != nil {
		t.Fatalf("QueryActiveAntigravityCycle failed: %v", err)
	}
	if cycle == nil {
		t.Fatal("Expected active cycle for model-a")
	}
}

func TestAntigravityTracker_UsageIncrement_UpdatesDelta(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	baseTime := time.Now()
	resetTime := baseTime.Add(24 * time.Hour)

	// First snapshot: 80% remaining
	s1 := makeAntigravitySnapshot(baseTime, []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.8, &resetTime),
	})
	tr.Process(s1)

	// Second snapshot: 60% remaining (usage increased by 0.2)
	s2 := makeAntigravitySnapshot(baseTime.Add(time.Minute), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.6, &resetTime),
	})
	err := tr.Process(s2)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	cycle, _ := s.QueryActiveAntigravityCycle("model-a")
	if cycle.TotalDelta < 0.19 || cycle.TotalDelta > 0.21 {
		t.Errorf("TotalDelta = %v, want ~0.2", cycle.TotalDelta)
	}
	// Peak usage should be 0.4 (1.0 - 0.6)
	if cycle.PeakUsage < 0.39 || cycle.PeakUsage > 0.41 {
		t.Errorf("PeakUsage = %v, want ~0.4", cycle.PeakUsage)
	}
}

func TestAntigravityTracker_DetectsReset_ResetTimeChanged(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	baseTime := time.Now()
	resetTime1 := baseTime.Add(24 * time.Hour)

	// First snapshot
	s1 := makeAntigravitySnapshot(baseTime, []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.5, &resetTime1),
	})
	tr.Process(s1)

	// Second snapshot with different reset time (>10 min difference)
	resetTime2 := baseTime.Add(48 * time.Hour)
	s2 := makeAntigravitySnapshot(baseTime.Add(time.Minute), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.9, &resetTime2),
	})
	err := tr.Process(s2)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Old cycle should be closed
	history, _ := s.QueryAntigravityCycleHistory("model-a")
	if len(history) != 1 {
		t.Errorf("Expected 1 closed cycle, got %d", len(history))
	}

	// New cycle should exist
	cycle, _ := s.QueryActiveAntigravityCycle("model-a")
	if cycle == nil {
		t.Fatal("Expected new active cycle")
	}
}

func TestAntigravityTracker_DetectsReset_FractionIncreased(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	baseTime := time.Now()
	resetTime := baseTime.Add(24 * time.Hour)

	// First snapshot: 30% remaining
	s1 := makeAntigravitySnapshot(baseTime, []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.3, &resetTime),
	})
	tr.Process(s1)

	// Second snapshot: remaining fraction increased by >0.1 (reset)
	s2 := makeAntigravitySnapshot(baseTime.Add(time.Minute), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.95, &resetTime),
	})
	err := tr.Process(s2)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	history, _ := s.QueryAntigravityCycleHistory("model-a")
	if len(history) != 1 {
		t.Errorf("Expected 1 closed cycle, got %d", len(history))
	}
}

func TestAntigravityTracker_DetectsReset_TimeBasedResetTimePassed(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	baseTime := time.Now()
	resetTime := baseTime.Add(1 * time.Hour)

	// First snapshot
	s1 := makeAntigravitySnapshot(baseTime, []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.5, &resetTime),
	})
	tr.Process(s1)

	// Second snapshot after reset time passed, with slight increase in remaining
	s2 := makeAntigravitySnapshot(baseTime.Add(2*time.Hour), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.55, &resetTime),
	})
	err := tr.Process(s2)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	history, _ := s.QueryAntigravityCycleHistory("model-a")
	if len(history) != 1 {
		t.Errorf("Expected 1 closed cycle, got %d", len(history))
	}
}

func TestAntigravityTracker_MultipleModels_IndependentCycles(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	baseTime := time.Now()
	resetTime1 := baseTime.Add(24 * time.Hour)
	resetTime2 := baseTime.Add(12 * time.Hour)

	// First snapshot with two models
	s1 := makeAntigravitySnapshot(baseTime, []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.8, &resetTime1),
		makeModel("model-b", "Model B", 0.6, &resetTime2),
	})
	tr.Process(s1)

	// Second snapshot: only model-a resets
	resetTime1New := baseTime.Add(48 * time.Hour)
	s2 := makeAntigravitySnapshot(baseTime.Add(time.Minute), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.95, &resetTime1New),
		makeModel("model-b", "Model B", 0.5, &resetTime2),
	})
	tr.Process(s2)

	// Model A should have 1 closed cycle
	historyA, _ := s.QueryAntigravityCycleHistory("model-a")
	if len(historyA) != 1 {
		t.Errorf("model-a: expected 1 closed cycle, got %d", len(historyA))
	}

	// Model B should have 0 closed cycles
	historyB, _ := s.QueryAntigravityCycleHistory("model-b")
	if len(historyB) != 0 {
		t.Errorf("model-b: expected 0 closed cycles, got %d", len(historyB))
	}
}

func TestAntigravityTracker_EmptyModelID_Skipped(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	resetTime := time.Now().Add(24 * time.Hour)
	snapshot := makeAntigravitySnapshot(time.Now(), []api.AntigravityModelQuota{
		makeModel("", "No ID Model", 0.8, &resetTime),
	})

	err := tr.Process(snapshot)
	if err != nil {
		t.Fatalf("Process should not fail for empty model ID: %v", err)
	}
}

func TestAntigravityTracker_SetOnReset_Called(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	var resetModelID string
	tr.SetOnReset(func(modelID string) {
		resetModelID = modelID
	})

	baseTime := time.Now()
	resetTime1 := baseTime.Add(24 * time.Hour)

	s1 := makeAntigravitySnapshot(baseTime, []api.AntigravityModelQuota{
		makeModel("model-x", "Model X", 0.5, &resetTime1),
	})
	tr.Process(s1)

	// Trigger reset
	resetTime2 := baseTime.Add(48 * time.Hour)
	s2 := makeAntigravitySnapshot(baseTime.Add(time.Minute), []api.AntigravityModelQuota{
		makeModel("model-x", "Model X", 0.95, &resetTime2),
	})
	tr.Process(s2)

	if resetModelID != "model-x" {
		t.Errorf("onReset called with %q, want %q", resetModelID, "model-x")
	}
}

func TestAntigravityTracker_PeakTracking(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	baseTime := time.Now()
	resetTime := baseTime.Add(24 * time.Hour)

	// Remaining fractions: 0.9, 0.7, 0.8, 0.5, 0.6, 0.3
	// Usage:               0.1, 0.3, 0.2, 0.5, 0.4, 0.7
	fractions := []float64{0.9, 0.7, 0.8, 0.5, 0.6, 0.3}

	for i, f := range fractions {
		snap := makeAntigravitySnapshot(baseTime.Add(time.Duration(i)*time.Minute), []api.AntigravityModelQuota{
			makeModel("model-a", "Model A", f, &resetTime),
		})
		tr.Process(snap)
	}

	cycle, _ := s.QueryActiveAntigravityCycle("model-a")
	// Peak usage should be 0.7 (1.0 - 0.3)
	if cycle.PeakUsage < 0.69 || cycle.PeakUsage > 0.71 {
		t.Errorf("PeakUsage = %v, want ~0.7", cycle.PeakUsage)
	}
}

func TestAntigravityTracker_UsageSummary_NoCycles(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)

	summary, err := tr.UsageSummary("model-a")
	if err != nil {
		t.Fatalf("UsageSummary failed: %v", err)
	}

	if summary.ModelID != "model-a" {
		t.Errorf("ModelID = %q, want 'model-a'", summary.ModelID)
	}
	if summary.CompletedCycles != 0 {
		t.Errorf("CompletedCycles = %d, want 0", summary.CompletedCycles)
	}
}

func TestAntigravityTracker_UsageSummary_WithCompletedCycle(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	baseTime := time.Now()
	resetTime1 := baseTime.Add(24 * time.Hour)

	// Create cycle
	s1 := makeAntigravitySnapshot(baseTime, []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.8, &resetTime1),
	})
	tr.Process(s1)
	s.InsertAntigravitySnapshot(s1)

	s2 := makeAntigravitySnapshot(baseTime.Add(time.Minute), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.5, &resetTime1),
	})
	tr.Process(s2)
	s.InsertAntigravitySnapshot(s2)

	// Trigger reset
	resetTime2 := baseTime.Add(48 * time.Hour)
	s3 := makeAntigravitySnapshot(baseTime.Add(2*time.Minute), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.9, &resetTime2),
	})
	tr.Process(s3)
	s.InsertAntigravitySnapshot(s3)

	summary, err := tr.UsageSummary("model-a")
	if err != nil {
		t.Fatalf("UsageSummary failed: %v", err)
	}

	if summary.CompletedCycles != 1 {
		t.Errorf("CompletedCycles = %d, want 1", summary.CompletedCycles)
	}
	if summary.Label != "Model A" {
		t.Errorf("Label = %q, want 'Model A'", summary.Label)
	}
}

func TestAntigravityTracker_Process_ExistingCycleAfterRestart_UpdatesPeakWithoutDelta(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	base := time.Date(2026, 3, 4, 13, 0, 0, 0, time.UTC)
	reset := base.Add(24 * time.Hour)

	if _, err := s.CreateAntigravityCycle("model-a", base, &reset); err != nil {
		t.Fatalf("CreateAntigravityCycle: %v", err)
	}
	if err := s.UpdateAntigravityCycle("model-a", 0.2, 0.1); err != nil {
		t.Fatalf("UpdateAntigravityCycle: %v", err)
	}

	model := makeModel("model-a", "Model A", 0.6, &reset) // usage=0.4
	if err := tr.processModel(model, base.Add(10*time.Minute)); err != nil {
		t.Fatalf("processModel: %v", err)
	}

	cycle, err := s.QueryActiveAntigravityCycle("model-a")
	if err != nil {
		t.Fatalf("QueryActiveAntigravityCycle: %v", err)
	}
	if cycle.PeakUsage < 0.39 || cycle.PeakUsage > 0.41 {
		t.Fatalf("PeakUsage = %v, want ~0.4", cycle.PeakUsage)
	}
	if cycle.TotalDelta < 0.099 || cycle.TotalDelta > 0.101 {
		t.Fatalf("TotalDelta = %v, want ~0.1", cycle.TotalDelta)
	}
}

func TestAntigravityTracker_UsageSummary_UsesSnapshotResetFallbackAndCapsNegativeDuration(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	base := time.Date(2026, 3, 4, 15, 0, 0, 0, time.UTC)
	pastReset := base.Add(-2 * time.Hour)

	if _, err := s.CreateAntigravityCycle("model-a", base, nil); err != nil {
		t.Fatalf("CreateAntigravityCycle: %v", err)
	}

	snap := makeAntigravitySnapshot(base.Add(time.Minute), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.75, &pastReset),
	})
	if _, err := s.InsertAntigravitySnapshot(snap); err != nil {
		t.Fatalf("InsertAntigravitySnapshot: %v", err)
	}

	summary, err := tr.UsageSummary("model-a")
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary.ResetTime == nil {
		t.Fatal("expected ResetTime fallback from latest snapshot")
	}
	if !summary.ResetTime.Equal(pastReset) {
		t.Fatalf("ResetTime = %v, want %v", summary.ResetTime, pastReset)
	}
	if summary.TimeUntilReset != 0 {
		t.Fatalf("TimeUntilReset = %v, want 0 when reset is in the past", summary.TimeUntilReset)
	}
	if summary.Label != "Model A" {
		t.Fatalf("Label = %q, want Model A", summary.Label)
	}
	if summary.RemainingFraction != 0.75 {
		t.Fatalf("RemainingFraction = %v, want 0.75", summary.RemainingFraction)
	}
}

func TestAntigravityTracker_UsageSummary_CalculatesRateAndClampsProjection(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()

	tr := NewAntigravityTracker(s, nil)
	cycleStart := time.Now().UTC().Add(-2 * time.Hour)
	resetTime := time.Now().UTC().Add(2 * time.Hour)

	if _, err := s.CreateAntigravityCycle("model-a", cycleStart, &resetTime); err != nil {
		t.Fatalf("CreateAntigravityCycle: %v", err)
	}
	if err := s.UpdateAntigravityCycle("model-a", 0.9, 0.4); err != nil {
		t.Fatalf("UpdateAntigravityCycle: %v", err)
	}

	snap := makeAntigravitySnapshot(time.Now().UTC(), []api.AntigravityModelQuota{
		makeModel("model-a", "Model A", 0.2, &resetTime), // current usage 0.8
	})
	if _, err := s.InsertAntigravitySnapshot(snap); err != nil {
		t.Fatalf("InsertAntigravitySnapshot: %v", err)
	}

	summary, err := tr.UsageSummary("model-a")
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary.CurrentRate <= 0 {
		t.Fatalf("CurrentRate = %v, want > 0", summary.CurrentRate)
	}
	if summary.ProjectedUsage != 1.0 {
		t.Fatalf("ProjectedUsage = %v, want 1.0 (clamped)", summary.ProjectedUsage)
	}
}

// After a restart or polling gap the tracker has no in-memory history, so a
// window whose reset time has passed must still be closed. agy 1.5.x also
// omits resetTime on an untouched bucket (issue #168 follow-up).
func TestAntigravityTracker_ExpiredWindowAfterRestart_ClosesCycle(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 21, 19, 0, 0, 0, time.UTC)
	oldReset := base.Add(5 * time.Hour)
	captured := base.Add(5 * 24 * time.Hour)
	newReset := captured.Add(5 * time.Hour)

	for name, modelReset := range map[string]*time.Time{
		"later reset": &newReset,
		"no reset":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := store.New(":memory:")
			defer s.Close()
			if _, err := s.CreateAntigravityCycle("3p-5h", base, &oldReset); err != nil {
				t.Fatalf("CreateAntigravityCycle: %v", err)
			}

			tr := NewAntigravityTracker(s, nil) // fresh: no lastFractions/lastResetTimes
			if err := tr.processModel(makeModel("3p-5h", "Claude + GPT 5h", 1.0, modelReset), captured); err != nil {
				t.Fatalf("processModel: %v", err)
			}

			history, _ := s.QueryAntigravityCycleHistory("3p-5h")
			if len(history) != 1 {
				t.Fatalf("closed cycles = %d, want 1", len(history))
			}
			if history[0].CycleEnd == nil || !history[0].CycleEnd.Equal(oldReset) {
				t.Fatalf("CycleEnd = %v, want %v", history[0].CycleEnd, oldReset)
			}
			active, _ := s.QueryActiveAntigravityCycle("3p-5h")
			if active == nil || !active.CycleStart.Equal(captured) {
				t.Fatalf("active cycle = %+v, want one starting at %v", active, captured)
			}
			if (modelReset == nil) != (active.ResetTime == nil) {
				t.Fatalf("active ResetTime = %v, want %v", active.ResetTime, modelReset)
			}
		})
	}
}

func TestAntigravityTracker_NoResetTimeWithinWindow_KeepsCycle(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()
	base := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	reset := base.Add(5 * time.Hour)
	if _, err := s.CreateAntigravityCycle("3p-5h", base, &reset); err != nil {
		t.Fatalf("CreateAntigravityCycle: %v", err)
	}

	tr := NewAntigravityTracker(s, nil)
	if err := tr.processModel(makeModel("3p-5h", "Claude + GPT 5h", 1.0, nil), base.Add(time.Hour)); err != nil {
		t.Fatalf("processModel: %v", err)
	}
	if history, _ := s.QueryAntigravityCycleHistory("3p-5h"); len(history) != 0 {
		t.Fatalf("closed cycles = %d, want 0", len(history))
	}
}

// A cycle opened while agy reported no window adopts the reset time once agy
// starts one, so the window can expire later even across restarts.
func TestAntigravityTracker_CycleWithoutReset_AdoptsResetTime(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()
	base := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)

	tr := NewAntigravityTracker(s, nil)
	if err := tr.Process(makeAntigravitySnapshot(base, []api.AntigravityModelQuota{
		makeModel("3p-5h", "Claude + GPT 5h", 1.0, nil),
	})); err != nil {
		t.Fatalf("Process: %v", err)
	}
	reset := base.Add(time.Hour + 5*time.Hour)
	if err := tr.Process(makeAntigravitySnapshot(base.Add(time.Hour), []api.AntigravityModelQuota{
		makeModel("3p-5h", "Claude + GPT 5h", 0.9, &reset),
	})); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if history, _ := s.QueryAntigravityCycleHistory("3p-5h"); len(history) != 0 {
		t.Fatalf("closed cycles = %d, want 0", len(history))
	}
	active, _ := s.QueryActiveAntigravityCycle("3p-5h")
	if active == nil || active.ResetTime == nil || !active.ResetTime.Equal(reset) {
		t.Fatalf("active cycle = %+v, want ResetTime %v", active, reset)
	}
	if active.TotalDelta < 0.099 || active.TotalDelta > 0.101 {
		t.Fatalf("TotalDelta = %v, want ~0.1", active.TotalDelta)
	}
}

func TestAntigravityTracker_ExpiredWindow_NotifiesOnlyAfterUsage(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	oldReset := base.Add(5 * time.Hour)

	for name, tc := range map[string]struct {
		peak float64
		want bool
	}{
		"idle window": {0, false},
		"used window": {0.3, true},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := store.New(":memory:")
			defer s.Close()
			if _, err := s.CreateAntigravityCycle("gemini-5h", base, &oldReset); err != nil {
				t.Fatalf("CreateAntigravityCycle: %v", err)
			}
			if err := s.UpdateAntigravityCycle("gemini-5h", tc.peak, tc.peak); err != nil {
				t.Fatalf("UpdateAntigravityCycle: %v", err)
			}

			tr := NewAntigravityTracker(s, nil)
			called := false
			tr.SetOnReset(func(string) { called = true })
			captured := oldReset.Add(time.Minute)
			newReset := captured.Add(5 * time.Hour)
			if err := tr.processModel(makeModel("gemini-5h", "Gemini 5h", 1.0, &newReset), captured); err != nil {
				t.Fatalf("processModel: %v", err)
			}
			if called != tc.want {
				t.Fatalf("onReset called = %v, want %v", called, tc.want)
			}
		})
	}
}

// Continuous polling of an idle bucket: the rollover is caught by Method 1
// (reset time moved), and must still not notify.
func TestAntigravityTracker_IdleRollover_ContinuousPolling_NoNotification(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()
	tr := NewAntigravityTracker(s, nil)
	calls := 0
	tr.SetOnReset(func(string) { calls++ })

	base := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	reset1 := base.Add(5 * time.Hour)
	reset2 := reset1.Add(5 * time.Hour)
	for _, snap := range []*api.AntigravitySnapshot{
		makeAntigravitySnapshot(base, []api.AntigravityModelQuota{makeModel("3p-5h", "Claude + GPT 5h", 1.0, &reset1)}),
		makeAntigravitySnapshot(base.Add(time.Hour), []api.AntigravityModelQuota{makeModel("3p-5h", "Claude + GPT 5h", 1.0, &reset1)}),
		makeAntigravitySnapshot(reset1.Add(time.Minute), []api.AntigravityModelQuota{makeModel("3p-5h", "Claude + GPT 5h", 1.0, &reset2)}),
	} {
		if err := tr.Process(snap); err != nil {
			t.Fatalf("Process: %v", err)
		}
	}
	if history, _ := s.QueryAntigravityCycleHistory("3p-5h"); len(history) != 1 {
		t.Fatalf("closed cycles = %d, want 1", len(history))
	}
	if calls != 0 {
		t.Fatalf("onReset calls = %d, want 0 for an idle window", calls)
	}
}

// Dated poll with usage -> expired undated poll -> first dated usage poll of
// the next window: one rollover, one alert, and the new window's first usage
// lands in the new cycle.
func TestAntigravityTracker_ExpiredUndatedThenDated_SingleRollover(t *testing.T) {
	t.Parallel()
	s, _ := store.New(":memory:")
	defer s.Close()
	tr := NewAntigravityTracker(s, nil)
	calls := 0
	tr.SetOnReset(func(string) { calls++ })

	base := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	reset1 := base.Add(5 * time.Hour)
	expired := reset1.Add(time.Hour)
	used := expired.Add(time.Hour)
	reset2 := used.Add(5 * time.Hour)
	for _, snap := range []*api.AntigravitySnapshot{
		makeAntigravitySnapshot(base, []api.AntigravityModelQuota{makeModel("3p-5h", "Claude + GPT 5h", 0.7, &reset1)}),
		makeAntigravitySnapshot(expired, []api.AntigravityModelQuota{makeModel("3p-5h", "Claude + GPT 5h", 1.0, nil)}),
		makeAntigravitySnapshot(used, []api.AntigravityModelQuota{makeModel("3p-5h", "Claude + GPT 5h", 0.9, &reset2)}),
	} {
		if err := tr.Process(snap); err != nil {
			t.Fatalf("Process: %v", err)
		}
	}

	if history, _ := s.QueryAntigravityCycleHistory("3p-5h"); len(history) != 1 {
		t.Fatalf("closed cycles = %d, want 1", len(history))
	}
	if calls != 1 {
		t.Fatalf("onReset calls = %d, want 1", calls)
	}
	active, _ := s.QueryActiveAntigravityCycle("3p-5h")
	if active == nil || !active.CycleStart.Equal(expired) || active.ResetTime == nil || !active.ResetTime.Equal(reset2) {
		t.Fatalf("active cycle = %+v, want start %v and ResetTime %v", active, expired, reset2)
	}
	if active.TotalDelta < 0.099 || active.TotalDelta > 0.101 {
		t.Fatalf("TotalDelta = %v, want ~0.1", active.TotalDelta)
	}
}

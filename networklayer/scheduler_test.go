package networklayer

import "testing"

func mustSchedule(t *testing.T, scheduler *WRRScheduler[string], priority int, v string) {
	t.Helper()
	if err := scheduler.Schedule(priority, v); err != nil {
		t.Fatalf("Schedule(%d, %q): %v", priority, v, err)
	}
}

func TestScheduler(t *testing.T) {
	scheduler := NewWRRScheduler[string](5, 2, 3)
	// from the example on wikipedia
	mustSchedule(t, scheduler, 0, "A")
	mustSchedule(t, scheduler, 0, "B")
	mustSchedule(t, scheduler, 0, "C")
	mustSchedule(t, scheduler, 0, "D")
	mustSchedule(t, scheduler, 0, "E")
	mustSchedule(t, scheduler, 0, "F")
	mustSchedule(t, scheduler, 0, "G")
	mustSchedule(t, scheduler, 1, "U")
	mustSchedule(t, scheduler, 1, "V")
	mustSchedule(t, scheduler, 1, "W")
	mustSchedule(t, scheduler, 2, "X")
	mustSchedule(t, scheduler, 2, "Y")

	result := []string{}
	for i := 0; i < 12; i++ {
		value, ok := scheduler.GetNext()
		if !ok {
			t.Fatal("was expecting a value")
		}
		result = append(result, value)
	}
	expected := []string{"A", "B", "C", "D", "E", "U", "V", "X", "Y", "F", "G", "W"}
	for i := 0; i < len(expected); i++ {
		if result[i] != expected[i] {
			t.Errorf("index %d: expected %s, got %s", i, expected[i], result[i])
		}
	}

	_, ok := scheduler.GetNext()
	if ok {
		t.Errorf("GetNext should have returned empty")
	}

}

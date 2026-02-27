package networklayer

import "testing"

func TestScheduler(t *testing.T) {
	scheduler := NewWRRScheduler[string](5, 2, 3)
	// from the example on wikipedia
	scheduler.Schedule(0, "A")
	scheduler.Schedule(0, "B")
	scheduler.Schedule(0, "C")
	scheduler.Schedule(0, "D")
	scheduler.Schedule(0, "E")
	scheduler.Schedule(0, "F")
	scheduler.Schedule(0, "G")
	scheduler.Schedule(1, "U")
	scheduler.Schedule(1, "V")
	scheduler.Schedule(1, "W")
	scheduler.Schedule(2, "X")
	scheduler.Schedule(2, "Y")

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

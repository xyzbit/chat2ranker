package postgres

import "testing"

func TestModelBudgetSurvivesStorageAndCapacityProjection(t *testing.T) {
	store := testStore(t, 4)
	value := executionValue("model-budget")
	value.MaxModelRequests = 7
	if err := store.Create(t.Context(), value, queuedEvent(value)); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(t.Context(), value.ID)
	if err != nil || loaded.MaxModelRequests != 7 {
		t.Fatalf("budget lost: %+v %v", loaded, err)
	}
	state, err := store.CapacityState(t.Context())
	if err != nil || len(state.Entries) != 1 || state.Entries[0].MaxModelRequests != 7 {
		t.Fatalf("budget projection lost: %+v %v", state, err)
	}
}

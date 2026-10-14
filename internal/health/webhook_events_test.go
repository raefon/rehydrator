package health

import "testing"

func TestIsDeleteEvent(t *testing.T) {
	for _, event := range []string{"MovieDelete", "moviedelete", "MovieDeleted", "SeriesDelete", "SeriesDeleted"} {
		if !isDeleteEvent(event, "MovieDelete", "MovieDeleted", "SeriesDelete", "SeriesDeleted") {
			t.Errorf("expected delete event %q to match", event)
		}
	}
	for _, event := range []string{"Download", "Grab", "Test", "SeriesAdd", ""} {
		if isDeleteEvent(event, "MovieDelete", "MovieDeleted", "SeriesDelete", "SeriesDeleted") {
			t.Errorf("unexpected delete event match for %q", event)
		}
	}
}

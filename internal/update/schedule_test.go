package update

import (
	"testing"
	"time"
)

// The setting is a time of day in a timezone, and the timezone is the
// half that makes the number mean anything: 03:00 on a server's clock
// is not the middle of anybody's night.
func TestTheHourIsReadInTheChosenTimezone(t *testing.T) {
	// São Paulo, because it is three hours behind UTC all year: a zone
	// that changes with the season would make this test a test about
	// which month it is.
	saoPaulo, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skip("no timezone database on this machine")
	}
	utc := time.Date(2026, 1, 15, 6, 0, 0, 0, time.UTC)
	if got := utc.In(saoPaulo).Format("15:04"); got != "03:00" {
		t.Errorf("06:00 UTC read as %s in São Paulo, so an instance told 03:00 would update at the wrong hour", got)
	}
}

// **The timezone database is in the binary.** The image is Alpine, which
// ships none, so `time.LoadLocation` found nothing there and every zone
// name was refused — an instance told to update at 03:00 in
// America/Bahia was told that is not a timezone this machine knows.
//
// This test passes on a developer's machine either way, because macOS
// and the CI runner both have a system database. What it pins is the
// zone names this instance promises to accept; what makes it true
// inside the image is the blank import in cmd/cubeshipd, and nothing
// here can see that from the outside.
func TestTheZonesAnInstanceAccepts(t *testing.T) {
	for _, zone := range []string{
		"America/Bahia", "America/Sao_Paulo", "Europe/Lisbon", "Asia/Tokyo", "UTC",
	} {
		if _, err := time.LoadLocation(zone); err != nil {
			t.Errorf("%s: %v", zone, err)
		}
	}
}

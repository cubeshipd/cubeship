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

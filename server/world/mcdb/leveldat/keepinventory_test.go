package leveldat

import "testing"

// The keepinventory game rule is saved in level.dat and read back.
func TestKeepInventoryRoundTrip(t *testing.T) {
	var d Data
	d.FillDefault()
	s := d.Settings()
	s.KeepInventory = true
	d.PutSettings(s)
	if !d.KeepInventory || !d.Settings().KeepInventory {
		t.Fatal("keepinventory lost")
	}
}

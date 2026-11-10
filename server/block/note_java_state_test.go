package block

import "testing"

// TestNoteJavaState checks note block states against the 26.3 blocks report: harp note 0 powered is 680, bass
// note 3 unpowered 887, pling note 24 unpowered 1479.
func TestNoteJavaState(t *testing.T) {
	for _, c := range []struct {
		n    Note
		want int32
	}{
		{Note{Powered: true}, 680},
		{Note{}, 681},
		{Note{Pitch: 3, javaInstrument: 4}, 887},
		{Note{Pitch: 24, javaInstrument: 15}, 1479},
	} {
		if got := c.n.JavaState(); got != c.want {
			t.Errorf("%+v: JavaState %d, want %d", c.n, got, c.want)
		}
	}
	n := Note{Pitch: 7, Powered: true, javaInstrument: 6}
	if got := n.DecodeNBT(n.EncodeNBT()).(Note); got != n {
		t.Errorf("NBT round trip: %+v, want %+v", got, n)
	}
	if _, ok := n.EncodeNetworkNBT()[noteInstrumentKey]; ok {
		t.Error("the instrument went to Bedrock clients")
	}
}

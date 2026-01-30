package bacnet

import "testing"

func TestBitStringSetBit(t *testing.T) {
	bs := NewBitString()
	bs.SetBit(1).SetBit(3)
	// The BitString should now be
	// 01010000

	if bs.unusedBits != 4 {
		t.Fatalf("wrong unused bits, expected %d, got %d", 4, bs.unusedBits)
	}
	if bs.octets[0] != 0x50 {
		t.Fatalf("wrong last octet, expected %x, go %x", 0x50, bs.octets[0])
	}

	bs.SetBit(9)
	// The BitString should now be
	// 01010000 01000000
	if bs.unusedBits != 6 {
		t.Fatalf("wrong unused bits, expected %d, got %d", 6, bs.unusedBits)
	}
	if bs.octets[1] != 0x40 {
		t.Fatalf("wrong last octet, expected %x, go %x", 0x40, bs.octets[1])
	}

	bs.SetBit(15)
	// The BitString should now be
	// 01010000 01000001
	if bs.unusedBits != 0 {
		t.Fatalf("wrong unused bits, expected %d, got %d", 0, bs.unusedBits)
	}
	if bs.octets[1] != 0x41 {
		t.Fatalf("wrong last octet, expected %x, go %x", 0x41, bs.octets[1])
	}

	bs.SetBit(29)
	// The BitString should now be
	// 01010000 01000001 00000000 00000100
	if bs.unusedBits != 2 {
		t.Fatalf("wrong unused bits, expected %d, got %d", 2, bs.unusedBits)
	}
	if bs.octets[3] != 0x04 {
		t.Fatalf("wrong last octet, expected %x, go %x", 0x04, bs.octets[3])
	}

}
func TestBitStringUnsetBit(t *testing.T) {
	bs := NewBitString()
	bs.SetBit(1).SetBit(3).UnsetBit(1)
	// The BitString should now be
	// 00010000

	if bs.unusedBits != 4 {
		t.Fatalf("wrong unused bits, expected %d, got %d", 4, bs.unusedBits)
	}
	if bs.octets[0] != 0x10 {
		t.Fatalf("wrong last octet, expected %x, go %x", 0x10, bs.octets[0])
	}

	bs.SetBit(1).UnsetBit(3)
	// The BitString should now be
	// 01000000

	if bs.unusedBits != 6 {
		t.Fatalf("wrong unused bits, expected %d, got %d", 6, bs.unusedBits)
	}
	if bs.octets[0] != 0x40 {
		t.Fatalf("wrong last octet, expected %x, go %x", 0x40, bs.octets[0])
	}

	bs.SetBit(1).SetBit(3).SetBit(9).SetBit(29)
	// The BitString should now be
	// 01010000 01000000 00000000 00000100
	if bs.unusedBits != 2 {
		t.Fatalf("wrong unused bits, expected %d, got %d", 2, bs.unusedBits)
	}
	if bs.octets[3] != 0x04 {
		t.Fatalf("wrong last octet, expected %x, go %x", 0x04, bs.octets[3])
	}

	bs.UnsetBit(29)
	// The BitString should now be
	// 01010000 01000000
	if bs.unusedBits != 6 {
		t.Fatalf("wrong unused bits, expected %d, got %d", 6, bs.unusedBits)
	}
	if bs.octets[1] != 0x40 {
		t.Fatalf("wrong last octet, expected %x, go %x", 0x40, bs.octets[1])
	}
	if len(bs.octets) != 2 {
		t.Fatalf("buffer not resized, expected length: 2, got %d", len(bs.octets))
	}

	bs.UnsetBit(9).UnsetBit(3).UnsetBit(1)
	if bs.unusedBits != 0 {
		t.Fatalf("wrong unused bits, expected %d, got %d", 0, bs.unusedBits)
	}
	if len(bs.octets) != 0 {
		t.Fatalf("buffer not resized, expected length: 0, got %d", len(bs.octets))
	}
}

func TestBitStringIsBitSet(t *testing.T) {
	bs := NewBitString()
	bs.SetBit(3).SetBit(23)

	if bs.IsBitSet(0) {
		t.Fatalf("bit 0 should not have been set")
	}
	if !bs.IsBitSet(3) {
		t.Fatalf("bit 3 should have been set")
	}
	if bs.IsBitSet(22) {
		t.Fatalf("bit 22 should not have been set")
	}
	if !bs.IsBitSet(23) {
		t.Fatalf("bit 23 should have been set")
	}
	if bs.IsBitSet(33) {
		t.Fatalf("bit 33 should not have been set")
	}
}

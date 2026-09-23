package feed

import (
	"encoding/binary"
	"testing"
)

func TestDecodeNGF1WithInnerRHF2(t *testing.T) {
	// Build a minimal RHF2 frame, wrap in NGF1.
	ethTx := []byte{0x02, 0xaa}
	payloadLen := 16 + len(ethTx)
	rhf2Total := rhf2HeaderSize + payloadLen + rhf2TrailerSize
	rhf2 := make([]byte, rhf2Total)
	copy(rhf2[0:4], []byte(rhf2Magic))
	binary.BigEndian.PutUint16(rhf2[4:6], 2)
	binary.BigEndian.PutUint32(rhf2[16:20], 100)
	binary.BigEndian.PutUint32(rhf2[24:28], 100)
	binary.BigEndian.PutUint32(rhf2[44:48], uint32(payloadLen))
	rhf2[rhf2HeaderSize] = 0 // NGF1 quirk: marker 0
	rhf2[rhf2HeaderSize+3] = 0
	// timestamp left 0 — patch should fill
	copy(rhf2[rhf2HeaderSize+16:], ethTx)

	// PRH1 prefix (4B) then RHF2
	inner := append([]byte("PRH1"), rhf2...)
	ngf1Total := ngf1HeaderSize + len(inner) + ngf1TrailerSize
	buf := make([]byte, ngf1Total)
	copy(buf[0:4], []byte(ngf1Magic))
	buf[4], buf[5] = 0x04, 0x02
	binary.BigEndian.PutUint32(buf[ngf1PlenOffset:ngf1PlenOffset+4], uint32(len(inner)))
	copy(buf[ngf1HeaderSize:], inner)

	frames, consumed, err := DecodeNGF1Frame(buf)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != ngf1Total {
		t.Fatalf("consumed=%d want=%d", consumed, ngf1Total)
	}
	if len(frames) != 1 {
		t.Fatalf("frames=%d", len(frames))
	}
	if frames[0].SeqTo != 100 || frames[0].TxIndex != 0 {
		t.Fatalf("frame=%+v", frames[0])
	}
	if frames[0].Timestamp == 0 {
		t.Fatal("expected patched timestamp")
	}
}

func TestDecodeNGF1RejectsDIRECTShape(t *testing.T) {
	// Pure DIRECT outer without NGF1 magic should fail.
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:4], directFlag|48)
	_, _, err := DecodeNGF1Frame(hdr)
	if err == nil {
		t.Fatal("expected bad magic")
	}
}

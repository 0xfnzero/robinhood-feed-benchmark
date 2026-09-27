package feed

// ethTxWireLen returns the byte length of one Ethereum tx at the start of b
// (legacy RLP list, or EIP-2718 typed tx = type || RLP).
func ethTxWireLen(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}
	// Legacy transactions are RLP lists (first byte >= 0xc0).
	if b[0] >= 0xc0 {
		return rlpItemLen(b)
	}
	// Typed tx: 1-byte type + RLP payload.
	if len(b) < 2 {
		return 0, false
	}
	n, ok := rlpItemLen(b[1:])
	if !ok {
		return 0, false
	}
	return 1 + n, true
}

func rlpItemLen(b []byte) (int, bool) {
	if len(b) == 0 {
		return 0, false
	}
	prefix := b[0]
	switch {
	case prefix <= 0x7f:
		return 1, true
	case prefix <= 0xb7:
		strLen := int(prefix - 0x80)
		total := 1 + strLen
		if len(b) < total {
			return 0, false
		}
		return total, true
	case prefix <= 0xbf:
		lenOfLen := int(prefix - 0xb7)
		if len(b) < 1+lenOfLen {
			return 0, false
		}
		strLen := 0
		for i := 0; i < lenOfLen; i++ {
			strLen = (strLen << 8) | int(b[1+i])
		}
		total := 1 + lenOfLen + strLen
		if strLen < 0 || len(b) < total {
			return 0, false
		}
		return total, true
	case prefix <= 0xf7:
		listLen := int(prefix - 0xc0)
		total := 1 + listLen
		if len(b) < total {
			return 0, false
		}
		return total, true
	default:
		lenOfLen := int(prefix - 0xf7)
		if len(b) < 1+lenOfLen {
			return 0, false
		}
		listLen := 0
		for i := 0; i < lenOfLen; i++ {
			listLen = (listLen << 8) | int(b[1+i])
		}
		total := 1 + lenOfLen + listLen
		if listLen < 0 || len(b) < total {
			return 0, false
		}
		return total, true
	}
}

package utils

import (
	"encoding/binary"
	"math/big"
	"math/bits"
	"unsafe"

	"golang.org/x/exp/constraints"
)

func (nv *NodeValue12) IsNil() bool {
	if nv != nil {
		isNil := true
		for i := 0; i < 12; i++ {
			isNil = isNil && nv[i] == nil
		}

		return isNil
	} else {
		return true
	}
}

const hextable = "0123456789abcdef"

func ArrayToHex[T constraints.Unsigned](array []T) string {
	if len(array) == 0 {
		return "0x0"
	}
	byteLen := len(array) * int(unsafe.Sizeof(array[0]))
	byteArray := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(array))), byteLen)

	nonZeroPos := len(byteArray)
	for i := len(byteArray) - 1; i >= 0; i-- {
		if byteArray[i] == 0 {
			nonZeroPos -= 1
		} else {
			break
		}
	}
	byteArray = byteArray[:nonZeroPos]
	if len(byteArray) == 0 {
		return "0x0"
	}

	buf := make([]byte, len(byteArray)*2+2)

	j := len(buf) - 2
	for _, v := range byteArray {
		buf[j] = hextable[v>>4]
		buf[j+1] = hextable[v&0x0f]
		j -= 2
	}

	if buf[2] == '0' {
		buf = buf[1:]
	}
	buf[0] = '0'
	buf[1] = 'x'

	return unsafe.String(&buf[0], len(buf))
}

// fast path for 64-bit systems
func arrayToScalarBigFast(array []*big.Int) (*big.Int, bool) {
	if len(array) == 0 || bits.UintSize != 64 {
		return nil, false
	}

	scalarBitsSize := len(array)
	intBits := make([]big.Word, scalarBitsSize)

	for i := 0; i < len(array); i++ {
		if array[i] == nil || len(array[i].Bits()) == 0 {
			intBits[i] = 0
		} else {
			intBits[i] = array[i].Bits()[0]
			if array[i].Sign() < 0 {
				return nil, false
			}
			for _, v := range array[i].Bits()[1:] {
				if v != 0 {
					return nil, false
				}
			}
		}
	}
	return new(big.Int).SetBits(intBits), true
}

func arrayToScalarBigSlow(array []*big.Int) *big.Int {
	scalar := new(big.Int)
	for i := len(array) - 1; i >= 0; i-- {
		scalar.Lsh(scalar, 64)
		scalar.Add(scalar, array[i])
	}
	return scalar
}

func scalarToRootSlow(s *big.Int) NodeKey {
	var result [4]uint64
	divisor := new(big.Int).Exp(big.NewInt(2), big.NewInt(64), nil)

	sCopy := new(big.Int).Set(s)

	for i := 0; i < 4; i++ {
		mod := new(big.Int).Mod(sCopy, divisor)
		result[i] = mod.Uint64()
		sCopy.Div(sCopy, divisor)
	}
	return result
}

// fast path for 64-bit systems
func scalarToNodeValueFast(scalarIn *big.Int, out *[12]*big.Int) bool {
	if bits.UintSize != 64 || scalarIn.Sign() < 0 {
		return false
	}

	outData := [12]big.Int{}
	words := scalarIn.Bits()
	outDataBits := make([][1]big.Word, len(words))
	for i := 0; i < 12; i++ {
		if i < len(words) {
			outDataBits[i][0] = words[i]
			out[i] = (&outData[i]).SetBits(outDataBits[i][:])
		} else {
			out[i] = &outData[i]
		}
	}
	return true
}

func scalarToNodeValueSlow(scalarIn *big.Int) NodeValue12 {
	out := [12]*big.Int{}
	mask := new(big.Int).SetBytes([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	scalar := new(big.Int).Set(scalarIn)

	for i := 0; i < 12; i++ {
		value := new(big.Int).And(scalar, mask)
		out[i] = value
		scalar.Rsh(scalar, 64)
	}
	return out
}

func ConvertUint64ToBytes(n uint64) []byte {
	bytes := make([]byte, 8)
	binary.BigEndian.PutUint64(bytes, n)
	// or use binary.LittleEndian.PutUint64(bytes, n)
	return bytes
}

func ConvertBytesToUint64(bytes []byte) (uint64, error) {
	if bytes == nil || len(bytes) == 0 {
		return 0, nil
	}

	n := binary.BigEndian.Uint64(bytes)
	// or use binary.LittleEndian.Uint64(bytes)
	return n, nil
}

func UnsafeBytesToString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func UnsafeStringToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

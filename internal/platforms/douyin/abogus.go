package douyin

import (
	"encoding/binary"
	"math/bits"
	"math/rand/v2"
	"time"
)

// 本文件的 SM3、RC4 和变体 Base64 组合参考 douyinLive/sign 的 MIT 实现，
// 并按 R-plugin 当前使用的 Web 参数布局适配。许可文本随插件一并发布。

const douyinABogusAlphabetS3 = "ckdp1h4ZKsUB80/Mfvw36XIgR25+WQAlEi7NLboqYTOPuzmFjJnryx9HVGDaStCe"

const douyinABogusAlphabetS4 = "Dkdpgh2ZmsQB80/MfvV36XI1R45-WUAlEixNLwoqYTOPuzKFjJnry79HbGcaStCe"

const douyinABogusWindow = "1536|747|1536|834|0|30|0|0|1536|834|1536|864|1525|747|24|24|Win32"

func douyinABogus(query, userAgent string, now time.Time) string {
	randomValues := [3]int{rand.IntN(10000), rand.IntN(10000), rand.IntN(10000)}
	milliseconds := now.UnixMilli()
	return douyinABogusWithInputs(query, userAgent, milliseconds, milliseconds, randomValues)
}

func douyinABogusWithInputs(query, userAgent string, startMillis, endMillis int64, randomValues [3]int) string {
	urlHashFirst := douyinSM3Sum([]byte(query + "cus"))
	urlHash := douyinSM3Sum(urlHashFirst[:])
	cusHashFirst := douyinSM3Sum([]byte("cus"))
	cusHash := douyinSM3Sum(cusHashFirst[:])
	uaCipher := douyinRC4(douyinCodeUnits(userAgent), []uint16{0, 1, 14})
	uaEncoded := douyinABogusEncode(uaCipher, douyinABogusAlphabetS3)
	uaHash := douyinSM3Sum([]byte(uaEncoded))

	b := map[int]int{
		8: 3, 18: 44,
		20: int(uint64(startMillis) >> 24 & 255), 21: int(uint64(startMillis) >> 16 & 255),
		22: int(uint64(startMillis) >> 8 & 255), 23: int(uint64(startMillis) & 255),
		24: int(startMillis / (1 << 32)), 25: int(startMillis / (1 << 40)),
		26: 0, 27: 0, 28: 0, 29: 0,
		30: 0, 31: 1, 32: 0, 33: 0,
		34: 0, 35: 0, 36: 0, 37: 14,
		38: int(urlHash[21]), 39: int(urlHash[22]),
		40: int(cusHash[21]), 41: int(cusHash[22]),
		42: int(uaHash[23]), 43: int(uaHash[24]),
		44: int(uint64(endMillis) >> 24 & 255), 45: int(uint64(endMillis) >> 16 & 255),
		46: int(uint64(endMillis) >> 8 & 255), 47: int(uint64(endMillis) & 255),
		48: 3, 49: int(endMillis / (1 << 32)), 50: int(endMillis / (1 << 40)),
		52: 0, 53: 0, 54: 24, 55: 97,
		57: 239, 58: 24, 59: 0, 60: 0,
		65: len(douyinABogusWindow) & 255, 66: len(douyinABogusWindow) >> 8 & 255,
		70: 0, 71: 0,
	}
	checksumKeys := []int{
		18, 20, 26, 30, 38, 40, 42, 21, 27, 31, 35, 39, 41, 43, 22, 28, 32, 36,
		23, 29, 33, 37, 44, 45, 46, 47, 48, 49, 50, 24, 25, 52, 53, 54, 55, 57, 58,
		59, 60, 65, 66, 70, 71,
	}
	checksum := 0
	for _, key := range checksumKeys {
		checksum ^= b[key]
	}
	order := []int{
		18, 20, 52, 26, 30, 34, 58, 38, 40, 53, 42, 21, 27, 54, 55, 31, 35, 57,
		39, 41, 43, 22, 28, 32, 60, 36, 23, 29, 33, 37, 44, 45, 59, 46, 47, 48,
		49, 50, 24, 25, 65, 66, 70, 71,
	}
	plain := make([]uint16, 0, len(order)+len(douyinABogusWindow)+1)
	for _, key := range order {
		plain = append(plain, uint16(b[key]))
	}
	plain = append(plain, douyinCodeUnits(douyinABogusWindow)...)
	plain = append(plain, uint16(checksum))
	cipher := douyinRC4(plain, []uint16{121})
	prefix := make([]uint16, 0, 12)
	prefix = append(prefix, douyinABogusRandom(randomValues[0], 3, 45)...)
	prefix = append(prefix, douyinABogusRandom(randomValues[1], 1, 0)...)
	prefix = append(prefix, douyinABogusRandom(randomValues[2], 1, 5)...)
	return douyinABogusEncode(append(prefix, cipher...), douyinABogusAlphabetS4) + "="
}

func douyinABogusRandom(value, first, second int) []uint16 {
	low := value & 255
	high := value >> 8 & 255
	return []uint16{
		uint16(low&170 | first&85), uint16(low&85 | first&170),
		uint16(high&170 | second&85), uint16(high&85 | second&170),
	}
}

func douyinCodeUnits(value string) []uint16 {
	result := make([]uint16, len(value))
	for index := range value {
		result[index] = uint16(value[index])
	}
	return result
}

func douyinRC4(plain, key []uint16) []uint16 {
	state := make([]int, 256)
	for index := range state {
		state[index] = index
	}
	position := 0
	for index := range state {
		position = (position + state[index] + int(key[index%len(key)])) % 256
		state[index], state[position] = state[position], state[index]
	}
	result := make([]uint16, len(plain))
	left, right := 0, 0
	for index, value := range plain {
		left = (left + 1) % 256
		right = (right + state[left]) % 256
		state[left], state[right] = state[right], state[left]
		stream := state[(state[left]+state[right])%256]
		result[index] = uint16(int(value) ^ stream)
	}
	return result
}

func douyinABogusEncode(value []uint16, alphabet string) string {
	count := (len(value)*4 + 2) / 3
	result := make([]byte, 0, count)
	for index := 0; index < count; index++ {
		round := index / 4
		start := round * 3
		var first, second, third uint32
		if start < len(value) {
			first = uint32(value[start])
		}
		if start+1 < len(value) {
			second = uint32(value[start+1])
		}
		if start+2 < len(value) {
			third = uint32(value[start+2])
		}
		group := first<<16 | second<<8 | third
		shifts := [...]uint{18, 12, 6, 0}
		result = append(result, alphabet[group>>shifts[index%4]&63])
	}
	return string(result)
}

func douyinSM3Sum(input []byte) [32]byte {
	bitLength := uint64(len(input)) * 8
	message := append([]byte(nil), input...)
	message = append(message, 0x80)
	for len(message)%64 != 56 {
		message = append(message, 0)
	}
	lengthBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(lengthBytes, bitLength)
	message = append(message, lengthBytes...)

	state := [8]uint32{0x7380166f, 0x4914b2b9, 0x172442d7, 0xda8a0600, 0xa96f30bc, 0x163138aa, 0xe38dee4d, 0xb0fb0e4e}
	for offset := 0; offset < len(message); offset += 64 {
		block := message[offset : offset+64]
		var words [68]uint32
		var expanded [64]uint32
		for index := 0; index < 16; index++ {
			words[index] = binary.BigEndian.Uint32(block[index*4 : index*4+4])
		}
		for index := 16; index < 68; index++ {
			mixed := words[index-16] ^ words[index-9] ^ bits.RotateLeft32(words[index-3], 15)
			p1 := mixed ^ bits.RotateLeft32(mixed, 15) ^ bits.RotateLeft32(mixed, 23)
			words[index] = p1 ^ bits.RotateLeft32(words[index-13], 7) ^ words[index-6]
		}
		for index := 0; index < 64; index++ {
			expanded[index] = words[index] ^ words[index+4]
		}
		a, b, c, d := state[0], state[1], state[2], state[3]
		e, f, g, h := state[4], state[5], state[6], state[7]
		for index := 0; index < 64; index++ {
			constant := uint32(0x79cc4519)
			if index >= 16 {
				constant = 0x7a879d8a
			}
			ss1 := bits.RotateLeft32(bits.RotateLeft32(a, 12)+e+bits.RotateLeft32(constant, index), 7)
			ss2 := ss1 ^ bits.RotateLeft32(a, 12)
			ff, gg := a^b^c, e^f^g
			if index >= 16 {
				ff = a&b | a&c | b&c
				gg = e&f | ^e&g
			}
			tt1 := ff + d + ss2 + expanded[index]
			tt2 := gg + h + ss1 + words[index]
			d, c, b, a = c, bits.RotateLeft32(b, 9), a, tt1
			h, g, f = g, bits.RotateLeft32(f, 19), e
			e = tt2 ^ bits.RotateLeft32(tt2, 9) ^ bits.RotateLeft32(tt2, 17)
		}
		state[0] ^= a
		state[1] ^= b
		state[2] ^= c
		state[3] ^= d
		state[4] ^= e
		state[5] ^= f
		state[6] ^= g
		state[7] ^= h
	}
	var result [32]byte
	for index, value := range state {
		binary.BigEndian.PutUint32(result[index*4:index*4+4], value)
	}
	return result
}

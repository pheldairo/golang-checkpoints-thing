package protocol

const (
	crcInitial    = 0xffff
	crcPolynomial = 0xa001
)

func CRC16(data []byte) uint16 {
	crc := uint16(crcInitial)
	for _, value := range data {
		crc ^= uint16(value)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ crcPolynomial
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

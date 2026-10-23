package notify

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
)

var supportedOpusSampleRates = []int{8000, 12000, 16000, 24000, 48000}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// NormalizeOpusSampleRate 将采样率规整到 Opus 支持的标准采样率
func NormalizeOpusSampleRate(sampleRate int) int {
	if sampleRate <= 0 {
		return 16000
	}

	best := supportedOpusSampleRates[0]
	bestDistance := absInt(sampleRate - best)
	for _, candidate := range supportedOpusSampleRates[1:] {
		distance := absInt(sampleRate - candidate)
		if distance < bestDistance {
			best = candidate
			bestDistance = distance
		}
	}
	return best
}

// BuildOpusHeadPacket 构造 19 字节的 OpusHead 头部数据包
func BuildOpusHeadPacket(sampleRate int, channels int) []byte {
	var buf bytes.Buffer
	_, _ = buf.WriteString("OpusHead")
	_ = buf.WriteByte(1)                  // Version: 1
	_ = buf.WriteByte(byte(channels))     // Channel count: 1 (Mono)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0)) // Pre-skip: 0
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate)) // Sample rate
	_ = binary.Write(&buf, binary.LittleEndian, int16(0))  // Output gain: 0
	_ = buf.WriteByte(0)                  // Channel mapping family: 0
	return buf.Bytes()
}

// BuildOpusTagsPacket 构造 OpusTags 头部数据包
func BuildOpusTagsPacket() []byte {
	vendor := []byte("xiaozhi-server-golang")
	var buf bytes.Buffer
	_, _ = buf.WriteString("OpusTags")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(vendor)))
	_, _ = buf.Write(vendor)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0)) // 0 user comments
	return buf.Bytes()
}

// BuildOggPage 构造标准的 Ogg 页面 (OggS)
func BuildOggPage(serial uint32, sequence uint32, headerType byte, granulePosition uint64, packet []byte) []byte {
	segments := BuildOggSegments(len(packet))
	pageSize := 27 + len(segments) + len(packet)
	page := make([]byte, pageSize)

	copy(page[:4], []byte("OggS"))
	page[4] = 0 // Stream structure version
	page[5] = headerType // 0x02 = BOS, 0x00 = Normal, 0x04 = EOS
	binary.LittleEndian.PutUint64(page[6:14], granulePosition)
	binary.LittleEndian.PutUint32(page[14:18], serial)
	binary.LittleEndian.PutUint32(page[18:22], sequence)
	page[26] = byte(len(segments))
	copy(page[27:27+len(segments)], segments)
	copy(page[27+len(segments):], packet)

	// 计算并写入 CRC 校验码
	checksum := crc32.ChecksumIEEE(page)
	binary.LittleEndian.PutUint32(page[22:26], checksum)
	return page
}

// BuildOggSegments 将 Packet 长度切分为 lacing values (每段最大 255 字节)
func BuildOggSegments(packetLen int) []byte {
	if packetLen <= 0 {
		return []byte{0}
	}

	segments := make([]byte, 0, packetLen/255+1)
	remaining := packetLen
	for remaining >= 255 {
		segments = append(segments, 255)
		remaining -= 255
	}
	segments = append(segments, byte(remaining))
	return segments
}

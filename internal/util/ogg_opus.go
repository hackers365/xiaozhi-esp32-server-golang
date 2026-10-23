package util

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"

	"gopkg.in/hraban/opus.v2"
)

var supportedOpusSampleRates = []int{8000, 12000, 16000, 24000, 48000}

// NormalizeOpusSampleRate 将采样率规整到 Opus 支持的标准采样率。
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

// PCM16ToOggOpus 将 PCM16 数据编码为 Ogg/Opus。
func PCM16ToOggOpus(samples []int16, sampleRate int, channels int, frameDurationMs int) ([]byte, error) {
	if channels < 1 || channels > 2 {
		return nil, fmt.Errorf("Opus 仅支持 1 或 2 声道，当前: %d", channels)
	}

	sampleRate = NormalizeOpusSampleRate(sampleRate)
	if frameDurationMs <= 0 {
		frameDurationMs = 20
	}

	frameSizePerChannel := sampleRate * frameDurationMs / 1000
	if frameSizePerChannel <= 0 {
		return nil, fmt.Errorf("无效的 Opus 帧时长: %d ms", frameDurationMs)
	}

	frameSize := frameSizePerChannel * channels
	encoder, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil {
		return nil, fmt.Errorf("创建 Opus 编码器失败: %v", err)
	}

	packets := make([][]byte, 0, len(samples)/maxInt(frameSize, 1)+1)
	opusBuffer := make([]byte, 4000)

	for offset := 0; offset < len(samples); offset += frameSize {
		frame := make([]int16, frameSize)
		end := offset + frameSize
		if end > len(samples) {
			end = len(samples)
		}
		copy(frame, samples[offset:end])

		n, err := encoder.Encode(frame, opusBuffer)
		if err != nil {
			return nil, fmt.Errorf("Opus 编码失败: %v", err)
		}
		packet := make([]byte, n)
		copy(packet, opusBuffer[:n])
		packets = append(packets, packet)
	}

	return WrapOggOpusPackets(packets, sampleRate, channels, frameSizePerChannel), nil
}

// WrapOggOpusPackets 将原始 Opus packet 包装为 Ogg/Opus 数据流。
func WrapOggOpusPackets(packets [][]byte, sampleRate int, channels int, frameSizePerChannel int) []byte {
	var out bytes.Buffer
	const serial = uint32(0x58495a48)

	_, _ = out.Write(BuildOggPage(serial, 0, 0x02, 0, BuildOpusHeadPacket(sampleRate, channels)))
	_, _ = out.Write(BuildOggPage(serial, 1, 0x00, 0, BuildOpusTagsPacket()))

	var granulePosition uint64
	for i, packet := range packets {
		granulePosition += uint64(frameSizePerChannel)
		headerType := byte(0)
		if i == len(packets)-1 {
			headerType = 0x04
		}
		_, _ = out.Write(BuildOggPage(serial, uint32(i+2), headerType, granulePosition, packet))
	}

	return out.Bytes()
}

func BuildOpusHeadPacket(sampleRate int, channels int) []byte {
	var buf bytes.Buffer
	_, _ = buf.WriteString("OpusHead")
	_ = buf.WriteByte(1)
	_ = buf.WriteByte(byte(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, int16(0))
	_ = buf.WriteByte(0)
	return buf.Bytes()
}

func BuildOpusTagsPacket() []byte {
	vendor := []byte("xiaozhi-server-golang")
	var buf bytes.Buffer
	_, _ = buf.WriteString("OpusTags")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(vendor)))
	_, _ = buf.Write(vendor)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
	return buf.Bytes()
}

func BuildOggPage(serial uint32, sequence uint32, headerType byte, granulePosition uint64, packet []byte) []byte {
	segments := BuildOggSegments(len(packet))
	pageSize := 27 + len(segments) + len(packet)
	page := make([]byte, pageSize)

	copy(page[:4], []byte("OggS"))
	page[4] = 0
	page[5] = headerType
	binary.LittleEndian.PutUint64(page[6:14], granulePosition)
	binary.LittleEndian.PutUint32(page[14:18], serial)
	binary.LittleEndian.PutUint32(page[18:22], sequence)
	page[26] = byte(len(segments))
	copy(page[27:27+len(segments)], segments)
	copy(page[27+len(segments):], packet)

	checksum := crc32.ChecksumIEEE(page)
	binary.LittleEndian.PutUint32(page[22:26], checksum)
	return page
}

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

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

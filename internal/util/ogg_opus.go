package util

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"gopkg.in/hraban/opus.v2"
)

var supportedOpusSampleRates = []int{8000, 12000, 16000, 24000, 48000}

// oggCRCTable 为 RFC 3533 Section 6 定义的 Ogg 非反射直接 CRC-32 查表（生成多项式 0x04C11DB7，初值 0，无末端异或）。
var oggCRCTable = func() [256]uint32 {
	var table [256]uint32
	const poly = uint32(0x04c11db7)
	for i := 0; i < 256; i++ {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if (r & 0x80000000) != 0 {
				r = (r << 1) ^ poly
			} else {
				r <<= 1
			}
		}
		table[i] = r
	}
	return table
}()

// OggCRC32 计算符合 RFC 3533 规范的 Ogg 页面 CRC-32 校验和。
func OggCRC32(data []byte) uint32 {
	var crc uint32
	for _, b := range data {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	return crc
}

// OpusPacketSamples48k 按 RFC 7845 / RFC 6716 计算单个 Opus packet 在固定 48kHz 时钟基准下的采样数。
// 若 packet 为空或无法解析出合法帧数，则回退为 fallbackDurationMs 对应的 48kHz 采样数。
func OpusPacketSamples48k(packet []byte, fallbackDurationMs int) uint64 {
	if len(packet) > 0 {
		if frameCount, err := opusPacketFrameCount(packet); err == nil && frameCount > 0 {
			samplesPerFrame := opusPacketSamplesPerFrame(packet[0], 48000)
			total := samplesPerFrame * frameCount
			// RFC 6716: 单个 Opus packet 总时长不超过 120ms (5760 samples @ 48kHz)
			if total > 0 && total <= 5760 {
				return uint64(total)
			}
		}
	}
	if fallbackDurationMs <= 0 {
		fallbackDurationMs = 60
	}
	samples := 48000 * fallbackDurationMs / 1000
	if samples <= 0 {
		samples = 960
	}
	return uint64(samples)
}

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

// WrapOggOpusPackets 将原始 Opus packet 包装为符合 RFC 3533 / RFC 7845 的 Ogg/Opus 数据流。
func WrapOggOpusPackets(packets [][]byte, sampleRate int, channels int, frameSizePerChannel int) []byte {
	var out bytes.Buffer
	const serial = uint32(0x58495a48)

	fallbackDurationMs := 20
	if sampleRate > 0 && frameSizePerChannel > 0 {
		if d := frameSizePerChannel * 1000 / sampleRate; d > 0 {
			fallbackDurationMs = d
		}
	}

	var sequence uint32
	pages, nextSeq := BuildOggPages(serial, sequence, 0x02, 0, BuildOpusHeadPacket(sampleRate, channels))
	_, _ = out.Write(pages)
	sequence = nextSeq

	pages, nextSeq = BuildOggPages(serial, sequence, 0x00, 0, BuildOpusTagsPacket())
	_, _ = out.Write(pages)
	sequence = nextSeq

	var granulePosition uint64
	for i, packet := range packets {
		granulePosition += OpusPacketSamples48k(packet, fallbackDurationMs)
		headerType := byte(0)
		if i == len(packets)-1 {
			headerType = 0x04
		}
		pages, nextSeq = BuildOggPages(serial, sequence, headerType, granulePosition, packet)
		_, _ = out.Write(pages)
		sequence = nextSeq
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

// BuildOggPage 构造标准的 Ogg 页面 (OggS)。若单个 packet 超过 255 个 segment，会自动拆分为连续页。
func BuildOggPage(serial uint32, sequence uint32, headerType byte, granulePosition uint64, packet []byte) []byte {
	pages, _ := BuildOggPages(serial, sequence, headerType, granulePosition, packet)
	return pages
}

// BuildOggPages 构造一个或多个 Ogg 页面并返回下一页面的 sequence 序号。
func BuildOggPages(serial uint32, sequence uint32, headerType byte, granulePosition uint64, packet []byte) ([]byte, uint32) {
	segments := BuildOggSegments(len(packet))
	if len(segments) <= 255 {
		return buildSingleOggPage(serial, sequence, headerType, granulePosition, segments, packet), sequence + 1
	}

	var out bytes.Buffer
	payloadOffset := 0
	for segOffset := 0; segOffset < len(segments); segOffset += 255 {
		segEnd := segOffset + 255
		if segEnd > len(segments) {
			segEnd = len(segments)
		}
		segChunk := segments[segOffset:segEnd]
		chunkPayloadLen := 0
		for _, segLen := range segChunk {
			chunkPayloadLen += int(segLen)
		}
		payloadChunk := packet[payloadOffset : payloadOffset+chunkPayloadLen]
		payloadOffset += chunkPayloadLen

		isFirst := segOffset == 0
		isLast := segEnd == len(segments)

		pageFlags := headerType
		if !isFirst {
			pageFlags = (pageFlags &^ 0x02) | 0x01 // continuation page, clear BOS
		}
		pageGranule := granulePosition
		if !isLast {
			pageFlags &^= 0x04 // clear EOS until final page
			pageGranule = ^uint64(0)
		}

		_, _ = out.Write(buildSingleOggPage(serial, sequence, pageFlags, pageGranule, segChunk, payloadChunk))
		sequence++
	}
	return out.Bytes(), sequence
}

func buildSingleOggPage(serial uint32, sequence uint32, headerType byte, granulePosition uint64, segments []byte, payload []byte) []byte {
	pageSize := 27 + len(segments) + len(payload)
	page := make([]byte, pageSize)

	copy(page[:4], []byte("OggS"))
	page[4] = 0
	page[5] = headerType
	binary.LittleEndian.PutUint64(page[6:14], granulePosition)
	binary.LittleEndian.PutUint32(page[14:18], serial)
	binary.LittleEndian.PutUint32(page[18:22], sequence)
	// page[22:26] 初始为 0，满足 RFC 3533 计算 CRC 前校验字段清零的要求
	page[26] = byte(len(segments))
	copy(page[27:27+len(segments)], segments)
	copy(page[27+len(segments):], payload)

	checksum := OggCRC32(page)
	binary.LittleEndian.PutUint32(page[22:26], checksum)
	return page
}

// BuildOggSegments 将 Packet 长度切分为 lacing values (每段最大 255 字节)。
// 当 packetLen <= 0 时返回空切片（page_segments = 0），表示不包含数据包的空页面。
func BuildOggSegments(packetLen int) []byte {
	if packetLen <= 0 {
		return nil
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

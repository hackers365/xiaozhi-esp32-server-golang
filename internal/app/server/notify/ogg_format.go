package notify

import "xiaozhi-esp32-server-golang/internal/util"

// OggCRC32 计算符合 RFC 3533 规范的 Ogg 页面 CRC-32 校验和
func OggCRC32(data []byte) uint32 {
	return util.OggCRC32(data)
}

// OpusPacketSamples48k 按 RFC 7845 / RFC 6716 计算单个 Opus packet 在 48kHz 时钟基准下的采样数
func OpusPacketSamples48k(packet []byte, fallbackDurationMs int) uint64 {
	return util.OpusPacketSamples48k(packet, fallbackDurationMs)
}

// NormalizeOpusSampleRate 将采样率规整到 Opus 支持的标准采样率
func NormalizeOpusSampleRate(sampleRate int) int {
	return util.NormalizeOpusSampleRate(sampleRate)
}

// BuildOpusHeadPacket 构造 19 字节的 OpusHead 头部数据包
func BuildOpusHeadPacket(sampleRate int, channels int) []byte {
	return util.BuildOpusHeadPacket(sampleRate, channels)
}

// BuildOpusTagsPacket 构造 OpusTags 头部数据包
func BuildOpusTagsPacket() []byte {
	return util.BuildOpusTagsPacket()
}

// BuildOggPage 构造标准的 Ogg 页面 (OggS)
func BuildOggPage(serial uint32, sequence uint32, headerType byte, granulePosition uint64, packet []byte) []byte {
	return util.BuildOggPage(serial, sequence, headerType, granulePosition, packet)
}

// BuildOggPages 构造一个或多个 Ogg 页面并返回下一页面序号
func BuildOggPages(serial uint32, sequence uint32, headerType byte, granulePosition uint64, packet []byte) ([]byte, uint32) {
	return util.BuildOggPages(serial, sequence, headerType, granulePosition, packet)
}

// BuildOggSegments 将 Packet 长度切分为 lacing values (每段最大 255 字节)
func BuildOggSegments(packetLen int) []byte {
	return util.BuildOggSegments(packetLen)
}

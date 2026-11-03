package notify

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"

	log "xiaozhi-esp32-server-golang/logger"
)

// StreamOpusToOgg 将 Opus 帧数据通道流式封装为 Ogg Opus 格式并写入 io.Writer
func StreamOpusToOgg(ctx context.Context, w io.Writer, flusher http.Flusher, opusChan <-chan []byte, sampleRate int, channels int, frameDurationMs int) error {
	return streamOpusToOggWithFirstPacket(ctx, w, flusher, nil, opusChan, sampleRate, channels, frameDurationMs)
}

// streamOpusToOggWithFirstPacket 支持传入已预取的首帧 Opus 数据并继续消费 opusChan，
// 使用单帧延迟写出（1-packet lookahead）直接将最后一帧音频页标记为 EOS (0x04)，
// 按 RFC 7845 固定 48kHz 时钟计算 Granule Position。
func streamOpusToOggWithFirstPacket(
	ctx context.Context,
	w io.Writer,
	flusher http.Flusher,
	firstPacket []byte,
	opusChan <-chan []byte,
	sampleRate int,
	channels int,
	frameDurationMs int,
) error {
	sampleRate = NormalizeOpusSampleRate(sampleRate)
	if channels < 1 || channels > 2 {
		channels = 1
	}
	if frameDurationMs <= 0 {
		frameDurationMs = 60
	}

	// 随机生成 32 位 Stream Serial Number
	var serial uint32
	if err := binary.Read(rand.Reader, binary.LittleEndian, &serial); err != nil {
		serial = 0x58495a48 // 'XIZH'
	}

	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	var sequence uint32

	// 1. Page 0: OpusHead (BOS, headerType = 0x02, granule = 0)
	headPage, nextSeq := BuildOggPages(serial, sequence, 0x02, 0, BuildOpusHeadPacket(sampleRate, channels))
	if _, err := w.Write(headPage); err != nil {
		return fmt.Errorf("write OpusHead page failed: %w", err)
	}
	sequence = nextSeq
	flush()

	// 2. Page 1: OpusTags (headerType = 0x00, granule = 0)
	tagsPage, nextSeq := BuildOggPages(serial, sequence, 0x00, 0, BuildOpusTagsPacket())
	if _, err := w.Write(tagsPage); err != nil {
		return fmt.Errorf("write OpusTags page failed: %w", err)
	}
	sequence = nextSeq
	flush()

	// 3. 循环流式读取 Opus 帧并写入 Ogg Page（48kHz Granule Position + 尾帧直接打 EOS）
	var granulePosition uint64
	var prevPacket []byte
	packetCount := 0

	if len(firstPacket) > 0 {
		granulePosition += OpusPacketSamples48k(firstPacket, frameDurationMs)
		prevPacket = firstPacket
		packetCount = 1
	}

	for {
		select {
		case <-ctx.Done():
			log.Debugf("StreamOpusToOgg: context cancelled, sent %d packets", packetCount)
			return ctx.Err()
		case packet, ok := <-opusChan:
			if !ok {
				if prevPacket != nil {
					// 最后一包直接标记为 EOS (0x04)
					eosPage, _ := BuildOggPages(serial, sequence, 0x04, granulePosition, prevPacket)
					if _, err := w.Write(eosPage); err != nil {
						return fmt.Errorf("write EOS page failed: %w", err)
					}
					flush()
				} else {
					// 直接调用且无任何音频帧时，发送 page_segments=0 的标准空 EOS 页
					eosPage := BuildOggPage(serial, sequence, 0x04, 0, nil)
					if _, err := w.Write(eosPage); err != nil {
						return fmt.Errorf("write empty EOS page failed: %w", err)
					}
					flush()
				}
				log.Debugf("StreamOpusToOgg: completed successfully, total %d packets", packetCount)
				return nil
			}

			if len(packet) == 0 {
				continue
			}

			if prevPacket != nil {
				page, nextSeq := BuildOggPages(serial, sequence, 0x00, granulePosition, prevPacket)
				if _, err := w.Write(page); err != nil {
					return fmt.Errorf("write audio page %d failed: %w", sequence, err)
				}
				sequence = nextSeq
				flush()
			}

			granulePosition += OpusPacketSamples48k(packet, frameDurationMs)
			prevPacket = packet
			packetCount++
		}
	}
}

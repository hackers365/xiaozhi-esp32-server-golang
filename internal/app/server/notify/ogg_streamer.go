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
	sampleRate = NormalizeOpusSampleRate(sampleRate)
	if channels < 1 || channels > 2 {
		channels = 1
	}
	if frameDurationMs <= 0 {
		frameDurationMs = 60
	}

	frameSamplesPerChannel := sampleRate * frameDurationMs / 1000
	if frameSamplesPerChannel <= 0 {
		frameSamplesPerChannel = 960
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

	// 1. Page 0: OpusHead (BOS, headerType = 0x02)
	headPage := BuildOggPage(serial, 0, 0x02, 0, BuildOpusHeadPacket(sampleRate, channels))
	if _, err := w.Write(headPage); err != nil {
		return fmt.Errorf("write OpusHead page failed: %w", err)
	}
	flush()

	// 2. Page 1: OpusTags (headerType = 0x00)
	tagsPage := BuildOggPage(serial, 1, 0x00, 0, BuildOpusTagsPacket())
	if _, err := w.Write(tagsPage); err != nil {
		return fmt.Errorf("write OpusTags page failed: %w", err)
	}
	flush()

	// 3. 循环流式读取 Opus 帧并写入 Ogg Page
	var sequence uint32 = 2
	var granulePosition uint64 = 0
	var prevPacket []byte
	packetCount := 0

	for {
		select {
		case <-ctx.Done():
			log.Debugf("StreamOpusToOgg: context cancelled, sent %d packets", packetCount)
			return ctx.Err()
		case packet, ok := <-opusChan:
			if !ok {
				// opusChan 关闭，准备写入 EOS Page
				if prevPacket != nil {
					// 最后一包标记为 EOS (0x04)
					eosPage := BuildOggPage(serial, sequence, 0x04, granulePosition, prevPacket)
					if _, err := w.Write(eosPage); err != nil {
						return fmt.Errorf("write EOS page failed: %w", err)
					}
					flush()
				} else {
					// 没有音频数据，发送空 EOS page
					eosPage := BuildOggPage(serial, sequence, 0x04, granulePosition, nil)
					_, _ = w.Write(eosPage)
					flush()
				}
				log.Debugf("StreamOpusToOgg: completed successfully, total %d packets", packetCount)
				return nil
			}

			if len(packet) == 0 {
				continue
			}

			if prevPacket != nil {
				// 输出前一个普通帧
				page := BuildOggPage(serial, sequence, 0x00, granulePosition, prevPacket)
				if _, err := w.Write(page); err != nil {
					return fmt.Errorf("write audio page %d failed: %w", sequence, err)
				}
				flush()
				sequence++
			}

			granulePosition += uint64(frameSamplesPerChannel)
			prevPacket = packet
			packetCount++
		}
	}
}

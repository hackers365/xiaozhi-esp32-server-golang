package notify

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

func parseAndVerifyOggStream(t *testing.T, data []byte) []struct {
	headerType byte
	granule    uint64
	sequence   uint32
	segCount   int
	body       []byte
} {
	t.Helper()
	var pages []struct {
		headerType byte
		granule    uint64
		sequence   uint32
		segCount   int
		body       []byte
	}
	offset := 0
	for offset < len(data) {
		if len(data)-offset < 27 {
			t.Fatalf("truncated Ogg page header at %d", offset)
		}
		if !bytes.Equal(data[offset:offset+4], []byte("OggS")) {
			t.Fatalf("missing OggS magic at %d", offset)
		}
		segCount := int(data[offset+26])
		headerLen := 27 + segCount
		bodyLen := 0
		for _, seg := range data[offset+27 : offset+headerLen] {
			bodyLen += int(seg)
		}
		pageLen := headerLen + bodyLen
		rawPage := append([]byte(nil), data[offset:offset+pageLen]...)
		gotCRC := binary.LittleEndian.Uint32(rawPage[22:26])
		binary.LittleEndian.PutUint32(rawPage[22:26], 0)
		wantCRC := OggCRC32(rawPage)
		if gotCRC != wantCRC {
			t.Fatalf("CRC mismatch on page %d: got 0x%08x want 0x%08x", len(pages), gotCRC, wantCRC)
		}
		pages = append(pages, struct {
			headerType byte
			granule    uint64
			sequence   uint32
			segCount   int
			body       []byte
		}{
			headerType: data[offset+5],
			granule:    binary.LittleEndian.Uint64(data[offset+6 : offset+14]),
			sequence:   binary.LittleEndian.Uint32(data[offset+18 : offset+22]),
			segCount:   segCount,
			body:       append([]byte(nil), data[offset+headerLen:offset+pageLen]...),
		})
		offset += pageLen
	}
	return pages
}

func TestStreamOpusToOgg(t *testing.T) {
	var buf bytes.Buffer
	opusChan := make(chan []byte, 10)

	// 注入 3 个模拟 60ms SILK WB Opus 帧 (0x58: config 11 = SILK WB 60ms -> 2880 samples @ 48kHz)
	dummyFrame1 := []byte{0x58, 0xff, 0xfe}
	dummyFrame2 := []byte{0x58, 0x12, 0x34}
	dummyFrame3 := []byte{0x58, 0x56, 0x78}

	go func() {
		opusChan <- dummyFrame1
		opusChan <- dummyFrame2
		opusChan <- dummyFrame3
		close(opusChan)
	}()

	err := StreamOpusToOgg(context.Background(), &buf, nil, opusChan, 16000, 1, 60)
	if err != nil {
		t.Fatalf("StreamOpusToOgg failed: %v", err)
	}

	pages := parseAndVerifyOggStream(t, buf.Bytes())
	// Page 0: OpusHead (BOS), Page 1: OpusTags, Page 2: frame1, Page 3: frame2, Page 4: frame3 (EOS)
	if len(pages) != 5 {
		t.Fatalf("expected 5 Ogg pages without extra trailing empty EOS page, got %d", len(pages))
	}
	if pages[0].headerType != 0x02 || !bytes.HasPrefix(pages[0].body, []byte("OpusHead")) {
		t.Fatalf("invalid page 0: headerType=0x%02x", pages[0].headerType)
	}
	if pages[1].headerType != 0x00 || !bytes.HasPrefix(pages[1].body, []byte("OpusTags")) {
		t.Fatalf("invalid page 1: headerType=0x%02x", pages[1].headerType)
	}
	if pages[2].granule != 2880 || pages[2].headerType != 0x00 {
		t.Fatalf("page 2 expected granule=2880 header=0x00, got granule=%d header=0x%02x", pages[2].granule, pages[2].headerType)
	}
	if pages[3].granule != 5760 || pages[3].headerType != 0x00 {
		t.Fatalf("page 3 expected granule=5760 header=0x00, got granule=%d header=0x%02x", pages[3].granule, pages[3].headerType)
	}
	if pages[4].granule != 8640 || pages[4].headerType != 0x04 || !bytes.Equal(pages[4].body, dummyFrame3) {
		t.Fatalf("page 4 expected EOS with frame3 and granule=8640, got granule=%d header=0x%02x", pages[4].granule, pages[4].headerType)
	}
}

func TestStreamOpusToOggEmpty(t *testing.T) {
	var buf bytes.Buffer
	opusChan := make(chan []byte)
	close(opusChan)

	err := StreamOpusToOgg(context.Background(), &buf, nil, opusChan, 16000, 1, 60)
	if err != nil {
		t.Fatalf("StreamOpusToOgg with empty channel failed: %v", err)
	}

	pages := parseAndVerifyOggStream(t, buf.Bytes())
	if len(pages) != 3 {
		t.Fatalf("expected 3 pages (OpusHead, OpusTags, empty EOS), got %d", len(pages))
	}
	// 空 EOS 页必须为 page_segments = 0，确保 ESP32 OggDemuxer 回到 FIND_PAGE 状态顺利通过 Finish()
	if pages[2].headerType != 0x04 || pages[2].segCount != 0 || len(pages[2].body) != 0 {
		t.Fatalf("expected 0-segment empty EOS page, got header=0x%02x segCount=%d bodyLen=%d",
			pages[2].headerType, pages[2].segCount, len(pages[2].body))
	}
}

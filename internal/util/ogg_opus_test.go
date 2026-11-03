package util

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
)

// oggCRC32Bitwise 按 RFC 3533 Section 6 逐位计算直接 CRC-32（无查表参考实现），用于交叉验证查表实现。
func oggCRC32Bitwise(data []byte) uint32 {
	const poly = uint32(0x04c11db7)
	var crc uint32
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if (crc & 0x80000000) != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func TestOggCRC32MatchesRFC3533(t *testing.T) {
	sample := []byte("123456789")
	got := OggCRC32(sample)
	want := oggCRC32Bitwise(sample)
	if got != want {
		t.Fatalf("OggCRC32 mismatch: got 0x%08x, want 0x%08x", got, want)
	}
	// RFC 3533 Ogg CRC-32 ("123456789") 应为 0x89A1897F，绝不能等于 IEEE CRC-32 (0xCBF43926)
	if got != 0x89a1897f {
		t.Fatalf("unexpected RFC 3533 check value: got 0x%08x, want 0x89a1897f", got)
	}
	if got == crc32.ChecksumIEEE(sample) {
		t.Fatal("OggCRC32 must not use reflected IEEE CRC-32")
	}

	headPage := BuildOggPage(0x58495a48, 0, 0x02, 0, BuildOpusHeadPacket(16000, 1))
	storedCRC := binary.LittleEndian.Uint32(headPage[22:26])
	verifyBuf := append([]byte(nil), headPage...)
	binary.LittleEndian.PutUint32(verifyBuf[22:26], 0)
	if expected := oggCRC32Bitwise(verifyBuf); storedCRC != expected {
		t.Fatalf("page CRC mismatch: stored=0x%08x expected=0x%08x", storedCRC, expected)
	}
}

func TestOpusPacketSamples48kAndGranulePosition(t *testing.T) {
	// 0x58: SILK WB 60ms, 1 frame -> 2880 samples @ 48kHz (即便输入采样率是 16kHz 也必须为 2880)
	silk60ms := []byte{0x58, 0x11, 0x22}
	if got := OpusPacketSamples48k(silk60ms, 60); got != 2880 {
		t.Fatalf("SILK 60ms expected 2880 samples @48kHz, got %d", got)
	}

	// 0xf8: CELT FB 20ms, 1 frame -> 960 samples @ 48kHz
	celt20ms := []byte{0xf8, 0x11, 0x22}
	if got := OpusPacketSamples48k(celt20ms, 60); got != 960 {
		t.Fatalf("CELT 20ms expected 960 samples @48kHz, got %d", got)
	}

	// 0x4b, 0x03: SILK WB 20ms, code 3 with 3 frames -> 3 * 960 = 2880 samples @ 48kHz
	multiFrame60ms := []byte{0x4b, 0x03, 0xaa, 0xbb}
	if got := OpusPacketSamples48k(multiFrame60ms, 20); got != 2880 {
		t.Fatalf("3x20ms packet expected 2880 samples @48kHz, got %d", got)
	}

	// 验证 WrapOggOpusPackets 在 16kHz 输入下依然以 48kHz 时钟递增 Granule Position
	oggBytes := WrapOggOpusPackets([][]byte{silk60ms, silk60ms}, 16000, 1, 960)
	pages := mustParseOggPages(t, oggBytes)
	if len(pages) != 4 {
		t.Fatalf("expected 4 pages (OpusHead, OpusTags, 2 audio pages), got %d", len(pages))
	}
	if pages[2].Granule != 2880 {
		t.Fatalf("audio page 1 granule expected 2880, got %d", pages[2].Granule)
	}
	if pages[3].Granule != 5760 || pages[3].HeaderType != 0x04 {
		t.Fatalf("audio page 2 expected granule=5760 headerType=0x04, got granule=%d headerType=0x%02x", pages[3].Granule, pages[3].HeaderType)
	}
}

func TestBuildOggPageEmptyPacketHasZeroSegments(t *testing.T) {
	emptyEOS := BuildOggPage(0x12345678, 2, 0x04, 0, nil)
	if len(emptyEOS) != 27 {
		t.Fatalf("expected 27-byte empty Ogg page, got %d bytes", len(emptyEOS))
	}
	if emptyEOS[26] != 0 {
		t.Fatalf("expected page_segments=0 on empty page, got %d", emptyEOS[26])
	}
}

type parsedTestOggPage struct {
	HeaderType byte
	Granule    uint64
	Serial     uint32
	Sequence   uint32
	Segments   []byte
	Body       []byte
}

func mustParseOggPages(t *testing.T, raw []byte) []parsedTestOggPage {
	t.Helper()
	var pages []parsedTestOggPage
	offset := 0
	for offset < len(raw) {
		if len(raw)-offset < 27 {
			t.Fatalf("truncated Ogg header at offset %d", offset)
		}
		if !bytes.Equal(raw[offset:offset+4], []byte("OggS")) {
			t.Fatalf("invalid OggS magic at offset %d", offset)
		}
		segCount := int(raw[offset+26])
		headerLen := 27 + segCount
		if len(raw)-offset < headerLen {
			t.Fatalf("truncated segment table at offset %d", offset)
		}
		segments := append([]byte(nil), raw[offset+27:offset+headerLen]...)
		bodyLen := 0
		for _, s := range segments {
			bodyLen += int(s)
		}
		pageLen := headerLen + bodyLen
		if len(raw)-offset < pageLen {
			t.Fatalf("truncated page body at offset %d", offset)
		}

		pageBytes := append([]byte(nil), raw[offset:offset+pageLen]...)
		gotCRC := binary.LittleEndian.Uint32(pageBytes[22:26])
		binary.LittleEndian.PutUint32(pageBytes[22:26], 0)
		wantCRC := oggCRC32Bitwise(pageBytes)
		if gotCRC != wantCRC {
			t.Fatalf("page %d CRC mismatch: got 0x%08x want 0x%08x", len(pages), gotCRC, wantCRC)
		}

		pages = append(pages, parsedTestOggPage{
			HeaderType: raw[offset+5],
			Granule:    binary.LittleEndian.Uint64(raw[offset+6 : offset+14]),
			Serial:     binary.LittleEndian.Uint32(raw[offset+14 : offset+18]),
			Sequence:   binary.LittleEndian.Uint32(raw[offset+18 : offset+22]),
			Segments:   segments,
			Body:       append([]byte(nil), raw[offset+headerLen:offset+pageLen]...),
		})
		offset += pageLen
	}
	return pages
}

package notify

import (
	"bytes"
	"context"
	"testing"
)

func TestStreamOpusToOgg(t *testing.T) {
	var buf bytes.Buffer
	opusChan := make(chan []byte, 10)

	// 注入 3 个模拟 Opus 帧
	dummyFrame1 := []byte{0xf8, 0xff, 0xfe} // 模拟 TOC
	dummyFrame2 := []byte{0xf8, 0x12, 0x34}
	dummyFrame3 := []byte{0xf8, 0x56, 0x78}

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

	data := buf.Bytes()
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}

	// 验证包含 OggS 标识
	if !bytes.HasPrefix(data, []byte("OggS")) {
		t.Fatal("expected output to start with OggS")
	}

	// 验证包含 OpusHead
	if !bytes.Contains(data, []byte("OpusHead")) {
		t.Fatal("expected output to contain OpusHead")
	}

	// 验证包含 OpusTags
	if !bytes.Contains(data, []byte("OpusTags")) {
		t.Fatal("expected output to contain OpusTags")
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

	data := buf.Bytes()
	if !bytes.HasPrefix(data, []byte("OggS")) {
		t.Fatal("expected empty stream to still output valid Ogg header")
	}
}

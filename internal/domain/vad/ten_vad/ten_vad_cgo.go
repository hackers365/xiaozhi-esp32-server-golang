//go:build cgo

package ten_vad

// #cgo LDFLAGS: -lm
// #cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../../../../lib/ten-vad/lib/Windows/x64 -lten_vad
// #cgo linux,amd64   LDFLAGS: -L${SRCDIR}/../../../../lib/ten-vad/lib/Linux/x64 -lten_vad -lc++ -lc++abi
// #cgo darwin,amd64  LDFLAGS: -F${SRCDIR}/../../../../lib/ten-vad/lib/macOS -framework ten_vad -Wl,-rpath,${SRCDIR}/../../../../lib/ten-vad/lib/macOS
// #cgo darwin,arm64  LDFLAGS: -F${SRCDIR}/../../../../lib/ten-vad/lib/macOS -framework ten_vad -Wl,-rpath,${SRCDIR}/../../../../lib/ten-vad/lib/macOS
// #cgo windows,amd64 CFLAGS:  -I${SRCDIR}/../../../../lib/ten-vad/include
// #cgo linux,amd64   CFLAGS:  -I${SRCDIR}/../../../../lib/ten-vad/include
// #cgo darwin,amd64  CFLAGS:  -I${SRCDIR}/../../../../lib/ten-vad/include -F${SRCDIR}/../../../../lib/ten-vad/lib/macOS
// #cgo darwin,arm64  CFLAGS:  -I${SRCDIR}/../../../../lib/ten-vad/include -F${SRCDIR}/../../../../lib/ten-vad/lib/macOS
// #include "ten_vad.h"
import "C"
import (
	"errors"
	"sync"
	"unsafe"
)

// TenVADDLL TEN-VAD的动态库绑定
type TenVADDLL struct{}

// 全局单例
var (
	globalTenVAD *TenVADDLL
	dllOnce      sync.Once
)

// GetInstance 创建并返回 TEN-VAD 动态库单例
func GetInstance() *TenVADDLL {
	dllOnce.Do(func() {
		globalTenVAD = &TenVADDLL{}
	})
	return globalTenVAD
}

// CreateInstance 创建TEN-VAD实例（共享模型）
func (t *TenVADDLL) CreateInstance(hopSize int, threshold float32) (unsafe.Pointer, error) {
	var handle C.ten_vad_handle_t
	ret := C.ten_vad_create(&handle, C.size_t(hopSize), C.float(threshold))
	if ret != 0 {
		return nil, errors.New("failed to create ten-vad instance (C.ten_vad_create returned non-zero)")
	}
	return unsafe.Pointer(handle), nil
}

// ProcessAudio 处理音频数据
func (t *TenVADDLL) ProcessAudio(handle unsafe.Pointer, audioData []int16) (float32, int32, error) {
	if handle == nil {
		return 0, 0, errors.New("nil handle for ten-vad process")
	}
	if len(audioData) == 0 {
		return 0, 0, errors.New("empty audio data for ten-vad process")
	}
	var prob C.float
	var flag C.int
	ret := C.ten_vad_process(
		C.ten_vad_handle_t(handle),
		(*C.int16_t)(unsafe.Pointer(&audioData[0])),
		C.size_t(len(audioData)),
		&prob,
		&flag,
	)
	if ret != 0 {
		return 0, 0, errors.New("ten-vad process failed (C.ten_vad_process != 0)")
	}
	return float32(prob), int32(flag), nil
}

// DestroyInstance 销毁TEN-VAD实例
func (t *TenVADDLL) DestroyInstance(handle unsafe.Pointer) error {
	if handle == nil {
		return errors.New("nil handle for destroy")
	}
	h := C.ten_vad_handle_t(handle)
	ret := C.ten_vad_destroy(&h)
	if ret != 0 {
		return errors.New("ten-vad destroy failed")
	}
	return nil
}

// GetVersion 获取TEN-VAD版本
func (t *TenVADDLL) GetVersion() string {
	ver := C.ten_vad_get_version()
	return C.GoString(ver)
}

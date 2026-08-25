package controllers

import (
	"strings"

	"xiaozhi-esp32-server-golang/pkg/aliyuntts"
)

// qwenTTSOfficialPreviewURLs comes from Aliyun's Qwen-TTS system voice page.
// These samples belong to Qwen-TTS only and must never be used for Qwen-Audio-TTS.
var qwenTTSOfficialPreviewURLs = map[string]string{
	"aiden":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/hgxtqi/Aiden.wav",
	"alek":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/wtklus/Alek.wav",
	"andre":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/hhfogy/Andre.wav",
	"arthur":      "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/ynqwyu/Arthur.wav",
	"bella":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/optibu/Bella.wav",
	"bellona":     "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/wztwli/Bellona.wav",
	"bodega":      "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/jxnuap/Bodega.wav",
	"bunny":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/aswewm/Bunny.wav",
	"chelsie":     "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250211/vnpxgw/chelsie.wav",
	"cherry":      "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250211/tixcef/cherry.wav",
	"dolce":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/pirhim/Dolce.wav",
	"dylan":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250910/ultaxm/Dylan.wav",
	"eldric sage": "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/hbvhwj/Eldric+Sage.wav",
	"elias":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/rhbvqx/Elias.wav",
	"emilien":     "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/qltlde/Emilien.wav",
	"eric":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250910/qhbznw/Eric.wav",
	"ethan":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250211/emaqdp/ethan.wav",
	"jada":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250910/qjfmmi/Jada.wav",
	"jennifer":    "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/fejjiv/Jennifer.wav",
	"kai":         "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/maiqbf/Kai.wav",
	"katerina":    "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/fschpb/Katerina.wav",
	"kiki":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250910/qwinef/KiKi.wav",
	"lenn":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/arnzdt/Lenn.wav",
	"li":          "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250916/frgdes/Li.wav",
	"maia":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/fewawx/Maia.wav",
	"marcus":      "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250910/dwnnrg/Marcus.wav",
	"mia":         "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/gpvlix/Mia.wav",
	"mochi":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/zapcpe/Mochi.wav",
	"momo":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/rvzrcx/Momo.wav",
	"moon":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/bcaqju/Moon.wav",
	"neil":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/ucmfkt/Neil.wav",
	"nini":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/lppeba/Nini.wav",
	"nofish":      "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/xurcmx/Nofish.wav",
	"ono anna":    "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/mvfbxy/Ono+Anna.wav",
	"peter":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250910/twvnsp/Peter.wav",
	"pip":         "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/gqxoub/Pip.wav",
	"radio gol":   "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/vnezxq/Radio+Gol.wav",
	"rocky":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250910/kfxxgp/Rocky.wav",
	"roy":         "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250910/stsfsz/Roy.wav",
	"ryan":        "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/wsytum/Ryan.wav",
	"seren":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/xlksoe/Seren.wav",
	"serena":      "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250211/bxokea/serena.wav",
	"sohee":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/wwphft/Sohee.wav",
	"sonrisa":     "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/uywoxb/Sonrisa.wav",
	"stella":      "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/azikxr/Stella.wav",
	"sunny":       "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250910/jtrktt/Sunny.wav",
	"vincent":     "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/skfrkq/Vincent.wav",
	"vivian":      "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20251120/eetwkj/Vivian.wav",
}

func qwenTTSOfficialPreviewURL(model, voice string) string {
	if aliyuntts.GetAliyunModelCapability(model).Category != aliyuntts.CategoryQwenTTS {
		return ""
	}
	return qwenTTSOfficialPreviewURLs[strings.ToLower(strings.TrimSpace(voice))]
}

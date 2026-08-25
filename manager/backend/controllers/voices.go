package controllers

import "strings"

// VoiceInfo 描述一个千问 TTS 音色
type VoiceInfo struct {
	Value       string   `json:"value"`       // API 的 voice 参数，例如 "Cherry"
	Label       string   `json:"label"`       // 显示名称，例如 "芊悦"
	Description string   `json:"description"` // 简短描述
	Languages   []string `json:"languages"`   // 支持语种
}

// ModelVoiceMap 模型或模型家族 -> 支持的音色列表
// Qwen Audio 的 Flash/Plus 音色参数包含模型前缀，因此分别维护精确模型键。
var ModelVoiceMap = map[string][]VoiceInfo{
	// 通义千问3-TTS-Flash 系列（qwen3-tts-flash / qwen3-tts-flash-2025-11-27 / qwen3-tts-flash-2025-09-18）
	"qwen3-tts-flash": {
		{Value: "Cherry", Label: "芊悦", Description: "阳光积极、亲切自然小姐姐（女性）"},
		{Value: "Serena", Label: "苏瑶", Description: "温柔小姐姐（女性）"},
		{Value: "Ethan", Label: "晨煦", Description: "标准普通话，带部分北方口音，阳光、温暖、活力（男性）"},
		{Value: "Chelsie", Label: "千雪", Description: "二次元虚拟女友（女性）"},
		{Value: "Momo", Label: "茉兔", Description: "撒娇搞怪，逗你开心（女性）"},
		{Value: "Vivian", Label: "十三", Description: "拽拽的、可爱的小暴躁（女性）"},
		{Value: "Moon", Label: "月白", Description: "率性帅气的月白（男性）"},
		{Value: "Maia", Label: "四月", Description: "知性与温柔的碰撞（女性）"},
		{Value: "Kai", Label: "凯", Description: "耳朵的一场SPA（男性）"},
		{Value: "Nofish", Label: "不吃鱼", Description: "不会翘舌音的设计师（男性）"},
		{Value: "Bella", Label: "萌宝", Description: "喝酒不打醉拳的小萝莉（女性）"},
		{Value: "Jennifer", Label: "詹妮弗", Description: "品牌级、电影质感般美语女声（女性）"},
		{Value: "Ryan", Label: "甜茶", Description: "节奏拉满，戏感炸裂，真实与张力共舞（男性）"},
		{Value: "Katerina", Label: "卡捷琳娜", Description: "御姐音色，韵律回味十足（女性）"},
		{Value: "Aiden", Label: "艾登", Description: "精通厨艺的美语大男孩（男性）"},
		{Value: "Eldric Sage", Label: "沧明子", Description: "沉稳睿智的老者，沧桑如松却心明如镜（男性）"},
		{Value: "Mia", Label: "乖小妹", Description: "温顺如春水，乖巧如初雪（女性）"},
		{Value: "Mochi", Label: "沙小弥", Description: "聪明伶俐的小大人，童真未泯却早慧如禅（男性）"},
		{Value: "Bellona", Label: "燕铮莺", Description: "声音洪亮，吐字清晰，人物鲜活（女性）"},
		{Value: "Vincent", Label: "田叔", Description: "沙哑烟嗓，道尽千军万马与江湖豪情（男性）"},
		{Value: "Bunny", Label: "萌小姬", Description: "\"萌属性\"爆棚的小萝莉（女性）"},
		{Value: "Neil", Label: "阿闻", Description: "最专业的新闻主持人（男性）"},
		{Value: "Elias", Label: "墨讲师", Description: "严谨又具叙事感的讲师音色（女性）"},
		{Value: "Arthur", Label: "徐大爷", Description: "被岁月和旱烟浸泡过的质朴嗓音（男性）"},
		{Value: "Nini", Label: "邻家妹妹", Description: "糯米糍一样又软又黏的嗓音（女性）"},
		{Value: "Ebona", Label: "诡婆婆", Description: "略带惊悚风格的老奶奶音色（女性）"},
		{Value: "Seren", Label: "小婉", Description: "温和舒缓，助眠系音色（女性）"},
		{Value: "Pip", Label: "顽屁小孩", Description: "调皮捣蛋却充满童真（男性）"},
		{Value: "Stella", Label: "少女阿月", Description: "平时甜到发腻，关键时刻充满正义感（女性）"},
		{Value: "Bodega", Label: "博德加", Description: "热情的西班牙大叔（男性）"},
		{Value: "Sonrisa", Label: "索尼莎", Description: "热情开朗的拉美大姐（女性）"},
		{Value: "Alek", Label: "阿列克", Description: "战斗民族冷外表下的温暖嗓音（男性）"},
		{Value: "Dolce", Label: "多尔切", Description: "慵懒的意大利大叔（男性）"},
		{Value: "Sohee", Label: "素熙", Description: "温柔开朗、情绪丰富的韩国欧尼（女性）"},
		{Value: "Ono Anna", Label: "小野杏", Description: "鬼灵精怪的青梅竹马（女性）"},
		{Value: "Lenn", Label: "莱恩", Description: "理性为底色、叛逆藏在细节里的德国青年（男性）"},
		{Value: "Emilien", Label: "埃米尔安", Description: "浪漫的法国大哥哥（男性）"},
		{Value: "Andre", Label: "安德雷", Description: "磁性、自然、沉稳的男声（男性）"},
		{Value: "Radio Gol", Label: "拉迪奥·戈尔", Description: "足球诗人式解说（男性）"},
		{Value: "Jada", Label: "上海-阿珍", Description: "风风火火的沪上阿姐（女性）"},
		{Value: "Dylan", Label: "北京-晓东", Description: "北京胡同里长大的少年（男性）"},
		{Value: "Li", Label: "南京-老李", Description: "耐心的瑜伽老师（男性）"},
		{Value: "Marcus", Label: "陕西-秦川", Description: "老陕味儿十足（男性）"},
		{Value: "Roy", Label: "闽南-阿杰", Description: "诙谐直爽的台湾哥仔（男性）"},
		{Value: "Peter", Label: "天津-李彼得", Description: "天津相声专业捧哏（男性）"},
		{Value: "Sunny", Label: "四川-晴儿", Description: "甜到你心里的川妹子（女性）"},
		{Value: "Eric", Label: "四川-程川", Description: "跳脱市井的四川成都男子（男性）"},
		{Value: "Rocky", Label: "粤语-阿强", Description: "幽默风趣的阿强（男性）"},
		{Value: "Kiki", Label: "粤语-阿清", Description: "甜美的港妹闺蜜（女性）"},
	},

	// 通义千问旧版 TTS 系列（qwen-tts / qwen-tts-latest / qwen-tts-YYYY-MM-DD）。
	"qwen-tts": {
		{Value: "Cherry", Label: "芊悦", Description: "阳光积极、亲切自然小姐姐（女性）"},
		{Value: "Serena", Label: "苏瑶", Description: "温柔小姐姐（女性）"},
		{Value: "Ethan", Label: "晨煦", Description: "标准普通话，带部分北方口音，阳光、温暖、活力（男性）"},
		{Value: "Chelsie", Label: "千雪", Description: "二次元虚拟女友（女性）"},
		{Value: "Momo", Label: "茉兔", Description: "撒娇搞怪，逗你开心（女性）"},
	},

	// 阿里云百炼 CosyVoice 系列（cosyvoice-v3-flash / cosyvoice-v2 / cosyvoice-v1 / cosyvoice-3b-latest）
	"cosyvoice": {
		{Value: "longxiaochun", Label: "龙小春", Description: "清纯甜美女声（默认推荐）"},
		{Value: "longwan", Label: "龙婉", Description: "温婉知性女声"},
		{Value: "longyue", Label: "龙悦", Description: "大气从容女声"},
		{Value: "longzhe", Label: "龙哲", Description: "沉稳磁性男声"},
		{Value: "longsong", Label: "龙宋", Description: "成熟稳重男声"},
		{Value: "xiaogui", Label: "小鬼", Description: "搞怪活泼童声"},
		{Value: "cosyvoice-v2-longmiao", Label: "龙妙（v2）", Description: "灵动自然女声（CosyVoice v2）"},
	},
}

// normalizeModel 将具体模型名归一化为模型家族键
// 例如：qwen3-tts-flash-2025-11-27 -> qwen3-tts-flash
//
//	cosyvoice-v3-flash        -> cosyvoice
//	qwen-audio-3.0-tts-flash  -> qwen-audio-3.0-tts-flash
func normalizeModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	lower := strings.ToLower(model)
	if strings.HasPrefix(lower, "cosyvoice") {
		return "cosyvoice"
	}
	if strings.HasPrefix(lower, qwenAudioFlashModel) {
		return qwenAudioFlashModel
	}
	if strings.HasPrefix(lower, qwenAudioPlusModel) {
		return qwenAudioPlusModel
	}
	if strings.HasPrefix(lower, "qwen3-tts") || strings.Contains(model, "Qwen3-TTS") {
		return "qwen3-tts-flash"
	}
	if strings.HasPrefix(lower, "qwen-tts") {
		return "qwen-tts"
	}
	return model
}

// GetVoicesByModel 根据模型名称获取支持的音色列表
func GetVoicesByModel(model string) []VoiceInfo {
	key := normalizeModel(model)
	if voices, ok := ModelVoiceMap[key]; ok {
		return voices
	}
	return nil
}

// IsVoiceSupported 检查指定模型是否支持某个音色
func IsVoiceSupported(model, voice string) bool {
	if voice == "" {
		return false
	}
	for _, v := range GetVoicesByModel(model) {
		if v.Value == voice {
			return true
		}
	}
	return false
}

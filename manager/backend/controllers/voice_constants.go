package controllers

import "strings"

// VoiceOption 音色选项
type VoiceOption struct {
	Value string `json:"value"` // 音色值
	Label string `json:"label"` // 音色显示名称
}

// VoiceOptions 定义各provider的音色选项
// 根据火山引擎豆包语音文档：https://www.volcengine.com/docs/6561/97465?lang=zh
// 和豆包WebSocket文档：https://www.volcengine.com/docs/6561/1257544?lang=zh
var VoiceOptions = map[string][]VoiceOption{
	// MiMo 内置音色，与 TTS 配置表单候选保持一致。
	"mimo": {
		{Value: "mimo_default", Label: "默认（mimo_default）"},
		{Value: "冰糖", Label: "冰糖（温柔女声）"},
		{Value: "茉莉", Label: "茉莉"},
		{Value: "苏打", Label: "苏打"},
		{Value: "白桦", Label: "白桦"},
		{Value: "Mia", Label: "Mia（英文女声）"},
		{Value: "Chloe", Label: "Chloe（英文女声）"},
		{Value: "Milo", Label: "Milo（英文男声）"},
		{Value: "Dean", Label: "Dean（英文男声）"},
	},
	// Edge TTS 音色列表（中文）
	// 参考：https://blog.csdn.net/u012917925/article/details/134683773
	"edge": {
		{Value: "zh-CN-XiaoxiaoNeural", Label: "晓晓（女声）"},
		{Value: "zh-CN-YunxiNeural", Label: "云希（男声）"},
		{Value: "zh-CN-YunyangNeural", Label: "云扬（男声）"},
		{Value: "zh-CN-XiaoyiNeural", Label: "晓伊（女声）"},
		{Value: "zh-CN-YunjianNeural", Label: "云健（男声）"},
		{Value: "zh-CN-YunxiaNeural", Label: "云夏（男声）"},
		{Value: "zh-CN-YunhaoNeural", Label: "云皓（男声）"},
		{Value: "zh-CN-XiaohanNeural", Label: "晓涵（女声）"},
		{Value: "zh-CN-XiaomoNeural", Label: "晓墨（女声）"},
		{Value: "zh-CN-XiaoxuanNeural", Label: "晓萱（女声）"},
		{Value: "zh-CN-XiaoruiNeural", Label: "晓睿（女声）"},
		{Value: "zh-CN-XiaoshuangNeural", Label: "晓双（女声）"},
		{Value: "zh-CN-XiaoyanNeural", Label: "晓颜（女声）"},
		{Value: "zh-CN-XiaoyouNeural", Label: "晓悠（女声）"},
		{Value: "zh-CN-XiaozhenNeural", Label: "晓甄（女声）"},
		{Value: "zh-CN-YunfengNeural", Label: "云枫（男声）"},
		{Value: "zh-CN-YunyeNeural", Label: "云野（男声）"},
		{Value: "zh-CN-YunzeNeural", Label: "云泽（男声）"},
	},

	// Microsoft TTS 音色列表（中文）
	"microsoft": {
		{Value: "zh-CN-XiaoxiaoNeural", Label: "晓晓（女声）"},
		{Value: "zh-CN-YunxiNeural", Label: "云希（男声）"},
		{Value: "zh-CN-YunyangNeural", Label: "云扬（男声）"},
		{Value: "zh-CN-XiaoyiNeural", Label: "晓伊（女声）"},
		{Value: "zh-CN-YunjianNeural", Label: "云健（男声）"},
		{Value: "zh-CN-YunxiaNeural", Label: "云夏（男声）"},
		{Value: "zh-CN-YunhaoNeural", Label: "云皓（男声）"},
		{Value: "zh-CN-XiaohanNeural", Label: "晓涵（女声）"},
		{Value: "zh-CN-XiaomoNeural", Label: "晓墨（女声）"},
		{Value: "zh-CN-XiaoxuanNeural", Label: "晓萱（女声）"},
		{Value: "zh-CN-XiaoruiNeural", Label: "晓睿（女声）"},
		{Value: "zh-CN-XiaoshuangNeural", Label: "晓双（女声）"},
		{Value: "zh-CN-XiaoyanNeural", Label: "晓颜（女声）"},
		{Value: "zh-CN-XiaoyouNeural", Label: "晓悠（女声）"},
		{Value: "zh-CN-XiaozhenNeural", Label: "晓甄（女声）"},
		{Value: "zh-CN-YunfengNeural", Label: "云枫（男声）"},
		{Value: "zh-CN-YunyeNeural", Label: "云野（男声）"},
		{Value: "zh-CN-YunzeNeural", Label: "云泽（男声）"},
	},

	// 豆包 TTS（doubao / doubao_ws）音色列表统一由 doubao_voices.go 在 init() 中
	// 从嵌入的官方 tone-list-3 数据（doubao_voices.json）加载，并按模型版本过滤。

	// Minimax TTS 音色列表
	// 参考：https://www.minimaxi.com/document/guides/tts-model
	"minimax": {
		// 中文 (普通话)
		{Value: "male-qn-qingse", Label: "青涩青年音色"},
		{Value: "male-qn-jingying", Label: "精英青年音色"},
		{Value: "male-qn-badao", Label: "霸道青年音色"},
		{Value: "male-qn-daxuesheng", Label: "青年大学生音色"},
		{Value: "female-shaonv", Label: "少女音色"},
		{Value: "female-yujie", Label: "御姐音色"},
		{Value: "female-chengshu", Label: "成熟女性音色"},
		{Value: "female-tianmei", Label: "甜美女性音色"},
		{Value: "male-qn-qingse-jingpin", Label: "青涩青年音色-beta"},
		{Value: "male-qn-jingying-jingpin", Label: "精英青年音色-beta"},
		{Value: "male-qn-badao-jingpin", Label: "霸道青年音色-beta"},
		{Value: "male-qn-daxuesheng-jingpin", Label: "青年大学生音色-beta"},
		{Value: "female-shaonv-jingpin", Label: "少女音色-beta"},
		{Value: "female-yujie-jingpin", Label: "御姐音色-beta"},
		{Value: "female-chengshu-jingpin", Label: "成熟女性音色-beta"},
		{Value: "female-tianmei-jingpin", Label: "甜美女性音色-beta"},
		{Value: "clever_boy", Label: "聪明男童"},
		{Value: "cute_boy", Label: "可爱男童"},
		{Value: "lovely_girl", Label: "萌萌女童"},
		{Value: "cartoon_pig", Label: "卡通猪小琪"},
		{Value: "bingjiao_didi", Label: "病娇弟弟"},
		{Value: "junlang_nanyou", Label: "俊朗男友"},
		{Value: "chunzhen_xuedi", Label: "纯真学弟"},
		{Value: "lengdan_xiongzhang", Label: "冷淡学长"},
		{Value: "badao_shaoye", Label: "霸道少爷"},
		{Value: "tianxin_xiaoling", Label: "甜心小玲"},
		{Value: "qiaopi_mengmei", Label: "俏皮萌妹"},
		{Value: "wumei_yujie", Label: "妩媚御姐"},
		{Value: "diadia_xuemei", Label: "嗲嗲学妹"},
		{Value: "danya_xuejie", Label: "淡雅学姐"},
		{Value: "Chinese (Mandarin)_Reliable_Executive", Label: "沉稳高管"},
		{Value: "Chinese (Mandarin)_News_Anchor", Label: "新闻女声"},
		{Value: "Chinese (Mandarin)_Mature_Woman", Label: "傲娇御姐"},
		{Value: "Chinese (Mandarin)_Unrestrained_Young_Man", Label: "不羁青年"},
		{Value: "Arrogant_Miss", Label: "嚣张小姐"},
		{Value: "Robot_Armor", Label: "机械战甲"},
		{Value: "Chinese (Mandarin)_Kind-hearted_Antie", Label: "热心大婶"},
		{Value: "Chinese (Mandarin)_HK_Flight_Attendant", Label: "港普空姐"},
		{Value: "Chinese (Mandarin)_Humorous_Elder", Label: "搞笑大爷"},
		{Value: "Chinese (Mandarin)_Gentleman", Label: "温润男声"},
		{Value: "Chinese (Mandarin)_Warm_Bestie", Label: "温暖闺蜜"},
		{Value: "Chinese (Mandarin)_Male_Announcer", Label: "播报男声"},
		{Value: "Chinese (Mandarin)_Sweet_Lady", Label: "甜美女声"},
		{Value: "Chinese (Mandarin)_Southern_Young_Man", Label: "南方小哥"},
		{Value: "Chinese (Mandarin)_Wise_Women", Label: "阅历姐姐"},
		{Value: "Chinese (Mandarin)_Gentle_Youth", Label: "温润青年"},
		{Value: "Chinese (Mandarin)_Warm_Girl", Label: "温暖少女"},
		{Value: "Chinese (Mandarin)_Kind-hearted_Elder", Label: "花甲奶奶"},
		{Value: "Chinese (Mandarin)_Cute_Spirit", Label: "憨憨萌兽"},
		{Value: "Chinese (Mandarin)_Radio_Host", Label: "电台男主播"},
		{Value: "Chinese (Mandarin)_Lyrical_Voice", Label: "抒情男声"},
		{Value: "Chinese (Mandarin)_Straightforward_Boy", Label: "率真弟弟"},
		{Value: "Chinese (Mandarin)_Sincere_Adult", Label: "真诚青年"},
		{Value: "Chinese (Mandarin)_Gentle_Senior", Label: "温柔学姐"},
		{Value: "Chinese (Mandarin)_Stubborn_Friend", Label: "嘴硬竹马"},
		{Value: "Chinese (Mandarin)_Crisp_Girl", Label: "清脆少女"},
		{Value: "Chinese (Mandarin)_Pure-hearted_Boy", Label: "清澈邻家弟弟"},
		{Value: "Chinese (Mandarin)_Soft_Girl", Label: "柔和少女"},
		// 中文 (粤语)
		{Value: "Cantonese_ProfessionalHost（F)", Label: "专业女主持"},
		{Value: "Cantonese_GentleLady", Label: "温柔女声"},
		{Value: "Cantonese_ProfessionalHost（M)", Label: "专业男主持"},
		{Value: "Cantonese_PlayfulMan", Label: "活泼男声"},
		{Value: "Cantonese_CuteGirl", Label: "可爱女孩"},
		{Value: "Cantonese_KindWoman", Label: "善良女声"},
		// 英文
		{Value: "Santa_Claus", Label: "Santa Claus"},
		{Value: "Grinch", Label: "Grinch"},
		{Value: "Rudolph", Label: "Rudolph"},
		{Value: "Arnold", Label: "Arnold"},
		{Value: "Charming_Santa", Label: "Charming Santa"},
		{Value: "Charming_Lady", Label: "Charming Lady"},
		{Value: "Sweet_Girl", Label: "Sweet Girl"},
		{Value: "Cute_Elf", Label: "Cute Elf"},
		{Value: "Attractive_Girl", Label: "Attractive Girl"},
		{Value: "Serene_Woman", Label: "Serene Woman"},
		{Value: "English_Trustworthy_Man", Label: "Trustworthy Man"},
		{Value: "English_Graceful_Lady", Label: "Graceful Lady"},
		{Value: "English_Aussie_Bloke", Label: "Aussie Bloke"},
		{Value: "English_Whispering_girl", Label: "Whispering girl"},
		{Value: "English_Diligent_Man", Label: "Diligent Man"},
		{Value: "English_Gentle-voiced_man", Label: "Gentle-voiced man"},
	},

	// 阿里云千问 TTS 音色列表（基础列表，模型过滤由 GetAliyunQwenVoicesByModel 处理）
	"aliyun_qwen": {
		{Value: "Cherry", Label: "芊悦"},
		{Value: "Serena", Label: "苏瑶"},
		{Value: "Ethan", Label: "晨煦"},
		{Value: "Chelsie", Label: "千雪"},
		{Value: "Momo", Label: "茉兔"},
		{Value: "Vivian", Label: "十三"},
		{Value: "Moon", Label: "月白"},
		{Value: "Maia", Label: "四月"},
		{Value: "Kai", Label: "凯"},
		{Value: "Nofish", Label: "不吃鱼"},
		{Value: "Bella", Label: "萌宝"},
		{Value: "Jennifer", Label: "詹妮弗"},
		{Value: "Ryan", Label: "甜茶"},
	},

	// 讯飞在线 TTS 音色列表
	// 说明：这里保留一组常用静态音色，最终是否可用以讯飞控制台实际授权为准。
	// 参考：
	// https://www.xfyun.cn/doc/tts/online_tts/API.html
	// https://aiui.xfyun.cn/doc/aiui/3_access_service/access_interact/functions/speech_synthesis.html
	"xunfei": {
		{Value: "xiaoyan", Label: "小燕（女声，默认推荐）"},
		{Value: "xiaofeng", Label: "晓峰（男声）"},
		{Value: "yezi", Label: "小露（女声）"},
		{Value: "yifei", Label: "一菲（女声）"},
		{Value: "yiping", Label: "一萍（女声）"},
		{Value: "qige", Label: "七哥（男声）"},
		{Value: "chaoge", Label: "超哥（男声）"},
		{Value: "pengfei", Label: "小鹏（男声）"},
		{Value: "xiaoxin", Label: "萌小新（童声）"},
		{Value: "john", Label: "John（英文男声）"},
		{Value: "catherine", Label: "Catherine（英文女声）"},
	},

	// 讯飞超拟人 TTS 音色列表
	// 说明：保留一组推荐静态音色，最终可用性以讯飞控制台授权为准。
	"xunfei_super_tts": {
		{Value: "x6_lingxiaoxue_pro", Label: "灵小雪（x6）"},
		{Value: "x6_lingfeiyi_pro", Label: "灵飞逸（x6）"},
		{Value: "x6_lingxiaoli_pro", Label: "灵小丽（x6）"},
		{Value: "x6_lingxiaoyue_pro", Label: "灵小玥（x6）"},
		{Value: "x6_lingxiaoxuan_pro", Label: "灵小萱（x6）"},
		{Value: "x6_lingyuyan_pro", Label: "灵语嫣（x6）"},
		{Value: "x6_lingyouyou_pro", Label: "灵悠悠（x6）"},
		{Value: "x6_feizheChat_pro", Label: "飞哲 Chat（x6）"},
		{Value: "x6_xiaoqiChat_pro", Label: "小琪 Chat（x6）"},
		{Value: "x5_lingxiaotang_flow", Label: "灵小棠（x5）"},
		{Value: "x5_lingyuzhao_flow", Label: "灵语昭（x5）"},
		{Value: "x4_zijin_oral", Label: "紫瑾（x4，口语化）"},
		{Value: "x4_ziyang_oral", Label: "紫阳（x4，口语化）"},
	},

	// 智谱 TTS 音色列表
	"zhipu": {
		{Value: "tongtong", Label: "彤彤（默认音色）"},
		{Value: "chuichui", Label: "锤锤"},
		{Value: "xiaochen", Label: "小陈"},
		{Value: "jam", Label: "动动动物圈jam音色"},
		{Value: "kazi", Label: "动动动物圈kazi音色"},
		{Value: "douji", Label: "动动动物圈douji音色"},
		{Value: "luodo", Label: "动动动物圈luodo音色"},
	},

	// OpenAI TTS 常用内置音色列表
	// 参考：https://platform.openai.com/docs/guides/text-to-speech
	// 说明：OpenAI 官方协议未提供列举音色接口，这里维护官方内置音色候选项，同时前端支持自定义输入。
	"openai": {
		{Value: "alloy", Label: "Alloy（中性均衡，默认音色）"},
		{Value: "ash", Label: "Ash（沉稳男声）"},
		{Value: "ballad", Label: "Ballad（温暖抒情）"},
		{Value: "coral", Label: "Coral（清晰女声）"},
		{Value: "echo", Label: "Echo（温和男声）"},
		{Value: "fable", Label: "Fable（英式叙事）"},
		{Value: "onyx", Label: "Onyx（深沉男声）"},
		{Value: "nova", Label: "Nova（活力女声）"},
		{Value: "sage", Label: "Sage（知性女声）"},
		{Value: "shimmer", Label: "Shimmer（轻柔女声）"},
		{Value: "verse", Label: "Verse（自然朗读）"},
	},
}

// GetVoiceOptionsByProvider 根据provider获取音色列表
func GetVoiceOptionsByProvider(provider string) []VoiceOption {
	if voices, ok := VoiceOptions[provider]; ok {
		return voices
	}
	return []VoiceOption{}
}

// GetAliyunQwenVoicesByModel 根据千问模型名称获取音色列表
// 使用 qwen 包中的模型映射来获取准确的音色列表
func GetAliyunQwenVoicesByModel(model string) []VoiceOption {
	model = strings.TrimSpace(model)
	if model == "" {
		// 如果没有模型，返回基础列表
		return GetVoiceOptionsByProvider("aliyun_qwen")
	}

	// 使用本地函数获取模型对应的音色列表
	voices := GetVoicesByModel(model)
	if voices == nil || len(voices) == 0 {
		// 自定义或未知模型没有可靠的系统音色表，由前端保留手工输入能力。
		return []VoiceOption{}
	}

	// 将 VoiceInfo 转换为 VoiceOption
	result := make([]VoiceOption, 0, len(voices))
	for _, v := range voices {
		result = append(result, VoiceOption{
			Value: v.Value,
			Label: v.Label,
		})
	}
	return result
}

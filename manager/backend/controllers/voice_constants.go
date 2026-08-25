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

	// 豆包 TTS 音色列表（HTTP接口）
	// 参考：https://www.volcengine.com/docs/6561/97465?lang=zh
	"doubao": {
		{Value: "BV700_V2_streaming", Label: "灿灿 2.0"},
		{Value: "BV705_streaming", Label: "炀炀"},
		{Value: "BV701_V2_streaming", Label: "擎苍 2.0"},
		{Value: "BV001_V2_streaming", Label: "通用女声 2.0"},
		{Value: "BV700_streaming", Label: "灿灿"},
		{Value: "BV406_V2_streaming", Label: "超自然音色-梓梓2.0"},
		{Value: "BV406_streaming", Label: "超自然音色-梓梓"},
		{Value: "BV407_V2_streaming", Label: "超自然音色-燃燃2.0"},
		{Value: "BV407_streaming", Label: "超自然音色-燃燃"},
		{Value: "BV001_streaming", Label: "通用女声"},
		{Value: "BV002_streaming", Label: "通用男声"},
		{Value: "BV701_streaming", Label: "擎苍"},
		{Value: "BV119_streaming", Label: "通用赘婿"},
		{Value: "BV102_streaming", Label: "儒雅青年"},
		{Value: "BV113_streaming", Label: "甜宠少御"},
		{Value: "BV115_streaming", Label: "古风少御"},
		{Value: "BV007_streaming", Label: "亲切女声"},
		{Value: "BV056_streaming", Label: "阳光男声"},
		{Value: "BV005_streaming", Label: "活泼女声"},
		{Value: "BV051_streaming", Label: "奶气萌娃"},
		{Value: "BV034_streaming", Label: "知性姐姐-双语"},
		{Value: "BV033_streaming", Label: "温柔小哥"},
		{Value: "BV021_streaming", Label: "东北老铁"},
		{Value: "BV019_streaming", Label: "重庆小伙"},
		{Value: "BV213_streaming", Label: "广西表哥"},
		{Value: "BV503_streaming", Label: "活力女声-Ariana"},
		{Value: "BV504_streaming", Label: "活力男声-Jackson"},
		{Value: "BV522_streaming", Label: "气质女生"},
		{Value: "BV524_streaming", Label: "日语男声"},
		{Value: "BV104_streaming", Label: "温柔淑女"},
		{Value: "BV004_streaming", Label: "开朗青年"},
		{Value: "BV009_streaming", Label: "知性女声"},
		{Value: "BV008_streaming", Label: "亲切男声"},
		{Value: "BV064_streaming", Label: "小萝莉"},
		{Value: "BV437_streaming", Label: "解说小帅-多情感"},
		{Value: "BV511_streaming", Label: "慵懒女声-Ava"},
		{Value: "BV040_streaming", Label: "亲切女声-Anna"},
		{Value: "BV138_streaming", Label: "情感女声-Lawrence"},
		{Value: "BV704_streaming", Label: "方言灿灿"},
		{Value: "BV702_streaming", Label: "Stefan"},
		{Value: "BV421_streaming", Label: "天才少女"},
	},

	// 豆包 WebSocket TTS 音色列表
	// 参考官方文档《音色列表》：
	// https://www.volcengine.com/docs/6561/1257544?lang=zh
	// 这里维护的是项目内常用的官方在线音色候选项。
	// 注意：音色列表仅作为候选项展示，不再依据音色名强绑定 model/resource_id。
	// 实际可用性仍取决于当前 appid/access_token 在火山控制台实际开通的资源。

	"doubao_ws": {
		// 女声音色
		{Value: "zh_female_cancan_mars_bigtts", Label: "灿灿 / Shiny（女声）"},
		{Value: "zh_female_vv_uranus_bigtts", Label: "vivi 2.0（女声）"},
		{Value: "zh_female_vv_jupiter_bigtts", Label: "vivi O版（女声）"},
		{Value: "zh_female_xiaohe_jupiter_bigtts", Label: "小何 O版（女声）"},
		{Value: "saturn_zh_female_cancan_tob", Label: "知性灿灿（女声）"},
		{Value: "saturn_zh_female_keainvsheng_tob", Label: "可爱女生（女声）"},
		{Value: "saturn_zh_female_tiaopigongzhu_tob", Label: "调皮公主（女声）"},
		{Value: "zh_female_xiaohe_uranus_bigtts", Label: "小何（女声）"},
		{Value: "zh_female_tianmeitaozi_mars_bigtts", Label: "甜美桃子（女声）"},
		{Value: "zh_female_wanwanxiaohe_moon_bigtts", Label: "湾湾小何（女声）"},
		{Value: "zh_female_qinqienvsheng_moon_bigtts", Label: "亲切女声（女声）"},
		{Value: "zh_female_vv_mars_bigtts", Label: "Vivi（女声）"},
		{Value: "zh_female_tianmeixiaoyuan_moon_bigtts", Label: "甜美小源（女声）"},
		{Value: "zh_female_qingchezizi_moon_bigtts", Label: "清澈梓梓（女声）"},
		{Value: "zh_female_kailangjiejie_moon_bigtts", Label: "开朗姐姐（女声）"},
		{Value: "zh_female_tianmeiyueyue_moon_bigtts", Label: "甜美悦悦（女声）"},
		{Value: "zh_female_xinlingjitang_moon_bigtts", Label: "心灵鸡汤（女声）"},
		{Value: "zh_female_zhixingnvsheng_mars_bigtts", Label: "知性女声（女声）"},
		{Value: "zh_female_wenroushunv_mars_bigtts", Label: "温柔淑女（女声）"},
		{Value: "zh_female_wenrouxiaoya_moon_bigtts", Label: "温柔小雅（女声）"},
		{Value: "zh_female_linjianvhai_moon_bigtts", Label: "邻家女孩（女声）"},
		{Value: "zh_female_shuangkuaisisi_moon_bigtts", Label: "爽快思思/Skye（女声）"},
		{Value: "zh_female_gaolengyujie_moon_bigtts", Label: "高冷御姐（女声）"},
		{Value: "zh_female_meilinvyou_moon_bigtts", Label: "魅力女友（女声）"},
		{Value: "zh_female_sajiaonvyou_moon_bigtts", Label: "柔美女友（撒娇）（女声）"},
		{Value: "zh_female_yuanqinvyou_moon_bigtts", Label: "撒娇学妹（女声）"},
		{Value: "ICL_zh_female_wenrounvshen_239eff5e8ffa_tob", Label: "温柔女神（女声）"},
		{Value: "ICL_zh_female_chunzhenshaonv_e588402fb8ad_tob", Label: "纯真少女（女声）"},
		{Value: "ICL_zh_female_jinglingxiangdao_1beb294a9e3e_tob", Label: "精灵向导（女声）"},
		{Value: "ICL_zh_female_yilin_tob", Label: "贴心妹妹（女声）"},
		{Value: "ICL_zh_female_chengshujiejie_tob", Label: "成熟姐姐（女声）"},
		{Value: "ICL_zh_female_bingjiaojiejie_tob", Label: "病娇姐姐（女声）"},
		{Value: "ICL_zh_female_wumeiyujie_tob", Label: "妩媚御姐（女声）"},
		{Value: "ICL_zh_female_aojiaonvyou_tob", Label: "傲娇女友（女声）"},
		{Value: "ICL_zh_female_tiexinnvyou_tob", Label: "贴心女友（女声）"},
		{Value: "ICL_zh_female_xingganyujie_tob", Label: "性感御姐（女声）"},
		{Value: "ICL_zh_female_lixingyuanzi_cs_tob", Label: "理性圆子（客服女声）"},
		{Value: "ICL_zh_female_wuxi_tob", Label: "元气甜妹（女声）"},
		{Value: "ICL_zh_female_zhixingwenwan_tob", Label: "知性温婉（女声）"},

		// 男声音色
		{Value: "saturn_zh_male_shuanglangshaonian_tob", Label: "爽朗少年（男声）"},
		{Value: "saturn_zh_male_tiancaitongzhuo_tob", Label: "天才同桌（男声）"},
		{Value: "zh_male_yunzhou_jupiter_bigtts", Label: "云舟 O版（男声）"},
		{Value: "zh_male_xiaotian_jupiter_bigtts", Label: "小天 O版（男声）"},
		{Value: "zh_male_m191_uranus_bigtts", Label: "云舟（男声）"},
		{Value: "zh_male_taocheng_uranus_bigtts", Label: "小天（男声）"},
		{Value: "en_male_tim_uranus_bigtts", Label: "Tim（英文男声）"},
		{Value: "zh_male_yangguangqingnian_moon_bigtts", Label: "阳光青年（男声）"},
		{Value: "zh_male_qingshuangnanda_mars_bigtts", Label: "清爽男大（男声）"},
		{Value: "zh_male_wenrouxiaoge_mars_bigtts", Label: "温柔小哥（男声）"},
		{Value: "zh_male_qingcang_mars_bigtts", Label: "擎苍（男声）"},
		{Value: "zh_male_ruyaqingnian_mars_bigtts", Label: "儒雅青年（男声）"},
		{Value: "zh_male_jieshuoxiaoming_moon_bigtts", Label: "解说小明（男声）"},
		{Value: "zh_male_linjiananhai_moon_bigtts", Label: "邻家男孩（男声）"},
		{Value: "zh_male_yuanboxiaoshu_moon_bigtts", Label: "渊博小叔（男声）"},
		{Value: "zh_male_wennuanahu_moon_bigtts", Label: "温暖阿虎/Alvin（男声）"},
		{Value: "zh_male_shaonianzixin_moon_bigtts", Label: "少年梓辛/Brayan（男声）"},
		{Value: "zh_male_beijingxiaoye_moon_bigtts", Label: "北京小爷（男声）"},
		{Value: "zh_male_jingqiangkanye_moon_bigtts", Label: "京腔侃爷/Harmony（男声）"},
		{Value: "zh_male_guozhoudege_moon_bigtts", Label: "广州德哥（男声）"},
		{Value: "zh_male_haoyuxiaoge_moon_bigtts", Label: "浩宇小哥（男声）"},
		{Value: "zh_male_shenyeboke_moon_bigtts", Label: "深夜播客（男声）"},
		{Value: "zh_male_aojiaobazong_moon_bigtts", Label: "傲娇霸总（男声）"},
		{Value: "zh_male_dongfanghaoran_moon_bigtts", Label: "东方浩然（男声）"},
		{Value: "zh_male_M100_conversation_wvae_bigtts", Label: "悠悠君子/Lucas（男声）"},
		{Value: "zh_male_xudong_conversation_wvae_bigtts", Label: "快乐小东/Daníel（男声）"},
		{Value: "zh_male_qingyiyuxuan_mars_bigtts", Label: "阳光阿辰（男声）"},
		{Value: "en_male_jason_conversation_wvae_bigtts", Label: "开朗学长（男声）"},
		{Value: "ICL_zh_male_lengkugege_v1_tob", Label: "冷酷哥哥（男声）"},
		{Value: "ICL_zh_male_shenmi_v1_tob", Label: "机灵小伙（男声）"},
		{Value: "ICL_zh_male_BV705_streaming_cs_tob", Label: "炀炀（男声）"},
		{Value: "ICL_zh_male_menyoupingxiaoge_ffed9fc2fee7_tob", Label: "闷油瓶小哥（男声）"},
		{Value: "ICL_zh_male_anrenqinzhu_cd62e63dcdab_tob", Label: "黯刃秦主（男声）"},
		{Value: "ICL_zh_male_guaogongzi_v1_tob", Label: "孤傲公子（男声）"},
		{Value: "ICL_zh_male_bingruogongzi_tob", Label: "病弱公子（男声）"},
		{Value: "ICL_zh_male_bingjiaodidi_tob", Label: "病娇弟弟（男声）"},
		{Value: "ICL_zh_male_aomanshaoye_tob", Label: "傲慢少爷（男声）"},
		{Value: "ICL_zh_male_chunzhenxuedi_tob", Label: "纯真学弟（男声）"},
		{Value: "ICL_zh_male_yourougongzi_tob", Label: "优柔公子（男声）"},
		{Value: "ICL_zh_male_tiexinnanyou_tob", Label: "贴心男友（男声）"},
		{Value: "ICL_zh_male_shaonianjiangjun_tob", Label: "少年将军（男声）"},
		{Value: "ICL_zh_male_bingjiaogege_tob", Label: "病娇哥哥（男声）"},
		{Value: "ICL_zh_male_xuebanantongzhuo_tob", Label: "学霸男同桌（男声）"},
		{Value: "ICL_zh_male_youmoshushu_tob", Label: "幽默叔叔（男声）"},
		{Value: "ICL_zh_male_wenrounantongzhuo_tob", Label: "温柔男同桌（男声）"},
		{Value: "ICL_zh_male_youmodaye_tob", Label: "幽默大爷（男声）"},
		{Value: "ICL_zh_male_shenmifashi_tob", Label: "神秘法师（男声）"},
		{Value: "ICL_zh_male_lengjunshangsi_tob", Label: "冷峻上司（男声）"},
		{Value: "ICL_en_male_michael_tob", Label: "Michael（美式英语男声）"},

		// IP/特色音色
		{Value: "zh_male_lubanqihao_mars_bigtts", Label: "鲁班七号（男声）"},
		{Value: "zh_female_yangmi_mars_bigtts", Label: "林潇（女声）"},
		{Value: "zh_female_linzhiling_mars_bigtts", Label: "玲玲姐姐（女声）"},
		{Value: "zh_female_jiyejizi2_mars_bigtts", Label: "春日部姐姐（女声）"},
		{Value: "zh_male_tangseng_mars_bigtts", Label: "唐僧（男声）"},
		{Value: "zh_male_zhubajie_mars_bigtts", Label: "猪八戒（男声）"},
		{Value: "zh_female_naying_mars_bigtts", Label: "直率英子（女声）"},
		{Value: "zh_female_leidian_mars_bigtts", Label: "女雷神（女声）"},
		{Value: "zh_male_sunwukong_mars_bigtts", Label: "猴哥（男声）"},
		{Value: "zh_male_xionger_mars_bigtts", Label: "熊二（男声）"},
		{Value: "zh_female_peiqi_mars_bigtts", Label: "佩奇猪（女声）"},
		{Value: "zh_female_yingtaowanzi_mars_bigtts", Label: "樱桃丸子（女声）"},
		{Value: "zh_male_silang_mars_bigtts", Label: "四郎（男声）"},
	},

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

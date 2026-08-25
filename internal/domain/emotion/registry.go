package emotion

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	log "xiaozhi-esp32-server-golang/logger"

	"gorm.io/gorm"
)

// EmotionItem 定义单条情绪配置项
type EmotionItem struct {
	ID             int    `json:"id"`
	Key            string `json:"key"`
	Name           string `json:"name"`
	PromptDesc     string `json:"prompt_desc"`
	VoicePrompt    string `json:"voice_prompt"`
	MinimaxEmotion string `json:"minimax_emotion"`
	InlineTag      string `json:"inline_tag"`
	Enabled        bool   `json:"enabled"`
}

var (
	registryLock sync.RWMutex
	activeItems  []EmotionItem
	itemsMap     map[int]EmotionItem
)

func init() {
	ResetToDefaultRegistry()
}

// DefaultEmotionItems 返回系统预设的 28 种情绪配置
func DefaultEmotionItems() []EmotionItem {
	return []EmotionItem{
		{
			ID:             0,
			Key:            "angry",
			Name:           "愤怒平息",
			PromptDesc:     "用户情绪激动、抱怨、质问时，你需要冷静但有明显压住怒意的回应",
			VoicePrompt:    "语速偏快，音量略高，语气明显带怒意和压迫感，重点词加重，句尾干脆有力，可以有轻微质问感，但不要破音或失真。",
			MinimaxEmotion: "angry",
			InlineTag:      "[angry]",
			Enabled:        true,
		},
		{
			ID:             1,
			Key:            "comfort",
			Name:           "安慰",
			PromptDesc:     "用户沮丧、失望、疲惫时，你给予支持",
			VoicePrompt:    "语速慢一点，音量偏低，声线柔和贴近，停顿自然，像在认真安慰对方，语气稳定、有耐心。",
			MinimaxEmotion: "calm",
			InlineTag:      "[empathetic]",
			Enabled:        true,
		},
		{
			ID:             2,
			Key:            "happy",
			Name:           "高兴",
			PromptDesc:     "轻松愉快的话题",
			VoicePrompt:    "语速略快，音色明亮，语气轻快自然，带真实开心和笑意，但不要过度亢奋或表演化。",
			MinimaxEmotion: "happy",
			InlineTag:      "[excited]",
			Enabled:        true,
		},
		{
			ID:             3,
			Key:            "neutral",
			Name:           "中性",
			PromptDesc:     "普通信息类回复，默认",
			VoicePrompt:    "",
			MinimaxEmotion: "neutral",
			InlineTag:      "",
			Enabled:        true,
		},
		{
			ID:             4,
			Key:            "serious",
			Name:           "严肃",
			PromptDesc:     "重要事项、正式场合或需要认真说明",
			VoicePrompt:    "语速适中偏慢，音量稳定，语气沉稳认真，句尾干净，不要嬉笑，不要夸张压低嗓音。",
			MinimaxEmotion: "calm",
			InlineTag:      "[serious]",
			Enabled:        true,
		},
		{
			ID:             5,
			Key:            "excited",
			Name:           "激动",
			PromptDesc:     "用户分享好消息，你表示兴奋",
			VoicePrompt:    "语速稍快，音量略高，语气有兴奋感和推进感，重点词更有力度，但不要喊叫。",
			MinimaxEmotion: "happy",
			InlineTag:      "[excited]",
			Enabled:        true,
		},
		{
			ID:             6,
			Key:            "apologetic",
			Name:           "道歉",
			PromptDesc:     "你需要致歉或承认问题",
			VoicePrompt:    "语速稍慢，音量偏低，语气诚恳、放软，带一点歉意和退让感，停顿自然，不要哭腔。",
			MinimaxEmotion: "calm",
			InlineTag:      "[empathetic]",
			Enabled:        true,
		},
		{
			ID:             7,
			Key:            "encouraging",
			Name:           "鼓励",
			PromptDesc:     "用户在学习、尝试、坚持或需要信心",
			VoicePrompt:    "语速适中，音色明亮温和，语气坚定、有支持感，关键短句稍微加强，像在认真鼓励对方。",
			MinimaxEmotion: "happy",
			InlineTag:      "[excited]",
			Enabled:        true,
		},
		{
			ID:             8,
			Key:            "curious",
			Name:           "好奇",
			PromptDesc:     "探讨性、开放性话题",
			VoicePrompt:    "语速适中，语调轻微上扬，语气自然好奇、带一点探索感，不要装可爱，不要夸张疑问。",
			MinimaxEmotion: "surprised",
			InlineTag:      "[curious]",
			Enabled:        true,
		},
		{
			ID:             9,
			Key:            "warm",
			Name:           "温暖",
			PromptDesc:     "问候、关怀、陪伴",
			VoicePrompt:    "语速稍慢，音量中低，声线柔和贴近，句尾轻轻落下，短句之间保留自然停顿，像在安静陪伴对方。",
			MinimaxEmotion: "happy",
			InlineTag:      "[empathetic]",
			Enabled:        true,
		},
		{
			ID:             10,
			Key:            "sad",
			Name:           "悲伤",
			PromptDesc:     "离别、失去、分手、难过、遗憾等场景",
			VoicePrompt:    "语速很慢，音量偏低，气息明显变轻，句尾下沉，带明显哭腔和委屈感，可以有轻微哽咽和停顿，表达强烈难过，但不要尖叫或失真。",
			MinimaxEmotion: "sad",
			InlineTag:      "[sad]",
			Enabled:        true,
		},
		{
			ID:             11,
			Key:            "amazed",
			Name:           "惊叹",
			PromptDesc:     "明显惊讶、赞叹、不可思议",
			VoicePrompt:    "用赞叹、惊讶的语气朗读，语调明显上扬。",
			MinimaxEmotion: "surprised",
			InlineTag:      "[amazed]",
			Enabled:        true,
		},
		{
			ID:             12,
			Key:            "deep_shout",
			Name:           "深沉大声呐喊",
			PromptDesc:     "需要低沉、有力量地强调，不常用",
			VoicePrompt:    "用低沉大声呐喊的语气朗读，强调语气与力量感。",
			MinimaxEmotion: "neutral",
			InlineTag:      "[deep and loud shouting]",
			Enabled:        true,
		},
		{
			ID:             13,
			Key:            "trembling",
			Name:           "颤抖",
			PromptDesc:     "害怕、紧张、强烈不安",
			VoicePrompt:    "用害怕颤抖的语气朗读，带有不自然的短促停顿。",
			MinimaxEmotion: "sad",
			InlineTag:      "[trembling]",
			Enabled:        true,
		},
		{
			ID:             14,
			Key:            "sarcastic",
			Name:           "讽刺",
			PromptDesc:     "轻微反讽或冷幽默，避免攻击用户",
			VoicePrompt:    "用轻微反讽和冷幽默的语气朗读，语调略带玩味。",
			MinimaxEmotion: "neutral",
			InlineTag:      "[sarcastic]",
			Enabled:        true,
		},
		{
			ID:             15,
			Key:            "dracula",
			Name:           "德古拉风格",
			PromptDesc:     "低沉、阴森、戏剧化，适合角色扮演",
			VoicePrompt:    "用低沉阴森、戏剧化的德古拉角色口吻朗读。",
			MinimaxEmotion: "neutral",
			InlineTag:      "[like dracula]",
			Enabled:        true,
		},
		{
			ID:             16,
			Key:            "bored",
			Name:           "无聊",
			PromptDesc:     "乏味、提不起劲的表达，谨慎使用",
			VoicePrompt:    "用无聊乏味、提不起劲的懒散语气朗读。",
			MinimaxEmotion: "neutral",
			InlineTag:      "[bored]",
			Enabled:        true,
		},
		{
			ID:             17,
			Key:            "tired",
			Name:           "疲惫",
			PromptDesc:     "困倦、累、没精神",
			VoicePrompt:    "用困倦疲惫、气息不足的语气朗读。",
			MinimaxEmotion: "sad",
			InlineTag:      "[tired]",
			Enabled:        true,
		},
		{
			ID:             18,
			Key:            "scornful",
			Name:           "轻蔑",
			PromptDesc:     "不赞同、轻微不屑，避免对用户使用",
			VoicePrompt:    "用轻微不屑与不赞同的语气朗读。",
			MinimaxEmotion: "neutral",
			InlineTag:      "[scornful]",
			Enabled:        true,
		},
		{
			ID:             19,
			Key:            "shouting",
			Name:           "大喊",
			PromptDesc:     "需要远距离呼喊或强提醒，不常用",
			VoicePrompt:    "用大声呼喊和高音量的语气朗读。",
			MinimaxEmotion: "happy",
			InlineTag:      "[shouting]",
			Enabled:        true,
		},
		{
			ID:             20,
			Key:            "asmr",
			Name:           "ASMR 轻柔耳语",
			PromptDesc:     "非常轻柔、贴近、安静",
			VoicePrompt:    "用极度轻柔、靠近麦克风的 ASMR 耳语口吻朗读。",
			MinimaxEmotion: "calm",
			InlineTag:      "[asmr]",
			Enabled:        true,
		},
		{
			ID:             21,
			Key:            "panicked",
			Name:           "恐慌",
			PromptDesc:     "紧急、突发危机、极度慌乱",
			VoicePrompt:    "用惊慌失措、语速急促混乱的语气朗读。",
			MinimaxEmotion: "sad",
			InlineTag:      "[panicked]",
			Enabled:        true,
		},
		{
			ID:             22,
			Key:            "mischievous",
			Name:           "调皮",
			PromptDesc:     "逗趣、开玩笑、戏谑",
			VoicePrompt:    "用调皮搞怪、带着坏笑和戏谑的语气朗读。",
			MinimaxEmotion: "happy",
			InlineTag:      "[mischievously]",
			Enabled:        true,
		},
		{
			ID:             23,
			Key:            "whisper",
			Name:           "耳语",
			PromptDesc:     "小声说话、秘密、安静环境",
			VoicePrompt:    "用压低声音的小声耳语口吻朗读。",
			MinimaxEmotion: "calm",
			InlineTag:      "[whispers]",
			Enabled:        true,
		},
		{
			ID:             24,
			Key:            "reluctant",
			Name:           "不情愿",
			PromptDesc:     "勉强答应、不太乐意、犹豫",
			VoicePrompt:    "用勉强答应、拖长音且不情愿的懒散语气朗读。",
			MinimaxEmotion: "neutral",
			InlineTag:      "[reluctantly]",
			Enabled:        true,
		},
		{
			ID:             25,
			Key:            "crying",
			Name:           "哭泣",
			PromptDesc:     "极度悲痛、大哭、泣不成声",
			VoicePrompt:    "用泣不成声、断断续续带抽泣的哭腔语气朗读。",
			MinimaxEmotion: "sad",
			InlineTag:      "[crying]",
			Enabled:        true,
		},
		{
			ID:             26,
			Key:            "very_slow",
			Name:           "非常缓慢",
			PromptDesc:     "字句极度缓慢、一字一顿、催眠",
			VoicePrompt:    "用极度缓慢、一字一顿且拖长音的节奏朗读。",
			MinimaxEmotion: "calm",
			InlineTag:      "[very slowly]",
			Enabled:        true,
		},
		{
			ID:             27,
			Key:            "very_fast",
			Name:           "非常快速",
			PromptDesc:     "极速报数、急切催促、机关枪式说话",
			VoicePrompt:    "用极快语速、连珠炮式紧凑不换气的节奏朗读。",
			MinimaxEmotion: "happy",
			InlineTag:      "[very fast]",
			Enabled:        true,
		},
	}
}

// ResetToDefaultRegistry 重置注册表为内置默认
func ResetToDefaultRegistry() {
	registryLock.Lock()
	defer registryLock.Unlock()

	activeItems = DefaultEmotionItems()
	itemsMap = make(map[int]EmotionItem, len(activeItems))
	for _, it := range activeItems {
		itemsMap[it.ID] = it
	}
}

// UpdateRegistry 更新注册表中的配置项
func UpdateRegistry(items []EmotionItem) {
	registryLock.Lock()
	defer registryLock.Unlock()

	activeItems = items
	itemsMap = make(map[int]EmotionItem, len(items))
	for _, it := range items {
		itemsMap[it.ID] = it
	}
}

// GetRegistryItems 获取当前所有情绪配置
func GetRegistryItems() []EmotionItem {
	registryLock.RLock()
	defer registryLock.RUnlock()

	result := make([]EmotionItem, len(activeItems))
	copy(result, activeItems)
	return result
}

// GetEmotionItemByID 根据 ID 获取情绪项
func GetEmotionItemByID(id int) (EmotionItem, bool) {
	registryLock.RLock()
	defer registryLock.RUnlock()

	item, ok := itemsMap[id]
	return item, ok
}

// GetVoicePromptForEmotion 获取特定情绪的控音 VoicePrompt
func GetVoicePromptForEmotion(emo Emotion) string {
	if item, ok := GetEmotionItemByID(int(emo)); ok && item.Enabled {
		return item.VoicePrompt
	}
	return ""
}

// GetMinimaxEmotionForEmotion 获取 MiniMax 映射情绪
func GetMinimaxEmotionForEmotion(emo Emotion) string {
	if item, ok := GetEmotionItemByID(int(emo)); ok && item.Enabled {
		if item.MinimaxEmotion != "" {
			return item.MinimaxEmotion
		}
	}
	switch emo {
	case EmotionAngry:
		return "angry"
	case EmotionHappy, EmotionExcited, EmotionEncouraging, EmotionWarm:
		return "happy"
	case EmotionComfort, EmotionSerious, EmotionApologetic:
		return "calm"
	case EmotionCurious, EmotionAmazed:
		return "surprised"
	case EmotionSad, EmotionCrying, EmotionTired:
		return "sad"
	default:
		return "neutral"
	}
}

// GetInlineTagForEmotion 获取内嵌表情标签
func GetInlineTagForEmotion(emo Emotion) string {
	if item, ok := GetEmotionItemByID(int(emo)); ok && item.Enabled {
		return item.InlineTag
	}
	return ""
}

type configDBRow struct {
	ID       uint   `gorm:"primaryKey;autoIncrement"`
	Type     string `gorm:"size:50;not null;index"`
	ConfigID string `gorm:"column:config_id;size:100;not null;index"`
	Name     string `gorm:"size:100;not null"`
	JSONData string `gorm:"column:json_data;type:text;not null"`
	Enabled  bool   `gorm:"not null;default:true"`
}

func (configDBRow) TableName() string {
	return "configs"
}

// LoadEmotionConfigFromDB 从数据库加载自定义情绪 VoicePrompt
func LoadEmotionConfigFromDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	var row configDBRow
	err := db.Where("type = ? AND config_id = ?", "emotion_config", "global_emotion_config").First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("查询情绪配置失败: %v", err)
	}

	var voicePrompts map[string]string
	if err := json.Unmarshal([]byte(row.JSONData), &voicePrompts); err != nil {
		log.Warnf("反序列化情绪 VoicePrompt 配置失败，使用默认配置: %v", err)
		return nil
	}

	overrides := make(map[int]string)
	for k, v := range voicePrompts {
		var id int
		if _, err := fmt.Sscanf(k, "%d", &id); err == nil {
			overrides[id] = v
		}
	}

	applyVoicePromptOverrides(overrides)
	return nil
}

func applyVoicePromptOverrides(overrides map[int]string) {
	defaults := DefaultEmotionItems()
	for i, item := range defaults {
		if vp, ok := overrides[item.ID]; ok && strings.TrimSpace(vp) != "" {
			defaults[i].VoicePrompt = vp
		}
	}
	UpdateRegistry(defaults)
}

// SaveEmotionConfigToDB 将情绪配置保存到数据库
func SaveEmotionConfigToDB(db *gorm.DB, items []EmotionItem) error {
	voicePrompts := make(map[string]string)
	defaults := DefaultEmotionItems()
	defaultMap := make(map[int]string)
	for _, d := range defaults {
		defaultMap[d.ID] = d.VoicePrompt
	}

	activeOverriddenItems := make([]EmotionItem, len(defaults))
	copy(activeOverriddenItems, defaults)

	for i, it := range activeOverriddenItems {
		for _, reqIt := range items {
			if reqIt.ID == it.ID {
				activeOverriddenItems[i].VoicePrompt = reqIt.VoicePrompt
				activeOverriddenItems[i].Enabled = reqIt.Enabled
				if reqIt.VoicePrompt != defaultMap[it.ID] {
					voicePrompts[fmt.Sprintf("%d", it.ID)] = reqIt.VoicePrompt
				}
				break
			}
		}
	}

	UpdateRegistry(activeOverriddenItems)

	if db == nil {
		return nil
	}

	jsonData, err := json.Marshal(voicePrompts)
	if err != nil {
		return fmt.Errorf("序列化情绪 VoicePrompt 配置失败: %v", err)
	}

	var row configDBRow
	err = db.Where("type = ? AND config_id = ?", "emotion_config", "global_emotion_config").First(&row).Error
	if err == gorm.ErrRecordNotFound {
		row = configDBRow{
			Type:     "emotion_config",
			ConfigID: "global_emotion_config",
			Name:     "全局情绪指令配置",
			JSONData: string(jsonData),
			Enabled:  true,
		}
		if err := db.Create(&row).Error; err != nil {
			return fmt.Errorf("创建情绪配置失败: %v", err)
		}
	} else if err != nil {
		return fmt.Errorf("查询情绪配置失败: %v", err)
	} else {
		row.JSONData = string(jsonData)
		row.Enabled = true
		if err := db.Save(&row).Error; err != nil {
			return fmt.Errorf("保存情绪配置失败: %v", err)
		}
	}

	return nil
}

// ResetEmotionConfigInDB 删除数据库中的自定义配置并恢复内置预设
func ResetEmotionConfigInDB(db *gorm.DB) error {
	ResetToDefaultRegistry()
	if db != nil {
		if err := db.Where("type = ? AND config_id = ?", "emotion_config", "global_emotion_config").Delete(&configDBRow{}).Error; err != nil {
			return fmt.Errorf("删除数据库自定义情绪配置记录失败: %v", err)
		}
	}
	return nil
}

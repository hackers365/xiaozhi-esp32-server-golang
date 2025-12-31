package types

type TtsChunkStartCallback func()
type TtsChunkEndCallback func()

type Options struct {
	SampleRate            int
	Channel               int
	FrameDuration         int
	TtsChunkStartCallback TtsChunkStartCallback // Chunk 级别：每个文本块处理开始时调用
	TtsChunkEndCallback   TtsChunkEndCallback   // Chunk 级别：每个文本块处理结束时调用
}

type Option struct {
	apply             func(opts *Options)
	implSpecificOptFn any
}

func WithTtsChunkStartCallback(callback TtsChunkStartCallback) Option {
	return Option{
		apply: func(opts *Options) {
			opts.TtsChunkStartCallback = callback
		},
	}
}

func WithTtsChunkEndCallback(callback TtsChunkEndCallback) Option {
	return Option{
		apply: func(opts *Options) {
			opts.TtsChunkEndCallback = callback
		},
	}
}

func WithSampleRate(sampleRate int) Option {
	return Option{
		apply: func(opts *Options) {
			opts.SampleRate = sampleRate
		},
	}
}

func WithChannel(channel int) Option {
	return Option{
		apply: func(opts *Options) {
			opts.Channel = channel
		},
	}
}

func WithFrameDuration(frameDuration int) Option {
	return Option{
		apply: func(opts *Options) {
			opts.FrameDuration = frameDuration
		},
	}
}

func GetImplSpecificOptions[T any](base *T, opts ...Option) *T {
	if base == nil {
		base = new(T)
	}

	for i := range opts {
		opt := opts[i]
		// 首先应用基础选项（apply函数）
		if opt.apply != nil {
			// 尝试将 base 转换为 *Options 以应用基础选项
			if optsPtr, ok := any(base).(*Options); ok {
				opt.apply(optsPtr)
			}
		}
		// 然后处理实现特定的选项
		if opt.implSpecificOptFn != nil {
			optFn, ok := opt.implSpecificOptFn.(func(*T))
			if ok {
				optFn(base)
			}
		}
	}

	return base
}

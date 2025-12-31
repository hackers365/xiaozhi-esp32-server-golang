# Eino Graph 调用方式总结

## 三种调用方式对比

### 1. Graph.Invoke()

**输入输出类型：**
- 输入：`I`（非流式）
- 输出：`O`（非流式）

**内部节点执行范式：**
- ✅ **所有内部节点使用 Invoke 范式执行**

**组件自动转换：**
- Stream 组件 → 直接使用 Stream 方法（非流式输入，流式输出）
- Transform 组件 → 自动转换为 Invoke（通过 `invokeByTransform`）
- Collect 组件 → 自动转换为 Invoke（通过 `invokeByCollect`）

**数据传递方式：**
- ❌ **非流式传递**：节点之间批量传递
- 所有流式输出会被 `concatStreamReader` 等待并合并为完整结果后传递给下游节点
- 上游节点必须等待所有 chunk 完成后，下游节点才能开始处理

**适用场景：**
- 需要完整结果的场景
- 不关注中间过程的流式输出
- 适合批量处理，所有数据准备就绪后一次性处理

**示例：**
```go
result, err := graph.Invoke(ctx, input)
// result 是完整的最终结果
```

---

### 2. Graph.Stream()

**输入输出类型：**
- 输入：`I`（非流式）
- 输出：`*schema.StreamReader[O]`（流式）

**内部节点执行范式：**
- ✅ **所有内部节点使用 Transform 范式执行**

**组件自动转换：**
- Stream 组件 → 自动转换为 Transform（通过 `transformByStream`）
- Transform 组件 → 直接使用 Transform 方法
- Collect 组件 → 自动转换为 Transform（通过 `transformByCollect`）

**数据传递方式：**
- ✅ **流式传递**：节点之间通过 `StreamReader` 流式传递
- 上游节点的每个输出 chunk 可以立即传递给下游节点
- 支持实时处理，无需等待所有数据完成

**适用场景：**
- 需要流式输出的场景
- 关注实时性和响应速度
- 适合需要立即展示结果的场景（如聊天、语音合成等）

**示例：**
```go
streamReader, err := graph.Stream(ctx, input)
for {
    chunk, err := streamReader.Recv()
    if err == io.EOF {
        break
    }
    // 立即处理每个 chunk
}
```

---

### 3. Graph.Transform()

**输入输出类型：**
- 输入：`*schema.StreamReader[I]`（流式）
- 输出：`*schema.StreamReader[O]`（流式）

**内部节点执行范式：**
- ✅ **所有内部节点使用 Transform 范式执行**

**组件自动转换：**
- Stream 组件 → 自动转换为 Transform（通过 `transformByStream`）
- Transform 组件 → 直接使用 Transform 方法
- Collect 组件 → 自动转换为 Transform（通过 `transformByCollect`）

**数据传递方式：**
- ✅ **流式传递**：节点之间通过 `StreamReader` 流式传递
- 与 Stream 方式在内部执行机制上完全相同
- 区别仅在于输入类型：Transform 接受流式输入

**适用场景：**
- 需要流式输入和流式输出的场景
- 上游数据源本身就是流式的（如音频流、视频流）
- 适合管道式处理，数据源持续产生数据

**示例：**
```go
inputStreamReader := schema.StreamReaderFromArray([]string{"chunk1", "chunk2"})
outputStreamReader, err := graph.Transform(ctx, inputStreamReader)
for {
    chunk, err := outputStreamReader.Recv()
    if err == io.EOF {
        break
    }
    // 处理每个输出 chunk
}
```

---

## 核心规则总结

### 官方文档规则

根据 Eino 官方文档：

> **以 Invoke 方式运行 Graph，内部各节点均以 Invoke 范式运行**  
> **以 Stream, Collect 或 Transform 方式运行 Graph，内部各节点均以 Transform 范式运行**

### 范式转换表

| Graph 调用方式 | 内部节点范式 | Stream 组件转换 | Transform 组件转换 | Collect 组件转换 |
|---------------|------------|----------------|-------------------|-----------------|
| `Invoke`      | Invoke     | 直接使用 Stream | `invokeByTransform` | `invokeByCollect` |
| `Stream`      | Transform  | `transformByStream` | 直接使用 Transform | `transformByCollect` |
| `Transform`   | Transform  | `transformByStream` | 直接使用 Transform | `transformByCollect` |
| `Collect`     | Transform  | `transformByStream` | 直接使用 Transform | `transformByCollect` |

### 关键差异对比

| 特性 | Invoke | Stream | Transform |
|-----|--------|--------|-----------|
| **输入类型** | 非流式 `I` | 非流式 `I` | 流式 `*StreamReader[I]` |
| **输出类型** | 非流式 `O` | 流式 `*StreamReader[O]` | 流式 `*StreamReader[O]` |
| **内部节点范式** | Invoke | Transform | Transform |
| **数据传递方式** | 批量传递 | 流式传递 | 流式传递 |
| **流式输出处理** | 等待合并 | 立即传递 | 立即传递 |
| **实时性** | 低（等待完成） | 高（即时处理） | 高（即时处理） |

---

## 实际测试观察

基于测试代码的观察：

### Invoke 方式
```
组件A 输出3个chunk → 被 concatStreamReader 合并 → 组件B 只接收到1个完整chunk
```

### Stream/Transform 方式
```
组件A 输出 chunk#1 → 立即传递给组件B → 组件B 处理 chunk#1
组件A 输出 chunk#2 → 立即传递给组件B → 组件B 处理 chunk#2
组件A 输出 chunk#3 → 立即传递给组件B → 组件B 处理 chunk#3
```

---

## 选择建议

1. **使用 Invoke**：当你需要完整的最终结果，不关心中间过程，适合批处理场景
2. **使用 Stream**：当你需要流式输出但输入是非流式的，适合实时交互场景
3. **使用 Transform**：当你需要流式输入和流式输出，适合管道式处理场景


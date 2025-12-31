package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

/**
 * 组件:
 * componentA: 输入文本(非流式), 输出文本(流式)		  Stream (使用WithOutputKey("outputA"))
 * componentB: 输入文本(流式), 输出文本(非流式)		  Collect (使用WithOutputKey("outputB"))
 * componentC: 输入文本(流式), 输出文本(流式)		  Passthrough (eino框架内置透传组件)
 *
 * 组件之间的依赖关系:
 * componentA -> componentB -> branch -> componentC -> END
 *                                    -> END (直接结束)
 */

// 组件A: 非流式输入，流式输出（Stream 范式）
// 模拟一个流式处理组件，接收非流式字符串输入，将其分割成多个输出chunk
func componentA(ctx context.Context, input string) (*schema.StreamReader[string], error) {
	fmt.Println("[组件A] Stream 方法被调用 - 开始处理非流式输入")
	fmt.Printf("[组件A] 接收到输入: %s\n", input)

	outputReader, outputWriter := schema.Pipe[string](10)

	go func() {
		defer func() {
			outputWriter.Close()
			fmt.Println("[组件A] 输出流已关闭")
		}()

		outputChunkCount := 0
		// 将输入分割成多个输出chunk（模拟流式输出）
		parts := []string{"part1", "part2", "part3"}
		for i, part := range parts {
			select {
			case <-ctx.Done():
				fmt.Println("[组件A] Context 已取消")
				return
			default:
			}

			processed := fmt.Sprintf("[A处理-%d]%s-%s", i+1, input, part)
			outputChunkCount++

			// 模拟处理延迟
			time.Sleep(50 * time.Millisecond)

			fmt.Printf("[组件A] 准备发送输出 chunk #%d: %s\n", outputChunkCount, processed)
			if closed := outputWriter.Send(processed, nil); closed {
				fmt.Println("[组件A] 输出流已关闭，停止发送")
				return
			}
			fmt.Printf("[组件A] 已发送输出 chunk #%d: %s\n", outputChunkCount, processed)
		}
		fmt.Printf("[组件A] 处理完成，共输出 %d 个chunk\n", outputChunkCount)
	}()

	return outputReader, nil
}

// 组件B: 流式输入，非流式输出（Collect 范式）
// 模拟一个收集组件，接收字符串流，收集所有内容后返回
// 当上游节点使用WithOutputKey时，输入类型是map[string]interface{}，需要从中提取值
func componentB(ctx context.Context, input *schema.StreamReader[map[string]interface{}]) (string, error) {
	fmt.Println("[组件B] Collect 方法被调用 - 开始收集流式输入")

	var result []string
	chunkCount := 0

	for {
		select {
		case <-ctx.Done():
			fmt.Println("[组件B] Context 已取消")
			return "", ctx.Err()
		default:
		}

		chunk, err := input.Recv()
		if err != nil {
			if err == io.EOF {
				fmt.Printf("[组件B] 输入流结束，共收集 %d 个 chunk\n", chunkCount)
				break
			}
			fmt.Printf("[组件B] 读取输入流失败: %v\n", err)
			return "", err
		}

		chunkCount++
		// 从map中提取outputA的值
		if outputA, ok := chunk["outputA"].(string); ok {
			result = append(result, outputA)
			fmt.Printf("[组件B] 收集到 chunk #%d: %s\n", chunkCount, outputA)
		} else {
			// 如果不是string类型，尝试转换为字符串
			result = append(result, fmt.Sprintf("%v", chunk["outputA"]))
			fmt.Printf("[组件B] 收集到 chunk #%d (从map): %v\n", chunkCount, chunk["outputA"])
		}
	}

	// 合并所有结果
	combined := fmt.Sprintf("[B收集结果]%v", result)
	fmt.Printf("[组件B] 收集完成，返回结果: %s\n", combined)
	return combined, nil
}

// 定义图状态结构（用于存储组件执行过程中的状态信息）
type graphState struct {
	componentAInput  string
	componentAOutput []string
}

// 创建 callback handler 来监听组件生命周期
func createCallbackHandler() callbacks.Handler {
	handler := callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			// 打印日志，确保函数被调用
			fmt.Printf("✅ [Callback] OnStart: name=%s, component=%v, type=%v\n", info.Name, info.Component, info.Type)
			// 判断是否是 Graph 级别的 callback
			if info.Component == compose.ComponentOfGraph {
				fmt.Println("   → Graph 级别 OnStart 被触发")
			} else {
				fmt.Printf("   → 节点 '%s' 开始执行\n", info.Name)
			}
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			// 打印日志，确保函数被调用
			fmt.Printf("✅ [Callback] OnEnd: name=%s, component=%v, type=%v\n", info.Name, info.Component, info.Type)
			// 判断是否是 Graph 级别的 callback
			if info.Component == compose.ComponentOfGraph {
				fmt.Println("   → Graph 级别 OnEnd 被触发")
			} else {
				fmt.Printf("   → 节点 '%s' 执行完成\n", info.Name)
			}
			return ctx
		}).
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			// 打印日志，确保函数被调用
			fmt.Printf("❌ [Callback] OnError: name=%s, component=%v, err=%v\n", info.Name, info.Component, err)
			// 判断是否是 Graph 级别的 callback
			if info.Component == compose.ComponentOfGraph {
				fmt.Printf("   → Graph 执行出错: %v\n", err)
			} else {
				fmt.Printf("   → 节点 '%s' 执行出错: %v\n", info.Name, err)
			}
			return ctx
		}).
		Build()

	fmt.Println("Callback handler 构建完成")
	return handler
}

func main() {
	ctx := context.Background()

	// 创建 callback handler
	handler := createCallbackHandler()

	// 创建组件
	componentANode := compose.StreamableLambda(componentA)
	componentBNode := compose.CollectableLambda(componentB)

	// 创建 graph，添加状态支持
	// 输入类型: string（非流式）
	// 输出类型: map[string]interface{}（非流式，因为使用了WithOutputKey）
	graph := compose.NewGraph[string, map[string]interface{}](
		compose.WithGenLocalState(func(ctx context.Context) *graphState {
			return &graphState{
				componentAOutput: make([]string, 0),
			}
		}),
	)

	// 添加节点
	// 为组件A添加StatePreHandler和StatePostHandler来打印参数，并使用WithOutputKey
	if err := graph.AddLambdaNode("componentA", componentANode,
		compose.WithOutputKey("outputA"),
		/*		compose.WithStatePreHandler(func(ctx context.Context, input string, state *graphState) (string, error) {
					fmt.Printf("[组件A PreHandler] 接收到输入参数: %s\n", input)
					fmt.Printf("[组件A PreHandler] Context: %v\n", ctx)
					fmt.Printf("[组件A PreHandler] State: %+v\n", state)
					// 保存输入到状态
					state.componentAInput = input
					// 返回输入，可以修改后返回
					return input, nil
				}),
				compose.WithStatePostHandler(func(ctx context.Context, output string, state *graphState) (string, error) {
					fmt.Printf("[组件A PostHandler] 接收到输出参数: %s\n", output)
					fmt.Printf("[组件A PostHandler] Context: %v\n", ctx)
					fmt.Printf("[组件A PostHandler] State: %+v\n", state)
					fmt.Printf("[组件A PostHandler] 输出参数: %s\n", output)
					return output, nil
				}),

				compose.WithStreamStatePostHandler(func(ctx context.Context, output *schema.StreamReader[string], state *graphState) (*schema.StreamReader[string], error) {
					fmt.Printf("[组件A PostHandler] 接收到输出流: %v\n", output)
					fmt.Printf("[组件A PostHandler] Context: %v\n", ctx)
					fmt.Printf("[组件A PostHandler] State: %+v\n", state)

					// 创建一个新的StreamReader和StreamWriter来转发数据
					newReader, newWriter := schema.Pipe[string](10)

					// 在goroutine中读取所有chunk并打印，同时转发到新的流
					go func() {
						defer newWriter.Close()

						chunkCount := 0
						fmt.Printf("[组件A PostHandler] 开始读取输出流的所有chunk...\n")

						for {
							select {
							case <-ctx.Done():
								fmt.Printf("[组件A PostHandler] Context已取消，停止读取\n")
								return
							default:
							}

							// 从原始output读取chunk
							chunk, err := output.Recv()
							if err != nil {
								if err == io.EOF {
									fmt.Printf("[组件A PostHandler] 输出流结束，共读取 %d 个chunk\n", chunkCount)
									break
								}
								fmt.Printf("[组件A PostHandler] 读取输出流失败: %v\n", err)
								return
							}

							chunkCount++
							// 打印每个chunk
							fmt.Printf("[组件A PostHandler] 读取到chunk #%d: %s\n", chunkCount, chunk)

							// 将chunk转发到新的流，供下游节点使用
							if closed := newWriter.Send(chunk, nil); closed {
								fmt.Printf("[组件A PostHandler] 新流已关闭，停止转发\n")
								return
							}
						}

						fmt.Printf("[组件A PostHandler] 完成读取和转发，共处理 %d 个chunk\n", chunkCount)
					}()

					// 返回新的StreamReader，供下游节点使用
					return newReader, nil
				}),*/
	); err != nil {
		panic(fmt.Sprintf("添加组件A失败: %v", err))
	}
	if err := graph.AddLambdaNode("componentB", componentBNode,
		compose.WithOutputKey("outputB"),
	); err != nil {
		panic(fmt.Sprintf("添加组件B失败: %v", err))
	}

	// 使用eino框架内置的Passthrough节点
	if err := graph.AddPassthroughNode("componentC"); err != nil {
		panic(fmt.Sprintf("添加Passthrough组件C失败: %v", err))
	}

	// 创建branch节点：根据输入内容决定路由
	// 当上游节点使用WithOutputKey时，输入类型是map[string]interface{}，需要从中提取值
	branchCondition := func(ctx context.Context, input map[string]interface{}) (string, error) {
		fmt.Printf("[Branch] 接收到输入: %v\n", input)
		// 从map中提取outputB的值
		var inputStr string
		if outputB, ok := input["outputB"].(string); ok {
			inputStr = outputB
		} else {
			// 如果不是string类型，尝试转换为字符串
			inputStr = fmt.Sprintf("%v", input["outputB"])
		}
		fmt.Printf("[Branch] 提取的输入内容: %s\n", inputStr)
		// 简单的条件判断：如果输入包含"B"则返回END，否则返回componentC
		if strings.Contains(inputStr, "B收集结果") {
			fmt.Println("[Branch] 路由到 END")
			return compose.END, nil
		}
		fmt.Println("[Branch] 路由到 componentC")
		return "componentC", nil
	}
	branch := compose.NewGraphBranch(branchCondition, map[string]bool{
		"componentC": true,
		compose.END:  true,
	})

	// 添加边
	// START -> componentA
	if err := graph.AddEdge(compose.START, "componentA"); err != nil {
		panic(fmt.Sprintf("添加边 START->componentA 失败: %v", err))
	}
	// componentA -> componentB
	if err := graph.AddEdge("componentA", "componentB"); err != nil {
		panic(fmt.Sprintf("添加边 componentA->componentB 失败: %v", err))
	}
	// componentB -> branch节点
	if err := graph.AddBranch("componentB", branch); err != nil {
		panic(fmt.Sprintf("添加分支 componentB->branch 失败: %v", err))
	}
	// componentC -> END（如果branch路由到componentC，最终也会到END）
	if err := graph.AddEdge("componentC", compose.END); err != nil {
		panic(fmt.Sprintf("添加边 componentC->END 失败: %v", err))
	}

	// 编译 graph
	runnable, err := graph.Compile(ctx)
	if err != nil {
		panic(fmt.Sprintf("编译 graph 失败: %v", err))
	}

	// 测试数据：创建一个会产生多个chunk的输入生成器
	// 我们通过创建一个模拟的StreamReader来生成多个chunk
	testInputChunks := []string{"chunk1", "chunk2", "chunk3", "chunk4", "chunk5"}
	testInput := strings.Join(testInputChunks, "")

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("测试 1: 使用 Invoke 方式调用 Graph")
	fmt.Println(strings.Repeat("=", 80))

	// 测试1: 使用 Invoke 调用（带 callback）
	startTime := time.Now()
	/*result1, err := runnable.Invoke(ctx, testInput, compose.WithCallbacks(handler))
	duration1 := time.Since(startTime)

	if err != nil {
		fmt.Printf("Invoke 调用失败: %v\n", err)
	} else {
		fmt.Printf("\n✅ Invoke 调用成功，耗时: %v\n", duration1)
		fmt.Printf("   最终结果: %s\n", result1)
	}

	time.Sleep(1 * time.Second)*/

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("测试 2: 使用 Stream 方式调用 Graph")
	fmt.Println(strings.Repeat("=", 80))

	// 测试2: 使用 Stream 调用（带 callback）
	startTime = time.Now()
	streamReader, err := runnable.Stream(ctx, testInput, compose.WithCallbacks(handler))
	if err != nil {
		panic(fmt.Sprintf("Stream 调用失败: %v", err))
	}

	fmt.Println("开始接收流式结果...")
	chunkCount := 0
	for {
		chunk, err := streamReader.Recv()
		if err != nil {
			if err == io.EOF {
				fmt.Printf("Stream 接收完成，共接收 %d 个 chunk\n", chunkCount)
				break
			}
			fmt.Printf("Stream 接收失败: %v\n", err)
			break
		}
		chunkCount++
		// 输出类型是 map[string]interface{}，需要格式化输出
		fmt.Printf("[Stream接收] chunk #%d: %v\n", chunkCount, chunk)
		// 如果包含 outputB 键，提取并打印
		if outputB, ok := chunk["outputB"].(string); ok {
			fmt.Printf("  -> 提取 outputB 值: %s\n", outputB)
		}
	}
	duration2 := time.Since(startTime)

	fmt.Printf("\n✅ Stream 调用成功，耗时: %v\n", duration2)

	time.Sleep(1 * time.Second)

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("测试 3: 使用 Transform 方式调用 Graph")
	fmt.Println(strings.Repeat("=", 80))

	// 测试3: 使用 Transform 调用
	// 首先需要创建一个 StreamReader 作为输入
	/*inputStreamReader := schema.StreamReaderFromArray([]string{testInput})

	startTime = time.Now()
	transformStreamReader, err := runnable.Transform(ctx, inputStreamReader, compose.WithCallbacks(handler))
	if err != nil {
		panic(fmt.Sprintf("Transform 调用失败: %v", err))
	}

	fmt.Println("开始接收流式结果...")
	chunkCount = 0
	for {
		chunk, err := transformStreamReader.Recv()
		if err != nil {
			if err == io.EOF {
				fmt.Printf("Transform 接收完成，共接收 %d 个 chunk\n", chunkCount)
				break
			}
			fmt.Printf("Transform 接收失败: %v\n", err)
			break
		}
		chunkCount++
		fmt.Printf("[Transform接收] chunk #%d: %s\n", chunkCount, chunk)
	}
	duration3 := time.Since(startTime)

	fmt.Printf("\n✅ Transform 调用成功，耗时: %v\n", duration3)*/

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("结论对比:")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("1. Invoke 方式:")
	fmt.Println("   - 内部节点使用 Invoke 范式执行")
	fmt.Println("   - 组件A (Stream) -> 直接使用 Stream 方法")
	fmt.Println("   - 组件B (Collect) -> 自动转换为 Invoke (通过 invokeByCollect)")
	fmt.Println("   - 所有流式输出会被 concatStreamReader 等待并合并")
	fmt.Println("   - 节点之间批量传递（非流式）")
	//fmt.Printf("   - 总耗时: %v\n", duration1)
	fmt.Println()
	fmt.Println("2. Stream 方式:")
	fmt.Println("   - 内部节点使用 Transform 范式执行")
	fmt.Println("   - 组件A (Stream) -> 自动转换为 Transform (通过 transformByStream)")
	fmt.Println("   - 组件B (Collect) -> 自动转换为 Transform (通过 transformByCollect)")
	fmt.Println("   - 节点之间流式传递（StreamReader）")
	fmt.Printf("   - 总耗时: %v\n", duration2)
	fmt.Println()
	fmt.Println("3. Transform 方式:")
	fmt.Println("   - 内部节点使用 Transform 范式执行")
	fmt.Println("   - 组件A (Stream) -> 自动转换为 Transform (通过 transformByStream)")
	fmt.Println("   - 组件B (Collect) -> 自动转换为 Transform (通过 transformByCollect)")
	fmt.Println("   - 节点之间流式传递（StreamReader）")
	//fmt.Printf("   - 总耗时: %v\n", duration3)
	fmt.Println()
	fmt.Println("关键观察点:")
	fmt.Println("  - Invoke方式：观察组件A是否等待所有输出chunk完成后才传递给组件B")
	fmt.Println("  - Stream方式：观察组件A的每个输出chunk是否能立即传递给组件B")
	fmt.Println("  - 查看日志中的时间戳和chunk接收顺序来验证流式传递")
}

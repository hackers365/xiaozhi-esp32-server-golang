package main

import (
	"context"
	"fmt"
	"io"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// 节点A: 输入string, 输出StreamReader[string]
func nodeA(ctx context.Context, input string) (string, error) {
	fmt.Printf("节点A: 输入=%s, 开始流式输出\n", input)

	return "NodeA output", nil
}

// 节点B: 输入map[string]any（包含来自A的StreamReader），输出string
// 在Invoke模式下，A的流式输出会被收集，但这里A输出的是StreamReader，所以需要特殊处理
func nodeB(ctx context.Context, input map[string]any) (string, error) {
	fmt.Printf("节点B: 输入=%v\n", input)
	result := fmt.Sprintf("[B处理]%v", input)
	fmt.Printf("节点B: 输出=%s\n", result)
	return result, nil
}

// 节点C: 输入StreamReader[map[string]any]（包含来自A和B的输出），输出string
// 节点C接收来自A和B的流式输出，每个chunk是map[string]any
func nodeC(ctx context.Context, input string) (*schema.StreamReader[string], error) {
	outputReader, outputWriter := schema.Pipe[string](10)

	go func() {
		defer outputWriter.Close()
		fmt.Printf("节点C: 输入=%v, 开始流式输出\n", input)
		outputWriter.Send(fmt.Sprintf("[C处理]%v", input), nil)
	}()

	return outputReader, nil
}

// 节点D: 输入StreamReader[map[string]any]（包含来自A和C的输出），输出string
// 节点D接收来自A和C的输出，每个chunk是map[string]any
func nodeD(ctx context.Context, input *schema.StreamReader[map[string]any]) (string, error) {
	var allChunks []string
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		chunk, err := input.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		// 从map中提取值，可能是outputA（来自A）或outputC（来自C）
		// 注意：在Invoke模式下，多个上游节点的输出会被合并到同一个map中
		fmt.Printf("节点D: 接收到原始chunk: %+v\n", chunk)
		allChunks = append(allChunks, fmt.Sprintf("%v", chunk))
	}

	result := fmt.Sprintf("[D处理]%v", allChunks)
	fmt.Printf("节点D: 输入=%v, 输出=%s\n", allChunks, result)
	return result, nil
}

func main() {
	ctx := context.Background()

	// 创建节点
	// 节点A: 使用 InvokableLambda (非流式输入 -> 非流式输出)
	nodeANode := compose.InvokableLambda(nodeA)
	// 节点B: 使用 InvokableLambda (非流式输入 -> 非流式输出)
	nodeBNode := compose.InvokableLambda(nodeB)
	// 节点C: 使用 StreamableLambda (非流式输入 -> 流式输出)
	nodeCNode := compose.StreamableLambda(nodeC)
	// 节点D: 使用 CollectableLambda (流式输入 -> 非流式输出)
	nodeDNode := compose.CollectableLambda(nodeD)

	// 创建图: 输入类型为string, 输出类型为string
	graph := compose.NewGraph[string, string]()

	// 添加节点
	if err := graph.AddLambdaNode("nodeA", nodeANode, compose.WithNodeName("nodeA"), compose.WithOutputKey("outputA")); err != nil {
		panic(fmt.Sprintf("添加节点A失败: %v", err))
	}
	if err := graph.AddLambdaNode("nodeB", nodeBNode, compose.WithNodeName("nodeB")); err != nil {
		panic(fmt.Sprintf("添加节点B失败: %v", err))
	}
	if err := graph.AddLambdaNode("nodeC", nodeCNode, compose.WithNodeName("nodeC"), compose.WithOutputKey("outputC")); err != nil {
		panic(fmt.Sprintf("添加节点C失败: %v", err))
	}
	if err := graph.AddLambdaNode("nodeD", nodeDNode); err != nil {
		panic(fmt.Sprintf("添加节点D失败: %v", err))
	}
	// 添加passThrough节点用于平衡分支
	if err := graph.AddPassthroughNode("passThrough1"); err != nil {
		panic(fmt.Sprintf("添加passThrough1节点失败: %v", err))
	}
	if err := graph.AddPassthroughNode("passThrough2"); err != nil {
		panic(fmt.Sprintf("添加passThrough2节点失败: %v", err))
	}

	// 添加边
	// START -> A
	if err := graph.AddEdge(compose.START, "nodeA"); err != nil {
		panic(fmt.Sprintf("添加边 START->nodeA 失败: %v", err))
	}
	// A -> passThrough1 -> passThrough2 -> D (分支1: A => passThrough1 => passThrough2 => D => END)
	if err := graph.AddEdge("nodeA", "passThrough1"); err != nil {
		panic(fmt.Sprintf("添加边 nodeA->passThrough1 失败: %v", err))
	}
	if err := graph.AddEdge("passThrough1", "passThrough2"); err != nil {
		panic(fmt.Sprintf("添加边 passThrough1->passThrough2 失败: %v", err))
	}
	if err := graph.AddEdge("passThrough2", "nodeD"); err != nil {
		panic(fmt.Sprintf("添加边 passThrough2->nodeD 失败: %v", err))
	}
	// A -> B (分支2: A => B => C => D => END)
	if err := graph.AddEdge("nodeA", "nodeB"); err != nil {
		panic(fmt.Sprintf("添加边 nodeA->nodeB 失败: %v", err))
	}
	// B -> C (分支2的继续)
	if err := graph.AddEdge("nodeB", "nodeC"); err != nil {
		panic(fmt.Sprintf("添加边 nodeB->nodeC 失败: %v", err))
	}
	// C -> D
	if err := graph.AddEdge("nodeC", "nodeD"); err != nil {
		panic(fmt.Sprintf("添加边 nodeC->nodeD 失败: %v", err))
	}
	// D -> END
	if err := graph.AddEdge("nodeD", compose.END); err != nil {
		panic(fmt.Sprintf("添加边 nodeD->END 失败: %v", err))
	}

	// 编译图
	runnable, err := graph.Compile(ctx)
	if err != nil {
		panic(fmt.Sprintf("编译图失败: %v", err))
	}

	// 执行图
	testInput := "测试输入"
	fmt.Println("\n开始执行图...")
	fmt.Println("输入:", testInput)
	fmt.Println()

	result, err := runnable.Invoke(ctx, testInput)
	if err != nil {
		panic(fmt.Sprintf("执行图失败: %v", err))
	}

	fmt.Println()
	fmt.Println("执行完成")
	fmt.Println("输出:", result)
}

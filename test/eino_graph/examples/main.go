package main

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

// 节点A: 输入string, 输出string
func nodeA(ctx context.Context, input string) (string, error) {
	result := fmt.Sprintf("[A处理]%s", input)
	fmt.Printf("节点A: 输入=%s, 输出=%s\n", input, result)
	return result, nil
}

// 节点B: 输入string, 输出string
func nodeB(ctx context.Context, input map[string]any) (string, error) {
	result := fmt.Sprintf("[B处理]%v", input)
	fmt.Printf("节点B: 输入=%v, 输出=%s\n", input, result)
	return "END", nil
}

// 节点C: 输入string, 输出string
func nodeC(ctx context.Context, input map[string]any) (string, error) {
	result := fmt.Sprintf("[C处理]%v", input)
	fmt.Printf("节点C: 输入=%v, 输出=%s\n", input, result)
	return "END", nil
}

func main() {
	ctx := context.Background()

	// 创建节点
	nodeANode := compose.InvokableLambda(nodeA)
	nodeBNode := compose.InvokableLambda(nodeB)
	nodeCNode := compose.InvokableLambda(nodeC)

	// 创建图: 输入类型为string, 输出类型为string
	graph := compose.NewGraph[string, string]()

	// 添加节点
	if err := graph.AddLambdaNode("nodeA", nodeANode, compose.WithNodeName("nodeA"), compose.WithOutputKey("outputA")); err != nil {
		panic(fmt.Sprintf("添加节点A失败: %v", err))
	}
	if err := graph.AddLambdaNode("nodeB", nodeBNode, compose.WithNodeName("nodeB"), compose.WithOutputKey("outputB")); err != nil {
		panic(fmt.Sprintf("添加节点B失败: %v", err))
	}
	if err := graph.AddLambdaNode("nodeC", nodeCNode); err != nil {
		panic(fmt.Sprintf("添加节点C失败: %v", err))
	}

	// 添加边
	// START -> A
	if err := graph.AddEdge(compose.START, "nodeA"); err != nil {
		panic(fmt.Sprintf("添加边 START->nodeA 失败: %v", err))
	}
	// A -> B
	if err := graph.AddEdge("nodeA", "nodeB"); err != nil {
		panic(fmt.Sprintf("添加边 nodeA->nodeB 失败: %v", err))
	}
	// A -> C
	if err := graph.AddEdge("nodeA", "nodeC"); err != nil {
		panic(fmt.Sprintf("添加边 nodeA->nodeC 失败: %v", err))
	}
	// B -> C
	if err := graph.AddEdge("nodeB", "nodeC"); err != nil {
		panic(fmt.Sprintf("添加边 nodeB->nodeC 失败: %v", err))
	}
	// C -> END
	if err := graph.AddEdge("nodeC", compose.END); err != nil {
		panic(fmt.Sprintf("添加边 nodeB->END 失败: %v", err))
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

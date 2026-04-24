package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	var (
		csvPath = flag.String("csv", "../002484.csv", "Tick数据CSV文件路径")
		speed   = flag.Float64("speed", 1.0, "测试速度倍数，0表示不延时（最快速度）")
		mode    = flag.String("mode", "normal", "Mock服务模式: normal/bull/bear/volatile/error")
		port    = flag.Int("port", 5000, "Mock服务端口")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "实时交易模拟测试工具\n")
		fmt.Fprintf(os.Stderr, "使用方法: %s [选项]\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n示例:\n")
		fmt.Fprintf(os.Stderr, "  # 正常速度测试\n  %s --csv your_ticks.csv --speed 1\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # 10倍速快速测试\n  %s --csv your_ticks.csv --speed 10\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # 全速测试（无延迟）\n  %s --csv your_ticks.csv --speed 0\n", os.Args[0])
	}

	flag.Parse()

	if *csvPath == "" {
		fmt.Println("错误: 请指定CSV文件路径")
		flag.Usage()
		os.Exit(1)
	}

	if _, err := os.Stat(*csvPath); os.IsNotExist(err) {
		fmt.Printf("错误: 文件不存在: %s\n", *csvPath)
		os.Exit(1)
	}

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("实时交易模拟测试")
	fmt.Printf("CSV文件: %s\n", *csvPath)
	fmt.Printf("测试速度: %.1fx\n", *speed)
	fmt.Printf("Mock模式: %s\n", *mode)
	fmt.Printf("Mock端口: %d\n", *port)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()

	// 运行测试
	_, err := RunTestFromCSV(*csvPath, *speed)
	if err != nil {
		fmt.Printf("测试失败: %v\n", err)
		os.Exit(1)
	}
}

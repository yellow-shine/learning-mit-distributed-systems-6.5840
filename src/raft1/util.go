package raft

import "log"

// Debug 开关：设置为 true 时启用调试日志输出
const Debug = false

// DPrintf 辅助调试函数：当 Debug 开启时向控制台打印格式化日志
func DPrintf(format string, a ...interface{}) {
	if Debug {
		log.Printf(format, a...)
	}
}

package raft

import (
	"log"

	"6.5840/raftapi"
	"6.5840/tester1"
)

// Rfproxy 用于测试器（Tester 进程）向 Raft 服务端（Raft Server 守护进程）发起 RPC 调用的客户端代理
type Rfproxy struct {
	dc *tester.DaemonClnt
}

func newRfproxy(dc *tester.DaemonClnt) *Rfproxy {
	return &Rfproxy{dc: dc}
}

// GetState 向 Raft 服务端发起 RPC 请求，查询当前任期及是否为 Leader
func (rfp *Rfproxy) GetState() (int, bool) {
	args := &GetStateArgs{}
	var rep GetStateReply
	//log.Printf("rfp.GetState %v", rep)
	if ok := rfp.dc.Call("rfsrv.GetStateRPC", args, &rep); !ok {
		log.Printf("rfp.GetState failed")
	}
	return rep.Term, rep.Leader
}

// Start 向 Raft 服务端发起 RPC 请求，提交一条新日志命令
func (rfp *Rfproxy) Start(command interface{}) (int, int, bool) {
	args := &StartArgs{
		Command: command,
	}
	var rep StartReply
	if ok := rfp.dc.Call("rfsrv.StartRPC", args, &rep); !ok {
		//log.Printf("rfp.Start %v failed", args)
	}
	//log.Printf("rfp.Start reply i %d t %d leader  %t", rep.Index, rep.Term, rep.Leader)
	return rep.Index, rep.Term, rep.Leader
}

// TesterProxy 用于 Raft 服务端向测试器（Tester）报告状态的 RPC 客户端代理
type TesterProxy struct {
	*tester.TesterClnt
}

func newTesterProxy(tc *tester.TesterClnt) *TesterProxy {
	return &TesterProxy{tc}
}

// CheckLogs 将服务端已应用的日志消息汇报给 Tester 进行正确性校验
func (tp *TesterProxy) CheckLogs(index int, m raftapi.ApplyMsg) (string, bool) {
	args := &CheckLogsArgs{
		Index: index,
		Msg:   m,
	}
	var rep CheckLogsReply
	ok := tp.Call("Test.CheckLogsRPC", args, &rep)
	if !ok {
		return "ErrRPC", false
	}
	return rep.Err, rep.Prevok
}

// IngestLog 将服务端的日志快照映射传递给 Tester
func (tp *TesterProxy) IngestLog(index int, m map[int]any) {
	args := &IngestLogArgs{
		Index: index,
		Log:   m,
	}
	var rep IngestLogReply
	ok := tp.Call("Test.IngestLogRPC", args, &rep)
	if !ok {
		//log.Printf("IngestLog: IngestLogRPC %d ok %t", index, ok)
	}
}

// ApplyErr 向 Tester 汇报状态机应用日志时的异常错误
func (tp *TesterProxy) ApplyErr(index int, err string) {
	args := &ApplyErrArgs{
		Index: index,
		Err:   err,
	}
	var rep ApplyErrReply
	ok := tp.Call("Test.ApplyErrRPC", args, &rep)
	if !ok {
		//log.Printf("ApplyErr: ApplyErrRPC %d %q ok %t", index, err, ok)
	}
}

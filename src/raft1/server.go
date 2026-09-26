package raft

import (
	"bytes"
	"fmt"
	"log"
	"sync"

	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raftapi"
	"6.5840/tester1"

)

const (
	// SnapShotInterval 每应用 10 条日志触发一次快照
	SnapShotInterval = 10
)

// Itester 定义服务端（独立进程）与测试器（Tester 独立进程）之间的通信接口
type Itester interface {
	CheckLogs(int, raftapi.ApplyMsg) (string, bool)
	IngestLog(int, map[int]any)
	ApplyErr(int, string)
}

// rfsrv 模拟运行在 Raft 之上的服务端（类似于 KVServer），负责接收 applyCh 消息并与测试器校验
type rfsrv struct {
	ts          Itester           // 与测试器通信的接口
	me          int               // 当前节点编号
	lastApplied int               // 最近应用的日志索引
	persister   *tester.Persister // 持久化组件引用

	mu   sync.Mutex
	raft raftapi.Raft       // 底层 Raft 实例
	log  map[int]any        // 记录已应用的日志命令映射（用于生成与还原快照）
}

// NewRfsrv 创建并返回新的 Raft 服务端实例及底层的 Raft 实例
func NewRfsrv(tc *tester.TesterClnt, ends []*labrpc.ClientEnd, grp tester.Tgid, srv int, persister *tester.Persister) []any {
	// tc 是与测试器通信的客户端
	ts := newTesterProxy(tc)
	s := newRfsrv(ts, ends, grp, srv, persister, tester.MaxRaftState > 0)
	return []any{s.raft, s}
}

// newRfsrv 初始化 rfsrv，根据配置决定是否启用快照功能，并启动后台应用协程
func newRfsrv(ts Itester, ends []*labrpc.ClientEnd, grp tester.Tgid, srv int, persister *tester.Persister, snapshot bool) *rfsrv {
	// 读取初始快照副本，避免与 Make() 启动的后台协程竞争
	sn := persister.ReadSnapshot()

	s := &rfsrv{
		ts:        ts,
		me:        srv,
		log:       map[int]any{},
		persister: persister,
	}
	applyCh := make(chan raftapi.ApplyMsg)
	if !tester.UseRaftStateMachine {
		s.raft = Make(ends, srv, persister, applyCh)
	}
	if snapshot {
		if sn != nil && len(sn) > 0 {
			// 模拟 KVServer 立即处理并加载快照
			err := s.ingestSnap(sn, -1)
			if err != "" {
				ts.ApplyErr(srv, err)
				log.Fatalf("ingestSnap err %v", err)
			}
			ts.IngestLog(s.me, s.log)
		}
		go s.applierSnap(applyCh)
	} else {
		go s.applier(applyCh)
	}
	return s
}

func (rs *rfsrv) Start(command interface{}) (int, int, bool) {
	rf := rs.getraft()
	if rf == nil {
		return 0, 0, false
	}
	return rf.Start(command)
}

func (rs *rfsrv) GetState() (int, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.raft.GetState()
}

func (rs *rfsrv) getraft() raftapi.Raft {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.raft
}

// applier 从 applyCh 读取已提交命令，并通过 CheckLogs RPC 发送给 Tester 验证日志顺序与一致性
func (rs *rfsrv) applier(applyCh chan raftapi.ApplyMsg) {
	for m := range applyCh {
		if m.CommandValid == false {
			// 忽略非命令类型的 ApplyMsg
		} else {
			err_msg, prevok := rs.ts.CheckLogs(rs.me, m)
			if m.CommandIndex > 1 && prevok == false {
				err_msg = fmt.Sprintf("server %v apply out of order %v", rs.me, m.CommandIndex)
			}
			if err_msg != "" {
				rs.ts.ApplyErr(rs.me, err_msg)
				// 发生错误后继续读取，防止 Raft 因为持锁阻塞在 channel 发送上
			}
		}
	}
}

// applierSnap 启用快照模式下的日志应用循环：
// 1. 处理来自 applyCh 的快照并重放
// 2. 检查日志顺序并保存到本地 log 映射中
// 3. 达到 SnapShotInterval 间隔时触发 Raft 的 Snapshot 截断
func (rs *rfsrv) applierSnap(applyCh chan raftapi.ApplyMsg) {
	if rs.raft == nil {
		return
	}

	for m := range applyCh {
		err_msg := ""
		if m.SnapshotValid {
			// 处理快照：反序列化快照并同步状态机
			err_msg = rs.ingestSnap(m.Snapshot, m.SnapshotIndex)
			rs.ts.IngestLog(rs.me, rs.log)
		} else if m.CommandValid {
			// 校验应用日志的连续性
			if m.CommandIndex != rs.lastApplied+1 {
				err_msg = fmt.Sprintf("server %v apply out of order, expected index %v, got %v", rs.me, rs.lastApplied+1, m.CommandIndex)
			}

			if err_msg == "" {
				var prevok bool
				err_msg, prevok = rs.ts.CheckLogs(rs.me, m)
				if err_msg != "ErrRPC" && m.CommandIndex > 1 && prevok == false {
					err_msg = fmt.Sprintf("server %v apply out of order %v", rs.me, m.CommandIndex)
				}
			}

			rs.log[m.CommandIndex] = m.Command // 存入本地内存 log 供生成快照使用
			rs.lastApplied = m.CommandIndex

			// 达到设定的快照间隔，执行日志压缩生成快照
			if (m.CommandIndex+1)%SnapShotInterval == 0 {
				w := new(bytes.Buffer)
				e := labgob.NewEncoder(w)
				e.Encode(m.CommandIndex)
				var xlog []any
				for j := 0; j <= m.CommandIndex; j++ {
					xlog = append(xlog, rs.log[j])
				}
				e.Encode(xlog)
				start := tester.GetAnnotatorTimestamp()
				rf := rs.getraft()
				rf.Snapshot(m.CommandIndex, w.Bytes()) // 通知 Raft 截断快照点之前的日志
				desp := fmt.Sprintf("snapshot created by %v", rs.me)
				details := fmt.Sprintf(
					"snapshot created by server %v after applying the command at index %v",
					rs.me,
					m.CommandIndex)
				tester.PostAnnotatorInfoInterval(start, desp, details)
			}
		} else {
			// 忽略其他类型的 ApplyMsg
		}
		if err_msg != "" {
			rs.ts.ApplyErr(rs.me, err_msg)
		}
	}
}

// ingestSnap 将快照数据反序列化并更新本地 log 映射和 lastApplied
func (rs *rfsrv) ingestSnap(snapshot []byte, index int) string {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if snapshot == nil {
		return "nil snapshot"
	}
	r := bytes.NewBuffer(snapshot)
	d := labgob.NewDecoder(r)
	var lastIncludedIndex int
	var xlog []any
	if d.Decode(&lastIncludedIndex) != nil ||
		d.Decode(&xlog) != nil {
		return "failed to decode snapshot"
	}
	if index != -1 && index != lastIncludedIndex {
		err := fmt.Sprintf("server %v snapshot doesn't match m.SnapshotIndex", rs.me)
		return err
	}
	rs.log = map[int]any{}
	for j := 0; j < len(xlog); j++ {
		rs.log[j] = xlog[j]
	}
	rs.lastApplied = lastIncludedIndex
	return ""
}

// GetStateArgs 查询状态 RPC 参数
type GetStateArgs struct{}

// GetStateReply 查询状态 RPC 响应
type GetStateReply struct {
	Term   int
	Leader bool
}

// GetStateRPC 处理来自 Tester 的 GetState RPC
func (rs *rfsrv) GetStateRPC(args *GetStateArgs, rep *GetStateReply) {
	rep.Term, rep.Leader = rs.GetState()
}

// StartArgs 提交命令 RPC 参数
type StartArgs struct {
	Command any
}

// StartReply 提交命令 RPC 响应
type StartReply struct {
	Index  int
	Term   int
	Leader bool
}

// StartRPC 处理来自 Tester 的 Start RPC
func (rs *rfsrv) StartRPC(args *StartArgs, rep *StartReply) {
	rep.Index, rep.Term, rep.Leader = rs.Start(args.Command)
}

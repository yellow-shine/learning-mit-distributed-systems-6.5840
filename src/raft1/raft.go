package raft

// The file ../raftapi/raftapi.go defines the interface that raft must
// expose to servers (or the tester), but see comments below for each
// of these functions for more details.
//
// In addition,  Make() creates a new raft peer that implements the
// raft interface.

import (
	"bytes"
	"math/rand"
	"sync"
	"time"

	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raftapi"
	"6.5840/tester1"
)

// 节点角色状态常量
const (
	follower  = iota // 追随者
	candidate        // 候选人
	leader           // 领导者
)

// Leader 发送心跳的固定周期（100毫秒）
const heartbeatInterval = 100 * time.Millisecond

// LogEntry 日志条目结构体
type LogEntry struct {
	Term    int         // 生成该日志条目时 Leader 的任期号
	Command interface{} // 上层状态机执行的命令
}

// Raft 实现了单个 Raft 节点的共识算法对象
type Raft struct {
	mu        sync.Mutex          // 互斥锁，保护节点的所有共享可变状态
	peers     []*labrpc.ClientEnd // 集群中所有节点 RPC 通信端点列表
	persister *tester.Persister   // 持久化存储接口，保存 Raft 元数据与快照
	me        int                 // 当前节点在 peers[] 中的索引编号（节点 ID）

	// 应用层通信与同步
	applyCh   chan raftapi.ApplyMsg // 向应用层（状态机）交付已提交日志或快照的通道
	applyCond *sync.Cond            // 条件变量，当 commitIndex 推进时唤醒 applier 后台协程

	// 所有服务器上的持久化状态（需在持久化中保存）
	currentTerm       int        // 服务器已知最新的任期号（首次启动为 0，单调递增）
	votedFor          int        // 当前任期内收到本节点选票的候选人 ID（若无则为 -1）
	log               []LogEntry // 日志条目切片；log[0] 为占位哨兵项（对应 lastIncludedIndex）
	lastIncludedIndex int        // 快照中包含的最后一条日志的全局索引（日志压缩）
	lastIncludedTerm  int        // 快照中包含的最后一条日志的任期号

	// 快照数据
	snapshot []byte // 当前持久化的快照原始字节数据

	// 所有服务器上的易失状态
	commitIndex int // 已知已被提交的最高日志条目的全局索引（初始为 0）
	lastApplied int // 已经被应用到状态机的最高日志条目的全局索引（初始为 0）

	// Leader 节点上的易失状态（选举后重新初始化）
	nextIndex  []int // 针对每个节点，需要发送给其的下一个日志索引（初始化为 Leader 最后一条日志 + 1）
	matchIndex []int // 针对每个节点，已知已同步复制到该节点的最高日志索引（初始化为 0）

	// 角色与选举定时器状态
	role            int           // 当前节点角色（follower, candidate, leader）
	electionTimeout time.Duration // 选举超时时长（在 300~500ms 间随机选取，避免选票瓜分）
	lastReset       time.Time     // 上次重置选举计时器的时间戳
}

// GetState 返回节点当前的任期号以及是否为 Leader
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.role == leader
}

// resetElectionLocked 重置选举计时器：记录当前时间并随机生成 300~500ms 的超时时间（调用方须持锁）
func (rf *Raft) resetElectionLocked() {
	rf.lastReset = time.Now()
	rf.electionTimeout = time.Duration(300+rand.Intn(200)) * time.Millisecond
}

// becomeFollowerLocked 将节点转为 Follower 状态（调用方须持锁）：
// 若发现了更高的任期，则更新 currentTerm、重置 votedFor 并持久化；同时重置选举计时
func (rf *Raft) becomeFollowerLocked(term int) {
	if term > rf.currentTerm {
		rf.currentTerm = term
		rf.votedFor = -1
		rf.persist()
	}
	rf.role = follower
	rf.resetElectionLocked()
}

// lastIndexLocked 返回当前日志切片中最后一条日志的全局逻辑索引（调用方须持锁）
func (rf *Raft) lastIndexLocked() int {
	return rf.lastIncludedIndex + len(rf.log) - 1
}

// termLocked 根据全局逻辑索引 i 获取对应日志的任期号（自动计算快照偏移，调用方须持锁）
func (rf *Raft) termLocked(i int) int {
	return rf.log[i-rf.lastIncludedIndex].Term
}

// persist 将 Raft 的持久化元数据（currentTerm、votedFor、log、快照元信息）编码后存入 persister
func (rf *Raft) persist() {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludedTerm)
	rf.persister.Save(w.Bytes(), rf.snapshot)
}

// readPersist 从 persister 中恢复 Raft 的持久化元数据（节点崩溃重启恢复）
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 {
		return
	}
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var term int
	var votedFor int
	var log []LogEntry
	var lastIdx int
	var lastTerm int
	if d.Decode(&term) != nil || d.Decode(&votedFor) != nil || d.Decode(&log) != nil ||
		d.Decode(&lastIdx) != nil || d.Decode(&lastTerm) != nil {
		return
	}
	rf.currentTerm = term
	rf.votedFor = votedFor
	rf.log = log
	rf.lastIncludedIndex = lastIdx
	rf.lastIncludedTerm = lastTerm
}

// PersistBytes 获取当前 Raft 持久化状态的字节数大小（用于测试器评估内存/磁盘开销）
func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

// Snapshot 上层服务（如 KVServer）主动调用：通知 Raft 截断 index 及其之前的日志，并保存快照
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// 如果 index 小于等于已快照点，或超出已提交范围，则忽略本次请求
	if index <= rf.lastIncludedIndex || index > rf.commitIndex {
		return
	}
	// 计算在当前内存 log 中的偏移量并丢弃 index 之前的历史日志
	off := index - rf.lastIncludedIndex
	rf.log = append([]LogEntry{}, rf.log[off:]...)
	rf.lastIncludedIndex = index
	rf.lastIncludedTerm = rf.log[0].Term
	rf.snapshot = snapshot
	// 推进应用指针
	if rf.lastApplied < index {
		rf.lastApplied = index
	}
	rf.persist()
}

// RequestVoteArgs 请求投票 RPC 请求参数（对应 Raft 论文 Figure 2）
type RequestVoteArgs struct {
	Term         int // 候选人的任期号
	CandidateId  int // 请求选票的候选人 ID
	LastLogIndex int // 候选人最后一条日志的全局索引
	LastLogTerm  int // 候选人最后一条日志的任期号
}

// RequestVoteReply 请求投票 RPC 响应结果
type RequestVoteReply struct {
	Term        int  // 投票者当前任期号，供候选人更新自己
	VoteGranted bool // 是否投了赞成票
}

// lastLogLocked 辅助函数：返回当前节点最后一条日志的全局索引和任期号（调用方须持锁）
func (rf *Raft) lastLogLocked() (int, int) {
	i := rf.lastIndexLocked()
	return i, rf.termLocked(i)
}

// moreUpToDate 比较两份日志谁更“新”（遵循 Raft 论文 5.4.1 选举限制）：
// 1. 如果两者的最后日志任期号不同，任期号更大的更新
// 2. 如果任期号相同，日志更长（全局索引更大）的更新
func moreUpToDate(candLastIdx, candLastTerm, myLastIdx, myLastTerm int) bool {
	if candLastTerm != myLastTerm {
		return candLastTerm > myLastTerm
	}
	return candLastIdx >= myLastIdx
}

// RequestVote 处理来自候选人的拉票请求 RPC
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 规则 1：如果候选人任期大于本地任期，转为 Follower
	if args.Term > rf.currentTerm {
		rf.becomeFollowerLocked(args.Term)
	}
	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	// 规则 2：如果候选人任期小于本地任期，直接拒绝投票
	if args.Term < rf.currentTerm {
		return
	}

	// 规则 3：如果本地未给其他节点投票（或已经投给该候选人），且候选人日志至少和本地一样新，则投赞成票
	lastIdx, lastTerm := rf.lastLogLocked()
	if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) &&
		moreUpToDate(args.LastLogIndex, args.LastLogTerm, lastIdx, lastTerm) {
		rf.votedFor = args.CandidateId
		rf.persist()
		rf.resetElectionLocked() // 投出选票后重置选举超时
		reply.VoteGranted = true
	}
}

// sendRequestVote 发送 RequestVote RPC
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// AppendEntriesArgs 追加日志 / 心跳 RPC 请求参数
type AppendEntriesArgs struct {
	Term         int        // Leader 的当前任期号
	LeaderId     int        // Leader 的节点 ID（供 Follower 重定向客户端）
	PrevLogIndex int        // 紧接在本次新追加条目之前的日志索引
	PrevLogTerm  int        // PrevLogIndex 对应条目的任期号
	Entries      []LogEntry // 准备追加的日志条目（心跳时为空切片）
	LeaderCommit int        // Leader 的已知已提交日志索引 commitIndex
}

// AppendEntriesReply 追加日志 / 心跳 RPC 响应结果
type AppendEntriesReply struct {
	Term          int  // 目标节点当前任期号，供 Leader 更新自身
	Success       bool // 若 Follower 在 PrevLogIndex 处的日志与 PrevLogTerm 匹配，则为 true
	ConflictIndex int  // 快速冲突回退：冲突任期的第一条日志索引，或 Follower 日志长度+1
	ConflictTerm  int  // 快速冲突回退：冲突位置处的 Follower 日志任期号（若无则为 -1）
}

// AppendEntries 处理来自 Leader 的追加日志与心跳请求 RPC
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 规则 1：如果 Leader 任期小于本地当前任期，返回 false
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.Success = false
		return
	}

	// 规则 2：如果发现更高的任期或当前非 Follower，转换为 Follower；否则重置选举计时器
	if args.Term > rf.currentTerm || rf.role != follower {
		rf.becomeFollowerLocked(args.Term)
	} else {
		rf.resetElectionLocked()
	}

	reply.Term = rf.currentTerm

	// 规则 3：日志一致性校验及快速冲突回退优化（ConflictIndex/ConflictTerm）
	// 情况 3.1：PrevLogIndex 已被本地快照截断，让 Leader 从快照后第一条日志开始同步
	if args.PrevLogIndex < rf.lastIncludedIndex {
		reply.Success = false
		reply.ConflictIndex = rf.lastIncludedIndex + 1
		reply.ConflictTerm = -1
		return
	}
	// 情况 3.2：本地日志长度不足以包含 PrevLogIndex，返回本地下一条日志索引作为回退点
	if args.PrevLogIndex > rf.lastIndexLocked() {
		reply.Success = false
		reply.ConflictIndex = rf.lastIndexLocked() + 1
		reply.ConflictTerm = -1
		return
	}
	// 情况 3.3：PrevLogIndex 处的日志任期与 Leader 不匹配
	if rf.termLocked(args.PrevLogIndex) != args.PrevLogTerm {
		reply.Success = false
		reply.ConflictTerm = rf.termLocked(args.PrevLogIndex)
		// 找到本地该冲突任期出现的第一条日志索引，直接跳过整个冲突任期
		idx := args.PrevLogIndex
		for idx > rf.lastIncludedIndex && rf.termLocked(idx-1) == reply.ConflictTerm {
			idx--
		}
		reply.ConflictIndex = idx
		return
	}

	// 规则 4：追加新日志（若发现已有条目与新条目冲突，则截断冲突处及之后的内容并追加新日志）
	for i, e := range args.Entries {
		idx := args.PrevLogIndex + 1 + i
		off := idx - rf.lastIncludedIndex
		if off < len(rf.log) {
			if rf.log[off].Term != e.Term {
				rf.log = rf.log[:off]
				rf.log = append(rf.log, args.Entries[i:]...)
				break
			}
		} else {
			rf.log = append(rf.log, args.Entries[i:]...)
			break
		}
	}
	if len(args.Entries) > 0 {
		rf.persist()
	}

	// 规则 5：如果 LeaderCommit > commitIndex，将 commitIndex 推进为 min(LeaderCommit, 最新新日志索引)
	if args.LeaderCommit > rf.commitIndex {
		lastNew := args.PrevLogIndex + len(args.Entries)
		if lastNew > rf.commitIndex {
			if args.LeaderCommit < lastNew {
				rf.commitIndex = args.LeaderCommit
			} else {
				rf.commitIndex = lastNew
			}
			rf.applyCond.Broadcast() // 唤醒 applier 协程应用新提交的日志
		}
	}
	reply.Success = true
}

// sendAppendEntries 发送 AppendEntries RPC
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	return rf.peers[server].Call("Raft.AppendEntries", args, reply)
}

// InstallSnapshotArgs 安装快照 RPC 请求参数
type InstallSnapshotArgs struct {
	Term              int    // Leader 的当前任期号
	LeaderId          int    // Leader 的 ID
	LastIncludedIndex int    // 快照所包含的最后一条日志的全局索引
	LastIncludedTerm  int    // 快照所包含的最后一条日志的任期号
	Data              []byte // 快照原始二进制数据
}

// InstallSnapshotReply 安装快照 RPC 响应结果
type InstallSnapshotReply struct {
	Term int // 目标节点当前任期号，供 Leader 更新自身
}

// InstallSnapshot 处理来自 Leader 的安装快照 RPC
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	// 校验 Leader 任期
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		rf.mu.Unlock()
		return
	}
	if args.Term > rf.currentTerm {
		rf.becomeFollowerLocked(args.Term)
	} else {
		rf.resetElectionLocked()
	}
	reply.Term = rf.currentTerm

	// 如果收到的快照比本地已有的快照旧或相同，则无需重复安装
	if args.LastIncludedIndex <= rf.lastIncludedIndex {
		rf.mu.Unlock()
		return
	}

	// 日志裁剪：如果本地日志中包含快照之后的日志，且对应位点任期一致，则保留后续日志；否则清空日志
	if args.LastIncludedIndex < rf.lastIndexLocked() &&
		rf.termLocked(args.LastIncludedIndex) == args.LastIncludedTerm {
		off := args.LastIncludedIndex - rf.lastIncludedIndex
		rf.log = append([]LogEntry{}, rf.log[off:]...)
	} else {
		rf.log = []LogEntry{{Term: args.LastIncludedTerm}}
	}
	rf.lastIncludedIndex = args.LastIncludedIndex
	rf.lastIncludedTerm = args.LastIncludedTerm
	rf.snapshot = args.Data
	if rf.commitIndex < args.LastIncludedIndex {
		rf.commitIndex = args.LastIncludedIndex
	}
	if rf.lastApplied < args.LastIncludedIndex {
		rf.lastApplied = args.LastIncludedIndex
	}
	rf.persist()

	// 封装快照类型的 ApplyMsg 推入 applyCh，通知上层状态机加载快照
	msg := raftapi.ApplyMsg{
		SnapshotValid: true,
		Snapshot:      args.Data,
		SnapshotTerm:  args.LastIncludedTerm,
		SnapshotIndex: args.LastIncludedIndex,
	}
	rf.mu.Unlock()
	rf.applyCh <- msg
}

// maybeCommitLocked 检查是否有满足多数派复制的日志可以被提交（仅限 Leader 调用，调用方须持锁）
func (rf *Raft) maybeCommitLocked() {
	// 从最新未提交的日志往前倒序查找
	for n := rf.lastIndexLocked(); n > rf.commitIndex; n-- {
		// 遵循 Raft 论文 5.4.2 节约束：Leader 只能主动提交属于当前任期的日志
		if rf.termLocked(n) != rf.currentTerm {
			continue
		}
		// 统计复制了该日志（matchIndex >= n）的节点总数（包含 Leader 自身）
		count := 1
		for i := range rf.peers {
			if i != rf.me && rf.matchIndex[i] >= n {
				count++
			}
		}
		// 超过集群半数则确认提交
		if count > len(rf.peers)/2 {
			rf.commitIndex = n
			rf.applyCond.Broadcast() // 唤醒 applier 协程推入 applyCh
			return
		}
	}
}

// sendToPeer Leader 向指定节点 i 持续同步日志或快照，直到追赶上最新进度或不再是 Leader
func (rf *Raft) sendToPeer(i int) {
	for {
		rf.mu.Lock()
		// 如果不再是 Leader，停止同步
		if rf.role != leader {
			rf.mu.Unlock()
			return
		}

		// 情况 1：如果 Follower 缺失的日志已经被本地快照丢弃，则发送 InstallSnapshot RPC
		if rf.nextIndex[i] <= rf.lastIncludedIndex {
			args := InstallSnapshotArgs{
				Term:              rf.currentTerm,
				LeaderId:          rf.me,
				LastIncludedIndex: rf.lastIncludedIndex,
				LastIncludedTerm:  rf.lastIncludedTerm,
				Data:              rf.snapshot,
			}
			term := rf.currentTerm
			rf.mu.Unlock()
			reply := InstallSnapshotReply{}
			ok := rf.peers[i].Call("Raft.InstallSnapshot", &args, &reply)
			rf.mu.Lock()
			if !ok {
				rf.mu.Unlock()
				return
			}
			// 发现更高任期，立即降级为 Follower
			if reply.Term > rf.currentTerm {
				rf.becomeFollowerLocked(reply.Term)
				rf.mu.Unlock()
				return
			}
			// 如果身份发生变更或任期不一致，放弃处理
			if rf.role != leader || rf.currentTerm != term {
				rf.mu.Unlock()
				return
			}
			// 快照安装成功，更新该节点的进度索引
			rf.nextIndex[i] = args.LastIncludedIndex + 1
			rf.matchIndex[i] = args.LastIncludedIndex
			rf.maybeCommitLocked()
			rf.mu.Unlock()
			return
		}

		// 情况 2：通过 AppendEntries RPC 发送增量日志或心跳
		next := rf.nextIndex[i]
		if next < rf.lastIncludedIndex+1 {
			next = rf.lastIncludedIndex + 1
		}
		if next > rf.lastIndexLocked()+1 {
			next = rf.lastIndexLocked() + 1
		}
		prev := next - 1
		args := AppendEntriesArgs{
			Term:         rf.currentTerm,
			LeaderId:     rf.me,
			PrevLogIndex: prev,
			PrevLogTerm:  rf.termLocked(prev),
			Entries:      append([]LogEntry{}, rf.log[next-rf.lastIncludedIndex:]...),
			LeaderCommit: rf.commitIndex,
		}
		rf.mu.Unlock()

		reply := AppendEntriesReply{}
		if !rf.sendAppendEntries(i, &args, &reply) {
			return // RPC 失败直接结束本轮
		}

		rf.mu.Lock()
		// 检查是否有更高任期
		if reply.Term > rf.currentTerm {
			rf.becomeFollowerLocked(reply.Term)
			rf.mu.Unlock()
			return
		}
		if rf.role != leader || rf.currentTerm != args.Term {
			rf.mu.Unlock()
			return
		}

		// 日志同步成功
		if reply.Success {
			rf.nextIndex[i] = args.PrevLogIndex + 1 + len(args.Entries)
			rf.matchIndex[i] = rf.nextIndex[i] - 1
			rf.maybeCommitLocked() // 尝试推进提交点 commitIndex
			rf.mu.Unlock()
			return
		}

		// 日志同步冲突，使用快速回退优化（Fast Rollback）加速 nextIndex 定位
		if reply.ConflictTerm < 0 {
			// Follower 在 PrevLogIndex 处没有日志，直接跳到 Follower 的日志末尾后一位
			rf.nextIndex[i] = reply.ConflictIndex
		} else {
			// Follower 存在任期冲突，Leader 查找自己是否有冲突任期的日志
			last := -1
			for j := rf.lastIndexLocked(); j >= rf.lastIncludedIndex; j-- {
				if rf.termLocked(j) == reply.ConflictTerm {
					last = j
					break
				}
			}
			if last >= 0 {
				// Leader 拥有该任期日志，尝试从该任期最后一条日志的下一位开始
				rf.nextIndex[i] = last + 1
			} else {
				// Leader 没有该任期日志，跳过 Follower 该冲突任期的全部日志
				rf.nextIndex[i] = reply.ConflictIndex
			}
		}
		if rf.nextIndex[i] < 1 {
			rf.nextIndex[i] = 1
		}
		rf.mu.Unlock()
	}
}

// broadcastAppend Leader 向所有其他节点并发发送追加日志/心跳
func (rf *Raft) broadcastAppend() {
	rf.mu.Lock()
	if rf.role != leader {
		rf.mu.Unlock()
		return
	}
	rf.mu.Unlock()
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go rf.sendToPeer(i)
	}
}

// startElection Candidate 节点发起一轮新的选举
func (rf *Raft) startElection() {
	rf.mu.Lock()
	rf.role = candidate         // 转换为候选人
	rf.currentTerm++            // 自增任期号
	rf.votedFor = rf.me         // 投自己一票
	rf.persist()                // 持久化任期与投票信息
	term := rf.currentTerm
	lastIdx, lastTerm := rf.lastLogLocked()
	rf.resetElectionLocked()     // 重置选举计时器
	rf.mu.Unlock()

	votes := 1 // 自己的一票
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go func(i int) {
			args := RequestVoteArgs{
				Term:         term,
				CandidateId:  rf.me,
				LastLogIndex: lastIdx,
				LastLogTerm:  lastTerm,
			}
			reply := RequestVoteReply{}
			if !rf.sendRequestVote(i, &args, &reply) {
				return
			}
			rf.mu.Lock()
			defer rf.mu.Unlock()

			// 发现更高任期，退回 Follower
			if reply.Term > rf.currentTerm {
				rf.becomeFollowerLocked(reply.Term)
				return
			}
			// 如果当前已不是同一轮选举的 Candidate，直接放弃
			if rf.role != candidate || rf.currentTerm != term {
				return
			}
			// 统计收到的选票
			if reply.VoteGranted {
				votes++
				// 赢得超过半数选票，当选 Leader
				if votes > len(rf.peers)/2 {
					rf.role = leader
					rf.nextIndex = make([]int, len(rf.peers))
					rf.matchIndex = make([]int, len(rf.peers))
					last := rf.lastIndexLocked() + 1
					for j := range rf.peers {
						rf.nextIndex[j] = last // 初始化为本地最后一条日志 + 1
						rf.matchIndex[j] = 0   // 初始化为 0
					}
					// 立即向全集群广播心跳，宣告 Leader 确立并抑制其他选举
					go rf.broadcastAppend()
				}
			}
		}(i)
	}
}

// Start 供上层服务（如 KV 存储）提交新命令发起共识
// 返回值：
// - index: 该命令被分配的日志全局索引（若非 Leader 则无意义）
// - term: 该命令分配时的任期号
// - isLeader: 当前节点是否为 Leader（客户端仅能向 Leader 提交命令）
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	// 如果非 Leader，直接拒绝并返回 false
	if rf.role != leader {
		term := rf.currentTerm
		rf.mu.Unlock()
		return -1, term, false
	}
	// 追加到 Leader 本地日志末尾并持久化
	rf.log = append(rf.log, LogEntry{Term: rf.currentTerm, Command: command})
	rf.persist()
	index := rf.lastIndexLocked()
	term := rf.currentTerm
	rf.mu.Unlock()

	// 异步触发广播日志追加，加快复制效率
	go rf.broadcastAppend()
	return index, term, true
}

// applier 后台守护协程：负责将已达成共识提交的日志按序推入 applyCh，交付给上层状态机执行
func (rf *Raft) applier() {
	for {
		rf.mu.Lock()
		// 条件等待：当没有新提交的日志可应用时阻塞挂起
		for rf.lastApplied >= rf.commitIndex {
			rf.applyCond.Wait()
		}
		// 计算本次需要应用的日志范围 [start, end]
		start := rf.lastApplied + 1
		end := rf.commitIndex
		base := rf.lastIncludedIndex
		if start <= base {
			start = base + 1
		}
		var entries []LogEntry
		if start <= end {
			// 拷贝待应用的日志切片，避免释放锁后产生并发读写
			entries = append([]LogEntry{}, rf.log[start-base:end-base+1]...)
		}
		rf.lastApplied = end
		// 注意：向 applyCh 发送前必须释放锁，防止 channel 接收端处理慢导致持锁死锁
		rf.mu.Unlock()

		for i, e := range entries {
			rf.applyCh <- raftapi.ApplyMsg{
				CommandValid: true,
				Command:      e.Command,
				CommandIndex: start + i,
			}
		}
	}
}

// ticker 后台时钟轮询协程：周期性驱动心跳发送与选举超时检测
func (rf *Raft) ticker() {
	for {
		// 小步长高频轮询（每 10ms）以获得及时的超时响应
		time.Sleep(10 * time.Millisecond)
		rf.mu.Lock()
		role := rf.role
		due := time.Since(rf.lastReset) >= rf.electionTimeout
		rf.mu.Unlock()

		if role == leader {
			// Leader 角色：周期性向 Follower 广播心跳以维持权威
			rf.broadcastAppend()
			time.Sleep(heartbeatInterval)
		} else if due {
			// Follower / Candidate 角色：若超时未收到有效心跳，发起选举
			rf.startElection()
		}
	}
}

// Make 创建并初始化一个 Raft 节点实例，启动后台工作协程
// 参数说明：
// - peers: 所有节点的 RPC 连接端点
// - me: 当前节点在 peers[] 中的索引
// - persister: 持久化存储接口
// - applyCh: 交付 ApplyMsg 给状态机的通道
func Make(peers []*labrpc.ClientEnd, me int,
	persister *tester.Persister, applyCh chan raftapi.ApplyMsg) raftapi.Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	rf.applyCh = applyCh
	rf.applyCond = sync.NewCond(&rf.mu)
	rf.votedFor = -1
	rf.log = []LogEntry{{Term: 0}} // 初始化占位哨兵项
	rf.role = follower
	rf.resetElectionLocked()

	// 从持久化存储恢复元数据与快照（支持 Crash-Recovery）
	rf.readPersist(persister.ReadRaftState())
	rf.snapshot = persister.ReadSnapshot()
	if rf.lastIncludedIndex > 0 {
		rf.commitIndex = rf.lastIncludedIndex
		rf.lastApplied = rf.lastIncludedIndex
	}

	// 启动后台事件驱动协程
	go rf.ticker()
	go rf.applier()

	return rf
}

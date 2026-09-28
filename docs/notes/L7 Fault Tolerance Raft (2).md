好，这次直接把 **MIT 6.824 Lecture 7 — Fault Tolerance: Raft (2)** 完整串起来。

这一讲真正难的地方，不是 `AppendEntries` RPC 本身，而是理解这句话：

> **Replication 很容易；真正困难的是在 Leader crash、旧 Leader 复活、log 分叉、节点重启的情况下，确定“哪些历史已经不可撤销”。**

当前 6.5840 课程仍然把 Raft 分成两讲；Raft (2) 紧接 Leader Election / basic replication，并进一步讨论 practical fault tolerance，随后课程进入 Linearizability 和 ZooKeeper。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

---

# Part 1：这节课到底想解决什么问题？

### 如果只能记住一个问题

> **一个 Raft entry 到底什么时候真正变成“历史事实”，使得无论以后发生多少次 Leader crash 和重新选举，都不可能被另一个 command 覆盖？**

Raft (1) 给我们留下一个看起来已经不错的系统：

```
Client
   |
   v
Leader S1
  /    \
 v      v
S2     S3
```

Leader：

```
1. 收 Client command
2. append 到自己 log
3. 复制给 Followers
4. majority 收到
5. commit
```

看起来结束了。

但 Distributed System 中真正的问题发生在第 3、4 步附近。

例如：

```
S1: [A] [B]
S2: [A] [B]
S3: [A]
```

突然 S1 crash。

如果 S3 成了新 Leader，然后写：

```
S3: [A] [C]
```

怎么办？

现在：

```
S1: [A] [B]
S2: [A] [B]
S3: [A] [C]
```

哪个历史是真的？

---

单机系统一般不会面对这种问题，因为只有：

```
一个 WAL
一个 durable history
一个 authoritative copy
```

Distributed System 却可能同时存在：

```
Old Leader
New Leader
Delayed RPC
Partitioned Leader
Restarted Follower
```

每个人都持有某一段“曾经看起来合法”的历史。

所以 Raft (2) 的核心不是：

> 怎么复制 log？

而是：

> **怎么让“已经 committed 的历史”穿过 Leader changes，仍然不可推翻？**

---

最 naive 的方法是：

> **谁的 log 最长，谁当 Leader。**

但是这是错的。

考虑：

```
S1: [A term1] [X term1] [Y term1] [Z term1]

S2: [A term1] [B term2]
S3: [A term1] [B term2]
```

虽然 S1 长：

```
len(S1) = 4
```

但 S2/S3 的最后 entry 来自更新的 Leader epoch：

```
term2
```

如果 `B` 已经成为不可撤销历史，而你仅仅因为 S1 较长让它当 Leader，就可能：

```
[A B]
  ↓
[A X Y Z]
```

把已经确认的历史删除。

所以 Raft 判断日志 freshness 时，**term 比 length 更重要**。

这就是这一讲开始进入真正 Consensus safety 的地方。

---

# Part 2：在整个 6.824 知识地图里的位置

完整关系是：

```
RPC / Threads
      ↓
机器如何通信、并发
      ↓
GFS / Replication
      ↓
复制能提高容错
但 replica 会 diverge
      ↓
Primary-Backup
      ↓
需要决定：
谁是 Primary？
      ↓
Raft (1)
      ↓
Leader Election
Term
Heartbeat
Basic Log Replication
      ↓
问题：
Leader 换掉之后，
旧历史还安全吗？
      ↓
Raft (2)
      ↓
Log Matching
Election Restriction
Commit Rule
Persistence
Recovery
Snapshot
      ↓
Replicated State Machine
      ↓
Linearizability
      ↓
ZooKeeper / replicated service
```

理解几个边界非常重要。

### Replication vs Consensus

Replication：

```
把数据复制到多台机器
```

例如：

```
Leader → F1
       → F2
```

Consensus：

```
即使失败、并发、Leader change，
大家仍然对“最终历史”达成一致。
```

所以：

```
Replication ≠ Consensus
```

你完全可以复制出：

```
S1 = X
S2 = Y
S3 = Z
```

---

### Consensus vs State Machine Replication

Consensus 更抽象：

> 大家对一个 decision 达成一致。

State Machine Replication：

> 对无限长的 commands 序列达成一致，并按同一顺序执行。

```
Consensus:
choose X

SMR:
choose
[A, B, C, D, E, ...]
```

Raft实际上是在管理：

```
Replicated Log
        ↓
Ordered Commands
        ↓
State Machine
```

Raft 论文明确把自己定位为管理 replicated log 的 consensus algorithm。[Raft](https://raft.github.io/raft.pdf?utm_source=chatgpt.com)

---

### Raft vs Paxos

粗略 mental model：

```
Paxos
  ↓
解决 consensus 的核心安全问题

Multi-Paxos
  ↓
连续产生 decisions

Raft
  ↓
直接围绕 replicated log
设计完整 protocol：
leader election
log replication
safety
recovery
snapshot
membership
```

不是说 Paxos 做不到这些。

而是：

> Raft 把构建实际 Replicated State Machine 所需要的大量机制组织成了容易推理的结构。

MIT 课程也明确指出，可以把 Raft 看作解决了比“单次 Paxos”更完整的工程问题：长期 replicated log、crash recovery、missed messages 等。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/questions.html?lec=6&q=q-raft2&utm_source=chatgpt.com)

---

# Part 3：本讲最重要的 7 个 Mental Models

我建议你真正掌握下面 7 个。

---

### Concept 1：Log Matching

#### 它解决什么？

Followers 的日志可能分叉：

```
Leader:   A B C D
Follower: A B X Y
```

怎么安全修？

#### 一句话

> 如果两个 log 在同一个 index 上具有相同 term，那么从开头到这个位置的整个 prefix 都相同。

例如：

```
index:   1  2  3  4
S1:      A  B  C  D
term:    1  1  2  3

S2:      A  B  C
term:    1  1  2
```

因为：

```
index=3
term=2
```

匹配，所以：

```
[1..3]
```

也匹配。

这是 AppendEntries 的：

```
prevLogIndex
prevLogTerm
```

真正要建立的 invariant。

---

### Concept 2：Leader Completeness

#### 一句话

> 一个 entry 一旦 committed，它一定存在于所有未来 term 的 Leader 中。

这是整讲最重要的性质之一。

```
term 3:
X committed

term 4 Leader
    ↓
must contain X

term 5 Leader
    ↓
must contain X

term 100 Leader
    ↓
must contain X
```

否则：

```
Client:
Put(x=1) → success

后来换 Leader

Get(x)
→ 0
```

之前的 success 就变成谎言了。

---

### Concept 3：Election Restriction

Raft 不能允许任何 Candidate 当 Leader。

投票者要求：

```
candidate log 至少和我一样 up-to-date
```

比较规则不是：

```
谁更长
```

而是：

```
先比较 lastLogTerm
再比较 lastLogIndex
```

即：

```
(candidateLastTerm > myLastTerm)
OR
(candidateLastTerm == myLastTerm
 && candidateLastIndex >= myLastIndex)
```

Raft Figure 2 中 RequestVote 就包含 `lastLogIndex` 和 `lastLogTerm`，用于这个检查。[Department of Computer Science](https://www.cs.fsu.edu/~awang/courses/cop5611_s2026/raft.pdf?utm_source=chatgpt.com)

为什么 term 优先？

因为：

> term 表示这段 log 最后经过哪一代 Leader。

例如：

```
A:
index 1 2 3 4 5 6
term  1 1 1 1 1 1

B:
index 1 2 3
term  1 1 2
```

虽然：

```
len(A) > len(B)
```

但：

```
lastTerm(B)=2 > lastTerm(A)=1
```

所以 B 更新。

---

## Concept 4：Stored ≠ Committed ≠ Applied

这是 Raft 最重要的区分之一。

```
stored
   ↓
entry 已写进某节点 log

replicated
   ↓
多个节点已有 entry

committed
   ↓
Raft 已决定这个 entry
永远不会被推翻

applied
   ↓
entry 已交给 state machine 执行
```

例如：

```
Client: Put(x=10)

Leader:
append log         ← stored

Followers:
receive            ← replicated

majority confirmed
                    ← committed

stateMachine.Put()
                    ← applied
```

千万不要形成：

```
log 中存在
=
committed
```

的直觉。

这是错的。

---

## Concept 5：current-term commit rule

这是这一讲最容易真正卡住的地方。

很多人想：

> 一个 entry 已经存在于 majority 上，不就 committed 了吗？

**不一定。**

Raft 的 Leader 只通过“数副本”直接 commit：

> **当前 term 创建的 entry。**

Leader commit advancement 的关键条件相当于：

```
存在 N：

N > commitIndex

majority 的 matchIndex >= N

并且：

log[N].term == currentTerm
```

Raft 的标准规则就是这么定义的。[Stack Overflow](https://stackoverflow.com/questions/77413871/how-can-leader-get-elected-without-entries-stored-in-majority-servers?utm_source=chatgpt.com)

为什么多一个：

```
log[N].term == currentTerm
```

？

后面我们专门构造 counterexample。

---

## Concept 6：Persistent State

Raft 中有些东西 crash 后绝不能忘。

```
currentTerm
votedFor
log[]
```

必须 persistent，而且要在相应 RPC 回复前安全写入 stable storage。Raft Figure 2 明确把这三项列为 persistent state。[Department of Computer Science](https://www.cs.fsu.edu/~awang/courses/cop5611_s2026/raft.pdf?utm_source=chatgpt.com)

而：

```
commitIndex
lastApplied
```

基础模型里可以 volatile。

Leader 特有：

```
nextIndex[]
matchIndex[]
```

也是 volatile。

---

## Concept 7：Snapshot

Log：

```
1 2 3 4 5 ... 10,000,000
```

不能永久保存。

如果：

```
1..9,000,000
```

已经执行，state machine 当前状态已经等价于这些操作：

```
state = f(log[1..9000000])
```

那么可以：

```
Snapshot(state @ index=9,000,000)
```

然后只保留：

```
snapshot
+
log[9,000,001...]
```

这不是改变 Consensus。

而是：

> **压缩已经决定的历史。**

---

# Part 4：System Model / Assumptions

先把 Raft 能处理和不能处理的 failure 分清。

### Node Model

Raft 经典模型是：

```
Crash failure
+
Crash-recovery
```

节点可能：

```
正常
↓
crash
↓
restart
```

它不会：

```
撒谎
伪造消息
恶意发不同协议消息
```

所以：

```
Byzantine Fault
```

不在 Raft failure model 内。

---

### Persistent state

节点重启后保留：

```
currentTerm
votedFor
log[]
```

为什么？

稍后详细解释。

---

## Network Model

网络可以出现：

```
packet loss
delay
reordering
duplicate RPC
partition
```

协议不能依赖：

```
“500ms 没回复 = 对方肯定死了”
```

Timeout 只能意味着：

```
我现在不能确认它还正常工作
```

而不是：

```
它一定 crash
```

---

## Timing Model

Safety 本质上不依赖一个可靠的 latency upper bound。

但 Liveness 需要：

> 网络最终有一段足够稳定的时期，使 election 和 heartbeat 能成功。

所以工程上通常把它理解成：

```
eventually / partially synchronous
```

Raft 论文常用的可用性条件是：

```
broadcastTime << electionTimeout << MTBF
```

直觉上：

```
正常 RPC 要比 election timeout 快很多。
```

---

## Failure tolerance

假设：

```
N = 2f + 1
```

那么最多可以容忍：

```
f
```

个节点不可用，而继续 commit。

例如：

```
N=3
f=1

N=5
f=2

N=7
f=3
```

原因不是魔法公式。

5 节点时：

```
majority = 3
```

失去两个：

```
剩余 3
```

还可以形成 Quorum。

失去三个：

```
剩余 2
```

不能 commit。

重要的是：

> minority partition 不会继续写，而不是冒险制造两个历史。

---

# Part 5：Happy Path

先从最简单情况开始。

3 个节点：

```
S1 Leader
S2 Follower
S3 Follower
```

当前：

```
index     1
          A
term      1
```

Client：

```
Put(x=10)
```

### Step 1：Leader append

S1：

```
index     1   2
          A   B
term      1   2
```

其中：

```
B = Put(x=10)
```

---

### Step 2：AppendEntries

S1 → S2/S3：

```
AppendEntries {
    term = 2

    prevLogIndex = 1
    prevLogTerm  = 1

    entries = [
        {index=2, term=2, command=Put(x=10)}
    ]

    leaderCommit = 1
}
```

注意：

```
prevLogIndex
prevLogTerm
```

不是装饰。

它是在问：

> “你和我的历史，到 index=1 为止是否相同？”

---

### Step 3：Follower consistency check

S2 有：

```
index1 = (A, term1)
```

所以：

```
prevLogIndex=1
prevLogTerm=1
```

match。

于是 append：

```
S2: A B
```

并回复：

```
success
```

---

### Step 4：Majority

Leader 得知：

```
S1 has B
S2 has B
```

3 节点中的 2：

```
majority
```

且：

```
B.term == currentTerm == 2
```

所以：

```
commitIndex = 2
```

---

### Step 5：Apply

Leader：

```
lastApplied < commitIndex
```

所以：

```
apply B
```

State machine：

```
x = 10
```

随后 heartbeat 告诉 followers：

```
leaderCommit=2
```

followers 也执行。

---

# Part 6：用 timeline 看清状态

```
time →

Client:
       Put(x=10)
          |
          v

S1:
       append B
          |
          +-------- Append B --------+
          |                          |
          |                          |
S2:      receive B                   |
          |                          |
          +---- ACK ---------------->|
                                     |
S1:                           majority
                               commit B
                                  |
                               apply B
                                  |
Client:                         SUCCESS

S3:
                 delayed...
                           receive B later
```

某个瞬间可能是：

```
S1:
stored = B
committed = B
applied = B

S2:
stored = B
committed? 可能还不知道

S3:
甚至没有 B
```

这里很重要：

> **Commit 是全局协议事实，但每个节点对它的 knowledge 可能不同。**

S2 已经保存 B，却还没收到新的：

```
leaderCommit
```

所以它可能还不知道 B committed。

---

# Part 7：第一个 Failure —— log divergence

考虑：

```
term1

S1 Leader

S1: A
S2: A
S3: A
```

Client：

```
write X
```

S1：

```
A X
```

但 partition：

```
       S1
       |
       X
       |
     S2---S3
```

X 没有 majority：

```
S1: A X
S2: A
S3: A
```

因此：

```
X uncommitted
```

---

S2/S3 重新选 Leader。

S2 term2：

```
S2 Leader

Client → write Y
```

复制：

```
S2: A Y
S3: A Y
```

Y committed。

现在网络恢复：

```
S1: A X
S2: A Y
S3: A Y
```

怎么办？

答案：

> **新 Leader 的 log 赢。**

S1 的 X 必须删除。

---

## Log Repair 是怎么发生的？

S2 给 S1：

```
AppendEntries:

prevLogIndex = 1
prevLogTerm = term(A)

entries = Y
```

S1：

```
index1 = A
```

匹配。

然后发现：

```
index2:
local  = X term1
leader = Y term2
```

conflict。

所以 follower：

```
delete X and everything after it
append Y
```

得到：

```
S1: A Y
```

---

这揭示一个非常关键的原则：

> **Follower 的 uncommitted log 不是权威历史。**

它可以被删除。

---

# Part 8：nextIndex 是什么？

假设 divergence 更深：

```
Leader:
A B C D E

Follower:
A B X Y
```

Leader 一开始不知道 follower 从哪里开始不同。

Leader 为 follower 保存：

```
nextIndex[follower]
```

直觉：

> 下一次应该从哪个 index 开始给它发。

刚成为 Leader 时通常初始化为：

```
leaderLastIndex + 1
```

例如：

```
nextIndex = 6
```

尝试：

```
prevIndex=5
```

Follower：

```
我没有 index 5
```

reject。

Leader 后退：

```
nextIndex=5
```

再试。

……

直到找到：

```
index2
```

是共同 prefix。

然后：

```
truncate X Y
append C D E
```

最终：

```
Follower:
A B C D E
```

Stanford 对 Raft 的讲解也把这个过程概括为：失败时递减 `nextIndex` 重试，找到匹配 prefix 后 follower 删除 conflicting suffix，Leader 迫使 followers 的日志收敛到自己的日志。[Stanford University](https://web.stanford.edu/~ouster/cs190-winter24/lectures/raft/?utm_source=chatgpt.com)

实际实现通常会用 conflict term/index 优化，不必一个 index 一个 index 回退，但 mental model 先理解 basic algorithm。

---

# Part 9：真正困难的问题 —— Leader 怎么保证自己拥有 committed history？

到这里出现一个严重问题。

假设：

```
S1:
A B C D

S2:
A B

S3:
A B
```

如果：

```
C/D
```

已经 committed，而 S2 当 Leader：

```
S2:
A B X
```

它可能覆盖：

```
C D
```

所以必须保证：

> **缺少 committed entry 的 server 不能成为 Leader。**

这就是：

```
Leader Completeness
```

的核心。

---

## Election Restriction

投票时 follower 不只是判断：

```
term
votedFor
```

还会检查 candidate log。

Candidate：

```
RequestVote {
    term
    candidateId
    lastLogIndex
    lastLogTerm
}
```

Receiver：

```
candidate 是否至少和我一样 up-to-date？
```

判断：

```
candidateLastTerm > myLastTerm
```

或者：

```
candidateLastTerm == myLastTerm
AND
candidateLastIndex >= myLastIndex
```

才可能投票。[Department of Computer Science](https://www.cs.fsu.edu/~awang/courses/cop5611_s2026/raft.pdf?utm_source=chatgpt.com)

---

## 为什么这和 Quorum 结合就很强？

假设 entry X committed。

意味着至少存储在一个 majority：

```
Committed X:

S1 ✓
S2 ✓
S3 ✓
S4
S5
```

未来 Candidate 想成为 Leader，也必须拿 majority：

```
Vote quorum:
至少 3 个
```

任意两个 5 节点 majority：

```
size 3
```

必然 overlap：

```
old majority ∩ new majority ≠ ∅
```

所以未来 Leader 的 voters 中，至少有一个：

```
见过 committed history
```

它不会随便投给明显落后的 candidate。

这就是：

```
Quorum intersection
+
Election restriction
```

组合起来产生的力量。

---

# Part 10：为什么“majority 存在”仍然不一定等于 committed？

这是 Raft 最 subtle 的地方。

考虑 5 台机器。

最初 S1 在 term 2 当 Leader：

```
S1: A X(2)
S2: A X(2)
S3: A
S4: A
S5: A
```

X 还没 commit。

随后：

```
term3
```

S5 当 Leader，并产生：

```
Y(term3)
```

例如：

```
S5: A Y(3)
```

---

之后 term4，S1 又当上 Leader。

它把旧的：

```
X(term2)
```

复制给：

```
S1
S2
S3
```

现在：

```
X
```

存在于 5 台中的 3 台：

```
majority!
```

naive 想法：

```
X committed!
```

但是这是不安全的。

---

为什么？

当前状态类似：

```
S1: A X(2)
S2: A X(2)
S3: A X(2)

S4: A
S5: A Y(3)
```

S1 crash。

S5 竞选新的 Leader。

比较：

```
S5 lastTerm = 3
S1/S2/S3 lastTerm = 2
```

所以：

```
term3 > term2
```

S5 的 log 被认为更 up-to-date。

它可能拿到 majority，然后覆盖：

```
X
```

得到：

```
A Y
```

所以：

> **X 虽然曾出现在 majority，却仍然可能消失。**

Raft 论文专门用 Figure 8 说明这个陷阱：旧 term entry 即使之后存在于 majority，也不能只靠 replica count 直接判为 committed。[Google Groups](https://groups.google.com/g/raft-dev/c/OMh3VX0kuQY?utm_source=chatgpt.com)

---

## 那怎么安全？

S1 当前是：

```
term4 Leader
```

它首先追加当前 term entry：

```
Z(term4)
```

例如：

```
S1: A X(2) Z(4)
S2: A X(2) Z(4)
S3: A X(2) Z(4)
```

Z：

```
current term = 4
majority replicated
```

因此 Z committed。

而由于 log 是有序 prefix：

```
X precedes Z
```

所以：

```
X
```

也随之 committed。

这是非常重要的区别：

```
❌ 旧 term X：
majority replication
→ 直接 commit

✅ 当前 term Z：
majority replication
→ commit Z
→ 因为 log prefix
→ 间接 commit X
```

---

## 为什么这个 current-term entry 如此关键？

因为现在任何未来 candidate 如果没有：

```
Z(term4)
```

它的：

```
lastLogTerm < 4
```

那么至少那个保存 Z 的 majority 中节点会认为它落后。

而未来 candidate 同样需要 majority。

Quorum intersection 使它绕不过这个已保存 current-term entry 的集合。

所以：

```
current-term commit
```

相当于把历史：

```
A X
```

“钉死”。

---

# Part 11：Committed 的真正 Mental Model

所以不要把 committed 理解为：

> “复制了很多份。”

更好的理解：

> **Committed = 已经进入所有未来合法 Leader 都无法摆脱的历史。**

这是更接近 Consensus 本质的定义。

也就是说：

```
replication
回答：
“现在有几份？”

commitment
回答：
“未来还能不能反悔？”
```

这两个问题完全不同。

---

# Part 12：State / Variables

标准 Raft Server 主要状态如下。

### Persistent

```
currentTerm
votedFor
log[]
```

### Volatile on all servers

```
commitIndex
lastApplied
```

### Volatile on Leader

```
nextIndex[]
matchIndex[]
```

标准 Figure 2 也是这样划分。[Department of Computer Science](https://www.cs.fsu.edu/~awang/courses/cop5611_s2026/raft.pdf?utm_source=chatgpt.com)

---

## currentTerm

```
currentTerm
```

表示：

> 我见过的最大 epoch / generation。

必须 persistent。

否则：

```
term=10
crash
restart
term=0
```

它可能接受来自：

```
term=3
```

的旧 Leader。

---

## votedFor

表示：

```
这个 term 我投给谁
```

必须 persistent。

否则：

```
term5:
S1 votes A

S1 crash
S1 restart

忘记 vote

S1 votes B
```

一个节点：

```
same term
```

投两票。

于是理论上可能破坏：

```
Election Safety
```

这也是为什么 `votedFor` 必须穿过 restart 保留。[Google Groups](https://groups.google.com/g/raft-dev/c/ngrzCfgjYqo?utm_source=chatgpt.com)

---

## log[]

当然也必须 persistent。

想象 follower：

```
Leader:
把 X 发给 Follower

Follower:
写内存
返回 ACK

Follower:
crash
```

如果 log 不 persistent：

```
X 消失
```

但 Leader 可能已经根据这个 ACK 把 X 算进 majority。

整个 commit reasoning 就失效了。

MIT 过去的考试也专门考过这一点：即便 entry 当时在 follower 看来尚未 committed，它也必须在 ACK 前持久化，因为 Leader 可能基于这个 ACK commit。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q21-1-sol.pdf?utm_source=chatgpt.com)

---

## commitIndex

为什么可以 volatile？

因为它是：

> **knowledge about commit**

而不是产生 safety 的唯一证据。

Follower 重启后：

```
Leader heartbeat:
leaderCommit = N
```

可以重新告诉它。

---

## lastApplied

基础 Raft 模型中 State Machine 可以通过：

```
snapshot + committed log replay
```

重新恢复。

所以这也是为什么 `lastApplied` 在算法表里不是 Raft persistent consensus state。

真实持久化 application 则需要非常谨慎地协调：

```
application state
snapshot index
Raft log
dedup metadata
```

否则会出现 duplicate apply / state mismatch。

---

## nextIndex / matchIndex

只是当前 Leader 对 followers replication progress 的 knowledge：

```
nextIndex[S2] = 下一个要发送的位置

matchIndex[S2] = 已确认 S2 拥有到哪里
```

Leader crash 后重新计算即可。

所以不用 persistent。

---

# Part 13：最重要的 Invariants

Raft 的正确性不是靠“感觉靠谱”。

它围绕几个强 invariants。

### 1. Election Safety

```
每个 term 最多一个 Leader
```

否则：

```
Leader A: append X
Leader B: append Y
```

同一代就产生两个历史。

---

## 2. Leader Append-Only

Leader：

```
只 append 自己的 log
```

不会修改自己已经存在的 entries。

Follower 的 suffix 可以被新 Leader overwrite。

---

## 3. Log Matching Property

如果：

```
logA[i].term == logB[i].term
```

且 index 也相同，

那么：

```
logA[1..i] == logB[1..i]
```

---

## 4. Leader Completeness

如果：

```
entry E
committed in term T
```

那么：

```
∀ future leader term U > T:
E ∈ Leader_U.log
```

---

## 5. State Machine Safety

如果一个 server：

```
apply(index=10, X)
```

那么任何 server 永远不能：

```
apply(index=10, Y)
```

其中：

```
X != Y
```

这是最终用户真正关心的东西。

Raft 论文也把 State Machine Safety 作为核心 safety property，并给出了相关形式化 specification / proof work。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf?utm_source=chatgpt.com)

---

# Part 14：Safety 为什么成立？

把推理链连起来：

```
Entry E committed
      ↓
E 存在于一个 majority
      ↓
未来 Leader Election
也需要一个 majority
      ↓
两个 majority 必相交
      ↓
至少一个 voter 见过相关历史
      ↓
Election Restriction
不允许更 stale 的 candidate 轻易赢
      ↓
Current-term commit rule
避免“旧 entry 虽 majority 但仍可覆盖”的漏洞
      ↓
未来 Leader 包含 committed E
      ↓
Leader 不会覆盖 E
      ↓
同一个 index 不会出现不同 committed command
      ↓
State Machine Safety
```

这就是 Raft safety 的主干。

---

## Safety vs Liveness

一定要分开。

### Safety

> 坏事永远不发生。

即使：

```
网络断几个小时
RPC 无限 delay
Leader 一直选不出来
```

仍然不能：

```
两个不同 command
都 committed 在同一个 index
```

---

### Liveness

> 好事最终发生。

例如：

```
Client request
最终 commit
```

这需要额外条件：

```
大多数节点活着
+
它们最终能通信
+
election eventually succeeds
```

---

例如网络分裂：

```
5 nodes

Side A:
S1 S2

Side B:
S3 S4 S5
```

minority：

```
2
```

可能旧 Leader 就在这里。

但它：

```
不能获得 majority
```

所以：

```
不能 commit
```

majority side：

```
3
```

可以选新 Leader 继续运行。

这体现了：

```
Safety > Availability
```

在没有 quorum 的情况下，Raft宁愿停止。

---

# Part 15：Old Leader 是一个极好的 failure scenario

网络：

```
Old Leader S1
    |
    X partition
    |
S2---S3
```

S2/S3：

```
term=6
new leader=S2
```

但 S1 仍然：

```
term=5
state=Leader
```

注意：

> 分布式系统不存在一个神奇 notification 告诉 S1：“你已经不是 Leader 了。”

S1 完全可能继续认为：

```
I am leader
```

这就是为什么：

```
Leader status
```

不是永久 authority。

它必须通过：

```
term
+
quorum communication
```

维持有效性。

---

如果 S1 收 Client write：

```
Put(x=5)
```

它可以：

```
append locally
```

但不能：

```
majority replicate
```

所以不能 commit。

等网络恢复，S1 收到：

```
term=6
```

就必须：

```
currentTerm = 6
become follower
```

然后自己的 speculative suffix 可能被 S2 覆盖。

---

# Part 16：Read 是另一个陷阱

很多工程师此时会想：

> Write 需要 Raft，Read 直接读 Leader 本地内存不就行了吗？

不一定。

考虑旧 Leader：

```
      old S1
        |
        X partition
        |
      S2---S3

S2 已经是 term6 Leader
```

S1 仍然觉得自己是 Leader。

Client：

```
Get(x)
```

如果 S1：

```
直接读本地状态
```

可能返回 stale data。

所以：

```
Leader local read
```

并不自动 linearizable。

---

一种最简单但昂贵的方法：

```
把 Read 也写进 Raft log
```

例如：

```
Get(x)
→ log
→ majority
→ apply
→ read
```

这样很安全，但每个 read 都要 consensus。

MIT 当前 KV lab 就明确允许这种简单方案：把 `Get` 和 `Put` 都通过 Raft，以避免没有 majority 的旧 leader 返回 stale read。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-kvraft1.html?utm_source=chatgpt.com)

优化方式通常是：

```
Leader
↓
确认自己仍然拥有 quorum
↓
确认自己掌握足够新的 commit state
↓
再执行 local read
```

现代实现中你经常会看到类似：

```
ReadIndex
```

的设计。

---

# Part 17：Client retry / duplicate operation

再一个现实问题：

```
Client → Leader:
Put(balance -= 100)
```

Leader：

```
commit
apply
```

然后：

```
Leader crash
```

就在 Client 收到 response 之前。

Client看到：

```
timeout
```

它无法知道：

```
A. request 没执行

还是

B. request 已经执行，但 reply 丢了
```

于是 retry。

如果新 Leader 再执行一次：

```
balance -= 100
balance -= 100
```

就错了。

所以 Replicated State Machine 通常需要：

```
clientID
requestSequence
```

例如：

```
Client 123:
request #52
```

State machine 记录：

```
lastRequest[123] = 52
```

重试：

```
request #52
```

直接返回之前结果，不重新执行。

因此：

```
Raft
```

主要解决：

```
replica ordering / consensus
```

而：

```
at-most-once client semantics
```

是上层 service 还需要解决的问题。

---

# Part 18：Crash Recovery

现在节点真正 crash。

```
S3
 ↓
power loss
 ↓
restart
```

它恢复：

```
currentTerm
votedFor
log[]
```

例如：

```
log:
A B C D
```

但是可能：

```
commitIndex=0
lastApplied=0
```

然后 Leader 很快告诉它：

```
leaderCommit=4
```

于是：

```
commitIndex=4
```

并重新：

```
apply A
apply B
apply C
apply D
```

理论上可行。

问题是：

```
10 亿条 log
```

怎么办？

这就进入 Snapshot。

---

# Part 19：Snapshot 为什么必须出现？

假设 etcd 运行一年：

```
log:
1
2
3
...
1,000,000,000
```

现在 KV state 也许只有：

```
foo = 123
bar = 456
...
```

过去亿万个 operations：

```
Put(foo=1)
Put(foo=2)
Put(foo=3)
...
```

已经体现在当前 State Machine state 里。

永久保存全部 log 会导致：

```
disk usage ↑
restart replay time ↑
catch-up cost ↑
```

Raft 论文 Section 7 的目的就是解决 unbounded log growth，snapshot 是其主要 compaction 方法。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf?utm_source=chatgpt.com)

---

## Snapshot Mental Model

假设：

```
log:

1 A
2 B
3 C
4 D
5 E
6 F
```

已经：

```
applied through index 4
```

State：

```
State4 = execute(A,B,C,D)
```

保存：

```
snapshot {
    state = State4

    lastIncludedIndex = 4
    lastIncludedTerm = term(log[4])
}
```

然后可以删除：

```
A B C D
```

保留：

```
Snapshot@4
+
E F
```

---

注意必须保存：

```
lastIncludedIndex
lastIncludedTerm
```

因为 Raft仍然需要知道：

> snapshot 边界处的 log identity。

否则 `AppendEntries` 无法继续做 Log Matching。

---

# Part 20：Follower 落后到 Leader 已经没 log 了怎么办？

Leader：

```
snapshot through 10000

log:
10001
10002
...
10500
```

Follower crash 很久：

```
Follower only has through 5000
```

普通 AppendEntries 没办法给：

```
5001..10000
```

因为 Leader 已经删除了。

怎么办？

```
InstallSnapshot RPC
```

Leader：

```
Leader
  |
  | InstallSnapshot(snapshot@10000)
  v
Follower
```

Follower：

```
state := snapshot
lastIncludedIndex = 10000
```

然后继续：

```
10001
10002
...
```

MIT Raft lab 也正是要求：如果 follower 落后到 leader 已经 discard 它需要的 entries，Leader 需要发送 snapshot，再继续发送 snapshot 后的 log。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)

---

## Snapshot 的一个重要 invariant

Snapshot 不能让 State Machine：

```
倒退
```

假设 follower 已经：

```
lastApplied = 12000
```

却收到：

```
snapshot@10000
```

如果直接 install：

```
state 回退到 10000
```

就错了。

因此 implementation 必须处理 stale snapshot。

当前 MIT paper question 甚至直接要求思考：

> InstallSnapshot 是否可能让 state machine backwards in time？

说明这正是实现时很容易踩的边界。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/questions.html?lec=6&q=q-raft2&utm_source=chatgpt.com)

---

# Part 21：Failure Matrix

|Failure|会发生什么|Safety|Availability|机制|
|---|---|---|---|---|
|Follower crash|少一个 replica|保持|quorum 尚在则保持|persistent log + catch-up|
|Leader crash|暂时停止写|保持|election 后恢复|term + election|
|Packet loss|RPC retry|保持|可能变慢|idempotent AppendEntries|
|Duplicate RPC|重复收到|保持|基本无影响|index/term matching|
|Delayed old RPC|term 可能过旧|保持|无显著影响|currentTerm reject|
|Network partition|minority 停止 commit|保持|majority side 可继续|quorum|
|Old Leader survives|可能仍自认为 Leader|保持|minority 无写能力|majority + term|
|Node restart|volatile state 丢失|保持|catch-up 后恢复|persisted term/vote/log|
|Log divergence|follower suffix 不同|保持|Leader 修复|AppendEntries consistency|
|Very stale follower|已落后于 compacted log|保持|snapshot 后恢复|InstallSnapshot|
|Majority unavailable|无法 commit|保持|不可用|CP trade-off|
|Byzantine node|可撒谎/破坏协议|**不保证**|不保证|超出 Raft model|
|Persistent storage corruption|视实现而定|经典模型不保证|视情况|checksums / backups 等工程措施|

---

# Part 22：Top 7 Misconceptions

### ❌ 1. Majority 有 entry = committed

不总是。

正确说法：

```
当前 term entry
+
majority
→ 可以直接 commit
```

旧 term entries 可以通过后续 current-term committed entry 间接 commit。

---

### ❌ 2. Leader 的 log 一定最长

错。

例如：

```
A:
lastIndex=100
lastTerm=3

B:
lastIndex=80
lastTerm=5
```

B 更 up-to-date。

Raft：

```
term first
index second
```

---

### ❌ 3. Leader 永远有最新数据

当前合法 Leader：

```
必须包含所有 committed history
```

但未必包含所有曾经存在过的 uncommitted entries。

某个旧 Leader：

```
A B X Y Z
```

可能长度非常长，但这些都可以被删除。

---

### ❌ 4. Timeout 证明对方死了

Timeout 只能说明：

```
我暂时没收到它的消息
```

原因可能：

```
crash
network delay
packet loss
GC pause
partition
CPU stall
```

---

### ❌ 5. commit 和 apply 是一回事

不是。

```
commitIndex=100
lastApplied=97
```

完全合法。

它意味着：

```
98..100
已经决定
但 State Machine 尚未执行
```

---

### ❌ 6. Snapshot 是备份

不是主要概念。

Snapshot 在 Raft 中主要是：

```
log compaction
+
restart/catch-up optimization
```

备份涉及：

```
disaster recovery
off-cluster copies
restore policy
```

是另一层问题。

---

### ❌ 7. Raft 保证 exactly-once client request

Raft 本身不自动解决：

```
reply lost → Client retry
```

导致 duplicate command 的问题。

需要上层：

```
request IDs
deduplication
```

---

# Part 23：和 Linearizability 的关系

下一讲正好是 Linearizability，这不是偶然。

Raft 内部建立：

```
single committed log history
```

然后 State Machine：

```
按 log 顺序 apply
```

这给构建 Linearizable service 提供基础。

但：

```
Raft safety
≠
自动所有 API 都 Linearizable
```

例如你乱做 follower read：

```
Client → stale Follower → Get()
```

仍然可能 stale。

所以要区分：

```
Consensus layer:
历史是什么？

API consistency layer:
客户端能看到什么？
```

下一讲就是从内部：

```
log
commit
leader
```

转向外部观察：

```
operation start/end
real-time ordering
```

---

# Part 24：和 ZooKeeper 的关系

下一阶段 ZooKeeper 更像是在问：

> 既然 Consensus / replication 如此贵，应用到底需要什么 coordination abstraction？

Raft：

```
Consensus protocol
```

ZooKeeper：

```
coordination service
```

提供：

```
znode
watch
ephemeral node
ordering semantics
```

ZooKeeper 的 replication protocol 并不是 Raft，而是 Zab。

但 mental model 类似：

```
Leader
+
ordered transaction log
+
replicas
+
leader change
+
committed prefix
```

---

# Part 25：和 2PC 的区别

这是你已经学过的东西，值得明确区分。

Raft：

```
多个 replicas
对同一个 replicated state/history 达成一致
```

2PC：

```
多个不同 resource managers
决定一个 distributed transaction：
commit or abort
```

例如：

```
Raft:
3 个 etcd nodes
共同维护一个 replicated KV history

2PC:
Orders DB
Payments DB
Inventory DB

共同决定：
Transaction T commit?
```

所以：

```
Consensus
≠
Atomic Commit
```

不过 Coordinator 自身为了容错，完全可以：

```
2PC Coordinator state
    ↓
replicated by Raft
```

---

# Part 26：和 Spanner 的关系

以后看 Spanner，可以看到两层问题。

```
一个 shard / tablet 的 replicas
        ↓
Consensus

多个 shards
        ↓
Distributed Transaction / 2PC
```

大概：

```
replication problem
      +
transaction problem
```

两层不是同一个问题。

Raft 主要帮你解决前一层。

---

# Part 27：和 Sharding 的关系

Raft：

```
解决 availability / correctness
```

Sharding：

```
解决 scalability
```

单个 Raft group：

```
Leader bottleneck
```

吞吐量有限。

于是大系统往往：

```
Shard A → Raft Group 1
Shard B → Raft Group 2
Shard C → Raft Group 3
```

这就是：

```
Multi-Raft
```

类架构的核心思路。

MIT 当前课程的后续 sharded KV lab 也是把 shards 分到多个 Raft-replicated groups 中，以获得并行吞吐。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-shard1.html?utm_source=chatgpt.com)

---

# Part 28：真实系统联系

### etcd

最典型映射：

```
Raft
 ↓
etcd replicated log
 ↓
KV state
```

Client：

```
Put("/foo", "bar")
```

概念上：

```
Proposal
   ↓
Raft log
   ↓
committed
   ↓
apply to etcd state machine
```

---

## Kubernetes

再往上一层：

```
kubectl
   ↓
API Server
   ↓
etcd
   ↓
Raft
```

你熟悉的 Controller：

```
Desired State
    ↓
Observe
    ↓
Reconcile
```

和 Raft 的关系不是：

```
Controller = Raft state machine
```

不能这么类比。

准确地说：

> Kubernetes Controller 的 desired/observed state 存在 API objects 里；etcd 为这些 authoritative objects 提供 fault-tolerant storage 和一致的 ordered updates。

所以：

```
Raft
解决：
“控制平面 authoritative state 如何不因节点 crash 丢掉？”

Controller
解决：
“如何让现实世界逐渐接近 desired state？”
```

完全是两个层次。

---

## Cloud Control Plane

例如你熟悉的：

```
CreateCluster
CreateVPC
CreateNodePool
```

如果 control plane metadata 需要强一致：

```
cluster ID → desired config
region → state
job → state transition
```

可以把 replicated metadata service 理解成：

```
Client/API
   ↓
Consensus log
   ↓
State Machine
   ↓
Desired State DB
   ↓
Controller/Reconciler
   ↓
Cloud resources
```

Raft负责的是：

```
Desired State history 本身不能 split-brain
```

Controller负责的是：

```
现实资源最终 converge
```

---

## Terraform

Terraform 本身通常不是一个 Consensus protocol。

但是类比非常有帮助：

```
Raft:
authoritative ordered log
→ state machine

Terraform:
authoritative desired configuration/state
→ provider operations
```

差异：

```
Raft:
重点是多个 replicas 对同一 history 的 consensus

Terraform:
重点是 desired vs actual infrastructure reconciliation
```

所以不要把：

```
Terraform state locking
```

误认为：

```
Raft Consensus
```

---

## Kafka

Kafka 的普通 partition replication 和 generic Raft 概念可以类比：

```
Leader
Followers
ordered log
replication
```

但 Kafka 整个协议不能直接说：

```
Kafka replication = Raft
```

Kafka 的 metadata plane 现在采用 KRaft，而数据 partition replication 又有自己的协议和 ISR/HW 等机制。

最重要的 mental connection 是：

```
ordered replicated log
```

---

## Redis

普通 Redis primary-replica：

```
Primary → Replica
```

更接近：

```
Replication
```

而不是完整的 Raft-style consensus。

你前面问过：

> Redis cache 能不能保证 Linearizable read？

这就是很好的区别：

```
replication
```

不会自动带来：

```
Consensus
+
linearizable read
```

---

# Part 29：Lab 对应关系

MIT 当前 Raft lab 仍要求实现 replicated log、persistence 和 snapshot，后续 KV lab 再把 Replicated State Machine 建在 Raft 上。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)

你写 Lab 时可以建立下面的对应关系。

```
term
↓
currentTerm

Election Safety
↓
votedFor persistence

Log Replication
↓
AppendEntries()

Election Restriction
↓
RequestVote():
lastLogTerm / lastLogIndex

Follower Catch-up
↓
nextIndex[]

Known replication progress
↓
matchIndex[]

Commit
↓
commitIndex

State machine execution
↓
lastApplied
↓
applyCh

Crash recovery
↓
persist()
readPersist()

Log compaction
↓
Snapshot()

Very stale follower
↓
InstallSnapshot
```

---

## Lab 最容易出的 concurrency bug

这是比“算法没懂”更常见的问题。

例如：

```
Leader goroutine:
准备发 AppendEntries term=5
```

此时另一个 goroutine：

```
收到 term=6 RPC
↓
currentTerm=6
↓
become follower
```

第一个 goroutine 如果没正确同步，却继续：

```
以错误 state 构造 RPC
```

就可能破坏 protocol。

MIT lab guidance 特别提醒：peer 可能在旧 Leader 仍以为自己是 Leader 时已经选出新 Leader，也可能某 RPC 发出时你是 Leader，reply 回来时早已不是。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/guidance.html?utm_source=chatgpt.com)

所以处理 RPC reply 时，经常必须重新检查：

```
currentTerm
state
```

不能假设：

```
send RPC 时我是 Leader
→
reply 时仍然是 Leader
```

---

## Lab Debugging 的几个核心 invariant

建议日志至少打印：

```
server
term
role
log last index/term
commitIndex
lastApplied
```

遇到 bug，不要先问：

> 哪行代码错？

先问：

```
第一次违反 invariant 是什么时候？
```

例如：

```
S2 applied X at index 7

后来
S3 applied Y at index 7
```

那么不要从最后 crash 开始查。

向前找到：

```
哪个 Leader 首次错误地认为某 entry committed？
```

再查：

```
为什么它的 matchIndex 错？
为什么 election restriction 没生效？
为什么旧 RPC reply 被接受？
```

这会比漫无目的看 goroutine 快很多。

---

# Part 30：Paper

对应核心论文：

**In Search of an Understandable Consensus Algorithm (Extended Version)**  
Diego Ongaro / John Ousterhout。

---

### Paper Problem

作者不是只想回答：

> 如何解决一个 Consensus instance？

而是：

> 如何让工程师能够理解并实现一个完整、实用的 replicated-log consensus system？

---

## Previous Approach

Paxos 理论上非常强大。

问题是实际构建：

```
Replicated State Machine
```

还涉及：

```
Leader election
multiple log entries
recovery
log repair
membership changes
compaction
client interaction
```

实际实现复杂。

---

## Key Insight

Raft 一个非常重要的设计哲学是：

> **限制系统可能出现的状态，而不是允许所有状态然后再处理。**

例如：

```
只有 Leader 接收新 log entries

Follower 不自行产生 entries

Leader 的 log authoritative

选 Leader 又限制 log freshness
```

这使 reasoning 空间显著缩小。

---

## Design

```
              Client
                 |
                 v
               Leader
          /       |       \
         v        v        v
     Follower  Follower  Follower
```

Leader：

```
orders commands
replicates log
tracks followers
advances commitIndex
```

Followers：

```
validate prefix
append leader entries
apply committed prefix
```

---

## Evaluation

Raft 论文除 protocol correctness 外，还通过 user study 讨论 understandability，并指出正常 established-leader 情况下，新 log replication 只需要到多数派的一次 round trip；还可通过 batching/pipelining 提高性能。[Raft](https://raft.github.io/raft.pdf?utm_source=chatgpt.com)

---

## What aged well?

非常多：

```
Term / epoch
Leader-based replicated log
Quorum
Log matching
Election restriction
Committed prefix
Snapshot
```

这些仍然是现代 distributed storage 的基本语言。

---

## Limitations

Raft 不解决所有问题：

```
Byzantine faults
arbitrary data corruption
cross-shard transactions
automatic sharding
business idempotency
schema design
application invariants
```

也不意味着：

```
Raft system = infinitely scalable
```

单 Leader 本身就是 throughput bottleneck，所以大型系统通常靠多个 Raft groups scale out。

---

# Part 31：Failure-driven Problem → Solution Chain

这一讲最重要的压缩方式就是这条链：

```
我们想复制 State Machine
        ↓
最简单：Leader 复制 log
        ↓
Failure:
Leader crash
        ↓
增加：
Leader Election + Term
        ↓
Failure:
新 Leader 可能缺旧历史
        ↓
增加：
Election Restriction
lastLogTerm + lastLogIndex
        ↓
Failure:
Followers log diverge
        ↓
增加：
prevLogIndex + prevLogTerm
nextIndex
suffix overwrite
        ↓
Failure:
旧-term entry 即使在 majority 上
仍可能被未来 Leader 覆盖
        ↓
增加：
只能按副本数直接 commit
current-term entry
        ↓
得到：
Leader Completeness
        ↓
Failure:
Server restart 忘记 term/vote/log
        ↓
增加：
persistent currentTerm
persistent votedFor
persistent log
        ↓
Failure:
log 无限增长
        ↓
增加：
Snapshot
        ↓
Failure:
Follower 落后于 snapshot boundary
        ↓
增加：
InstallSnapshot
        ↓
Failure:
旧 Leader 可以返回 stale read
        ↓
增加：
quorum-confirmed read /
log read /
ReadIndex-like mechanism
        ↓
Failure:
Client timeout retry
        ↓
增加：
client request ID + dedup
        ↓
最终得到：
Practical Replicated State Machine
```

这基本就是 Raft (2) 的完整 mental model。

---

# Part 32：30 秒版本

面试官问：

> Raft (2) 主要讲什么？

你可以说：

> Raft (2) 主要解决 Leader change 之后 replicated log 的 safety 和 recovery。核心是通过 Log Matching 和 AppendEntries 修复 diverged logs，通过 election restriction 保证新 Leader 足够 up-to-date，并通过“只有当前 term 的 entry 才能根据 majority replication 直接 commit”的规则建立 Leader Completeness。这样 committed entry 不会被未来 Leader 覆盖。它还需要持久化 term、vote 和 log 来支持 crash recovery，并用 snapshot/InstallSnapshot 处理无限增长的日志和严重落后的 follower。

---

# Part 33：3 分钟版本

更完整地说：

> Raft 把 client commands 放到 replicated log 中，Leader 负责决定顺序。但 replication 本身不等于 consensus，因为 crash 或 partition 后不同 replica 可能形成 diverged logs。
> 
> Raft首先通过 AppendEntries 的 `prevLogIndex/prevLogTerm` 建立 Log Matching；Leader 使用 `nextIndex` 找到公共 prefix，并覆盖 follower 的 uncommitted suffix。
> 
> 更关键的问题是 Leader change。一个 future Leader 必须包含所有 committed entries，因此 RequestVote 不只是比较 term，还检查 candidate 的 log 是否至少与 voter 一样 up-to-date，按 last term 优先、last index 次之比较。这与 Quorum intersection 一起建立 Leader Completeness。
> 
> 但一个 subtle point 是：旧 term entry 即使后来存在于 majority 上，也不能直接认为 committed，否则仍可能被一个拥有更高-term suffix 的 future Leader 覆盖。所以 Leader 只能通过 replica counting 直接 commit 当前 term 的 entry；一旦这个 current-term entry committed，所有 preceding entries 也间接 committed。
> 
> Crash recovery 要持久化 `currentTerm`、`votedFor` 和 `log`；`commitIndex`、`lastApplied` 可以重新恢复。由于 log 不能无限增长，Raft 用 snapshot 压缩 committed prefix，并通过 InstallSnapshot 让严重落后的 follower catch up。
> 
> 最终这些机制保证 State Machine Safety：任何两个 replicas 不会在同一 log index 应用不同 command。

---

# Part 34：深入版本

最终把整讲压缩为：

```
Problem
│
│ replica logs can diverge
v
Log Matching
│
│ prevLogIndex / prevLogTerm
│ nextIndex / conflict repair
v
Consistent prefixes
│
│ but leader may crash
v
Election Restriction
│
│ lastLogTerm first
│ lastLogIndex second
v
Leader Completeness
│
│ but old-term majority alone is unsafe
v
Current-Term Commit Rule
│
│ majority + log[N].term == currentTerm
v
Committed Prefix
│
│ must survive restart
v
Persistence
│
│ currentTerm
│ votedFor
│ log
v
Crash Recovery
│
│ log can't grow forever
v
Snapshot
│
│ stale follower may miss compacted history
v
InstallSnapshot
│
v
Practical Replicated State Machine
```

---

# Part 35：最后的知识网络

```
                         Distributed Systems
                                |
                +---------------+---------------+
                |                               |
            Replication                     Consistency
                |                               |
                v                               v
       Replicated State Machine          Linearizability
                |
                v
              Raft
                |
      +---------+---------+
      |                   |
Leader Election      Log / Safety
      |                   |
   Term               Log Matching
   Vote                    |
                           v
                  Election Restriction
                           |
                           v
                  Leader Completeness
                           |
                           v
                     Commit Rule
                           |
                           v
                  State Machine Safety
                           |
            +--------------+-------------+
            |                            |
       Persistence                   Snapshot
            |                            |
      Crash Recovery              Log Compaction
            |                            |
            +-------------+--------------+
                          |
                          v
                   Practical Systems
                          |
              +-----------+-----------+
              |                       |
             etcd                  ZooKeeper*
              |                       |
              v
        Kubernetes

* ZooKeeper 使用自己的 Zab 协议，
  这里是概念关系，不是说 ZooKeeper 使用 Raft。
```

---

### 这一讲真正应该留在脑子里的 5 句话

1. **Replication 是“有几份”；Commit 是“未来还能不能反悔”。**
2. **Leader Election 不只是选活着的人，而是选一个拥有足够安全历史的人。**
3. **Raft log freshness 先比较 `lastLogTerm`，再比较 `lastLogIndex`，不是简单谁长谁新。**
4. **旧 term entry 即使已经复制到 majority，也不能仅靠 replica count 直接 commit；current-term commit rule 是 Raft 最 subtle 的 safety 规则之一。**
5. **Raft 最终保护的不是 log 本身，而是 State Machine Safety：同一个 index 永远不能让不同 replica 执行两个不同 command。**

你如果把这 5 句话及其背后的 Figure-8 counterexample 真正吃透，Raft (2) 最难的部分基本就掌握了。
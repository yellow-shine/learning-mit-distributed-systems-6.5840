可以。下面把 **MIT 6.824 / 6.5840 Lecture 6 — Fault Tolerance: Raft (1)** 一次性完整讲完。

先给你一个总纲。官方课程把这节课配套到 Raft extended paper 的 **Section 5 结束**，也就是 Raft 的核心：`Leader Election → Log Replication → Safety`；实验紧接着要求先实现 Leader Election，再实现 replicated log。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

---

## 0. 先把整节课压成一句话

> **Raft 要解决的，不是“如何把数据复制三份”，而是：当机器 crash、网络丢包、延迟、分区，而且每台机器看到的世界都不一样时，如何让一组机器仍然维护唯一的一条 committed command history。**

最终目标是：

```
Client commands
      |
      v
+----------------+
| Consensus/Raft |
+----------------+
      |
      v
identical committed log
      |
      +-------------------+
      |         |         |
      v         v         v
   State      State      State
 Machine A  Machine B  Machine C
      |         |         |
      +---------+---------+
                |
                v
     logically one machine
```

Raft paper 对 replicated state machine 的核心描述就是：Consensus 管理 replicated log，各副本以相同顺序执行相同 commands，从而表现得像一台可靠机器。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

# Part 1：为什么必须发明 Raft？

假设我们有一个 metadata service：

```
Client
   |
   v
Server A
   |
   +------ Server B
   |
   +------ Server C
```

单机时代很简单：

```
x = 0

Client:
Set(x=1)

Server:
write x=1
return OK
```

只有一个 authoritative history：

```
Set(x=1)
Set(y=2)
Set(x=3)
```

不存在：

> “到底哪台机器说的历史是真的？”

但是我们为了 Fault Tolerance 加 Replication：

```
          A
        /   \
       B     C
```

现在来了：

```
Set(x=1)
```

A 写了：

```
A: x=1
B: x=1
C: x=0
```

然后 A crash。

突然问题从：

> 如何保存 x？

变成：

> B 和 C 谁代表真正的系统历史？

---

最 naive 的办法是：

```
A crash
   ↓
随便选一个活着的节点当新 Primary
```

如果选 C：

```
C: x=0
```

而用户已经收到：

```
Set(x=1) -> OK
```

那么：

```
Client:

Set(x=1) -> OK
...
Read(x) -> 0
```

已经确认的历史消失了。

所以 **Replication ≠ Fault-tolerant consistency**。

复制数据只是第一步。

真正困难的是：

> **Failover 时必须确保新 Leader 继承正确的 history。**

这就是 Consensus 问题开始出现的地方。

---

# Part 2：它在整个 6.824 中的位置

先看课程主线：

```
RPC / Threads
     |
     | 如何通信？
     | 如何处理本机并发？
     v
Distributed Service
     |
     | 单节点挂了怎么办？
     v
Replication
     |
     v
Primary / Backup
     |
     | Primary 挂了以后：
     | 谁接班？
     | 哪份状态正确？
     | split brain 怎么办？
     v
Consensus
     |
     v
========================
       Raft
========================
     |
     | 建立一条 agreed log
     v
State Machine Replication
     |
     v
Fault-tolerant Service
     |
     +------ etcd
     |
     +------ replicated DB
     |
     +------ coordination
```

其中几个非常重要的边界：

#### RPC vs Raft

RPC 解决：

```
A 如何调用 B？
```

Raft 解决：

```
A/B/C 对某个 history 应该如何达成一致？
```

RPC 可以：

```
丢包
超时
重复
重试
```

但 RPC 本身完全不告诉你：

> “这个请求到底是否已经成为系统历史的一部分？”

---

#### Threads / Concurrency vs Raft

Mutex 解决：

```
同一个进程：

goroutine 1
goroutine 2
       ↓
如何安全访问 memory
```

Raft 解决：

```
Machine A
Machine B
Machine C

其中一部分机器可能永远联系不上
```

没有一个 shared mutex 可以跨 crash/network partition 解决这个问题。

---

#### Replication vs Consensus

Replication：

> 保存多个副本。

Consensus：

> 当副本可能分歧时，决定哪一条 history 可以成为不可逆的事实。

因此：

```
Replication
    +
Consensus
    ↓
safe replicated state
```

---

#### Primary-Backup vs Raft

Primary-Backup 的核心思想：

```
一个 Primary 排序请求
其他 Backup 跟随
```

Raft 也使用 Leader。

但是 Raft 补上最难的部分：

```
旧 Leader 消失以后
        ↓
如何安全地产生新 Leader
        ↓
如何保证新 Leader 不丢 committed history
```

所以 Raft 可以看成：

> **把 leader-based replication 的 failover correctness 做完整。**

---

#### Consensus vs State Machine Replication

这两个概念非常容易混。

Consensus 抽象上更像：

```
大家决定一个 value
```

例如：

```
choose X
```

State Machine Replication 则需要：

```
choose command #1
choose command #2
choose command #3
...
```

结果：

```
log:

1 Set(x=1)
2 Set(y=2)
3 Set(x=3)
```

然后所有 deterministic state machine：

```
execute 1
execute 2
execute 3
```

得到相同 state。

Raft 本质上是：

> **用于 replicated log / State Machine Replication 的 Consensus protocol。**

Raft paper 明确把它描述为管理 replicated log 的 Consensus algorithm，并指出结果等价于 Multi-Paxos。[Raft](https://raft.github.io/raft.pdf?utm_source=chatgpt.com)

---

# Part 3：Raft 最重要的 7 个 Mental Models

---

### Concept 1：Replicated Log

#### 它解决什么？

我们不要直接同步：

```
x=3
y=4
```

而同步：

```
发生了哪些操作，以及操作顺序。
```

比如：

```
index      command

1          Set(x=1)
2          Set(y=10)
3          Set(x=2)
```

所有节点执行相同 log：

```
same initial state
       +
same commands
       +
same order
       =
same final state
```

这就是 State Machine Replication 的根基。

---

### Concept 2：Leader

如果让每台服务器都可以同时决定：

```
A: index 5 = x
B: index 5 = y
C: index 5 = z
```

Consensus 会非常复杂。

Raft 做了一个非常强的约束：

> **正常情况下，所有新 log entries 都从 Leader 流向 Followers。**

```
             Client
                |
                v
              Leader
             /      \
            v        v
       Follower    Follower
```

于是 command ordering 集中到 Leader。

注意：

> Leader 不是“永远正确的机器”。

Leader 只是：

```
当前 term 中被 quorum 授权负责排序 log 的节点。
```

---

### Concept 3：Term

这是理解 Raft 的关键。

Raft 不使用：

```
2026-09-27 22:00:01
```

这样的 wall-clock 来区分 Leader 世代。

而使用：

```
term 1
term 2
term 3
term 4
```

可以把 `term` 理解成：

> **Leader generation / epoch number。**

例如：

```
Term 1

A = Leader

       A crashes

Term 2

B = Leader

       B partition

Term 3

C = Leader
```

Term 的真正作用是识别：

```
你的信息是不是来自过去的世界？
```

例如：

```
A believes:
term = 3
I am leader

收到：

B → A
AppendEntries(term=4)
```

A 马上知道：

```
我的领导权已经过期。
```

于是：

```
Leader → Follower
```

Raft 的 term 单调增加，并通过 RPC 传播；server 看到更大的 term 会更新自己的 term，并转成 Follower。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

### Concept 4：Majority / Quorum

假设：

```
N = 5
```

majority：

```
3
```

为什么 3？

因为任意两个 3-node majority：

```
Q1 = {A,B,C}
Q2 = {C,D,E}

intersection = {C}
```

一定有交集。

这条性质极其重要：

```
old decision quorum
       ∩
new leader election quorum
       ≠ ∅
```

也就是说：

> 新一轮选举无法完全绕过保存过旧决定的所有服务器。

但仅仅“有交集”还不够。

Raft 后面还必须规定：

```
这个交集节点不能把票随便投给历史更旧的人。
```

这就是 **Election Restriction**。

---

### Concept 5：Replicated ≠ Committed ≠ Applied

这是整个 Raft 最应该牢牢记住的区分。

考虑：

```
Leader A:
log[10] = Set(x=1)

Follower B:
log[10] = Set(x=1)

Follower C:
还没有
```

有三个阶段：

```
replicated
    ↓
committed
    ↓
applied
```

#### replicated

entry 已经存在某些 server 的 log。

不代表它一定会活下来。

---

#### committed

Raft 已经确定：

> 这个 entry 已经成为不可逆历史。

之后未来 Leader 都必须包含它。

---

#### applied

server 把 committed command 执行进 state machine：

```
log:

Set(x=1)

      ↓ apply

KV database:

x=1
```

所以：

```
log entry exists
      ≠
committed
      ≠
state machine executed
```

这个区别对做 Lab 非常重要。

---

### Concept 6：Log Matching

Raft log entry 不是只有：

```
command
```

而是：

```
(index, term, command)
```

例如：

```
index:   1   2   3   4   5
term:    1   1   2   2   3
command: A   B   C   D   E
```

Raft 有一个关键性质：

> 如果两个 logs 在相同 index 存在相同 term 的 entry，那么此前的 prefix 也相同。

也就是：

```
log A:

1/1 2/1 3/2 4/2 5/3
                ^

log B:

1/1 2/1 3/2 4/2 5/3
                ^
```

相同：

```
(index=5, term=3)
```

意味着：

```
1..5
```

整个 prefix 相同。

论文把它称为 **Log Matching Property**。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

### Concept 7：Leader Completeness

这是最终 Safety 的桥梁。

> **一个 entry 一旦 committed，未来任何 Leader 都必须包含它。**

论文称之为：

````
Leader Completeness
``` :chatgpt-content-reference{index="5"}


然后才能得到：

```text
未来 Leader
不能用另一个 command
覆盖 committed entry
````

最终：

```
State Machine Safety
```

即：

> 如果某 server 在 index i apply 了 command X，就不可能另一 server 在 index i apply command Y。

这是整个算法最终想守住的 invariant。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

# Part 4：System Model

现在必须明确 Raft 到底假设了什么。

---

### Node Model

Raft 处理的是：

```
non-Byzantine crash failures
```

不是 Byzantine。

节点可以：

```
crash
restart
暂时 unreachable
```

但是不能：

```
恶意撒谎
伪造 term
给不同人发送恶意冲突数据
篡改 stable storage
```

这是非常重要的边界。

---

### Crash-stop 还是 Crash-recovery？

完整 Raft 更接近：

```
crash-recovery
```

节点：

```
运行
↓
crash
↓
restart
↓
重新加入
```

因此某些状态必须落到 persistent storage。

官方 Lab 也明确要求 persistent state 在 failure/restart 后恢复。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)

---

## Storage Model

Raft server 有：

#### Persistent state

```
currentTerm
votedFor
log[]
```

必须在响应相关 RPC 之前 durable。

论文 Figure 2 就这样区分。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

为什么？

想象：

```
B 收到 entry X
B → Leader: OK
```

Leader 因此：

```
X committed
```

但是 B 实际没把 X 落盘。

然后：

```
A crash
B crash
B restart 丢了 X
```

那么 quorum 的“记忆”可能被破坏。

MIT 的历史考试甚至专门用这个作为 correctness counterexample：延迟持久化可能导致同一 index commit 两个不同 entries。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q23-1-sol.pdf?utm_source=chatgpt.com)

---

#### Volatile state

所有 server：

```
commitIndex
lastApplied
```

Leader 额外：

````
nextIndex[]
matchIndex[]
``` :chatgpt-content-reference{index="10"}


---

# Network Model

应该假定网络可能：

```text
delay
drop
reorder
disconnect / partition
duplicate due to retry
````

RPC 失败通常不能区分：

```
request 没到
response 没回来
server crash
network partition
server very slow
```

所以：

> timeout ≠ failure detector 给出了“机器确定已经死亡”的证明。

Raft 只把 timeout 当：

```
“我已经太久没有看到有效 Leader，
应该尝试发起 election。”
```

---

## Timing Model

这里非常关键。

### Safety

**不依赖 timing correctness。**

网络再慢，也不能因此提交错误 history。

论文明确强调 Safety 不应该依赖事件发生快慢。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

### Liveness

需要 timing 最终变得合理。

典型要求：

```
broadcastTime << electionTimeout << MTBF
```

直觉：

```
RPC 正常传播速度
        <<
多久没 heartbeat 就选举
        <<
Leader 平均多久才真的故障一次
```

所以从理论风格上：

> Safety 可以在异步环境中维持；progress 则依赖最终有一段足够稳定、足够及时的网络时期。

这是很典型的 **partial synchrony flavor**。

---

## Failure Bound

如果：

```
N = 2f + 1
```

那么：

```
majority = f + 1
```

只要最多 `f` 台不可用，并且剩下的多数节点能互通，系统还能继续。

例如：

```
N = 5

f = 2

5 = 2*2 + 1
```

最多同时失去 2：

```
A X
B X
C ✓
D ✓
E ✓
```

还有 3：

```
majority = 3
```

可以工作。

Raft paper 举的典型五节点 cluster 正是可以容忍两台失败。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

注意 wording：

> **这是 availability tolerance，不是说第 3 台坏了数据马上就错。**

如果只剩：

```
2 / 5
```

Raft 的正常结果是：

```
不能 commit 新东西
```

而不是：

```
开始瞎 commit。
```

所以：

```
lose majority
→ lose availability
```

而不是：

```
lose majority
→ automatically lose safety
```

---

# Part 5：我们自己一步一步“发明” Raft

---

## Step 1：先假设 Leader 永远不挂

三个节点：

```
          Client
             |
             v
             A
          Leader
          /    \
         v      v
        B        C
```

Client：

```
Put(x=1)
```

Leader：

```
A log:
[ Set(x=1) ]
```

然后：

```
A ───────→ B
A ───────→ C
```

假设 B ACK：

```
A ✓
B ✓
C ?
```

现在有 majority。

Leader 可以把 entry 判定 committed。

然后 apply：

```
log
 ↓
state machine

x=1
```

Follower 之后也会知道 leader 的 `commitIndex` 并 apply。

这就是最简单 happy path。Leader 收到 client command 后先 append 本地 log，再通过 AppendEntries 复制，安全复制后 apply 并向 client 返回。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf?utm_source=chatgpt.com)

---

## Step 2：Leader 会 crash

```
A = leader

      X
      crash

B              C
```

必须选 Leader。

Naive：

```
谁先说“我是 leader”
谁就是
```

显然会 split brain：

```
partition:

A,B   |   C,D,E

A says leader
C says leader
```

所以需要：

```
quorum election
```

---

## Step 3：Follower → Candidate

所有节点最开始：

```
Follower
```

Leader 周期发送：

```
AppendEntries(empty)
```

也就是 heartbeat。

```
Leader
  |
  +------ heartbeat → B
  |
  +------ heartbeat → C
```

如果 B 很久没看到：

```
heartbeat
```

B 不能证明：

```
A dead
```

它只能说：

> “当前 Leader 可能已经无法提供服务，我尝试启动新 term。”

于是：

```
Follower
   |
 timeout
   v
Candidate
```

---

## Step 4：Candidate 开始 Election

B：

```
currentTerm++
voteFor = B
```

例如：

```
term 7 → term 8
```

然后：

```
       RequestVote(term=8)
          /          \
         v            v
        A              C
```

自己先投自己。

如果：

```
B vote
C vote
```

获得 2/3：

```
B → Leader(term=8)
```

Raft 要求 candidate 获得整个 cluster 的 majority，并且一个 server 在一个 term 最多给一个 candidate 投票，因此同一 term 不可能产生两个 majority winners。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf?utm_source=chatgpt.com)

这就是：

```
Election Safety
```

---

## Step 5：为什么一个 term 最多一个 Leader？

3 nodes：

```
A B C
```

一个 Leader 需要：

```
2 votes
```

假设 A 成 Leader：

```
votes = A,B
```

C 想同 term 成 Leader，也要：

```
2 votes
```

只能：

```
C + A

or

C + B
```

但：

```
A 或 B
```

已经投过票。

同 term 不允许再投。

因此不可能。

更一般：

```
quorum > N/2

Q1 ∩ Q2 ≠ ∅
```

加上：

```
one vote per term
```

得到：

```
at most one leader per term
```

---

## Step 6：但可能两个机器都认为自己是 Leader

这是 Raft 最容易误解的地方之一。

可能：

```
Term 3:
A = Leader
```

发生 partition：

```
A       | B C
```

A 收不到新消息，但还没意识到自己失效：

```
A:
"I'm still leader(term 3)"
```

B/C：

```
term 4 election

B = Leader(term 4)
```

于是物理时间上暂时：

```
A thinks leader(term 3)

B thinks leader(term 4)
```

这并不违反：

```
Election Safety
```

因为它只保证：

> 同一个 term 最多一个 Leader。

关键在于 A 没有 quorum，所以：

```
cannot commit new commands
```

一旦遇到 term 4：

```
A → follower
```

---

## Step 7：为什么需要 randomized election timeout？

假设：

```
B timeout
C timeout
```

几乎同时：

```
B candidate term 5
C candidate term 5
```

投票：

```
B votes B
C votes C

A 可能投 B
D 可能投 C
E ...
```

可能 split vote：

```
没人 majority
```

最 naive 的办法：

```
固定等 200ms
全部重新选
```

问题：

```
B 200ms timeout
C 200ms timeout
D 200ms timeout
```

下一次仍同时开始。

可能不断：

```
split
split
split
```

Raft 使用：

```
randomized election timeout
```

论文示例：

```
150ms ~ 300ms
```

于是可能：

```
B: 173ms
C: 247ms
D: 291ms
```

B 先出来：

```
B candidate
↓
拿到 majority
↓
马上 heartbeat
↓
C/D reset timer
```

Split vote 概率大幅下降。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

注意：

> randomness 保证的主要是 **Liveness**，不是 Safety。

即使所有 timeout 永远撞车：

```
系统可能一直选不出 leader
```

但：

```
不会因此 commit 两套 history。
```

---

# Part 6：现在开始 Log Replication

Leader B 收到：

```
Set(x=1)
```

假设：

```
term = 8
```

Leader 创建：

```
(index=5,
 term=8,
 command=Set(x=1))
```

log：

```
index  1  2  3  4  5
term   1  1  4  6  8
                         ↑
                    new entry
```

然后：

```
AppendEntries
```

发给 followers。

---

## AppendEntries 真正干两件事

名字容易让人误以为只是复制日志。

其实：

```
AppendEntries
    |
    +---- heartbeat
    |
    +---- log replication
```

空：

```
entries=[]
```

就是 heartbeat。

它的重要参数包括：

```
term
leaderId

prevLogIndex
prevLogTerm

entries[]

leaderCommit
```

论文 Figure 2 就是整个实现的核心规格。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

# Part 7：为什么需要 prevLogIndex + prevLogTerm？

假设 Leader：

```
index:  1 2 3 4 5
term:   1 1 2 3 3
```

Follower：

```
index:  1 2 3 4 5
term:   1 1 2 4 4
```

存在 divergent suffix。

Leader 想复制：

```
index 5
```

不能单纯说：

```
“请 append E”
```

因为 Follower 前缀可能已经错了。

所以 Leader 说：

```
prevLogIndex = 3
prevLogTerm  = 2
```

意思：

> 我接下来发的内容，是建立在 `(index=3, term=2)` 这个 prefix 上的。

Follower 检查：

```
我的 index 3 term 是否也是 2？
```

是：

```
yes
```

才能继续。

---

如果 Leader 尝试：

```
prevLogIndex=4
prevLogTerm=3
```

Follower：

```
index4.term = 4
```

不匹配：

```
AppendEntries → false
```

Leader：

```
nextIndex--
retry
```

最终找到：

```
共同 prefix
```

然后 Follower 删除 divergent suffix：

```
old follower:

1/1 2/1 3/2 4/4 5/4
              └───── old suffix

↓

1/1 2/1 3/2

↓

append leader entries

1/1 2/1 3/2 4/3 5/3
```

Raft 的 follower 规则明确规定：如果相同 index 的 existing entry 与新 entry term 冲突，就删除该 entry 以及其后所有 entries，再 append 新 entries。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

## 一个非常重要的 Mental Model

Raft 不是：

```
合并两条日志
```

而是：

```
Leader history
      ↓
强制 Followers 收敛到 Leader history
```

因此 Raft 是典型：

> **Strong Leader architecture**

Follower 的 divergent uncommitted suffix 可以被 Leader 删除。

---

# Part 8：为什么允许删除 log？

你可能会问：

> 日志不是应该 immutable 吗？

注意：

```
committed log
```

不可改。

但：

```
uncommitted log
```

只是：

> 某个旧 Leader 曾经尝试提出的 proposal。

它还不是系统事实。

例如：

```
Term 2 Leader A

A log:
index 7 = Set(x=1)

只写了：
A
```

然后 A 被隔离。

新 Leader B：

```
term 3
index7 = Set(x=2)
```

如果 term 3 的 entry 成功 committed：

```
旧 A 恢复
```

它的：

```
Set(x=1)
```

必须被删除。

所以：

```
log 中存在
≠
历史已经决定
```

---

# Part 9：Commit 到底是什么？

考虑 5 nodes：

```
A Leader
B
C
D
E
```

新 entry：

```
index=10 term=7
```

复制状态：

```
A ✓
B ✓
C ✓
D ?
E ?
```

3/5 majority。

Leader 可以认为：

```
entry 10 committed
```

这里真正重要的不是：

```
“有三份 copy”
```

而是：

> 已经达到了一个 quorum，而未来任何 Leader Election 也需要 quorum，因此两个 quorum 必然交叠。

不过这仍然不足以证明 safety。

还缺：

```
新 Leader election restriction
```

---

# Part 10：仅仅 majority election 为什么还是不够？

这是 Lecture 6 最关键的推理。

假设 5 nodes：

```
S1 S2 S3 S4 S5
```

Term 1 Leader S1 写：

```
X
```

复制给：

```
S1
S2
S3
```

所以：

```
X committed
```

然后 S1 crash。

现在 election：

```
S2: has X
S3: has X
S4: no X
S5: no X
```

假如我们只规定：

```
谁拿到 3 votes 谁 Leader
```

S4 可以拿：

```
S3
S4
S5
```

看起来 S4 拿到 majority。

可是：

```
S4 doesn't have X
```

然后作为 Leader：

```
覆盖旧 history
```

就可能破坏 committed X。

所以：

> **Quorum intersection 只保证 election quorum 中至少有人知道 X；但如果这个人仍然愿意给旧节点投票，intersection 没用。**

于是需要 Raft 最重要的 election rule。

---

# Part 11：Election Restriction

Follower 收到 RequestVote，不是：

```
没投票 → 就投
```

还必须问：

> Candidate 的 log 是否至少和我的一样 up-to-date？

比较方式不是简单：

```
谁 log 长谁赢
```

而是 lexicographic：

```
(lastLogTerm, lastLogIndex)
```

优先比较：

```
lastLogTerm
```

如果相同：

```
比较 lastLogIndex
```

也就是：

```
Candidate newer iff

candidate.lastTerm > my.lastTerm

OR

candidate.lastTerm == my.lastTerm
AND
candidate.lastIndex >= my.lastIndex
```

论文把这个限制用于确保未来 Leader 包含已 committed entries。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

## 为什么先比较 term，而不是 length？

例如：

```
A:

index: 1 2 3 4 5 6 7
term:  1 1 1 1 1 1 1


B:

index: 1 2 3 4
term:  1 1 1 5
```

A 更长：

```
len=7
```

B 更短：

```
len=4
```

但 B 的最新：

```
term=5
```

比 A：

```
term=1
```

更新。

因为 term 表示：

> 它参与过更新 Leader generation 的 history。

所以 B 更 up-to-date。

---

# Part 12：Majority + Election Restriction 怎么组合成 Safety？

这是 Raft 最漂亮的一段。

假设 entry X 已 committed。

意味着 X 存在于一个 majority：

```
Commit quorum:

{A,B,C}
```

未来选 Leader 也需要 majority：

```
Election quorum:

{C,D,E}
```

交集至少：

```
C
```

C 保存 X。

现在 candidate E 来要求 C 投票。

C 检查：

```
E.log >= C.log ?
```

如果 E 缺少必要 history：

```
NO
```

于是 E 拿不到这个 quorum。

所以最终能成为 Leader 的 server，必须具有足够新的 log。

论文的 Leader Completeness proof 正是利用：

```
commit majority
∩
election majority
```

必然存在共同 voter，再结合 up-to-date voting rule 推出未来 Leader 不可能缺少已 committed entry。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

可以压成：

```
Majority Intersection
        +
Election Restriction
        ↓
Leader Completeness
        ↓
State Machine Safety
```

这是整节课最重要的一条 proof chain。

---

# Part 13：用完整 Timeline 看一次 Write

三个 nodes：

```
A = Leader
B = Follower
C = Follower
```

初始：

```
term = 4
commitIndex = 7
```

Client：

```
Put(x=10)
```

时间线：

```
time →

Client:   Put(x=10)
             |
             v

A:       append log[8]=(term4, Put)
         persist
             |
             +--------------+
             |              |
             v              v

B:       receive AE      C: receive AE
         persist             ...
         ACK
             |
             v

A:       sees majority:
         A + B
             |
             v
         commitIndex=8
             |
             v
         apply log[8]
             |
             v
         x=10
             |
             v
Client:  <--- success
```

接着 heartbeat：

```
A -> B/C

leaderCommit=8
```

Follower：

```
commitIndex → 8
apply entry 8
```

所以 eventually：

```
A: x=10
B: x=10
C: x=10
```

---

## 在 timeline 每个位置 crash 会怎样？

### Crash #1：A 本地 append 后马上 crash

```
A only
```

entry：

```
not committed
```

未来可能消失。

没问题。

---

### Crash #2：A+B 有 entry，但 A 还没 commit？

这要区分 protocol knowledge 和事实。

如果当前 term entry 已经真正被 majority persistent storage 接受，那么 Leader 在正常 execution 中可以据此 advance `commitIndex`。

但如果 Leader crash 恰好发生在知道 ACK 之前，新 Leader 是否一定马上知道它 committed 是另一个问题。

Raft 保证的是：

```
safety of history
```

并通过后续 leader/log replication 继续推进；“某一瞬间哪些节点知道 commitIndex”可以不同。

---

### Crash #3：Leader commit 后、client reply 前 crash

这是非常经典的：

```
operation actually committed
```

但 client：

```
timeout
```

Client 无法知道：

```
executed?
not executed?
```

它通常只能 retry。

这就是：

```
Raft consensus
≠
exactly-once client semantics
```

应用层通常需要：

```
ClientID
RequestID
deduplication
```

Raft paper 后续 client interaction section 也讨论了 unique serial numbers；但这已经超出 Lecture 6 的核心 Section 5。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

# Part 14：一个非常 subtle 的问题——旧 Term entry 的 commit

这是很多人真正开始困惑的地方。

假设：

```
term 2
```

某 entry：

```
X
```

存在于 majority。

但是它在 term 2 没被安全确认成 committed，Leader crash。

后来 term 4 Leader 发现：

```
X exists on majority
```

可以不可以仅仅因为：

```
X majority replicated
```

直接说：

```
X committed
```

Raft 的规则比这更谨慎：

Leader 根据 majority `matchIndex` 推进 `commitIndex=N` 时，还要求：

```
log[N].term == currentTerm
```

Figure 2 明确包含这个条件。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

也就是 Leader 用“count replicas”的方式直接 commit 的 N：

```
必须来自自己的 current term。
```

一旦 current-term entry commit：

```
它之前的 entries
```

也会随着 prefix 间接成为 committed。

---

### 为什么需要这个规则？

因为 old-term entries 可能处于这种复杂分布：

```
old leader logs
+
不同 partition
+
未来 election
```

单纯看到：

```
某旧 entry 现在在 majority
```

不足以安全推断其不可覆盖性。

而 current-term entry 一旦通过 majority commit：

```
Leader Completeness
```

可以保证这个 Leader 的整个 prefix 被固定下来。

Mental model：

```
不要单独“追认”旧 term entry

而是：

commit 一个 current-term entry
          ↓
把它之前整个 prefix 一起固定
```

这是 Raft Safety 中非常重要但容易忽略的细节。

---

# Part 15：Raft Server State

把 Figure 2 直接转化成 mental model。

---

### Persistent

#### currentTerm

```
我见过的最高 term
```

谁修改：

```
启动 election
看到更高 term RPC
```

需要 persist。

为什么？

否则：

```
crash
↓
忘了见过 term 10
↓
重新相信 term 3
```

会破坏 protocol epoch。

---

### votedFor

```
这个 term 我投过谁
```

必须 persist。

为什么？

假设：

```
term=7

B votes A
↓
B crash
↓
forget vote
↓
B votes C
```

那么同一 term：

```
B 投了两票
```

可能帮助两个 candidates 得 majority。

所以必须记住。

---

### log[]

```
(index, term, command)
```

必须 persist。

因为 quorum correctness 本身假定：

> 节点说“我保存了”以后不会因为普通 crash 就忘记。

---

## Volatile state

### commitIndex

```
最高已知 committed index
```

单调上升。

---

### lastApplied

```
最高已 apply 到 state machine 的 index
```

所以始终：

```
lastApplied <= commitIndex
```

正常逻辑：

````
if commitIndex > lastApplied:
    lastApplied++
    apply(log[lastApplied])
``` :chatgpt-content-reference{index="22"}


---

# Leader-only volatile state

## nextIndex[i]

意思：

> 我下一次应该从哪个 index 开始给 follower i 发 log。

例如：

```text
Leader log length=10

nextIndex[B]=11
````

B reject：

```
prev mismatch
```

Leader 往回：

```
10
9
8
...
```

找到共同 prefix。

---

### matchIndex[i]

意思：

> 我确定 follower i 已经复制到哪个 index。

例如：

```
matchIndex:

A 10
B 10
C 8
D 10
E 7
```

对：

```
N=10
```

有：

```
A,B,D = 3/5
```

majority。

如果：

```
log[10].term == currentTerm
```

就可推进：

```
commitIndex=10
```

---

# Part 16：核心 Invariants

论文总结了五个非常好的 invariants。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

可以形成：

```
Election Safety
      ↓
Leader Append-Only
      ↓
Log Matching
      ↓
Leader Completeness
      ↓
State Machine Safety
```

---

### 1. Election Safety

```
一个 term 最多一个 Leader
```

否则：

```
Leader A:
index 8 = X

Leader B:
index 8 = Y
```

同 generation 会产生两个 authoritative histories。

---

### 2. Leader Append-Only

Leader：

```
不会修改自己已有的 entries
只 append
```

旧的 divergent Leader 恢复后：

```
它变 Follower
```

由新 Leader 修正。

---

### 3. Log Matching

如果：

```
A[index=i].term == B[index=i].term
```

则此前 prefix 一样。

这让一个：

```
(index,term)
```

pair 成为 prefix fingerprint。

---

### 4. Leader Completeness

如果 entry committed：

```
future leaders all contain it
```

这是 committed history 不丢的关键。

---

### 5. State Machine Safety

最终业务语义：

```
如果 server A:
apply(index=10, X)

那么永远不会：

server B:
apply(index=10, Y)
```

这才是用户真正关心的结果。

---

# Part 17：Safety Proof 不要背，用链条理解

真正要证明的是：

```
不会两个 state machines
在同一个 index 执行不同 command
```

也就是：

```
State Machine Safety
```

证明思路：

#### 第一步

某 entry X committed。

意味着当前 Leader 已经建立足够强的 quorum evidence。

---

#### 第二步

未来 Leader 必须通过 majority election。

```
commit quorum
∩
election quorum
≠ ∅
```

---

#### 第三步

intersection 中至少一个 voter 有 committed history。

它受到：

```
Election Restriction
```

约束：

```
不能投给 log 更旧 candidate
```

---

#### 第四步

所以未来被选出的 Leader 必须保持 committed history。

即：

```
Leader Completeness
```

论文就是这样用 quorum intersection + voting restriction 做 contradiction proof。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

#### 第五步

Follower 只能：

```
follow leader
```

而 committed entry 不会被未来 Leader 删除。

因此：

```
index i
```

永远不会 later committed 另一个 command。

---

#### 第六步

所有 server：

```
按 log index 顺序 apply
```

因此：

```
same committed sequence
        ↓
same state-machine execution
```

论文也明确从 Leader Completeness 推出 State Machine Safety。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

# Part 18：Safety vs Liveness

必须严格区分。

### Safety

> 永远不能发生坏事。

Raft 的核心：

```
同一个 log index
不能 committed 两个不同 command
```

即使：

```
network partition
RPC delay 10 minutes
packet reorder
leader confusion
```

都不能破坏。

---

## Liveness

> 好事最终能够发生。

例如：

```
最终可以 elect leader
最终可以 commit command
```

这需要：

```
majority alive
+
majority 能通信
+
network eventually reasonably timely
```

---

## Partition 时

假设：

```
5 nodes

A B | C D E
```

A 原来 Leader。

左边：

```
A B
```

只有：

```
2/5
```

不能 commit。

右边：

```
C D E
```

3/5。

可以 election：

```
C term++
C → Leader
```

继续工作。

---

### Safety

仍然有。

### Availability

只有 majority side。

这就是非常经典的：

```
partition
    ↓
minority:
safety yes
availability no

majority:
safety yes
availability yes
```

---

# Part 19：Failure Matrix

|Failure|Protocol behavior|数据 Safety|Availability|核心机制|
|---|---|---|---|---|
|Follower crash|Leader retry，其他 quorum 继续|✓|通常 ✓|Majority|
|Candidate crash|Election 重试|✓|通常 ✓|timeout|
|Leader crash|新 term election|✓|暂时中断|Election|
|Packet loss|RPC retry|✓|可能降级|idempotent protocol|
|Duplicate RPC|重复处理无害|✓|✓|index/term checks|
|Delayed old RPC|stale term 被拒绝|✓|✓|`currentTerm`|
|Split vote|新 term 重新选|✓|暂时 ✗|randomized timeout|
|Minority partition|minority 无法 commit|✓|minority ✗|quorum|
|Majority partition|majority elect leader|✓|majority ✓|quorum|
|Old Leader returns|看到 higher term 后 step down|✓|✓|term|
|Follower log divergent|Leader 找共同 prefix 并覆盖 suffix|✓|恢复后 ✓|AppendEntries consistency|
|Node restart|从 persistent term/vote/log 恢复|✓|catch-up 后 ✓|stable storage|
|Majority unavailable|不再 commit|✓|✗|quorum|
|Byzantine node|不在 Raft core failure model 内|不保证|不保证|需要 BFT|

Raft 对 follower/candidate crash 主要依赖 retry；RPC 设计成能够安全处理重复请求。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

---

# Part 20：最容易误解的 Top 5

### ❌ 1. “Majority 有 entry，所以它一定 committed”

不完全对。

最准确的 mental model 是：

```
entry 被多数复制
+
满足 Raft 当前 term commit rules
+
Leader advance commitIndex
```

特别是 old-term entry 不能简单按 replica count 直接 commit。

---

## ❌ 2. “Leader 上的数据一定最完整”

错。

某个 minority follower 可能有一些：

```
uncommitted old entries
```

新 Leader 不一定有。

例如：

```
Follower A:
1 2 3 4 5 6

Leader B:
1 2 3 4
```

只要：

```
5,6
```

不是 committed history，就允许被丢弃。

真正保证：

> 新 Leader 必须包含所有 **committed** entries。

不是所有 entries。

---

## ❌ 3. “Timeout 证明 Leader 死了”

错。

timeout 只说明：

```
我在 deadline 内没收到消息。
```

可能：

```
Leader alive
network slow
packet dropped
GC pause
CPU starvation
partition
```

所以 Leader Election 不是 failure oracle。

---

## ❌ 4. “Raft 保证任何时刻只有一个 Leader”

错。

正确：

> 同一个 **term** 最多一个 Leader。

不同 term 的：

```
old isolated Leader
+
new Leader
```

可能暂时共存。

Safety 来自：

```
旧 Leader 没 quorum
```

而不是它神奇地立刻知道自己已经失效。

---

## ❌ 5. “Raft = Linearizability”

不完全对。

Raft 提供：

```
ordered committed replicated log
```

这是构建 linearizable service 的非常重要基础。

但还要处理：

```
client retries
duplicate requests
read protocol
leader changes
request/result matching
```

例如官方下一阶段 KV Lab 才要求在 Raft 上实现 linearizable Put/Get 以及 at-most-once Put。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-kvraft1.html?utm_source=chatgpt.com)

所以：

```
Raft
   ↓
replicated ordered state-machine execution
   ↓
+ correct client/read protocol
   ↓
Linearizable KV service
```

---

# Part 21：Raft 与 Paxos

不要用：

```
Raft = easier Paxos
```

就结束。

两者目标：

```
Consensus / replicated state machine
```

Raft paper 明确说其结果相当于 Multi-Paxos，但设计结构不同，重点是 understandability。[Raft](https://raft.github.io/raft.pdf?utm_source=chatgpt.com)

Raft 强调：

```
Leader Election
      +
Log Replication
      +
Safety
```

分开理解。

而且采用更强的 Leader discipline：

```
entries largely flow leader → followers
```

减少可能的 protocol states。

Mental model：

```
Paxos:
从“proposal/value 如何被 quorum 接受”
理解 consensus

Raft:
从“Leader + replicated log + failover”
理解 consensus
```

不要把它们理解成：

```
一个 correct
一个 obsolete
```

它们属于同一个问题空间。

---

# Part 22：Raft 与 2PC

这个边界非常重要。

假设：

```
Transfer money

Shard A:
Alice -100

Shard B:
Bob +100
```

Raft 可以让：

```
Shard A replicas
```

对 A 的 log 一致。

也可以让：

```
Shard B replicas
```

对 B 的 log 一致。

但 Raft 不自动保证：

```
A commit
iff
B commit
```

这是：

```
Distributed Transaction / Atomic Commit
```

的问题。

通常：

```
         2PC
          |
     +----+----+
     |         |
     v         v
Shard A     Shard B
  |           |
 Raft        Raft
```

所以：

```
Raft:
replica agreement inside one group

2PC:
atomic decision across multiple groups
```

这是数据库系统里极其重要的层级关系。

---

# Part 23：与 Spanner 的关系

Spanner 这样的系统问题更大：

```
Replication
+
Consensus
+
Sharding
+
Transactions
+
Timestamp ordering
+
multi-region
```

Raft/Paxos 类 consensus 解决：

```
一个 replica group 的 ordered state
```

2PC 解决：

```
跨 replica groups 的 atomic transaction
```

Timestamp/TrueTime 解决：

```
transactions 之间的时间/serialization ordering
```

因此不要把：

```
Consensus
```

误认为整个 Distributed Database。

它只是非常核心的一层。

---

# Part 24：与 Chain Replication

Chain Replication：

```
Client write
   |
   v
Head → Replica2 → Replica3 → Tail
```

主要通过：

```
chain ordering
```

简化 replication/read-write path。

Raft：

```
             Leader
             /    \
            v      v
      Follower   Follower
```

核心则是：

```
quorum + elections + replicated log
```

两者都是 replication protocol。

但是：

```
Chain Replication
```

特别强调：

```
head/tail pipeline
```

而 Raft 特别强调：

```
safe leader changes + consensus log
```

后面的 Chain Replication lecture 会让你看到：

> Replication topology 本身也是一个设计维度。

---

# Part 25：和 ZooKeeper 的关系

不要说：

```
ZooKeeper = Raft
```

ZooKeeper 使用的是自己的 atomic broadcast 协议体系（Zab）。

但是 conceptual architecture 很像：

```
replicated log
        ↓
ordered state-machine updates
        ↓
coordination service
```

所以学完 Raft 再看 ZooKeeper，你会更容易理解：

```
zxid
leader
followers
atomic broadcast
session/coordination semantics
```

Raft 是 protocol mechanism。

ZooKeeper 是：

> 基于 replicated state / ordering 构建出的 coordination system 与 API semantics。

---

# Part 26：与 Distributed Cache / Redis 的区别

Redis replica：

```
Primary
   |
replication
   |
Replica
```

不意味着：

```
“天然具有 Raft 的 quorum commit + leader completeness”
```

例如异步复制系统 failover 时可能有：

```
acknowledged write loss
```

这取决于具体 replication/failover semantics。

所以做 system design 时，不要看到：

```
3 replicas
```

就自动脑补：

```
Consensus
Linearizability
quorum commit
```

这是非常常见的工程误区。

---

# Part 27：Raft 与 Sharding

Raft 解决单个 group：

```
       shard 1

A B C
 \|/
Raft group
```

如果数据量太大：

```
Shard 1 → Raft group 1
Shard 2 → Raft group 2
Shard 3 → Raft group 3
```

所以：

```
Raft
```

解决：

```
within-shard fault tolerance
```

而：

```
Sharding
```

解决：

```
horizontal scalability
```

官方后续 Lab 也是先构建 Raft KV，再让多个 Raft replicated groups 按 shard 并行工作，从而扩展吞吐量。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-shard1.html?utm_source=chatgpt.com)

---

# Part 28：与 MapReduce / Spark 的关系

通常不要把大量计算 task state 全走一个全球 Raft log。

Raft 更适合：

```
metadata
configuration
coordination
ownership
job metadata
scheduler state
```

而不是：

```
TB 级 shuffle data
```

Mental model：

```
Data plane:
大量、可并行、吞吐优先

Control/metadata plane:
小、关键、必须一致
          |
          v
      Raft 很适合
```

这与你熟悉的 Cloud / Kubernetes 架构高度一致。

---

# Part 29：真实系统——etcd

这是与你最贴近的例子。

etcd 使用 Raft 来维护 replicated state machine；etcd 官方 Raft library 也直接说明 replicated state machine 通过 replicated log 保持同步。[etcd](https://go.etcd.io/etcd/raft/v3?utm_source=chatgpt.com)

简单画：

```
             etcd-1
            Leader
            /    \
           v      v
       etcd-2    etcd-3
       follower  follower
```

写入：

```
PUT /foo bar
```

大致：

```
Leader
  ↓
Raft log
  ↓
quorum replication
  ↓
commit
  ↓
apply to etcd state machine
```

etcd 文档也说明 stored keys 通过 Raft protocol 复制到 cluster members。[etcd](https://etcd.io/docs/v3.7/dev-guide/interacting_v3/?utm_source=chatgpt.com)

---

# Part 30：然后来到 Kubernetes

Kubernetes：

```
kubectl
   |
   v
kube-apiserver
   |
   v
etcd
   |
   v
Raft
```

Kubernetes 官方把 etcd 定义为保存 API server / cluster data 的 consistent, highly available key-value store。[Kubernetes](https://kubernetes.io/docs/concepts/overview/components/?utm_source=chatgpt.com)

所以你平时看到：

```
Deployment
Node
Secret
ConfigMap
Lease
CRD
Custom Resource
```

这些 desired state 的核心持久化最终依赖 etcd。

---

# Part 31：Raft 和 Kubernetes Controller 是什么关系？

非常值得区分。

Controller：

```
Desired State
      |
      v
   Observe
      |
      v
Current State
      |
      v
Reconcile
```

Raft：

```
Command ordering
      |
      v
Replicated Log
      |
      v
Consensus
```

Controller 不负责：

```
多台 etcd 对同一次 update 如何达成一致
```

Raft 不负责：

```
Deployment replicas=3
怎么创建三个 Pod
```

组合起来：

```
          Kubernetes object
                |
                v
            API Server
                |
                v
         etcd / Raft
                |
        durable desired state
                |
                v
          Controllers
                |
                v
           reconciliation
                |
                v
         actual resources
```

一句话：

> **Raft 保护“Desired State 是什么”；Controller 负责让现实逐渐变成这个 Desired State。**

这是一个非常有用的边界。

---

# Part 32：Cloud Control Plane

把你熟悉的 Cloud / Infra 场景映射过来。

例如：

```
CreateCluster
```

你可能有：

```
API server
   |
   v
metadata store
   |
   v
controller
   |
   +--> create VPC
   +--> create EKS
   +--> create IAM
```

如果 metadata store 的：

```
desired state / workflow ownership
```

需要强一致，那么背后很可能需要某种：

```
Consensus / replicated database
```

Raft 并不会替你完成：

```
AWS API calls
reconciliation
retry external side effects
```

它只确保：

```
control-plane durable ordered state
```

所以这是：

```
Consensus
≠
Workflow Engine
≠
Controller/Reconciler
```

三个不同层次。

---

# Part 33：Multi-region 为什么 Raft 会贵？

如果：

```
Oregon
Virginia
Frankfurt
```

一个 write 要 quorum：

```
Leader
   |
   +---- cross-region follower
   |
   +---- cross-region follower
```

commit latency 至少会受 quorum RTT 影响。

于是出现经典 trade-off：

```
strong consistency
       ↔
write latency
       ↔
failure-domain diversity
```

把 replicas 放：

```
同 AZ
```

快但 failure correlation 高。

放：

```
跨 continent
```

fault isolation 更强，但 write latency 高。

Raft 并没有消除物理学。

它只是明确了：

> 你要在什么 communication evidence 上才能安全 commit。

---

# Part 34：Paper Reading

### Paper Problem

作者面对：

```
Consensus 很重要
但 Paxos 被普遍认为很难理解/实现
```

所以问题不只是：

> 能不能发明 Consensus？

而是：

> 能不能设计一个 correctness 足够强，同时结构清晰、容易解释和实现的 Consensus algorithm？

---

### Key Insight

核心设计原则：

> **Decomposition + Strong Leader**

把问题拆为：

```
Leader Election
Log Replication
Safety
Membership changes
Log compaction
Client interaction
```

其中 core algorithm 又非常 leader-centric。

论文自己强调把 leader election、log replication、safety 分开，并减少必须考虑的 system states，以提高 understandability。[Raft](https://raft.github.io/raft.pdf?utm_source=chatgpt.com)

---

## Previous Approach

最主要背景当然是：

```
Paxos / Multi-Paxos
```

Raft 不是宣称：

```
Consensus 从零发明
```

而是重新组织设计空间。

---

## Design

```
servers:
Follower
Candidate
Leader

logical time:
Terms

RPC:
RequestVote
AppendEntries
```

加：

```
replicated log
quorum
election restriction
```

---

## Mechanism

最核心的六步：

```
heartbeat
   ↓
timeout
   ↓
election
   ↓
leader
   ↓
append + replicate
   ↓
quorum commit
```

Safety 再通过：

```
Log Matching
+
Election Restriction
+
Leader Completeness
```

建立起来。

---

## Evaluation

Raft paper 不只是 benchmark throughput。

它一个非常独特的 evaluation target 是：

```
understandability
```

作者通过 student/user study 比较对 Raft 与 Paxos 的理解表现；论文还评估 election behavior 等。论文报告的 study 图包含 43 名参与者。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)

这本身也说明：

> Raft 的“容易推理”就是设计目标，不只是附带好处。

---

## Limitations

Raft core 没解决：

```
Byzantine failures
arbitrary application semantics
distributed transactions across groups
sharding strategy
exactly-once client semantics automatically
arbitrary stale/linearizable read policy automatically
```

另外 paper 后续才处理：

```
cluster membership
snapshot/log compaction
client interaction
```

所以千万不要把 Raft 看成：

```
万能 distributed system framework
```

---

## What aged well?

今天仍非常重要的是：

```
terms / epochs
quorum
leader election
replicated log
commit vs apply
majority intersection
persistent consensus state
log matching
```

这些 mental models 已经远超 Raft 本身。

---

# Part 35：公式——为什么 N ≥ 2f + 1？

先不写公式。

假设希望：

```
允许 f=2 台失败
```

坏两台后仍然需要 majority。

试：

```
N=4
```

坏两个：

```
2 alive
```

但 majority：

```
3
```

不能工作。

所以至少：

```
N=5
```

坏两个：

```
3 alive
```

而 majority：

```
3
```

正好。

所以：

```
N >= 2f + 1
```

符号：

```
N = voting replicas
f = simultaneously unavailable replicas
```

如果：

```
N = 2f+1
```

majority：

```
f+1
```

坏：

```
f
```

剩：

```
f+1
```

仍然可以 quorum。

---

## 为什么 Majority 一定相交？

假设两个 quorum：

```
Q1
Q2
```

而：

```
|Q1| > N/2
|Q2| > N/2
```

如果它们完全不交：

```
|Q1 ∪ Q2|
=
|Q1| + |Q2|
>
N
```

但系统总共只有 N 个节点。

矛盾。

所以：

```
Q1 ∩ Q2 ≠ ∅
```

这就是 Raft quorum reasoning 的数学底座。

---

# Part 36：和 6.824 Lab 的对应关系

官方 Lab 直接说明 Raft 被实现成一个 Go module，由多个 Raft instances 通过 RPC 维护 replicated log；committed entries 通过 `applyCh` 交给上层 service。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)

Mental mapping：

```
Paper concept
       ↓
Lab implementation
```

---

### Leader Election

你会实现类似：

```
state:
Follower
Candidate
Leader

currentTerm
votedFor

RequestVote()
```

后台 goroutine：

```
election timer
```

timeout：

```
startElection()
```

---

### heartbeat

概念：

```
Leader prevents election
```

Lab：

```
AppendEntries RPC
entries = empty
```

Part 3A 官方要求正是 Leader Election + empty AppendEntries heartbeats。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)

---

### Log Replication

核心：

```
log[]
AppendEntries
nextIndex[]
matchIndex[]
```

Lab Part 3B 开始复制 entries、处理基本 agreement 和 election restriction。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)

---

### Apply

```
commitIndex
lastApplied
```

然后：

```
applyCh <- ApplyMsg{...}
```

注意 conceptual boundary：

```
Raft:
decides command

Service:
executes command
```

---

# Part 37：goroutine / mutex 方面最容易出 Bug 的地方

Raft Lab 的难点通常并不是你“不知道 Raft”。

而是：

```
distributed concurrency
+
local concurrency
```

同时出现。

例如：

```
goroutine A:
send RequestVote(term=4)

                    network delay

meanwhile

goroutine B:
receive AppendEntries(term=5)
currentTerm=5
become follower

                    old reply arrives

goroutine A:
RequestVote reply for term=4
```

如果代码直接：

```
if reply.VoteGranted {
    becomeLeader()
}
```

你可能：

```
term 5 follower
↓
被旧 term 4 reply
↓
错误变成 leader
```

所以所有 async RPC response 都必须重新检查：

```
这个 response
还是不是针对我的 current term/state？
```

MIT Lab guidance 也特别提醒：一个 peer 完全可能已经在期间变成 Leader，或者 Leader 发出 RPC 后在 reply 回来之前已失去 leadership。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/guidance.html?utm_source=chatgpt.com)

---

# Part 38：Lab Debugging Mental Model

不要调：

```
为什么 TestX failed？
```

应该记录 protocol events：

```
time
server
state
term
event
log
commitIndex
```

例如：

```
12.100 S1 F T3 election timeout
12.101 S1 C T4 start election
12.103 S2 F T3 recv RV T4
12.104 S2 F T4 vote S1
12.109 S1 L T4 won election
12.120 S1 L T4 AE -> S2
```

一旦失败，找第一个：

```
invariant violation
```

而不是最后一个：

```
test assertion
```

---

## 最值得 assert 的 invariants

例如：

```
currentTerm monotonic
```

```
commitIndex monotonic
```

```
lastApplied <= commitIndex
```

```
same-term vote at most once
```

```
Leader only processes replies belonging to current leadership term
```

官方 guidance 也建议显式检查代码所依赖的 assumptions，并反复核对 Figure 2，因为漏掉一个 Figure 2 条件就是很常见的 bug 来源。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/guidance.html?utm_source=chatgpt.com)

---

# Part 39：5 个推理练习

你先不要看答案，可以以后回来做。

---

### Level 1

3 nodes：

```
A leader
B
C
```

A 把 X 写到：

```
A
B
```

B ACK 后 A crash。

问：

> X 是否可能被后续 Leader 覆盖？

关键不是回答“majority”三个字，而是解释：

```
future election quorum
+
voting restriction
```

如何保护它。

---

### Level 2

有：

```
A last log = (index=10, term=3)
B last log = (index=7, term=4)
```

B 请求 A 投票。

谁的 log 更 up-to-date？

答案：

```
B
```

因为优先比较：

```
lastLogTerm
```

不是 log length。

---

### Level 3

5 nodes：

```
A B | C D E
```

A 原来 Leader。

partition 后：

```
A,B
```

能否继续接受 client write 并返回 committed success？

不能。

因为：

```
2/5
```

没有 quorum。

C/D/E 可以产生 higher-term Leader。

---

### Level 4

假设我们去掉：

```
lastLogTerm / lastLogIndex
```

投票限制，只要：

```
candidate term 新
```

就给票。

问：

> 可以构造什么 counterexample？

方向：

```
old committed entry
       ↓
majority nodes 中有人拥有
       ↓
但它把票给缺 entry 的 candidate
       ↓
candidate 获 majority
       ↓
committed history 被覆盖
```

这直接破坏 Leader Completeness。

---

### Level 5

设计：

```
multi-region metadata service
```

需求：

```
RPO=0 for committed metadata
survive one region failure
```

你至少需要考虑：

```
Raft voters 放在哪些 failure domains？
quorum RTT？
region isolation 后哪边能写？
是否使用 3 voters 还是 5 voters？
control plane write latency 可以接受多少？
```

这就是把课程 Consensus 真正转成 infra design。

---

# Part 40：Problem → Solution Chain

这是我最建议你保存下来的部分。

```
Problem:
单机 crash 会丢服务
        ↓

Naive:
复制多份
        ↓

Failure:
副本可能分歧
        ↓

Mechanism:
一个 Leader 排序 writes
        ↓

Failure:
Leader crash
        ↓

Mechanism:
Leader Election
        ↓

Failure:
两个 candidate 都想当 leader
        ↓

Mechanism:
Term + one vote/term + majority
        ↓

Failure:
split vote
        ↓

Mechanism:
randomized election timeout
        ↓

Failure:
新 Leader 可能缺旧历史
        ↓

Mechanism:
Election Restriction
(lastLogTerm,lastLogIndex)
        ↓

Failure:
Followers logs divergent
        ↓

Mechanism:
prevLogIndex + prevLogTerm
        ↓

Failure:
divergent suffix
        ↓

Mechanism:
Leader forces follower log convergence
        ↓

Problem:
什么时候 history 不可逆？
        ↓

Mechanism:
quorum commit
+
current-term commit rule
        ↓

Result:
Leader Completeness
        ↓

Result:
State Machine Safety
        ↓

Final:
replicated deterministic state machines
behave like one reliable machine
```

如果你能自己从头推导出这条链：

> Raft 基本上就真的懂了。

---

# Part 41：30 秒版本

如果面试官问：

> Raft 是什么？

可以回答：

> Raft 是一个用于 Replicated State Machine 的 leader-based Consensus algorithm。它把时间分成 Terms，通过 majority voting 做 Leader Election；Leader 将 client commands 追加到 replicated log，并通过 quorum 复制后 commit。AppendEntries 的 index/term consistency check 让 follower logs 收敛，而 Election Restriction 保证未来 Leader 包含已经 committed 的 entries。最终由 Leader Completeness 和 Log Matching 保证不同 replicas 不会在同一个 log index apply 不同 command。Raft 在 minority failure 下牺牲 availability 而保持 safety。

---

# Part 42：3 分钟版本

可以讲成：

```
Raft 的问题背景是：
仅有 replication 并不足以获得 fault tolerance，
因为 leader crash 后不同 replicas 可能拥有不同 history。

Raft 使用一个强 leader architecture。
所有 server 都处于 follower/candidate/leader 三种状态，
并用 monotonically increasing term 区分不同 leader generations。

Follower 如果超过 randomized election timeout
没有收到 leader heartbeat，就进入 candidate，
增加 term、自投一票并发送 RequestVote。
Candidate 必须拿到整个 cluster 的 majority 才能成为 Leader。

Leader 将 client commands 追加到自己的 log，
然后通过 AppendEntries 复制到 followers。
AppendEntries 带 prevLogIndex 和 prevLogTerm，
所以 leader 可以发现 follower 的 divergent log，
找到共同 prefix 并覆盖 uncommitted suffix。

一个 entry 被安全 commit 后才能 apply 到 state machine。
Raft 的核心 safety 来自 quorum intersection 和 election restriction：
投票节点只有在 candidate log 至少跟自己一样 up-to-date 时才投票。
因此未来 leader 必须包含 committed entries，
即 Leader Completeness。
最终保证 State Machine Safety：
同一个 log index 永远不会 apply 两个不同 commands。

Raft safety 不依赖网络时间，
但 liveness 需要 majority 最终能够稳定通信。
```

---

# Part 43：深入版本

完整 mental model：

```
Problem
  |
  v
Replica histories may diverge after failures
  |
  v
Model
  |
  +-- crash-recovery
  +-- unreliable network
  +-- non-Byzantine
  +-- stable persistent state
  |
  v
Algorithm
  |
  +-- Follower/Candidate/Leader
  +-- Terms
  +-- RequestVote
  +-- AppendEntries
  +-- Majority
  |
  v
Replication
  |
  +-- leader append
  +-- prevLogIndex/prevLogTerm
  +-- repair divergent suffix
  |
  v
Commit
  |
  +-- quorum
  +-- current-term rule
  |
  v
Invariants
  |
  +-- Election Safety
  +-- Leader Append-Only
  +-- Log Matching
  +-- Leader Completeness
  +-- State Machine Safety
  |
  v
Safety
  |
  +-- no conflicting committed history
  |
  v
Liveness
  |
  +-- majority must eventually communicate
  +-- randomized elections
  |
  v
Trade-offs
  |
  +-- quorum latency
  +-- minority unavailable
  +-- leader bottleneck
  +-- replication/storage overhead
```

---

# Part 44：最后的知识网络

把这节挂到整个 Distributed Systems 树上：

```
                         Distributed Systems
                                  |
                  +---------------+---------------+
                  |                               |
             Fault Tolerance                  Consistency
                  |                               |
             Replication                    Linearizability
                  |
          Primary / Backup
                  |
                  | leader failover problem
                  v
              Consensus
                  |
           +------+------+
           |             |
         Paxos          Raft
                         |
              +----------+----------+
              |          |          |
            Terms     Election   Replicated Log
              |          |          |
              +----------+----------+
                         |
                   Quorum / Majority
                         |
                   Log Matching
                         |
                Election Restriction
                         |
                Leader Completeness
                         |
                State Machine Safety
                         |
             State Machine Replication
                         |
          +--------------+--------------+
          |              |              |
        etcd         Databases      Coordination
          |                             |
      Kubernetes                     ZooKeeper*
          |
     API Objects
          |
    Desired State
          |
     Controllers
```

`*` ZooKeeper 不是使用 Raft 本身，这里表示它和 Raft 都属于 replicated coordination/state-machine 这个问题空间。

---

## 最后只记住 6 句话

如果整节课最后只留下六句话，我建议是：

```
1. Replication 不是 Consensus；
   真正的问题是 failover 后哪条 history 是真的。

2. Term 是 Leader generation，
   不是 wall-clock timestamp。

3. Majority 的核心不是“超过一半很保险”，
   而是任意两个 majorities 必然相交。

4. log 中存在 ≠ committed ≠ applied。

5. Majority intersection + Election Restriction
   → Leader Completeness
   → State Machine Safety。

6. Raft 在 partition 下宁可停止写入，
   也不允许 minority 创造另一条 committed history。
```

其中第 **5** 句：

```
Majority Intersection
        +
Election Restriction
        ↓
Leader Completeness
        ↓
State Machine Safety
```

就是 **Raft (1) 最值得真正理解，而不是背诵的核心。**
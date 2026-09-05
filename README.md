可以。先纠正/精确化一下我上一条的说法：按照 **MIT 6.5840 Spring 2026** 的版本，确实可以把整个编程主线拆成 **13 个 milestone**：

**Lab 1 + Lab 2 + Lab 3A–3D + Lab 4A–4C + Lab 5A–5D = 13 个。**

而且这 13 个并不是互相独立的小作业，它们实际上是在一步步造出一个真正的、支持故障恢复和水平扩展的分布式 KV 系统：

> MapReduce → RPC/KV → Raft → RSM → replicated KV → snapshot → sharded KV → fault-tolerant reconfiguration

下面按这个主线讲。

---

# 总览：13 个 milestone 到底在造什么

| Milestone | 你在实现什么                 | 最核心概念                                | 难度感受   |
| --------- | ---------------------- | ------------------------------------ | ------ |
| Lab 1     | MapReduce              | RPC、并发、任务调度、故障重试                     | ★★     |
| Lab 2     | 单机 KV Server           | linearizability、版本号、RPC retry、lock   | ★★☆    |
| 3A        | Raft Leader Election   | term、投票、心跳、超时                        | ★★★    |
| 3B        | Raft Log Replication   | replicated log、commit、majority       | ★★★★   |
| 3C        | Raft Persistence       | crash/restart、持久化、unreliable network | ★★★★   |
| 3D        | Raft Snapshot          | log compaction、InstallSnapshot       | ★★★★★  |
| 4A        | RSM                    | Raft → 通用 replicated state machine   | ★★★★   |
| 4B        | KV Raft                | linearizable replicated KV           | ★★★★★  |
| 4C        | KV Snapshot            | 有界日志、状态恢复                            | ★★★★   |
| 5A        | Sharding               | shard migration、reconfiguration      | ★★★★★  |
| 5B        | Controller Recovery    | 可恢复的配置变更                             | ★★★★★  |
| 5C        | Concurrent Controllers | fencing、CAS、并发控制                     | ★★★★★+ |
| 5D        | Extension              | transactions / leases / Range / etc. | 看选题    |

其中真正的“大坎”通常是：

**3B → 3C → 3D → 4B → 5A/5C。**

---

# 1. Lab 1 — MapReduce

这是你的第一个分布式程序。

MIT 要你实现两个组件：

```text
                Coordinator
                /    |    \
              RPC   RPC   RPC
              /      |      \
         Worker1  Worker2  Worker3
```

Coordinator 不亲自计算，而是把任务发给 Worker。

Worker 会循环：

```text
向 Coordinator 要任务
        ↓
执行 Map / Reduce
        ↓
写输出文件
        ↓
再要一个任务
```

官方 Lab 要求实现一个 distributed MapReduce，由一个 coordinator 和多个 worker 构成，Worker 与 Coordinator 通过 RPC 通信。Coordinator 如果发现某个 worker 超过约 10 秒没有完成任务，要能够把任务重新分配。([PDOS][1])

### 你真正学的不是 MapReduce API

真正重要的是这几个问题。

**任务状态怎么管理？**

比如：

```go
Idle
Running
Done
```

Coordinator 需要知道：

```text
Map task #3
assigned to worker A
started at 10:01:03
```

10 秒之后还没完成：

```text
认为 worker A 可能死了
↓
重新把 task #3 发给 worker B
```

这就是非常基础的：

**failure detection + retry。**

### Map 和 Reduce 之间还有 barrier

不能：

```text
Map 做了一半
↓
马上开始所有 Reduce
```

而应该：

```text
全部 Map 完成
        ↓
全部 Reduce 开始
        ↓
Job Done
```

所以 Coordinator 本质上又是一个简单状态机：

```text
MAP
 ↓
REDUCE
 ↓
DONE
```

### 为什么这个 Lab 很重要？

因为后面整门课反复出现：

```text
RPC 丢了怎么办？
机器挂了怎么办？
怎么区分“慢”和“死”？
同一个任务执行两遍怎么办？
并发状态怎么加锁？
```

Lab 1 只是把这些问题放在比较容易理解的场景里。

---

# 2. Lab 2 — Key/Value Server

Lab 2 开始进入这门课真正的核心：

> **一个分布式客户端怎么可靠地操作远程状态？**

2026 版本的 API 大致是：

```go
Get(key)

Put(key, value, version)
```

Server 保存：

```text
key → (value, version)
```

例如：

```text
x → ("hello", 7)
```

客户端执行：

```text
Put("x", "world", 7)
```

如果 server 看到当前 version 确实是 7：

```text
x → ("world", 8)
```

否则：

```text
ErrVersion
```

MIT 2026 的 Lab 2 明确使用这种 versioned Put，并要求服务在客户端看来是 linearizable；同时还要求你在这个 KV 之上实现 lock。([PDOS][2])

---

## 这里第一次真正碰到经典分布式问题

假设：

```text
Client                         Server

Put(x, A, v=3)  ------------>
                    执行成功
                    x=A,v=4

        <------------ response
                   X 丢包
```

Client 没收到 reply。

那它不知道：

```text
情况 A：请求丢了，服务器压根没执行

情况 B：服务器执行了，只是 reply 丢了
```

这叫经典的：

> **uncertain outcome**

于是客户端只能 retry。

版本号的价值就在这里。

第二次：

```text
Put(x, A, v=3)
```

Server 已经是：

```text
v=4
```

所以能发现：

```text
这是旧操作/重复操作
```

---

# Lab 2 还要求实现分布式 Lock

比如：

```go
lock.Acquire()
lock.Release()
```

但 lock 状态实际上存到 KV Server。

可能类似：

```text
lock/foo → unlocked
```

两个客户端竞争：

```text
Client A              Client B

Get(foo)               Get(foo)
   ↓                       ↓
version=10              version=10

Put(locked,10)          Put(locked,10)
   ↓                       ↓
成功                    ErrVersion
```

因此 versioned Put 实际上有点像：

> **Compare-And-Swap / optimistic concurrency control**

这个思想到了 Lab 5C 会再次出现。

---

# 3. Lab 3A — Raft Leader Election

到这里正式进入 Raft。

目标：

> 一群机器里自动选出一个 Leader。

例如：

```text
S1 follower
S2 follower
S3 follower
```

一段时间收不到 heartbeat：

```text
S2 election timeout

S2:
term++
Candidate
RequestVote → S1
RequestVote → S3
```

如果得到多数票：

```text
S2 = Leader
```

官方要求 3A 实现 leader election，以及不携带日志条目的 `AppendEntries` heartbeat；还必须在旧 leader 失效或失联后选出新 leader。([PDOS][3])

---

## 你会实现的核心状态

Raft Server 大致：

```go
currentTerm
votedFor
state // follower/candidate/leader
```

RPC：

```go
RequestVote
AppendEntries // 此时主要作为 heartbeat
```

---

## 最容易踩的坑：Election Timeout

如果每个节点都：

```text
150 ms 超时
```

可能同时：

```text
S1 Candidate
S2 Candidate
S3 Candidate
```

大家互相不给票：

```text
1 : 1 : 1
```

没人拿到 majority。

所以 timeout 必须 randomized，例如：

```text
300~500ms
```

于是：

```text
S2 先 timeout
↓
其他节点还没 timeout
↓
给 S2 投票
↓
S2 leader
```

---

# 4. Lab 3B — Raft Log Replication

3A 只解决：

> 谁是 Leader？

3B 开始解决：

> **Leader 怎样让所有机器执行完全相同的操作？**

这是 Raft 真正的核心。

---

## 假设客户端提交

```text
SET x = 10
```

Leader 的 log：

```text
index  term  command
1      3     SET x=10
```

然后：

```text
                 Leader
                   |
         AppendEntries
             /           \
         Follower       Follower
```

得到多数确认：

```text
Leader + Follower1 = majority
```

Leader 就能认为：

```text
entry 1 committed
```

之后通过：

```go
applyCh
```

交给上层状态机。

---

## 你会维护更多状态

Leader 通常需要：

```go
nextIndex[]
matchIndex[]
```

所有 peer：

```go
log[]
commitIndex
lastApplied
```

以及：

```go
Start(command)
```

上层告诉 Raft：

```text
我要复制这个 command
```

官方 3B 要求实现 `Start()`、通过 AppendEntries 发送/接收日志、将 committed entry 发往 `applyCh`，同时实现 Raft 的 election restriction。测试还覆盖 follower/leader failure、重连、并发 Start，以及 leader 如何快速回退到 follower 的日志匹配点。([PDOS][4])

---

# 3B 最重要的三个思想

### ① Log Matching

如果：

```text
两个 log entry
拥有相同 index + term
```

那么它们之前的 log 应当一致。

---

### ② Majority Commit

一个 entry 不是写到 leader 就成功：

```text
Leader ✓
Follower A ✓
Follower B ✗
```

3 节点情况下：

```text
2/3
```

才能 commit。

---

### ③ conflicting log repair

假设旧 Leader：

```text
S1 log:
A B C D E
```

新 Leader：

```text
S2 log:
A B X Y
```

S1 重连以后不能直接 append：

```text
A B C D E X Y   ❌
```

而要修正成：

```text
A B X Y
```

因此 Leader 会利用：

```text
nextIndex
```

往回找共同 prefix。

这一块通常是第一次让人明显感觉：

> Raft 论文看懂 ≠ Raft 能写对。

---

# 5. Lab 3C — Persistence

目前如果 Server crash：

```text
RAM 全没了
```

那：

```text
currentTerm?
votedFor?
log?
```

全丢。

显然不行。

所以 3C 要实现：

```go
persist()
readPersist()
```

把关键 Raft state 保存到 `Persister`。

官方要求 Raft 在重启后从 Persister 恢复持久状态；测试不只是简单 restart，还包括 partitioned leader、Figure 8、unreliable network 和 churn。([PDOS][4])

---

## 一般持久化

```text
currentTerm
votedFor
log[]
```

比如：

```text
S1 crash

currentTerm = 9
votedFor = S3
log = [A,B,C,D]
```

restart 后必须仍然：

```text
term 9
vote S3
[A,B,C,D]
```

而不是：

```text
term 0
empty log
```

否则可能破坏 Raft safety。

---

## 3C 的难点其实不是 serialization

`labgob.Encode()` 本身并不难。

真正难的是：

> **每次哪些状态变化时必须 persist？**

例如：

```go
currentTerm++
```

之后：

```text
必须 persist
```

投票：

```go
votedFor = candidate
```

之后：

```text
必须 persist
```

log append：

```text
必须 persist
```

如果漏一个：

```text
crash
↓
restart
↓
状态回滚
↓
非常诡异的测试 failure
```

所以 3C 会迫使你开始认真思考：

> memory state 和 durable state 的边界。

---

# 6. Lab 3D — Log Compaction / Snapshot

假设 Raft 连续运行一年：

```text
log:
1
2
3
...
100,000,000
```

重启以后重新 replay 一亿条？

显然不现实。

所以需要：

# Snapshot

假设状态机已经执行到：

```text
index = 900000
```

当前 KV 状态是：

```text
x=3
y=7
z=100
```

可以保存：

```text
Snapshot @ 900000
{
 x=3
 y=7
 z=100
}
```

那么：

```text
log[0:900000]
```

就可以删除。

官方 3D 的目的正是让 service 定期保存状态 snapshot，然后 Raft 丢掉 snapshot 之前的日志。还需要实现 `Snapshot(index, snapshot)` 和 `InstallSnapshot` RPC，因为落后太远的 follower 可能已经无法靠 leader 当前保留的日志追上。([PDOS][4])

---

# 最大的新问题：Follower 太落后怎么办？

Leader 当前只保留：

```text
snapshot = index 1000

log:
1001
1002
1003
...
```

但 follower：

```text
lastIndex=300
```

你已经没有：

```text
301~1000
```

这些 log 了。

这时 Leader 不能继续普通 AppendEntries。

必须：

```text
InstallSnapshot
```

大概：

```text
Leader
   |
InstallSnapshot(snapshot @1000)
   |
Follower
```

Follower：

```text
加载 snapshot
↓
状态直接跳到 index 1000
↓
继续接收 1001+
```

---

# 3D 为什么特别容易出 bug？

因为之前你可能默认：

```go
log[index]
```

就是 Raft 的真实 index。

Snapshot 后：

```text
Raft index:       1000 1001 1002
slice index:        0    1    2
```

于是到处需要：

```text
absolute index ↔ slice offset
```

这是一个非常典型的 bug 源。

例如：

```go
log[i-lastIncludedIndex]
```

而不是：

```go
log[i]
```

从 3D 开始，Raft 实现通常需要一次比较大的整理。

---

# 7. Lab 4A — Replicated State Machine（RSM）

到这里，你已经有：

```text
Raft
```

但现在有一个设计问题：

难道以后每实现一个：

```text
KV
Queue
Lock Service
Metadata Server
```

都要重新写一遍：

```text
raft.Start()
applyCh
等待 commit
处理 leadership change
把结果返回 RPC
```

？

所以 4A 引入一个抽象层：

# RSM — Replicated State Machine

架构变成：

```text
       Application
        KV Server
            |
           RSM
            |
           Raft
```

---

## 核心接口

上层：

```go
result := rsm.Submit(op)
```

RSM：

```text
Submit(op)
   ↓
Raft.Start(op)
   ↓
等待
   ↓
applyCh 收到 committed op
   ↓
StateMachine.DoOp(op)
   ↓
把执行结果交还 Submit()
```

官方把 4A 定义为一个与具体服务无关的 `rsm` package。你要实现 `Submit()` 和读取 `applyCh` 的 reader goroutine，并正确处理并发 Submit、leader failure/partition 以及 restart。([PDOS][5])

---

# 一个很关键的问题

假设：

```text
Client RPC
 ↓
Leader S1
 ↓
rsm.Submit(X)
 ↓
Raft.Start(X)
```

这时 S1 突然失去 leadership。

新的 Leader S2 可能在同一个 log index 写：

```text
Y
```

所以：

```text
Start(X) 返回 index=10
```

绝不意味着：

```text
index 10 最终一定是 X
```

RSM 必须判断：

```text
我等到的是不是我自己的 operation？
term 有没有变化？
leader 有没有变？
```

这是 4A 最精华的地方。

---

# 8. Lab 4B — Fault-Tolerant KV Service

终于把：

```text
Lab 2 KV
+
Lab 3 Raft
+
Lab 4A RSM
```

拼在一起。

最终：

```text
                 Clerk
                   |
                   | Put/Get
                   v
        +--------------------+
        |     KV Server      |
        |        ↓           |
        |       RSM          |
        |        ↓           |
        |       Raft         |
        +--------------------+
             /       \
          Raft       Raft
          KV         KV
```

每台机器都有完整 KV 副本。

官方 4B 就是把 4A 的 RSM 用来复制 KV server。Clerk 会寻找当前 leader；如果服务器不可达或不是 leader，就重试其他机器。Server 把 Put/Get 都提交给 RSM。([PDOS][5])

---

# 为什么 Get 也需要 Raft？

这是很重要的一点。

很多人的第一反应：

```text
Put → Raft
Get → 直接读 map
```

问题：

旧 Leader 可能已经被网络 partition：

```text
       S1 old leader
          X
      partition

S2 + S3 elect new leader
```

如果客户端还能访问 S1：

```text
Get(x)
```

S1 直接读自己的 map：

```text
返回旧数据
```

破坏 linearizability。

所以这个 Lab 最简单的实现：

```text
Put → Raft log
Get → Raft log
```

官方也明确指出，KV server 不应该在无法与 majority 通信时完成 Get；简单方法就是把每个 Get 和 Put 都通过 `Submit()` 进入 Raft。([PDOS][5])

这虽然性能不高，但 safety 很清晰。

Lab 5D 后面甚至专门建议你做优化：

```text
leader lease
↓
安全地本地处理 Get
```

---

# 9. Lab 4C — KV + Snapshot

4B 有一个问题：

```text
Put(a)
Get(a)
Put(b)
Get(b)
...
```

每个 operation 都进入 Raft。

于是：

```text
Raft log 无限增长
```

所以现在把 3D Snapshot 真正接入业务系统：

```text
KV state
  ↓
serialize
  ↓
snapshot
  ↓
Raft.Snapshot()
```

官方 4C 要求 RSM 在 persisted Raft state 过大时创建 snapshot；restart 时读取 snapshot 并调用 StateMachine 的 `Restore()`，KV server 本身也要实现 `Snapshot()` / `Restore()`。([PDOS][6])

---

## 这时完整 recovery 流程变成

```text
Server crash
    ↓
restart
    ↓
ReadSnapshot()
    ↓
restore KV state
    ↓
Raft 恢复 snapshot 后面的 log
    ↓
继续 apply
```

例如：

```text
Snapshot @ 5000:
x=10
y=20

Raft log:
5001: Put z=30
5002: Put x=99
```

恢复：

```text
snapshot
↓
{x=10,y=20}

apply 5001
↓
{x=10,y=20,z=30}

apply 5002
↓
{x=99,y=20,z=30}
```

到这里，你已经有一个相当完整的：

> **线性一致、容错、可恢复、日志有界的 replicated KV store。**

---

# 10. Lab 5A — Moving Shards

4C 最大的问题是什么？

所有机器保存：

```text
整个数据库
```

假设：

```text
3 servers
```

所有 request 都由一个 Raft group 处理。

吞吐量受限。

于是引入：

# Sharding

例如：

```text
Shard 0 → Group A
Shard 1 → Group A
Shard 2 → Group B
Shard 3 → Group B
```

Group A：

```text
Raft cluster A
```

Group B：

```text
Raft cluster B
```

这样：

```text
Group A 和 Group B 可以并行处理 request
```

官方把 Lab 5 定义为一个由多个 Raft-replicated shard group 组成的 sharded KV system；Lab 5 复用 Lab 2 的 `kvsrv` 和 Lab 4 的 RSM/Raft。([PDOS][7])

---

## 5A 的核心难题不是“怎么 hash”

而是：

> **Shard 怎么从 Group A 安全迁到 Group B？**

假设：

```text
Config 10:
Shard 3 → A

Config 11:
Shard 3 → B
```

不能简单：

```text
A 删除
B 开始
```

否则中间可能：

```text
数据丢失
```

也不能：

```text
B 开始
A 继续
```

否则可能：

```text
两个 Group 都接受写入
```

产生 split-brain。

---

## 2026 版本设计是

Controller：

```text
Shard Controller
```

负责配置。

Shard group 支持类似操作：

```text
FreezeShard
InstallShard
DeleteShard
```

大概是：

```text
A owns shard
     ↓
Freeze A
     ↓
复制 shard state
     ↓
Install into B
     ↓
Delete from A
```

同时 shard RPC 带：

```text
configuration number Num
```

例如：

```text
FreezeShard(shard=3, Num=11)
```

如果以后一个旧 RPC：

```text
FreezeShard(shard=3, Num=10)
```

姗姗来迟：

```text
reject
```

这就是：

# fencing

官方 5A 明确要求 `FreezeShard`、`InstallShard`、`DeleteShard`，并利用 configuration `Num` 拒绝旧 RPC；还要处理 group join、leave、`ErrWrongGroup`、snapshot restart，以及配置切换期间不受影响 shard 继续提供服务。([PDOS][8])

这是一个非常重要的工业界思想。

---

# 11. Lab 5B — Handling a Failed Controller

现在假设 Controller 正在：

```text
Config 10 → Config 11
```

过程：

```text
Freeze A ✓
Install B ✓
Delete A ...
```

突然：

```text
Controller crash
```

怎么办？

如果新 Controller 什么都不知道：

```text
系统可能永远卡在一半迁移状态
```

---

# 解决：把 reconfiguration 本身也做成可恢复操作

官方建议保存：

```text
currentConfig
nextConfig
```

比如：

```text
current = 10
next    = 11
```

如果 Controller crash：

新 Controller 启动：

```text
看到：
current=10
next=11

说明：
上一个 Controller 没做完
```

于是：

```text
继续完成 Config 11
```

完成后：

```text
current = 11
next = none
```

官方 5B 的目标就是：第一个 controller 在 shard migration 中崩溃或被 partition 后，新 controller 能接手并完成旧 controller 没做完的 reconfiguration。官方建议在 `kvsrv` 中同时保存 current 和 next configuration。([PDOS][8])

---

## 这里的关键词

# Idempotency

新 Controller 可能根本不知道旧 Controller 做到了哪一步。

所以最好的操作模式是：

```text
Freeze again
Install again
Delete again
```

只要每个操作：

```text
重复执行仍然安全
```

就行。

而 5A 的：

```text
Num fencing
```

正好帮你做到这一点。

这是很漂亮的设计：

```text
5A 做 fencing
        ↓
5B 利用 fencing 实现 crash recovery
```

---

# 12. Lab 5C — Concurrent Controllers

5B 还是相对简单：

```text
Controller A 挂
↓
Controller B 接手
```

5C 更狠：

```text
A 不是真的挂了
只是被 partition
```

于是：

```text
Controller A
Controller B
Controller C
```

可能同时活着。

例如：

```text
A 认为：
Config 10 → 11A

B 认为：
Config 10 → 11B
```

两边配置号都：

```text
Num = 11
```

这时仅仅：

```text
if Num newer
```

已经不够了。

因为：

```text
11A
11B
```

Num 一样，但内容不同。

---

# 所以必须确保只有一个 controller 赢

官方 5C 特别指出：

> 多个 controller 可能读取相同 current config，然后各自生成不同但具有同一个 `Num` 的 next configuration。

解决办法可以利用 Lab 2 的：

```text
versioned Put
```

也就是一种 CAS。

例如：

```text
nextConfig key version = 7
```

A：

```text
Put(next=11A, version=7)
```

B：

```text
Put(next=11B, version=7)
```

A 先成功：

```text
version → 8
```

B：

```text
ErrVersion
```

于是只有：

```text
11A
```

成为正式 next config。

官方正是建议利用 key version 和 versioned Put，保证针对同一个配置号只有一个 controller 能发布 next configuration。之后还会测试多个 controller、partition 和不可靠网络。([PDOS][8])

---

# 注意这里发生了一个非常漂亮的“知识回环”

你在 Lab 2 学：

```text
versioned Put
```

当时看起来只是：

```text
处理 retry
做 lock
```

到了 Lab 5C：

```text
versioned Put
        ↓
optimistic concurrency control
        ↓
多个 configuration controller 竞争
        ↓
只有一个成功
```

这就是为什么我觉得 6.5840 Lab 设计得特别好。

前面的“小技巧”，后面会变成真正的系统设计 primitive。

---

# 13. Lab 5D — Extend Your Solution

2026 版 5D 和传统意义上的：

```text
Part D = 固定功能
```

不一样。

这是一个：

> **开放式扩展。**

官方要求你选择一个 extension 或自己设计一个，并写自己的测试以及 `extension.md`。([PDOS][8])

MIT 给的例子非常有意思。

---

## 选项 1：Controller 配置存储也改成 replicated KV

当前 Controller 的 metadata 可能存在 Lab 2：

```text
kvsrv
```

可以改成：

```text
kvraft
```

于是：

```text
Controller metadata
        ↓
Raft replicated
```

这会进一步去掉 single point of failure。([PDOS][8])

---

## 选项 2：Exactly-once semantics

加强：

```text
Put/Get retry
```

使系统识别重复请求。

这是经典的：

```text
client ID
request ID
dedup table
```

方向。([PDOS][8])

---

# 选项 3：Range Query + B-tree

增加：

```go
Range(low, high)
```

例如：

```text
Range("cat", "dog")
```

不能只是遍历整个：

```go
map[string]string
```

可以改成：

```text
B-tree
```

从：

```text
O(N)
```

走向：

```text
O(log N + K)
```

官方甚至要求你写一个测试，让 naive 全表扫描方案失败，而更合适的数据结构通过。([PDOS][8])

---

# 选项 4：Read-only Raft optimization / Leader Lease

目前：

```text
Get
 ↓
Raft.Start()
 ↓
AppendEntries
 ↓
majority
 ↓
reply
```

很贵。

可以优化：

```text
Get
 ↓
Leader local read
 ↓
reply
```

但怎么证明这个 leader：

```text
仍然是真 Leader？
```

一个方案：

# Lease

旧 Leader 必须确保在 lease 有效期间：

```text
不会存在另一个可安全服务线性一致读的新 leader
```

这属于非常经典的 production optimization。

官方 5D 直接建议实现 Raft paper Section 8 的 read optimization，并用 lease 保持 linearizability。([PDOS][8])

---

# 选项 5：Transactions

比如：

```text
Transaction:
    Get(A)
    Put(A, 10)
    Put(B, 20)

全部成功
or
全部失败
```

从单操作 linearizability 升级到：

```text
multi-operation atomicity
```

官方建议参考 etcd transaction。([PDOS][8])

---

# 选项 6：Cross-Shard Transactions

这个最硬。

例如：

```text
Shard A:
account Alice = $100

Shard B:
account Bob = $50
```

事务：

```text
Alice - $20
Bob   + $20
```

两个 shard 不同 Raft Group。

不能出现：

```text
Alice -20 成功
Bob +20 失败
```

需要类似：

```text
Two-Phase Locking
+
Two-Phase Commit
```

流程大致：

```text
Coordinator
    |
    | PREPARE
   / \
ShardA ShardB
  yes    yes
   \     /
    COMMIT
```

官方 5D 明确把 **跨 shard transaction + two-phase commit + two-phase locking** 列为较高级的扩展方向。([PDOS][8])

这已经非常接近真正数据库系统课程的内容了。

---

# 把 13 个 Lab 连成一条线，就更容易理解

你可以这样看：

```text
Lab 1
MapReduce
│
├─ RPC
├─ concurrency
├─ worker failure
└─ retry
        ↓

Lab 2
KV Server
│
├─ linearizability
├─ version
├─ retry
└─ distributed lock
        ↓

3A
Raft Election
│
├─ term
├─ vote
└─ leader
        ↓

3B
Raft Log
│
├─ replication
├─ majority
├─ commit
└─ log repair
        ↓

3C
Persistence
│
├─ crash
└─ restart
        ↓

3D
Snapshot
│
├─ log compaction
└─ InstallSnapshot
        ↓

4A
RSM abstraction
│
└─ Submit → Raft → Apply
        ↓

4B
Replicated KV
│
├─ fault tolerance
├─ linearizable Put
└─ linearizable Get
        ↓

4C
Replicated KV + Snapshot
│
└─ bounded storage
        ↓

5A
Sharding
│
├─ multiple Raft groups
├─ migration
├─ fencing
└─ reconfiguration
        ↓

5B
Recoverable Controller
│
├─ current/next
└─ idempotent recovery
        ↓

5C
Concurrent Controllers
│
├─ CAS
├─ fencing
└─ concurrency control
        ↓

5D
Production-style extensions
│
├─ lease
├─ range query
├─ transaction
└─ cross-shard transaction
```

---

# 从能力成长角度，这 13 个阶段实际上可以分成 5 层

### 第一层：分布式编程基础

```text
Lab 1
Lab 2
```

你学的是：

```text
goroutine
mutex
RPC
retry
timeout
concurrency
linearizability
```

---

### 第二层：共识协议

```text
3A
3B
3C
3D
```

学的是：

```text
Leader Election
Consensus
Replication
Durability
Snapshot
```

这是整门课最重要的一层。

---

### 第三层：从 Consensus 变成真正 Service

```text
4A
4B
4C
```

学的是：

```text
Raft 只是 replication mechanism
            ↓
真正应用需要：
RSM + Client retry + KV + snapshot
```

也就是说：

> **Consensus ≠ Distributed Database。**

Consensus 只是底层积木。

---

### 第四层：Scale Out

```text
5A
```

从：

```text
一个 Raft group
```

升级：

```text
多个 Raft groups
```

从：

```text
fault tolerance
```

开始解决：

```text
scalability
```

---

### 第五层：Distributed Coordination

```text
5B
5C
5D
```

你面对的不再只是：

```text
server crash
```

而是：

```text
管理系统本身也可能 crash
controller 可能 partition
两个 controller 都认为自己应该操作
跨 shard transaction
```

这就很接近真正生产级分布式系统的问题了。

---

## 我会特别注意这几个“概念升级点”

第一次做的话，可以把整门课的关键认知浓缩成：

```text
Lab 1:
RPC 会失败。

Lab 2:
RPC 失败后 retry 会产生 ambiguity。

3A:
节点会对谁是 leader 产生分歧。

3B:
节点会对日志内容产生分歧。

3C:
crash 会让 memory 消失。

3D:
日志不能无限增长。

4A:
Raft 本身不是应用。

4B:
读操作同样有一致性问题。

4C:
application state 和 consensus state 必须一起恢复。

5A:
数据迁移本身也是分布式协议。

5B:
执行分布式协议的 controller 自己也会挂。

5C:
旧 controller 可能没挂，只是你以为它挂了。

5D:
正确以后，还要考虑性能、事务和工程能力。
```

这基本就是 **6.5840 Labs 的灵魂**。

另外有个 2026 版值得注意的安排：**Lab 5 和 final project 是二选一方向**——官网说明学生可以做自选 final project，或者完成 Lab 5。([PDOS][9])

如果你准备实际开始做，我建议下一步重点看 **3A–3D 的代码架构应该怎么设计**。Raft 如果一开始 struct、goroutine、锁和 log index 设计得不好，到了 3D/4B 会非常痛苦；反过来，如果 3A 开始就按后面的需求设计，整个 3A→4C 会顺很多。

[1]: https://pdos.csail.mit.edu/6.824/labs/lab-mr.html?authuser=4&utm_source=chatgpt.com "6.5840 Lab 1: MapReduce"
[2]: https://pdos.csail.mit.edu/6.824/labs/lab-kvsrv1.html?utm_source=chatgpt.com "6.5840 Lab 2: Key/Value Server"
[3]: https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com "6.5840 Lab 3: Raft"
[4]: https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html "6.5840 Lab 3: Raft"
[5]: https://pdos.csail.mit.edu/6.824/labs/lab-kvraft1.html "6.5840 Lab 4: Fault-tolerant Key/Value Service"
[6]: https://pdos.csail.mit.edu/6.824/labs/lab-kvraft1.html?utm_source=chatgpt.com "6.5840 Lab 4: Fault-tolerant Key/Value Service"
[7]: https://pdos.csail.mit.edu/6.824/labs/lab-shard1.html?utm_source=chatgpt.com "6.5840 Lab 5: Sharded Key/Value Service"
[8]: https://pdos.csail.mit.edu/6.824/labs/lab-shard1.html "6.5840 Lab 5: Sharded Key/Value Service"
[9]: https://pdos.csail.mit.edu/6.824/project.html?utm_source=chatgpt.com "6.5840 Project"

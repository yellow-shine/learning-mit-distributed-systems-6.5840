## MIT 6.824 Lecture 1 — Introduction

这一课不是在教一个特定协议，而是在教你**以后应该用什么方式思考所有分布式系统**。官方目前将这门课编号为 6.5840；它在 2023 年以前叫 6.824。课程把 fault tolerance、replication、consistency 列为核心主题；Lecture 1 是 Introduction，并配套 MapReduce 论文和第一个 MapReduce Lab。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/general.html?utm_source=chatgpt.com)

---

# Part 1：这节课到底想解决什么问题？

如果这一课只能记住一个问题，我希望你记住：

> **当一个程序从一台机器变成许多独立机器通过网络协作之后，我们怎样让它既能利用多机带来的规模，又能在 concurrency、partial failure 和 communication uncertainty 存在时，仍然表现出明确、可靠的行为？**

注意，关键词不是“多台机器”。

而是：

```
independent nodes
      +
message passing
      +
concurrency
      +
partial failure
```

### 1.1 单机时，你有一个非常强的隐含前提

例如一个 KV Server：

```
Client
   |
   | Put(x, 1)
   v
+---------+
| Server  |
| x = 1   |
+---------+
```

程序执行：

```
Put(x, 1)

x = 1
return OK
```

你当然仍然有线程竞争、进程 crash、disk failure 等问题。

但至少你很容易形成一个 Mental Model：

```
         authoritative state
                |
                v
            Server A
```

内存在哪里、CPU 在执行什么、哪个线程拿了锁，通常可以在一个机器边界内观察。

---

### 1.2 现在把它变成三台机器

为了容错：

```
             Client
                |
                v
             Server A
             /      \
            v        v
       Server B    Server C
```

你决定每次写都复制三份：

```
Put(x = 1)
```

理想情况：

```
A: x = 1
B: x = 1
C: x = 1
```

可是实际可能发生：

```
time →

Client:   Put(x=1) ---------------------------->

A:            x=1
               |
               +------ x=1 ------> B
               |
               +------ x=1 ----X-> C
               |
             crash
```

结果：

```
A: crashed
B: x = 1
C: x = 0
```

现在一个看起来非常简单的问题出现了：

> **x 到底是多少？**

这不是哲学问题，而是 API 必须回答的问题。

---

### 1.3 真正麻烦的是 Partial Failure

单机程序经常是：

```
process alive
or
process dead
```

分布式系统不是。

可能：

```
A 能联系 B
B 能联系 C
A 不能联系 C
```

或者：

```
A 发消息给 B

B 执行了

B 发 ACK

ACK 丢了

A timeout
```

A 看到的是：

```
timeout
```

但 timeout 有两个完全不同的可能：

```
情况 1：

A ---- request --X--> B

操作没发生
```

或者：

```
情况 2：

A ---- request ----> B
                     |
                     | operation executed
                     |
A <------ ACK -------X

操作已经发生
```

因此：

> **“我没有收到结果” ≠ “操作没有执行”。**

这一条以后会贯穿：

- RPC retry
- idempotency
- transaction
- leader election
- distributed lock
- cloud API
- payment
- Terraform
- Kubernetes Controller

---

### 1.4 最 naive 的方法

最自然的想法是：

```
多复制几份
```

例如：

```
A
B
C
```

任何一台挂了，还有两台。

但是 Replication 立刻产生一个新问题：

```
Replication
    ↓
multiple copies of state
    ↓
copies can diverge
    ↓
who is correct?
```

于是：

> **Replication 解决 availability / durability 的同时，也创造了 consistency 问题。**

这就是 6.824 后半门课的大门。

---

# Part 2：它在整个 6.824 知识地图中的位置

官方课程把 Lecture 1 放在最开头，后面紧接 RPC/Threads 和 GFS；MapReduce 则作为第一课阅读和 Lab，让学生第一天就面对 parallelism、worker failure、retry 等问题。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

可以把整门课先压缩成：

```
                   Distributed Systems
                           |
          +----------------+----------------+
          |                |                |
     Scalability      Fault Tolerance    Consistency
          |                |                |
      Partition         Replication      Semantics
      Parallelism           |                |
          |                 |          Linearizability
     MapReduce          Consensus             |
       Spark               |                  |
                           v                  |
                          Raft ----------------+
                           |
                           v
                State Machine Replication
```

第一课就在最顶部。

它告诉你：

```
为什么需要 Distributed Systems
            ↓
Distributed Systems 难在哪里
            ↓
后面的 abstraction 为什么存在
```

---

### RPC 和这节课是什么关系？

RPC 解决：

> **A 怎么调用 B？**

```
A                B
|                |
|---- RPC ------>|
|                |
|<--- result ----|
```

但是 RPC **不解决**：

```
timeout 后操作执行了没有？
retry 会不会执行两次？
B crash 怎么办？
两个 B 怎么保持一致？
```

所以：

```
RPC = communication abstraction
```

而不是：

```
RPC = fault tolerance protocol
```

---

### Threads / Concurrency 呢？

Threads 解决的是：

```
同一进程内部
多个 execution flow
共享内存
```

Distributed Systems 是：

```
不同机器
独立 memory
message passing
partial failure
```

两者共同的问题是：

```
concurrency
```

但 distributed concurrency 多了一层：

```
network uncertainty + independent failures
```

---

### Replication 和 Consensus

这是以后必须始终区分的一对。

#### Replication

解决：

> 数据放多份。

```
x
├── replica A
├── replica B
└── replica C
```

#### Consensus

解决：

> 多个节点在 failure 存在时，就某个决定达成一致。

例如：

```
log[10] = Put(x=1)
```

大家必须决定：

```
index 10 到底是什么？
```

所以：

```
Replication
= copies

Consensus
= agreement
```

**Replication 并不会自动产生 Consensus。**

---

### Consensus 和 State Machine Replication

Consensus 通常解决：

```
大家决定一个 value/order
```

State Machine Replication 使用很多次 Consensus：

```
Consensus #1 -> command 1
Consensus #2 -> command 2
Consensus #3 -> command 3
...
```

所有节点：

```
same ordered log
       ↓
same deterministic state machine
       ↓
same state
```

这才形成 replicated service。

---

### Linearizability 又是什么？

Linearizability 不回答：

> 内部怎么复制？

它回答：

> **客户端应该观察到什么？**

例如：

```
Write(x=1) 完成
       ↓
之后的 Read(x)
       ↓
必须看到 1
```

所以：

```
Raft / Consensus
       ↓
内部 mechanism

Linearizability
       ↓
外部 semantics
```

这两个层次不能混。

---

### 2PC 又是什么？

2PC 解决：

> 一个 transaction 跨多个 participant 时，是全部 commit 还是全部 abort？

例如：

```
Transfer $100

DB A: -100
DB B: +100
```

需要 atomicity：

```
both commit
or
both abort
```

Consensus 解决的是：

```
distributed agreement under failure
```

而 2PC 是：

```
distributed transaction commit protocol
```

经典 2PC coordinator crash 时可能 blocking。

所以：

```
2PC ≠ Consensus
```

Spanner 之类系统最终会把：

```
replication
+ consensus
+ distributed transactions
+ time
```

组合起来。

---

### MapReduce / Spark

它们是另一条路线。

不是：

```
让多个 replica 维护一个 mutable state
```

而是：

```
把一个大 computation
拆成许多并行 task
```

核心问题是：

```
partition work
schedule work
recover failed work
move intermediate data
handle stragglers
```

这也是为什么 MapReduce 是第一课很好的案例。

---

# Part 3：Lecture 1 最重要的 6 个 Mental Models

---

## Concept 1：Partial Failure

#### 它解决的问题

为什么 distributed system 的 failure 比单机难？

#### 一句话定义

> 系统的一部分可能失败，而其他部分仍然正常运行，并且正常部分未必能确定失败部分到底是 dead 还是 slow。

#### 直觉

例如：

```
A ---- network ---- B
```

A 三秒没有收到 B 的回复。

可能：

```
B dead
```

也可能：

```
B 正在 GC
```

也可能：

```
packet dropped
```

也可能：

```
network partition
```

#### 最重要的误解

```
❌ timeout = server dead
```

正确：

```
timeout
=
I don't know what happened.
```

这句话非常重要。

---

## Concept 2：No Shared Global State

单机线程可以共享：

```
memory
```

节点 A 和 B 不可以。

```
A memory                B memory

x = 1                   x = 0
```

A 修改自己的：

```
x = 1
```

不会神奇地修改：

```
B.x
```

必须：

```
message
```

于是产生延迟：

```
time →

A: x=1 -------- update -------->

B: x=0 x=0 x=0       x=1
```

所以在一段时间内：

```
A believes x=1
B believes x=0
```

这不是 bug。

这是分布式系统的正常状态。

---

## Concept 3：Concurrency

假设两个 Client：

```
Client 1: Put(x=1)
Client 2: Put(x=2)
```

分别到达两个 replica：

```
Client1 ---> A

Client2 ---> B
```

可能发生：

```
A:
x=1
then receive x=2

B:
x=2
then receive x=1
```

最后：

```
A: x=2
B: x=1
```

所以 distributed system 必须回答：

> **操作顺序由谁决定？**

这将引出：

```
Lamport Clock
Vector Clock
Total Order
Consensus
Linearizability
```

---

## Concept 4：Replication

#### 一句话

> 用多个副本换取 fault tolerance / availability / read scalability。

但是：

```
1 copy
```

只有一个 state。

变成：

```
3 copies
```

你获得：

```
redundancy
```

同时获得：

```
3 states to synchronize
```

所以可以记住一句非常好的工程直觉：

> **Replication trades machine failure for coordination complexity.**

---

## Concept 5：Consistency Specification

假设：

```
A: x=1
B: x=0
```

到底是不是错误？

答案是：

> 不知道。

因为必须先知道系统承诺什么。

如果系统承诺：

```
Linearizability
```

某些 execution 不允许。

如果只承诺：

```
Eventual Consistency
```

短时间不同可能完全合法。

因此：

```
implementation
```

之前必须先有：

```
specification
```

这是你以后读任何 paper 都应该首先问的问题：

> **这个系统到底承诺什么 semantics？**

---

## Concept 6：Scalability

为什么不全部放一台巨大的机器？

因为可能遇到：

```
CPU limit
memory limit
disk limit
network bandwidth limit
availability limit
geographical limit
```

于是分布式系统采用：

```
partition work
```

比如：

```
1 PB input

           split
            |
    +-------+-------+
    |       |       |
   W1      W2      W3
```

但 partition 又产生：

```
coordination
load balance
data movement
failure recovery
```

因此 scalability 不是：

```
machines += 10
performance *= 10
```

现实更接近：

```
speedup
=
parallelism
-
coordination
-
communication
-
stragglers
-
contention
```

---

# Part 4：System Model 与 Assumptions

Lecture 1 是 Introduction，因此**不存在一个统一算法的固定 system model**。

这是一个很重要的区别：

> 后面读 Raft 时可以问“最多容忍几个节点失败”；但 Introduction 本身没有一个 `f`。

不过整门课可以先建立一个 baseline model。

|维度|初步 Mental Model|
|---|---|
|Node|独立进程/机器|
|Failure|主要考虑 crash / slow / unavailable|
|Byzantine|默认通常不是重点，除非课程明确进入 BFT|
|Network|延迟、丢包、断开、partition 都可能发生|
|Ordering|不应默认不同 receiver 看到相同顺序|
|Timing|不应依赖已知严格 latency 上界|
|State|memory 和 persistent storage 必须区分|
|Recovery|具体 protocol 决定 crash-stop 还是 crash-recovery|

---

### asynchronous 的真正含义

不要把 asynchronous 理解成：

```
async/await
```

Distributed Systems 里的 asynchronous model 是：

> **你不能根据经过多长时间，就确定另一台机器发生了什么。**

例如：

```
A -------- message --------> B
```

100ms 没回来。

你不知道：

```
B dead
```

还是：

```
network delay = 101ms
```

这就是 Failure Detector 为什么难。

---

# Part 5：第一课的第一个真实案例——MapReduce

Lecture 1 配套的是 Dean/Ghemawat 的 MapReduce 论文。论文的核心目标不是“发明 map 和 reduce 这两个函数”，而是把 parallelization、data distribution、scheduling、failure handling 等复杂性隐藏在 runtime 后面，让应用程序主要表达计算逻辑。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf?utm_source=chatgpt.com)

这其实是整门课第一个非常漂亮的 Distributed Systems abstraction。

---

## 5.1 原始问题

假设 Google 要统计海量网页中的单词：

```
10 TB documents
```

单机：

```
for every document:
    count words
```

可能要很久。

自然想法：

```
        10 TB
          |
    +-----+-----+
    |     |     |
   W1    W2    W3
```

但很快你会面对：

```
谁分配工作？
worker crash 怎么办？
一部分 worker 特别慢怎么办？
中间结果放哪里？
怎样 regroup 相同 key？
task 重复执行怎么办？
```

MapReduce 的价值就在这里：

> **把 distributed execution complexity 放到 runtime，而不是让每个 application programmer 重写。**

---

## 5.2 Programming Model

用户提供：

```
Map(k1, v1)
    -> list(k2, v2)
```

和：

```
Reduce(k2, [v2...])
    -> output
```

例如 Word Count。

Map：

```
"hello world hello"

↓

("hello", 1)
("world", 1)
("hello", 1)
```

Shuffle：

```
hello -> [1,1]
world -> [1]
```

Reduce：

```
hello -> 2
world -> 1
```

---

## 5.3 Happy Path

系统大概：

```
                         Coordinator
                        /     |      \
                       /      |       \
                      v       v        v
                   Worker1 Worker2 Worker3
                     MAP     MAP      MAP
                       \      |       /
                        \     |      /
                       intermediate
                           data
                            |
                         shuffle
                            |
                     +------+------+
                     |             |
                   Reduce1       Reduce2
```

论文中的原始架构也是一个 coordinator/master 负责 task assignment，Map worker 产生 partitioned intermediate files，Reduce worker 再读取这些文件。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

---

### Step 1：Split

输入：

```
Input
```

拆成：

```
split0
split1
split2
...
splitM
```

每个 Map Task 处理一个 split。

---

### Step 2：Assign

Coordinator：

```
Map0 -> W1
Map1 -> W2
Map2 -> W3
```

---

### Step 3：Map

例如：

```
Map0
```

产生：

```
("foo", 1)
("bar", 1)
```

---

### Step 4：Partition intermediate data

假设有 `R=3` 个 Reduce Task：

```
partition =
hash(key) mod R
```

于是：

```
foo -> Reduce 2
bar -> Reduce 0
```

这里有一个非常重要的 invariant：

> **相同 key 必须进入同一个 Reduce partition。**

否则：

```
foo -> R1
foo -> R2
```

两个 Reduce 各算一半：

```
foo=10
foo=20
```

你就得不到：

```
foo=30
```

---

### Step 5：Shuffle

Reduce worker 获取属于自己的 intermediate partition：

```
Map0 ──┐
Map1 ──┼──> Reduce0
Map2 ──┘
```

---

### Step 6：Reduce

把：

```
(foo, 1)
(foo, 1)
(foo, 1)
```

组合：

```
foo -> 3
```

---

# Part 6：现在制造第一个 failure

Happy Path 没什么值得学。

真正的 Distributed Systems 从这里开始。

假设：

```
Coordinator -> W1: Run Map17
```

时间线：

```
time →

Coordinator: assign Map17 -------------------->

W1:                       execute
                           |
                           write intermediate
                           |
                           Done(Map17) ------->

Coordinator:                          record complete
```

现在 W1 crash。

---

### 问题

Map17 已经执行过：

```
为什么还需要重新执行？
```

因为 Map output 在原始 MapReduce 设计中放在 worker local disk。

```
W1
├── memory
└── local disk
      └── Map17 output
```

W1 不可达：

```
Map17 output
```

也不可达。

因此：

```
Coordinator
     |
     | reassign Map17
     v
    W2
```

论文明确采用这种策略：worker 被判定失联后，其进行中的 task 会重新调度；即使某些 Map task 已完成，因为 intermediate output 位于失败 worker 的 local disk，也需要重新执行。已完成的 Reduce output 存在 global filesystem，因此不一定需要重算。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

这揭示了一个很重要的 fault-tolerance technique：

> **有些 state 与其昂贵地 replication，不如丢掉后 recompute。**

---

# Part 7：为什么 Re-execution 是可行的？

因为 MapReduce 有一个非常强的设计选择：

```
Map(input)
```

和：

```
Reduce(input)
```

最好是 deterministic。

也就是：

```
same input
    ↓
same output
```

于是：

```
Map17 execution #1
```

和：

```
Map17 execution #2
```

逻辑上应该得到相同结果。

因此 failure recovery 可以非常简单：

```
lost computation
       ↓
redo computation
```

这和数据库非常不同。

如果 operation 是：

```
bankAccount -= $100
```

你不能随便：

```
retry
retry
retry
```

否则：

```
-$100
-$100
-$100
```

MapReduce 的 abstraction 是故意设计得适合 retry 的。

---

# Part 8：Duplicate Execution

这里是第一个非常经典的 Distributed Systems 问题。

Coordinator：

```
W1 -> Map17
```

W1 很慢。

Coordinator timeout：

```
maybe W1 died
```

于是：

```
W2 -> Map17
```

但 W1 没死。

结果：

```
             Map17
            /     \
           v       v
          W1      W2
```

两个都运行。

---

### Timeline

```
time →

Coordinator: assign Map17 -> W1
                         |
                         | timeout
                         |
             assign Map17 -> W2

W1:           -------- compute -------- Done

W2:                       ---- compute ---- Done
```

所以：

> **timeout 不但不能证明 failure，还可能制造 duplicate execution。**

以后看到：

```
retry
```

你的脑子应该自动想到：

```
duplicate
```

---

## 那怎么办？

原始 MapReduce 的方法非常漂亮：

```
temporary output
       +
atomic commit
```

Reduce worker：

```
write:
tmp/reduce17-attempt2
```

完成后：

```
atomic rename
```

成：

```
output/reduce17
```

即使两个 execution：

```
attempt A
attempt B
```

最后只会形成一个 committed output。

论文对 deterministic Map/Reduce 的语义目标是：distributed execution 的结果等价于某个无故障 sequential execution；实现依赖 task output 的 atomic commit。已经完成的 Map task 再收到 completion 会忽略，Reduce output 则利用 filesystem atomic rename 形成一个最终输出。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

这已经开始出现一个以后极其重要的设计模式：

```
Maybe execute multiple times
             ↓
But expose one logical effect
```

也就是工程上常追求的：

```
effectively-once
```

而不是幻想网络能直接提供真正的：

```
exactly-once physical execution
```

---

# Part 9：MapReduce State

Coordinator 大致维护：

```
MapTask {
    state:
        IDLE
        IN_PROGRESS
        COMPLETED

    worker
    outputLocations
}

ReduceTask {
    state:
        IDLE
        IN_PROGRESS
        COMPLETED

    worker
}
```

论文里的 master 确实维护每个 Map/Reduce task 的 `idle / in-progress / completed` 状态，以及中间文件位置等 metadata。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

状态转换可以画成：

```
             assign
IDLE ----------------> IN_PROGRESS
 ^                         |
 |                         | success
 |                         v
 +---- timeout/failure   COMPLETED
```

注意：

```
IN_PROGRESS -> IDLE
```

非常关键。

因为：

```
worker failure
       ↓
retry
```

---

## 最重要的 Invariants

#### Invariant 1

每一个 logical Map task 最终必须贡献：

```
one logical result
```

不能漏。

---

#### Invariant 2

Reduce `r` 必须看到所有 Map task 给 partition `r` 的 logical outputs。

例如：

```
M0 -> R2
M1 -> R2
M2 -> R2
```

R2 必须得到：

```
M0 + M1 + M2
```

否则结果缺数据。

---

#### Invariant 3

一个 Reduce partition 最终只能暴露一个 committed output。

否则：

```
reduce17-v1
reduce17-v2
```

下游不知道哪个是真的。

---

#### Invariant 4

retry 不应改变 deterministic job 的 observable result。

也就是：

```
execution without failure
```

和：

```
execution with worker crashes + retries
```

应该得到相同 logical output。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

---

# Part 10：Safety 和 Liveness

现在正式建立这两个概念。

以后学：

```
Raft
Paxos
2PC
locks
distributed transactions
```

都应该首先拆成这两个问题。

---

### Safety

问的是：

> **什么事情绝对不能发生？**

MapReduce 的一个 safety property 可以理解成：

```
不能因为 retry
让一个 deterministic task 的作用
被 logical 地算两遍
```

或者：

```
不能产生半写完的 final result
```

机制：

```
temporary file
atomic commit / rename
deterministic computation
duplicate completion handling
```

---

### Liveness

问的是：

> **在合理假设下，系统最终能不能继续？**

例如 worker 挂：

```
W1 crash
```

系统：

```
detect timeout
    ↓
reassign task
    ↓
W2 executes
```

所以只要最终：

```
有可工作的 worker
+
网络恢复
+
Coordinator 活着
```

job 可以继续。

原始论文对 worker crash 做了重调度，但对单 master failure 的选择比较简单：当时实现直接终止当前 MapReduce computation，让 client 重新运行；论文也提到可以 checkpoint master state，但实际实现没有把 master 做成高可用 replicated service。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

所以：

```
Safety ≠ Liveness
```

完全可能：

```
系统不产生错误结果
```

但：

```
永远无法完成
```

---

# Part 11：Failure Matrix

以 MapReduce 为例：

|Failure|系统行为|Safety|Availability / Progress|机制|
|---|---|---|---|---|
|Worker crash during Map|task 重跑|可保持|暂时受影响|re-execution|
|Worker crash after Map|local intermediate 丢失，Map 重跑|可保持|暂时受影响|lineage/recompute|
|Worker crash during Reduce|Reduce 重跑|可保持|暂时受影响|re-execution|
|Worker slow|可能重复调度|可保持|改善尾延迟|duplicate/backup execution|
|RPC lost|Coordinator 可能认为 task 未完成|可保持|retry|idempotent handling|
|Duplicate completion|忽略多余 completion|可保持|基本不受影响|task state|
|Network partition|worker 可能被认为失败|可保持|partition 中任务重调度|timeout + retry|
|Delayed message|old execution 可能晚到|必须避免覆盖错误结果|通常可继续|task state / commit|
|Master crash|原始设计 job abort|数据不一定错误|job 停止|client retry|
|Map worker local disk lost|Map output 丢失|可恢复|需要 recompute|rerun Map|

原始论文中特别值得注意的是：

```
worker failure
```

被设计成正常事件。

不是：

```
OMG exceptional case
```

而是：

```
expected operational condition
```

这是大型分布式系统一个非常重要的思想变化。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

---

# Part 12：为什么 MapReduce 是 Lecture 1 极好的例子？

因为它第一次向你展示：

> **Distributed system 的威力来自一个好的 abstraction，而不是把 network API 暴露给 programmer。**

没有 MapReduce：

```
Application
   |
   +-- partition
   +-- RPC
   +-- schedule
   +-- retry
   +-- failure detection
   +-- data shuffle
   +-- straggler handling
   +-- output commit
```

MapReduce：

```
Application

Map(...)
Reduce(...)
```

下面：

```
             MapReduce Runtime
                    |
    +---------------+---------------+
    |               |               |
partition       scheduling     fault recovery
    |               |               |
shuffle         locality         retry
```

论文明确把这一点作为设计目标：让 runtime 隐藏 parallelization、fault tolerance、data distribution、load balancing 等复杂性。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf?utm_source=chatgpt.com)

这也是以后整门课的一个主题：

```
complex distributed mechanism
             ↓
simple abstraction
```

例如：

```
Raft
 ↓
replicated log

ZooKeeper
 ↓
small coordination API

Spanner
 ↓
distributed SQL transactions

GFS
 ↓
file abstraction
```

---

# Part 13：为什么不能简单用一台超级服务器？

这也是 Lecture 1 的 Why。

主要有四类动机。

### 1. Parallelism

例如：

```
1 machine: 100 hours

100 machines:
ideal ≈ 1 hour
```

虽然现实不会完美线性。

---

### 2. Fault Tolerance

如果：

```
Service = one machine
```

那么：

```
machine failure
=
service failure
```

增加 redundancy：

```
A
B
C
```

可以提高 survivability。

但是前面已经看到：

```
redundancy
→ consistency problem
```

---

### 3. Physical Distribution

有些系统天然分布：

```
US region
EU region
Asia region
```

或者：

```
phones
edge nodes
cloud
```

你不可能把它们塞进一台机器。

---

### 4. Scale

数据量：

```
1 PB
```

或者 QPS：

```
10,000,000 requests/sec
```

单机可能根本承受不了。

于是：

```
sharding
partitioning
parallelism
```

变成必要机制。

---

# Part 14：Scalability 为什么那么难？

假设你有一个任务：

```
T1 = 1000 seconds
```

10 台机器，naive 想：

```
T10 = 100 seconds
```

现实：

```
T10
=
computation
+ communication
+ coordination
+ synchronization
+ skew
+ stragglers
```

比如：

```
W1: 10 sec
W2: 11 sec
W3: 10 sec
W4: 300 sec
```

整个 stage：

```
300 sec
```

因为：

```
completion time
≈ slowest critical worker
```

这就是 tail latency / straggler 问题。

MapReduce 后来采用 backup task 等技术减少 straggler 对 job completion 的影响。

你以后会在：

```
Spark
distributed query execution
fan-out RPC
microservices
```

看到同一个问题。

---

# Part 15：为什么“Failure Detection”特别难？

假设 Coordinator：

```
ping Worker
```

5 秒没回复。

Coordinator：

```
Worker is dead!
```

这是逻辑错误。

你真正知道的只有：

```
no response within 5 seconds
```

无法区分：

```
Worker crashed
```

和：

```
Worker GC for 6 seconds
```

和：

```
network congestion
```

因此 timeout 的语义应该是：

> **“我不能继续依赖这个节点及时响应。”**

而不是：

> “我证明了这个节点已经死亡。”

这也是为什么后面的：

```
Raft election timeout
```

不能理解成：

```
leader death detector
```

它只是：

```
I haven't heard from a leader recently,
so let's try an election.
```

---

# Part 16：这一课开始训练你处理 RPC ambiguity

这是后面 Lecture 2 的直接入口。

假设：

```
Client                  Server

  |---- Increment(x) ---->|
                           |
                           | x++
                           |
  |<-------- OK -----------X
```

Client：

```
timeout
```

它应该 retry 吗？

如果 retry：

```
Increment(x)
```

再次执行：

```
x++
```

现在一次请求产生两次 effect。

---

### Naive solution

```
timeout -> retry
```

失败。

---

### Mechanism 1：Idempotent operation

例如：

```
Set(x, 10)
```

重复：

```
Set(x,10)
Set(x,10)
Set(x,10)
```

结果仍然：

```
x=10
```

---

### Mechanism 2：Request ID + deduplication

```
request_id = abc123
```

Server：

```
seen abc123?
```

如果已经执行：

```
return cached result
```

不再执行。

于是：

```
at-least-once delivery
+
deduplication
≈
exactly-once logical effect
```

这在你实际做 Cloud Control Plane 时尤其常见。

比如：

```
CreateVPC()
```

timeout 后最危险的问题不是：

```
API failed
```

而是：

```
VPC 到底有没有创建？
```

所以 cloud APIs 大量使用：

```
request ID
operation ID
resource ID
idempotency token
reconcile
```

---

# Part 17：Top 7 Misconceptions

### ❌ 1. Distributed System 就是很多机器一起工作

这只描述物理形态。

真正的问题是：

```
independent failure
+
network uncertainty
+
concurrent state
```

---

### ❌ 2. Replication 自动解决 reliability

Replication：

```
A B C
```

确实提供 redundancy。

但马上出现：

```
A=1
B=1
C=0
```

所以还需要：

```
consistency protocol
```

---

### ❌ 3. Timeout 意味着操作失败

错。

Timeout 只意味着：

```
result unknown
```

操作可能：

```
not executed
executing
executed
```

---

### ❌ 4. Timeout 意味着节点死了

错。

可能只是：

```
slow
partitioned
GC
CPU overloaded
packet loss
```

---

### ❌ 5. “Exactly once”意味着函数物理上只执行一次

很多实际系统根本做不到这么简单。

常见模式是：

```
execute maybe multiple times
        +
commit one logical result
```

---

### ❌ 6. MapReduce fault tolerance 依赖 Consensus

并不是。

经典 MapReduce 主要依赖：

```
central coordinator
+
task retry
+
deterministic recomputation
+
atomic output commit
```

原始设计甚至没有让 master 自身成为 fault-tolerant replicated state machine。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

---

### ❌ 7. 分布式系统首先应该考虑 performance

很多工程师很容易这样：

```
先实现
↓
然后跑快
↓
然后处理 failure
```

更好的顺序往往是：

```
semantics
↓
invariants
↓
failure model
↓
protocol
↓
performance
```

否则你甚至不知道所谓“优化”有没有破坏正确性。

---

# Part 18：和 Kubernetes / etcd 联系起来

这部分和你的 Infra 背景会非常有帮助。

---

### etcd

Kubernetes 需要一个 authoritative control-plane state：

```
Deployment replicas=3
Pod ...
Service ...
ConfigMap ...
```

如果只是三台普通 database：

```
etcd1
etcd2
etcd3
```

但每台可以各自修改：

```
state divergence
```

就不能作为可靠 control-plane state。

因此 etcd 使用：

```
Raft
```

获得：

```
ordered replicated log
```

再得到：

```
consistent state
```

Mental Model：

```
Kubernetes API
      |
      v
    etcd
      |
      v
Raft replicated state machine
```

---

## Kubernetes Controller

Controller：

```
Desired State
     |
     v
 Observe
     |
     v
Difference?
     |
     v
 Reconcile
```

比如：

```
desired replicas = 3

actual pods = 2
```

Controller 创建一个。

这和 MapReduce 的 retry 思想有一点共通：

```
operations may be retried
```

因此 reconciliation action 应尽可能：

```
idempotent
```

但是不要强行类比：

```
Kubernetes Controller ≠ Consensus algorithm
```

Controller 的任务是：

```
convergence toward desired state
```

Raft 的任务是：

```
agree on replicated operation order/state
```

---

## Terraform

Terraform 也能看到 Lecture 1 的核心问题。

假设：

```
Terraform
   |
   | CreateLoadBalancer
   v
Cloud API
```

然后：

```
timeout
```

Terraform 面对的问题：

```
LB 没创建？
LB 创建中？
LB 已经创建但 response lost？
```

这就是：

```
partial failure
+
RPC ambiguity
```

Terraform Provider 如果 retry 不正确：

```
LB1
LB2
LB3
```

可能创建多个资源。

所以需要：

```
resource identity
state
read-after-create
import/adoption
idempotent APIs
eventual reconciliation
```

Lecture 1 的思想并不抽象，它直接存在于每一次 cloud API 调用里。

---

## Kafka

Kafka 又展示另一组选择。

```
Topic
  |
  +-- Partition 0
  +-- Partition 1
  +-- Partition 2
```

这里：

```
partition
```

用于 scalability。

每个 partition 再：

```
replicate
```

用于 durability/fault tolerance。

所以：

```
Partitioning
    ↓
scalability

Replication
    ↓
fault tolerance
```

是两个完全不同的维度。

这点非常重要。

---

## Redis

Redis primary + replica：

```
       Primary
       /     \
      v       v
 Replica1  Replica2
```

如果 replication 是 asynchronous：

```
write Primary
return OK
crash before replica receives write
```

failover 后可能丢掉最近的 write。

这就是：

```
performance
vs
durability/consistency
```

的设计 trade-off。

所以：

> “有 replica”不能直接推导“linearizable”。

---

# Part 19：MapReduce Paper 阅读框架

### Paper Problem

应用 programmer 本来只是想：

```
count words
build inverted index
sort data
```

却被迫处理：

```
parallelism
scheduling
failure
communication
load balance
```

---

### Previous Approach

每个应用自行实现 distributed execution。

问题：

```
大量重复 infrastructure code
```

---

### Key Insight

把许多大规模 batch computation 表达成：

```
Map
+
Shuffle
+
Reduce
```

让 runtime 负责 distribution。

论文强调这种 functional-style abstraction 可以让 runtime 自动 parallelize，并利用 re-execution 处理 failure。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf?utm_source=chatgpt.com)

---

## Design

```
                  Master
             /      |       \
            /       |        \
           v        v         v
         Map       Map       Map
           \        |        /
            \       |       /
          intermediate data
                  |
                shuffle
                  |
             +----+----+
             |         |
          Reduce    Reduce
```

---

## Mechanism

核心机制不是只有 Map/Reduce 函数。

而是：

```
input splitting
task scheduling
data locality
partitioned shuffle
worker failure detection
task re-execution
atomic output commit
straggler mitigation
```

---

## Evaluation

论文展示这种模型能够运行在大量 commodity machines 上，并处理 terabyte-scale 数据；其目标之一正是让 large-cluster computation 可以规模化，同时对 machine failures 保持实用的容错能力。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf?utm_source=chatgpt.com)

这里真正应该关注的不是某个 2004 年 benchmark 数字，而是：

> **abstraction 没有把 scalability 换成不可接受的 overhead。**

---

## Limitations

经典 MapReduce 很适合：

```
large batch
mostly deterministic
data-parallel computation
```

不适合：

```
low-latency request processing
interactive queries
fine-grained mutable shared state
many iterative algorithms
continuous streaming
```

另外原始实现：

```
single master
```

本身并没有做成完整 fault-tolerant replicated service。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

---

## What aged well?

这些思想直到今天仍然非常强：

```
task abstraction
partitioning
re-execution
idempotency
data locality
straggler handling
logical vs physical execution
```

Spark、Flink、Ray、Kubernetes Job 等系统都能看到这些思想的延续，只是 execution model 更复杂。

---

## What changed?

后来系统从简单：

```
Map -> Reduce
```

发展成：

```
DAG
```

例如：

```
Stage A
  |
  +---- Stage B
  |
  +---- Stage C
          |
          v
        Stage D
```

Spark 特别强化：

```
in-memory computation
DAG execution
lineage
iterative workload
```

但根本思想没有消失：

```
lost state
↓
recompute from lineage
```

和 MapReduce 的：

```
lost Map output
↓
rerun Map task
```

是一条明显的思想演化线。

---

# Part 20：Lecture 1 里的公式

这一课不需要大量数学。

但 MapReduce 有一个值得真正理解的式子：

```
partition(key) = hash(key) mod R
```

其中：

```
R = number of Reduce tasks
```

例如：

```
R = 4
```

如果：

```
hash("alice") = 17
```

那么：

```
17 mod 4 = 1
```

所以：

```
alice -> Reduce 1
```

关键不是 `%` 运算。

关键 invariant 是：

```
same key
    ↓
same hash
    ↓
same partition
    ↓
same Reduce task
```

因此 Reduce 才能看到这个 key 的全部 values。

---

# Part 21：和 Lab 1 的关系

当前官方 Lab 1 要实现：

```
Coordinator
+
Workers
```

Worker 通过 RPC 请求任务；Coordinator 在 worker 长时间没有完成时重新分配任务。当前 handout 规定实验中大约十秒后可以认为 task 需要重新调度。Lab 还使用 shared filesystem 来简化环境。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-mr.html?utm_source=chatgpt.com)

真正应该学的不是：

```
怎么让测试绿
```

而是下面这些问题。

---

### Coordinator task state

你应该主动设计：

```
IDLE
RUNNING
DONE
```

以及：

```
task id
worker/attempt
start time
```

然后不断问：

```
谁能修改？
什么时候修改？
两个 goroutine 同时修改怎么办？
```

---

### Timeout

不要写成 Mental Model：

```
10 seconds
    ↓
worker dead
```

而应该：

```
10 seconds
    ↓
it's safe/useful to try another execution
```

区别非常重要。

---

### Retry

必须考虑：

```
W1 其实没挂
```

所以：

```
W1
+
W2
```

可能同时完成一个 task。

你的代码必须能处理 duplicate completion。

---

### Atomic output

不要：

```
open final file
write half
crash
```

否则另一个 worker：

```
看到半个 result
```

好的思路：

```
temp file
   ↓
complete write
   ↓
rename / publish
```

---

### Concurrency

Coordinator 处理多个 worker RPC：

```
W1 ---- RPC ----\
W2 ---- RPC ----- Coordinator
W3 ---- RPC ----/
```

共享 task table 就存在 race。

因此 Lab 1 会自然训练：

```
goroutine
mutex
RPC
timeout
retry
state transition
```

而这正好为 Lecture 2 RPC and Threads 做准备。

官方也特别提醒，分布式 lab 难 debug 的主要原因之一正是 concurrency、crash 和 unreliable network，并建议使用 race detector。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/guidance.html?utm_source=chatgpt.com)

---

## 你做 Lab 1 时最重要的 Debugging Strategy

不要首先看：

```
哪个 function 有 bug？
```

先看 state transition。

例如日志统一输出：

```
task=17 attempt=1 IDLE -> RUNNING worker=3
task=17 attempt=1 TIMEOUT
task=17 attempt=2 RUNNING worker=5
task=17 attempt=2 RUNNING -> DONE
task=17 attempt=1 late completion ignored
```

这样你调试的是：

```
distributed protocol
```

而不是：

```
random Go code
```

这是很大的思维升级。

---

# Part 22：Lecture 1 最重要的 Problem → Solution Chain

把整课压成这一条：

```
Need more compute / storage / availability
                  ↓
           use many machines
                  ↓
       machines execute concurrently
                  ↓
     communication has latency/failure
                  ↓
          partial failure appears
                  ↓
    state/work may become inconsistent
                  ↓
         need clear semantics
                  ↓
      introduce abstractions/protocols
                  ↓
 +----------------+------------------+
 |                |                  |
Partitioning   Replication       Coordination
 |                |                  |
Scalability  Fault Tolerance      Agreement
 |                |                  |
MapReduce        Raft           Consensus
 Spark             |
                State Machine
                Replication
```

还有一条 MapReduce 专属链：

```
Huge computation
      ↓
One machine too slow
      ↓
Split onto many workers
      ↓
Worker can fail
      ↓
Retry task
      ↓
Retry causes duplicate execution
      ↓
Deterministic functions
+
atomic output commit
      ↓
Failure becomes re-computation
```

这条链非常值得记住。

---

# Part 23：这节课其实埋下了以后几乎所有问题

你之后学每个系统，可以问同样的 8 个问题：

```
1. What state exists?

2. Who owns that state?

3. How is state replicated/partitioned?

4. What can fail?

5. What can each node know?

6. What happens when a message is lost/delayed/duplicated?

7. What are the invariants?

8. What semantics does the client observe?
```

例如 Raft：

```
state       -> replicated log
owner       -> leader coordinates
replication -> AppendEntries
failure     -> leader/follower crash
knowledge   -> terms/logs differ
network     -> delayed/duplicate RPC
invariant   -> committed entries preserved
semantics   -> can build linearizable KV
```

这就是一个 Distributed Systems Engineer 的阅读框架。

---

# Part 24：五级练习

这次你要求一次性讲完，所以练习也一起给你；建议先自己推理，再看下面的参考答案。

#### Level 1 — Concept

三台 server：

```
A B C
```

如果 A 挂了，B 和 C 还在：

> 为什么不能仅凭“还有两个 replica”就说系统一定能继续正确工作？

因为我们还不知道：

```
B/C state 是否相同
谁可以写
consistency specification 是什么
protocol 是否要求 quorum
```

---

#### Level 2 — Execution

```
Coordinator -> W1: Map7
W1 finishes Map7
Done message is lost
```

Coordinator 应该怎么办？

可能重新执行 Map7。

关键不是避免第二次 execution，而是保证：

```
second execution
不会造成 second logical effect
```

---

#### Level 3 — Failure

```
W1 runs Map7
Coordinator timeout
W2 runs Map7
W1 comes back
```

现在两个都发送完成。

正确设计不应该依赖：

```
只会有一个 completion
```

而应该：

```
接受一个 logical result
忽略/安全处理其他 attempt
```

---

#### Level 4 — Counterexample

假设 Map 函数是：

```
Map(record):
    chargeCreditCard($10)
    emit(...)
```

MapReduce 的普通 retry 还安全吗？

**不安全。**

因为：

```
Map rerun
```

会重复产生 external side effect。

因此 external effects 需要额外：

```
idempotency
transaction
deduplication
```

MapReduce paper 本身也明确要求这类 side effect 由应用负责做到 atomic/idempotent。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)

---

#### Level 5 — System Design

如果设计一个 distributed metadata service：

```
Get(key)
Put(key,value)
```

你不能照搬 MapReduce：

```
crash -> just recompute
```

因为 mutable state 不是单纯 deterministic computation。

这时候就需要另一套机制：

```
Replication
+
Consensus
+
State Machine Replication
```

这正是后面课程逐渐进入 Raft 的原因。

---

# Part 25：30 秒版本

如果面试官问：

> MIT 6.824 Introduction 在讲什么？

你可以说：

> Distributed systems 的核心挑战不是把程序运行在很多机器上，而是多台独立机器只能通过不可靠、具有延迟的网络协作，并且可能发生 partial failure。因此 scalability、fault tolerance 和 consistency 会互相影响。第一课通过 MapReduce 展示了一个重要思想：通过好的 abstraction，把 partitioning、scheduling、retry 和 failure recovery 隐藏在 runtime 中；同时它也埋下了后面 RPC ambiguity、Replication、Consensus 和 consistency semantics 等整个课程的核心问题。

---

# Part 26：3 分钟版本

可以这样组织：

```
Distributed System
不是
"single machine × N"

因为多个 node：
- 独立 state
- 并发运行
- 只能 message passing
- failure 不同步
```

所以出现三个主要问题：

```
Scalability
Fault Tolerance
Consistency
```

为了 scalability：

```
partition work
```

为了 fault tolerance：

```
replicate/retry
```

但：

```
retry -> duplicate execution
replication -> divergent copies
```

于是必须设计：

```
idempotency
atomic commit
consistency models
coordination protocols
```

MapReduce 是第一例：

```
Map + Reduce
```

作为简单 programming abstraction。

runtime 处理：

```
partition
schedule
shuffle
failure detection
re-execution
stragglers
```

尤其利用：

```
deterministic computation
+
retry
+
atomic output publication
```

把 worker failure 转化为：

```
recompute
```

后面的课程则开始处理更困难的 stateful service：

```
RPC
→ Replication
→ Consensus
→ Raft
→ State Machine Replication
→ Linearizability
→ Distributed Transactions
```

---

# Part 27：深入版本

整节课可以压缩成：

```
Problem
  ↓
One machine isn't enough
  ↓
Distribute computation/state
  ↓

Model
  ↓
independent machines
message passing
concurrency
partial failure

  ↓

New problems
  ↓
uncertain operation outcome
duplicate execution
inconsistent replicas
stragglers
network partitions

  ↓

Mechanisms
  ↓
partitioning
retry
idempotency
replication
atomic commit
coordination

  ↓

Invariants
  ↓
define what must remain true
despite concurrency/failure

  ↓

Safety
  ↓
bad outcomes never happen

  ↓

Liveness
  ↓
under suitable assumptions,
system eventually progresses

  ↓

Trade-offs
  ↓
performance
consistency
availability
coordination overhead
```

---

# Part 28：整个 6.824 的知识网络

现在你可以把第一课挂到这个位置：

```
                         Distributed Systems
                                  |
              +-------------------+-------------------+
              |                   |                   |
         Scalability         Fault Tolerance      Consistency
              |                   |                   |
        Partitioning          Redundancy         Specification
              |                   |                   |
       +------+------+       Replication       Linearizability
       |             |            |                   |
   MapReduce        Sharding      |                   |
       |                          |                   |
     Spark                     Consensus -------------+
                                  |
                         +--------+--------+
                         |                 |
                       Raft              Paxos
                         |
                         v
                State Machine Replication
                         |
                         +----------> etcd
                         |              |
                         |          Kubernetes
                         |
                         +----------> distributed KV

Distributed Storage
       |
      GFS

Distributed Coordination
       |
   ZooKeeper

Distributed Transactions
       |
      2PC
       |
 replication + consensus + timestamps
       |
    Spanner
```

---

## 最后，把 Lecture 1 真正压缩成一个 Mental Model

如果以后你遇到一个陌生 Distributed System，不要首先问：

```
它用了什么数据库？
```

也不要首先问：

```
用了 Raft 还是 Paxos？
```

先问：

```
                    SYSTEM
                       |
        +--------------+--------------+
        |              |              |
      State          Work           Failure
        |              |              |
Where is it?     Who executes?    What can fail?
        |              |              |
Replicated?      Can retry?       What can I know?
Partitioned?     Idempotent?      How recover?
        |
What semantics?
        |
What invariants?
```

然后再问：

```
如果这里 crash 呢？

如果 message 丢了呢？

如果 ACK 丢了呢？

如果 retry 呢？

如果两个节点同时认为自己应该行动呢？

如果 network partition 呢？

如果操作已经发生但 caller 不知道呢？
```

**如果这些问题开始成为你的条件反射，你就已经抓到了 MIT 6.824 第一课真正想训练的能力。**

而从下一课 **RPC and Threads** 开始，课程会把今天最重要的一个抽象问题放到显微镜下面：

```
Client ---- RPC ----> Server
```

看起来像一次普通函数调用。

实际上它隐藏着：

```
network
concurrency
timeout
retry
duplicate execution
ambiguous result
```

这就是 Lecture 1 → Lecture 2 最重要的衔接。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)
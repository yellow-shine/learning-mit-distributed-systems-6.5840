## MIT 6.824 Lecture 2 — RPC and Threads

这节课在 6.824 里看起来像一节“Go 编程基础课”，但它其实非常重要，因为它第一次把你带进 **distributed program 的真实 execution model**：

> **很多并发执行的 threads/goroutines，通过一个可能失败、延迟、重复尝试的 RPC abstraction 互相通信。**

2026 年 MIT 6.5840 仍然把 Lecture 2 安排为 **RPC and Threads**，核心例子仍然是 `crawler.go`、`kv.go` 和 condition-variable/vote examples。官方 notes 明确说，这些 Go 细节只是载体，背后的概念是广泛适用于分布式系统的。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html)

---

# Part 1：这节课到底想解决什么问题？

如果只允许你记住一个问题：

> **当一个程序由许多并发执行单元组成，并且这些执行单元位于不同机器上时，我们如何组织它们之间的执行与通信？更重要的是：当 concurrency 和 network failure 出现时，我们还能相信什么？**

单机程序里：

```
result = f(x)
```

你的 mental model 很简单：

```
调用 f
  ↓
f 执行
  ↓
f 返回
  ↓
我得到 result
```

调用者和 `f()` 通常处在同一个 failure domain。

如果整个进程没崩：

```
call
  ↓
execute
  ↓
return
```

是非常紧密的一件事。

---

进入 distributed system：

```
Client
   |
   | Put(x, 1)
   v
Server
```

RPC 想让你继续写：

```
reply := call("Server.Put", args)
```

看起来仍然像：

```
reply = Put(x, 1)
```

但真实执行是：

```
Client                     Server

serialize(args)

request -------------------->

                         deserialize
                         execute Put
                         serialize(reply)

        <---------------- reply

deserialize(reply)
return
```

这里突然插入了：

```
network
process crash
concurrency
serialization
timeout
```

因此一个最普通的问题：

```
RPC timeout
```

已经不能回答：

> **Server 到底执行了吗？**

可能是：

```
Case 1

Client ----X

request 根本没到 Server
```

也可能：

```
Case 2

Client ------------> Server
                     execute
                     x = 1

Client <------X----- reply
```

甚至：

```
Case 3

Client ------------> Server
                     execute
                     CRASH
```

Client 观察到的可能完全一样：

```
timeout
```

MIT 的 Lecture notes 特别强调：

> client 没收到 response，并不知道 server 有没有看到甚至执行 request。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

这就是 RPC 最深的坑。

---

与此同时，还有另一个问题。

Server 不可能一次只处理一个 client：

```
Client A ----\
Client B -----+----> Server
Client C ----/
```

RPC runtime 往往并发运行 handler。

于是：

```
RPC A → goroutine A
RPC B → goroutine B
RPC C → goroutine C
```

它们又会访问同一个：

```
server state
```

于是你同时面对两个世界：

```
        Local concurrency
              |
    goroutine / mutex / race
              |
              v
        Shared state

================================================

       Distributed communication
              |
             RPC
              |
              v
 delay / loss / crash / ambiguity
```

**Lecture 2 就是在建立这两个世界。**

---

# Part 2：放到整个 6.824 知识地图里

课程逻辑可以画成：

```
Lecture 1
Distributed Systems Introduction
        |
        | 为什么要 distribution？
        | performance / fault tolerance / scale
        v
Lecture 2
RPC + Threads
        |
        | distributed program 实际怎么运行？
        |
        +------------------------+
        |                        |
        v                        v
 concurrency                 communication
 goroutine                      RPC
 mutex                          timeout
 channel                        retry
 race                           ambiguity
        \                        /
         \                      /
          +----------+---------+
                     |
                     v
            failure + concurrency
                     |
                     v
              Replication
                     |
              Primary/Backup
                     |
             Consensus / Raft
                     |
       State Machine Replication
                     |
              Linearizability
```

官方课程顺序也体现得很明显：RPC and Threads 后面直接进入 GFS、Paxos/Raft、Linearizability、ZooKeeper、Distributed Transactions 等。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html)

---

### RPC vs Threads

RPC 解决：

> **两个 address space 怎么通信？**

Threads 解决：

> **一个 address space 内如何组织多个同时进行的 activity？**

例如 Raft：

```
              Raft Server

        +--------------------+
        | election goroutine |
        | heartbeat goroutine|
        | RPC handlers       |
        | applier goroutine  |
        +--------------------+
                  |
                 RPC
                  |
        +--------------------+
        |    Raft Server     |
        +--------------------+
```

所以两者经常一起出现。

---

### RPC vs Replication

RPC：

> A 怎么叫 B 做事情？

Replication：

> A、B、C 怎样保存同一份 logical state？

RPC 是 mechanism：

```
send RequestVote
send AppendEntries
```

Replication 是更高层 protocol。

---

### RPC vs Consensus

RPC 不能保证：

```
A、B、C 对同一个值达成一致
```

RPC 只能帮助它们交换消息。

比如：

```
A --RequestVote--> B
A --RequestVote--> C
```

Raft 才定义：

```
什么时候投票
谁能成为 leader
什么 entry 能 committed
```

---

### Threads vs Consensus

Mutex：

```
protect threads in ONE process
```

Consensus：

```
coordinate independent processes
across failures
```

你不能：

```
mu.Lock()
```

然后锁住另一台机器。

这是一个非常重要的边界。

---

# Part 3：核心 Mental Model

这节课建议真正掌握 7 个概念。

---

### Concept 1：Concurrency

#### 它解决的问题

程序有多件事情都不能一直等别人：

```
request A waiting disk
request B ready
request C waiting network
```

如果只有一个 execution flow：

```
A wait 100ms
B 只能跟着等
```

#### 一句话定义

> Concurrency 是多个 logical activities 在时间上重叠进行。

不一定同时执行。

单 CPU 也可以：

```
time →

T1: run ---- wait -------- run
T2:      run ---- run
T3:             run ------
```

---

### Concurrency vs Parallelism

这是非常值得区分的：

```
Concurrency
= 多件事情 independently in progress

Parallelism
= 多件事情 physically 同时执行
```

例如单核：

```
T1 --|
     |-- T2 --|
              |-- T1
```

有 concurrency，没有 parallel execution。

多核：

```
CPU0: T1 ---------------->
CPU1: T2 ---------------->
```

两者都有。

Lecture notes 给 threads 的三个主要理由就是：

```
I/O concurrency
multicore performance
programming convenience
```

例如一个请求等磁盘的时候，可以处理另一个请求。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

## Concept 2：Race Condition

考虑：

```
n++
```

不要把它 mental model 成：

```
atomic increment
```

可能实际上类似：

```
load n
add 1
store n
```

如果：

```
n = 10
```

两个 goroutine：

```
time →

T1: load 10
T2:        load 10
T1:                 add -> 11
T2:                        add -> 11
T1:                                 store 11
T2:                                          store 11
```

最终：

```
11
```

而不是：

```
12
```

---

### 更重要的：invariant race

Crawler：

```
if !fetched[url] {
    fetched[url] = true
    Fetch(url)
}
```

两个 goroutine：

```
           T1                    T2

read fetched[x] = false

                        read fetched[x] = false

fetched[x] = true

                        fetched[x] = true

Fetch(x)
                        Fetch(x)
```

结果：

```
x 被 fetch 两次
```

真正需要 atomic 的不是单独：

```
read
```

或：

```
write
```

而是：

```
check + set
```

整个逻辑。

这就是：

```
critical section
```

官方 crawler example 正是在讲这个问题。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

## Concept 3：Mutex

一句话：

> **Mutex 把一个需要保持原子性的临界区变成同一时间只有一个 goroutine 可以执行。**

例如：

```
mu.Lock()

if fetched[url] {
    mu.Unlock()
    return
}

fetched[url] = true

mu.Unlock()
```

于是：

```
T1              T2

Lock
read
write
Unlock
                Lock
                read=true
                Unlock
```

invariant：

```
∀ url:
successful test-and-set(url) <= 1
```

这里：

```
∀
```

就是：

> 对所有 URL。

---

#### 一个特别重要的事实

Mutex 并不知道：

```
它保护 fetched
```

Go runtime 不知道：

```
mu ↔ fetched
```

这是程序员定义的 protocol：

> 所有访问 `fetched` 的地方都必须遵循同一个锁规则。

Lecture notes 也特别指出这一点。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

## Concept 4：Coordination

Mutex 主要解决：

```
谁现在可以访问 state？
```

但另一个问题是：

```
我应该什么时候继续？
```

例如：

```
Worker:
    calculate()
    finished = true

Main:
    ??? wait until finished
```

naive：

```
for !finished {
}
```

这是 busy waiting。

浪费 CPU，而且依然需要 synchronization。

因此你需要：

```
WaitGroup
channel
condition variable
```

---

### WaitGroup

Mental model：

```
outstanding work counter
```

例如：

```
counter = 3

worker A done -> 2
worker B done -> 1
worker C done -> 0

Wait() returns
```

Crawler 用它判断：

> 所有 recursively spawned workers 是否结束。

官方 notes 把它描述为一个 Add/Done 平衡的 counter。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

## Concept 5：Channel

Channel 最重要的 mental model 不是：

> “一个队列”。

而是：

> **通过 communication 来完成 synchronization。**

例如：

```
ch <- urls
```

和：

```
urls := <-ch
```

建立：

```
producer
   |
   | produce data
   |
   v
 send
   |
   | happens-before-like synchronization
   v
receive
   |
   v
consumer
```

Lecture crawler 的 channel version 有一个非常漂亮的性质：

```
worker 不修改 fetched

           urls
worker ----------> coordinator
                     |
                     v
                  fetched
```

只有 coordinator 拥有：

```
fetched
```

所以根本不需要 mutex。

官方 notes 的总结非常好：

```
shared state → locks 往往自然
communication → channels 往往自然
```

并且课程 labs 更常建议用 `Mutex` / `Cond` 管理 replicated protocol state。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

## Concept 6：RPC

RPC：

> **Remote Procedure Call：试图把 request/response network communication 包装成 procedure call abstraction。**

你看到：

```
reply := Put(args)
```

底下：

```
Client                           Server

Application                     handler
    |                              ^
    v                              |
Client stub                    dispatcher
    |                              ^
    v                              |
RPC library                    RPC library
    |                              ^
    +----------- network ----------+
```

具体发生：

```
1 client 建 Args
2 RPC library marshal
3 send request
4 server receive
5 unmarshal
6 dispatcher 找 handler
7 handler execute
8 marshal Reply
9 send response
10 client unmarshal
11 Call returns
```

这是 Lecture notes 描述的完整路径。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

## Concept 7：RPC Failure Ambiguity

这是本 Lecture **最重要的 distributed-systems concept**。

```
timeout
```

只说明：

> 我没有得到 reply。

不说明：

```
server 没执行
```

定义：

```
Outcome uncertainty
```

客户端不能区分：

```
not executed
```

和：

```
executed but response lost
```

这会直接引出：

```
retry semantics
idempotency
request IDs
deduplication
at-most-once
state machine replication
transactions
```

---

# Part 4：System Model / Assumptions

这节课不是一篇定义严密 failure model 的 consensus paper，所以要区分：

> **课程教学模型** 和 **后续正式 distributed protocol model**。

---

### Node Model

主要考虑普通 crash failure：

```
Client crash
Server crash
```

不是 Byzantine：

```
Server 不会恶意伪造 response
```

Crawler 部分甚至完全是单机 process 内的 concurrency。

KV/RPC 部分开始进入：

```
independent processes
```

---

### Network Model

RPC 层可能观察到：

```
reply 不到达
connection failure
server 太慢
server crash
```

官方 lecture 明确把 lost packet、broken network、slow server、crashed server 作为 RPC failure 场景。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

不过这里有一个工程细节：

Go `net/rpc` 通常运行在 TCP 上，所以：

```
packet-level loss
```

往往由 TCP 重传隐藏。

应用真正看到的更接近：

```
connection breaks
timeout
no RPC reply
```

后续 6.824 `labrpc` 会故意把：

```
RPC loss
delay
reordering
```

暴露出来测试协议。官方 Raft lab 就明确允许 tester delay、reorder、discard RPCs。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)

---

### Timing Model

从 distributed algorithm mental model 来看：

> 应用不能依赖一个固定 network latency upper bound。

也就是你看到：

```
timeout
```

不能证明：

```
server dead
```

它可能只是：

```
slow
partitioned
GC pause
scheduler delay
network congestion
```

这就是你已经学过的 Failure Detector 问题：

```
timeout
   ↓
suspicion
≠
proof of crash
```

---

### Storage Model

本 Lecture 的 toy KV：

```
map[string]string
```

主要是 memory state。

所以：

```
server crash
   ↓
state may disappear
```

Persistent replication 并不是这节课解决的问题。

后面的：

```
GFS
Raft
KV Raft
```

才进一步处理它。

---

# Part 5：算法一步一步执行

本课不是 Raft 那种单一协议，但有两个非常重要的 progressively-built design。

---

## A. Crawler

### Version 1：Serial

网页：

```
        A
       / \
      B   C
       \ /
        D
```

做：

```
Fetch(A)
   ↓
Fetch(B)
   ↓
Fetch(D)
   ↓
Fetch(C)
```

用：

```
fetched = {}
```

防止：

```
B -> D
C -> D
```

导致 D fetch 两次。

---

#### 优点

正确性容易。

```
没有 concurrent map access
termination 明确
DFS 返回 = subtree 完成
```

#### 问题

网络 fetch 很慢。

假设：

```
one fetch = 100 ms
1000 pages
```

serial 理想化也至少：

```
100 seconds
```

---

## Version 2：Naive concurrency

于是你想：

```
go Crawl(u)
```

现在：

```
      A
    /   \
   v     v
  B       C
   \     /
     \ /
      D
```

B/C 并行了。

性能提升。

但新 failure：

```
B goroutine ---- check D=false
C goroutine ---- check D=false
```

双方都 fetch D。

---

## Mechanism：Mutex

把：

```
check fetched
+
mark fetched
```

放入同一个 critical section：

```
Lock
 |
 | check
 | mark
 |
Unlock
```

修复：

```
duplicate fetch
```

---

## 新问题：什么时候结束？

你 spawn 了：

```
A
├── B
│   └── D
└── C
```

main goroutine 不能看到：

```
Crawl(A)
```

返回就认为所有工作完成，因为 child goroutine 可能还活着。

于是需要：

```
WaitGroup
```

---

## Version 3：Channel

另外一种思想：

不要：

```
many workers
   ↓
shared fetched map
```

而是：

```
workers
  | \
  |  \
  v   v
  channel
     |
     v
 coordinator
     |
     v
 fetched
```

让 coordinator 独占：

```
fetched
```

于是从根源上消灭 data race。

这正是官方 crawler 的三种解法：Serial、shared-data + Mutex、channel coordinator。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

## B. RPC

### Happy Path

```
Client                           Server

Put(x,10)
 |
 | request
 |------------------------------->
                                  Put handler
                                  x = 10
                                  reply OK
 |<-------------------------------
 |
return OK
```

非常漂亮。

RPC 成功实现：

```
remote call ≈ local call
```

---

### Failure #1：request lost

```
Client                           Server

Put(x,10)
 |
 |------X

timeout
```

状态：

```
x unchanged
```

---

### Failure #2：reply lost

```
Client                           Server

Put(x,10)
 |-------------------------------->
                                   x = 10
                                   reply
                      X<------------

timeout
```

状态：

```
x = 10
```

但 client：

```
不知道
```

---

### Failure #3：retry

Client：

```
timeout
→ retry
```

可能：

```
request #1 ----------> execute
reply #1 ------X

request #2 ----------> execute again
reply #2 <------------
```

于是 operation 被执行两次。

---

# Part 6：时间线理解 RPC ambiguity

考虑银行操作：

```
Transfer(A, B, 100)
```

---

### Execution 1

```
time →

Client             Server

Transfer
  |
  | request -------->
                    A -= 100
                    B += 100
                    send OK
  |       X<--------- reply
  |
timeout
```

客户端看到：

```
timeout
```

如果 retry：

```
Transfer
  |
  |---------------->
                    A -= 100
                    B += 100
```

结果：

```
转了 $200
```

---

### 此时谁知道什么？

Server：

```
知道第一次执行完成
```

Client：

```
不知道第一次是否执行
```

这叫：

> **knowledge asymmetry**

Distributed systems 很多复杂性，本质上就是：

```
不同节点知道的信息不同
```

---

## 更隐蔽的问题：旧 retry 覆盖新 operation

这是 Lecture notes 给的典型问题。

Client：

```
Put(k, 10)
Put(k, 20)
```

你希望：

```
final k = 20
```

但：

```
time →

Client                         Server

Put(k,10) ------ delay ------------------->
          timeout

retry Put(k,10) -------->
                              k = 10
reply <------------------

Put(k,20) -------->
                              k = 20
reply <------------------

original Put(k,10) ----------------------->
                              k = 10
```

最后：

```
k = 10
```

这非常重要。

因为有人会说：

> “Put 是 idempotent，所以 retry 没问题。”

不够。

单独来看：

```
Put(k,10)
Put(k,10)
```

确实 harmless。

但：

```
Put(k,10)
Put(k,20)
late Put(k,10)
```

会违反 client operation ordering。

所以：

> **Idempotency 不等于 ordering correctness。**

官方 Lecture notes 正是用 `Put(k,10)` / `Put(k,20)` 的 delayed resend 说明 best-effort retry 的危险。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

# Part 7：State / Invariants

本 Lecture 不像 Raft 有标准 protocol state，但仍然值得明确 state。

---

## Crawler state

```
fetched[url]
outstandingWorkers
```

Invariant #1：

```
每个 url 只有一个 goroutine 可以把：

fetched[url]:

false → true
```

否则：

```
duplicate fetching
```

---

Invariant #2：

```
所有 fetched map access 遵循相同 synchronization discipline
```

否则：

```
data race
incorrect map state
runtime failure
```

---

Invariant #3：

```
outstanding = 
started workers - completed workers
```

只有：

```
outstanding == 0
```

才能宣布完成。

---

## RPC Server state

KV：

```
map[key]value
mutex
```

handler：

```
Get
Put
```

由于 RPC library 可以并发执行 handlers：

```
Put A
Put B
Get C
```

会 interleave。

所以 state 必须满足：

```
所有 logically atomic state transition
必须受到 synchronization
```

MIT notes 明确提醒：Go RPC 为请求创建 goroutine，因此 `Get()` / `Put()` handler 必须正确同步共享 state。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

## RPC retry state

更成熟的系统通常增加：

```
ClientID
RequestID
lastProcessedRequest
cachedReply
```

例如：

```
Client 7
Request 42
Transfer(A,B,100)
```

Server：

```
(Client7, Request42)
```

第一次：

```
execute
remember reply
```

retry：

```
already seen
→ DON'T execute
→ return old reply
```

这就是：

```
deduplication
```

注意：

Lecture 2 只是引出这个问题。

后续 labs 才真正设计 semantics。

当前 MIT Lab 2 就要求在 network failures 下让 Put 具备 **at-most-once** behavior，并通过 version 机制处理 retry；之后才会把 server replicated 来处理 server crash。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-kvsrv1.html?utm_source=chatgpt.com)

---

# Part 8：Correctness

这节课很适合第一次建立：

```
Safety
vs
Liveness
```

mental model。

---

## Crawler Safety

必须永远不能发生：

```
一个 URL 被真正 fetch 两次
```

或者至少在课程 specification 中：

```
same URL should be fetched only once
```

Mutex / single-owner coordinator 保证它。

---

## Crawler Liveness

最终：

```
如果所有 Fetch 最终返回，
crawler 最终结束。
```

可能破坏 liveness 的东西：

```
forget Unlock()
channel cyclic wait
WaitGroup counter never reaches zero
```

---

## RPC Safety

假设你提供 at-most-once：

```
一个 logical request
不能执行多次
```

这是 safety。

---

## RPC Liveness

希望：

```
只要 server 可达，
request 最终成功。
```

但是：

```
never retry
```

虽然容易提供“不会 duplicated”，却可能牺牲 liveness：

```
server A fail
→ operation 永远失败
```

后面 replication 正是要解决：

```
A 不可用
→ retry B
```

但一旦 retry 另一台 replica：

```
duplicate execution?
```

问题又回来了。

这也是为什么 RPC semantics 和 replication 紧密相连。

MIT notes 在最后正好点到这一点：简单 Go RPC 不重发请求可以形成一种简单的 at-most-once 行为，但这对于 replicated server 太受限，因为 client 希望第一台失败后 retry 另一台。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)

---

## Safety ≠ Liveness

一个简单设计：

```
如果有任何 uncertainty
→ 永远停止
```

非常 safe：

```
不会 double transfer
```

但是：

```
系统不可用
```

因此 distributed protocols 总在处理：

```
Safety:
不要犯错

Liveness:
不要永远停住
```

---

# Part 9：Failure Matrix

|Failure|看到什么|State 可能如何|核心问题|常见机制|
|---|---|---|---|---|
|request 丢失|timeout|未执行|client 不知道原因|retry|
|response 丢失|timeout|已执行|retry 可能重复|Request ID / dedup|
|Server crash before execute|timeout|未执行|无法区分|retry|
|Server crash after execute|timeout|已执行或 partially durable|outcome uncertain|persistent dedup / replication|
|delayed request|timeout/late arrival|旧操作可能覆盖新操作|ordering|version / sequence|
|duplicate RPC|多次到达|side effect 可能重复|at-most-once|dedup|
|network partition|timeout|remote side may still run|cannot prove failure|retry / consensus / fencing|
|concurrent RPC|正常 reply|shared state race|thread safety|mutex / ownership|
|lock leak|卡住|state 未必坏|liveness failure|disciplined locking|
|channel cycle|卡住|state 未必坏|deadlock|dependency design|

注意其中最深的一行：

```
network partition
```

并不意味着：

```
server crash
```

可能：

```
Client A      X      Server
                     |
                     |
                正常运行
```

这就是之后：

```
split brain
leader election
lease
fencing token
consensus
```

会出现的原因。

---

# Part 10：Top 5 Misconceptions

### ❌ 1. RPC 就是远程 function call

API 长得像。

Semantics 完全不一样。

Local:

```
process 活着
→ function call 的执行结果通常明确
```

Remote：

```
no response
→ outcome ambiguous
```

RPC 是：

> **对 message passing 的 abstraction。**

不能忘记底层 network。

---

## ❌ 2. Timeout 表示 Server 没执行

错误。

Timeout 只表示：

```
在某段时间内没有收到 response
```

可能：

```
server dead
network slow
packet lost
reply lost
server overloaded
GC pause
```

---

## ❌ 3. Retry 就能解决 RPC failure

Retry 修复：

```
request lost
```

但制造：

```
duplicate execution
late execution
reordering
```

Distributed systems 的经典模式就是：

```
Mechanism solves failure A
        ↓
introduces failure B
        ↓
need new mechanism
```

---

## ❌ 4. Idempotent 就意味着没有 ordering 问题

刚才：

```
Put(k,10)
Put(k,20)
```

每个 Put 自身都可以重复。

但是 delayed：

```
old Put(k,10)
```

仍然可以覆盖：

```
Put(k,20)
```

所以你还需要：

```
version
sequence number
CAS
logical request ordering
```

---

## ❌ 5. 使用 Mutex 后程序就是正确的

Mutex 只解决：

```
某些 concurrent memory accesses
```

它解决不了：

```
wrong lock scope
deadlock
logical race
network retry
distributed consistency
```

更不能解决：

```
Server A 和 Server B 的一致性
```

因为 Mutex 的 scope 是：

```
one address space
```

---

# Part 11：真实系统里的 RPC

现代 infrastructure 几乎到处都是这种结构。

---

### Kubernetes API

例如：

```
Controller
    |
    | HTTP / RPC-like calls
    v
API Server
    |
    v
etcd
```

Controller：

```
GET object
modify desired state
UPDATE object
```

如果：

```
UPDATE timeout
```

它不能简单说：

```
update didn't happen
```

所以 Kubernetes API 里大量依赖：

```
resourceVersion
optimistic concurrency
idempotent reconciliation
```

这里和 Lecture 2 的思想高度一致。

---

### etcd

etcd client：

```
RPC
 ↓
etcd leader
 ↓
Raft
```

这里：

```
RPC
```

只解决：

```
client ↔ etcd
peer ↔ peer communication
```

Raft 才解决：

```
replicated ordering
commit
leader changes
```

---

### Kafka

Producer：

```
send record
```

如果：

```
ACK lost
```

producer 会面临一样的问题：

```
broker 写进去了吗？
```

所以 Kafka 才有：

```
idempotent producer
producer ID
sequence number
```

这几乎就是 Lecture 2 RPC retry ambiguity 的工业版本。

---

### Database

SQL：

```
COMMIT
```

然后连接断：

```
COMMIT 成功了吗？
```

这叫：

```
ambiguous commit result
```

本质完全一样：

```
request succeeded
reply lost?
```

---

# Part 12：与你熟悉的基础设施系统联系

这部分尤其值得建立 mental model。

---

## Kubernetes Controller

Controller 的模型：

```
Observe
   ↓
Compare
   ↓
Reconcile
   ↓
Observe again
```

为什么 controller 通常要尽量设计为：

```
idempotent
```

？

因为：

```
API call timeout
controller restart
watch event duplicate
resync
```

都意味着同一个 logical operation 可能重新执行。

例如：

```
EnsureLoadBalancerExists()
```

比：

```
CreateAnotherLoadBalancer()
```

更适合 controller。

前者：

```
desired state operation
```

天然容易 retry。

---

## Terraform

Terraform：

```
desired state
     ↓
provider
     ↓
cloud API RPC
```

如果：

```
CreateInstance()
```

cloud provider 实际创建成功，但 response timeout：

```
Terraform
    |
    X timeout
    |
Cloud:
VM 已经存在
```

下一次 Apply 如果简单：

```
CreateInstance()
```

可能重复创建资源。

所以 real provider 经常需要：

```
request token
idempotency key
read-after-create
import/adopt existing resource
```

这就是 Lecture 2 的 retry problem。

---

## Cloud Control Plane

例如：

```
Control Plane
     |
 CreateLB
     |
     v
Cloud API
```

最安全的 API 往往不是：

```
do action exactly once
```

而是：

```
ensure desired state X
```

例如：

```
EnsureRouteExists(routeID)
```

这样 retry 更安全。

这就是：

> **declarative APIs 与 distributed retry 之间一个非常深的关系。**

---

# Part 13：Paper Reading

这节课没有一篇像 Raft/GFS 那样的核心 research paper。

当前课程 preparation 是 Go concurrency tutorial；Lecture 本身配 `crawler.go`、`kv.go`、vote examples。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html)

所以这一课的“论文阅读框架”不适用。

它更像：

```
Distributed Systems Programming Model
```

课程真正希望你形成的是：

```
Thread model
        +
RPC model
        ↓
future labs execution model
```

---

# Part 14：公式

这一 Lecture 几乎没有数学公式。

但有一个非常值得保留的抽象：

```
Outstanding Work

W = S - C
```

其中：

```
W = outstanding workers
S = started tasks
C = completed tasks
```

Crawler termination：

```
W = 0
```

才能结束。

这就是 WaitGroup 的直觉。

---

另一个逻辑 invariant：

```
∀ URL u:

fetchCount(u) ≤ 1
```

意思是：

> 对任意 URL `u`，真正开始 fetch 的次数最多为 1。

Mutex 的 `test-and-set` critical section 正是在维护这个 invariant。

---

# Part 15：与 Labs 的关系

这一 Lecture 几乎就是为 labs 铺路。

---

### goroutine

之后 Raft 中你会写很多：

```
election ticker
heartbeat sender
RPC handlers
Apply loop
```

它们并发执行。

官方 Raft lab 本身就明确让你创建 goroutine 来运行周期性逻辑。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)

---

### mutex

Raft 里：

```
currentTerm
state
votedFor
log
commitIndex
nextIndex
matchIndex
```

都会被多个 goroutines 访问。

所以：

```
rf.mu
```

不是“性能优化”。

它是：

> **协议 correctness 的一部分。**

---

### RPC

Raft：

```
RequestVote RPC
AppendEntries RPC
InstallSnapshot RPC
```

RPC failure：

```
Call() == false
```

不能理解成：

```
peer definitely didn't process it
```

这点直接来自 Lecture 2。

---

### timeout

你后面会碰到：

```
election timeout
```

千万不要形成：

```
timeout = leader dead
```

正确 mental model：

```
timeout =
I haven't heard from leader recently
```

所以：

```
suspect leader
→ start election
```

而不是证明它已死。

---

### race detector

这门课非常建议：

```
go test -race
```

因为 distributed code 的 bug 很多来自：

```
protocol interleaving
+
local data race
```

官方 debugging guidance 也特别强调 distributed systems 有许多 thread 和 machine interleavings。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/guidance.html?utm_source=chatgpt.com)

---

# Part 16：理解检查

你要求一次讲完，所以我不在中途停下来，但建议你自己先脑内回答。

#### Q1

```
Client                 Server

Inc(x)
   |------------------->
                         x++
                         send reply
                 X
timeout
```

Client retry。

如果没有 dedup：

```
x 增加几次？
```

答案：

```
可能 2 次
```

---

#### Q2

如果 operation 是：

```
Set(x, 5)
```

retry 是否一定安全？

对于：

```
Set(x,5)
Set(x,5)
```

通常安全。

但如果：

```
Set(x,5)
Set(x,6)
late Set(x,5)
```

仍可能破坏 ordering。

---

#### Q3

为什么：

```
timeout == failure detector
```

这个说法不严谨？

因为 timeout 只能提供：

```
suspicion
```

不能区别：

```
crash
slow
partition
packet loss
```

---

# Part 17：逐渐增加难度

### Level 1

```
RPC Call returns error
```

你能否得出：

```
server didn't execute
```

不能。

---

### Level 2

```
A: Put(x,1)
B: Get(x)
```

两个 handler 并发。

如果没有 mutex：

```
Get
```

看到什么依赖于具体 interleaving。

---

### Level 3

```
Client → Server A
```

timeout 后 retry：

```
Client → Server B
```

如果 A/B 是 replicas：

> 如何确保两边不会都执行？

这已经把你推向：

```
replication protocol
consensus
request deduplication
```

---

### Level 4

去掉 crawler 的 `Mutex`：

```
if !fetched[u] {
    fetched[u] = true
}
```

构造两个 goroutine 的 interleaving，就能证明 invariant 可被破坏。

这叫：

```
counterexample reasoning
```

也是学 distributed protocols 非常重要的方法。

---

### Level 5

设计 metadata service：

```
CreateProject("foo")
```

client timeout。

应该怎么办？

成熟方案通常考虑：

```
stable resource ID
idempotency key
request ID
conditional write
read-after-timeout
replication
```

而不是：

```
blind retry
```

---

# Part 18：Problem → Solution Chain

这是这节课最值得压缩的一条链。

```
Need to perform many activities
          ↓
Do everything serially
          ↓
Too slow while waiting for I/O
          ↓
Threads / goroutines
          ↓
Shared mutable state
          ↓
Race condition
          ↓
Mutex / synchronization
          ↓
Need workers to coordinate
          ↓
WaitGroup / Condition Variable / Channel
          ↓
Possible cyclic waiting
          ↓
Deadlock awareness
```

另一条：

```
Need machines to communicate
          ↓
Raw network messages are painful
          ↓
RPC abstraction
          ↓
Looks like local procedure call
          ↓
Network / server can fail
          ↓
No reply
          ↓
Was operation executed?
          ↓
Unknown
          ↓
Retry
          ↓
Duplicate / stale operations
          ↓
Idempotency / request ID / version / dedup
          ↓
Server itself may crash
          ↓
Replication
          ↓
Need replicas to agree on operation order
          ↓
Consensus / Raft
```

这后一条，就是接下来半门 6.824 的问题链。

---

# Part 19：知识压缩

### 30 秒版本

如果面试官问：

> Lecture 2 RPC and Threads 主要讲什么？

可以回答：

> 它建立 distributed program 的基本 execution model。Threads/goroutines 用于表达 local concurrency，但会引入 races、coordination 和 deadlock，因此需要 mutex、channels、condition variables 等 synchronization。RPC 则把 network request/response 包装成类似本地函数调用，但这种 abstraction 会因为 crash、delay 和 message loss 被打破：RPC timeout 不能告诉 client remote operation 是否执行，因此 retry 会进一步引入 duplicate 和 ordering 问题。这些问题是后续 replication、at-most-once semantics、Raft 和 distributed transactions 的基础。

---

## 3 分钟版本

这节课有两条线。

第一条是 concurrency。Distributed server 需要同时等待网络、磁盘和多个 clients，所以 serial execution 性能很差。Threads/goroutines 让我们自然表达多个 concurrent activities。但是多个 goroutine 共享 state 后会出现 race，比如 crawler 两个 goroutine 同时看到 URL 未访问然后都 fetch。Mutex 可以把 check-and-set 变成 critical section；WaitGroup、channel 和 condition variable 用来协调不同 goroutine。Channels 还提供另一种设计思路：让 state 只有一个 owner，其他 goroutine 通过 communication 传递数据。

第二条是 RPC。RPC 把 request、serialization、network transmission、dispatch 和 response 封装成类似 function call。但它无法真正提供 local call semantics。最关键的是：如果 client 没收到 reply，它不知道 server 是否执行过 request。盲目 retry 可以处理 request loss，却可能重复执行 operation；延迟的旧 retry 还可能覆盖后来的 operation。因此 real distributed systems 会使用 idempotency、request IDs、sequence/version numbers、deduplication 等机制。

这就是为什么后面需要 replication 和 consensus：RPC 只负责通信，Raft 等协议才负责让 replicas 对 operation ordering 和 commit 达成一致。

---

## 深入版本

```
Problem
    ↓
distributed programs require concurrency + communication

Model
    ↓
many concurrent execution flows
independent processes
unreliable/slow network

Concurrency
    ↓
goroutine/thread

Problem
    ↓
shared mutable state

Mechanism
    ↓
Mutex / Channel / WaitGroup / Cond

Invariant
    ↓
critical state transition must be atomic
work completion must be tracked correctly

RPC
    ↓
request / response abstraction

Problem
    ↓
no response is ambiguous

Failure
    ↓
request lost
reply lost
server crash
delay
partition

Naive Solution
    ↓
retry

New Failure
    ↓
duplicate execution
stale execution
reordering

Mechanisms
    ↓
idempotency
request ID
version
deduplication

Safety
    ↓
don't execute logical request incorrectly/multiple times
preserve state invariants

Liveness
    ↓
don't stop forever merely because one attempt failed

Next Problem
    ↓
server itself can disappear

Next Mechanism
    ↓
Replication

Next Problem
    ↓
replicas need common operation order

Next Mechanism
    ↓
Consensus / Raft
```

---

# Part 20：知识网络

把今天的内容放到你的 Distributed Systems Mental Model 里：

```
                         Distributed Systems
                                  |
               +------------------+------------------+
               |                                     |
        Local Concurrency                     Communication
               |                                     |
       Thread / Goroutine                            RPC
               |                                     |
      +--------+---------+                  +---------+---------+
      |        |         |                  |                   |
    Race    Coordination Deadlock        Timeout            Retry
      |        |                            |                   |
    Mutex   Channel / Cond                  |              Duplicate
      |                                     |                   |
      +-------------------+                 |             Request ID
                          |                 |             Deduplication
                          v                 v
                     Distributed Protocols
                              |
              +---------------+----------------+
              |                                |
         Replication                      Coordination
              |                                |
       Primary/Backup                     ZooKeeper
              |
           Consensus
              |
         Paxos / Raft
              |
     State Machine Replication
              |
         Linearizability
```

---

## 最后：这节课真正应该留下的 6 个句子

如果过一个月你忘了 Go syntax，我希望你还记得：

1. **Distributed system 是很多 concurrent execution flows 通过 network 交互。**
2. **Concurrency 的核心敌人不是“同时运行”，而是 uncontrolled interleaving。**
3. **Mutex 保护的是 invariant，不只是变量。**
4. **RPC 只是把 message passing 包装得像 function call，它并没有获得 local-call semantics。**
5. **Timeout 只能说明“我不知道发生了什么”，不能说明 remote operation 没发生。**
6. **Retry 修复 failure 的同时会制造 duplicate 和 ordering 问题；这正是后续 idempotency、deduplication、Replication 和 Consensus 出现的起点。**

其中我认为最值得你刻进脑子的是这一句：

> **RPC timeout 的真正含义不是 Failure，而是 Uncertainty。**

后面你学 **Primary/Backup、Raft、2PC、Spanner、Kafka producer、Kubernetes Controller、Terraform Provider** 时，这句话会反复出现。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/notes/l-rpc.txt)
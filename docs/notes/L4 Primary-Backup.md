下面一次性把这节 **Primary-Backup Replication / VMware FT** 完整串起来。

先说明课程版本差异：现在的 MIT 6.5840 课程安排已经变化，例如 2026 年 Lecture 4 是 Paxos；你看的这套旧版 6.824 Lecture 4 使用 VMware FT 来讲 Primary-Backup、Replication 和 Fault Tolerance。下面按你正在看的这套课来讲。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

---

## 1. 这节课到底想解决什么问题？

一句话：

> **如何让两台机器表现得像“一台不会因为单机 crash 而丢失已经对外承诺状态的机器”？**

注意，我没有说：

> “怎么复制数据？”

因为复制数据其实不难：

```
Client
   |
   v
Primary P
   |
   +----------> Backup B
```

真正困难的是 **时间**。

假设：

```
x = 0
```

Client：

```
Put(x=1)
```

最 naive 的方案：

```
1. P 修改 x=1
2. P 回复 Client OK
3. P 异步复制给 B
```

然后：

```
time →

Client:   Put(x=1) -------- OK
                            |
Primary:      x=1           X crash
                \
                 X replication 没完成

Backup:       x=0
```

Backup 接管以后：

```
Get(x) -> 0
```

Client 看到了：

```
Put(x=1) -> OK
Get(x)   -> 0
```

这不是简单的“replica 落后”。

而是：

> **系统已经对外宣称某件事发生了，故障之后却把这件事从历史里删除了。**

因此这一课最核心的问题其实是：

> **什么时候一个操作才有资格变成 externally visible？**

这句话后面你学 Raft、Paxos、数据库 commit、WAL、Quorum，都会不断遇到。

---

## 2. 放到整个 6.824 知识地图里

这节课的位置大概是：

```
RPC / Threads
     │
     │ 一台 Server 如何处理请求
     ▼
GFS
     │
     │ 数据可以复制到多台机器
     ▼
Primary-Backup             ← 今天
     │
     │ 故障时怎样保证执行历史不倒退？
     │ 谁可以接管？
     ▼
Leader Election / Split Brain
     │
     ▼
Consensus
     │
     ├── Paxos
     └── Raft
            │
            ▼
   State Machine Replication
            │
            ├── ZooKeeper
            ├── etcd
            ├── replicated DB
            └── distributed metadata service
```

### RPC vs Primary-Backup

RPC 解决：

```
Client 如何调用远程 Server？
消息丢了怎么办？
RPC timeout 怎么办？
retry 怎么办？
```

Primary-Backup 解决：

```
Server 本身消失了怎么办？
另一台机器怎样从正确位置继续？
```

RPC 的 failure ambiguity 会直接进入这节课：

```
Client -> Primary : request

Primary 执行了
Primary 回复了？
网络把回复丢了？

Client 不知道。
```

这也是为什么 retry 和 idempotency 后面非常重要。

---

### Replication vs Primary-Backup

Replication 是大概念：

> 保存多个副本。

Primary-Backup 是一种具体 Replication 架构：

```
        writes
Client -------> Primary
                  |
                  v
                Backup
```

特点：

```
一个 active
一个 passive / shadow
```

通常所有请求先经过 Primary。

---

### Primary-Backup vs Consensus

这是这一课非常重要的边界。

Primary-Backup 假设：

> “我们已经知道谁是 Primary。”

但：

```
P -------X------- B
```

网络断了。

P：

> B 挂了。

B：

> P 挂了。

如果两边都：

```
I am Primary!
```

就产生：

> Split Brain

所以：

> **Replication 并不能自动解决谁有资格成为 Primary。**

Consensus / Raft / Paxos 解决的是：

> 多台机器如何在 crash、message delay、partition 下对某件事情达成唯一决定。

Primary-Backup 通常需要某种额外的：

```
View Server
Quorum
Lease
Witness
Consensus
Shared-storage fencing
```

来解决这个问题。

---

### Primary-Backup vs State Machine Replication

State Machine Replication：

```
State S
   +
ordered commands

Put(x,1)
Put(y,2)
Delete(z)
```

只要：

```
所有 replicas
从相同初始状态开始

+
执行完全相同的命令
+
顺序相同
+
执行是 deterministic
```

那么：

```
S1 == S2 == S3
```

VMware FT 非常漂亮的一点：

> 它把整个 VM 当成一个巨大的 State Machine。

不是复制：

```
SQL INSERT
KV Put
filesystem operation
```

而是复制：

```
CPU execution
interrupt
network input
disk input
timer result
device event
```

VMware FT 论文明确采用这种 state-machine 思路：从相同状态开始，让 Primary 和 Backup 看到相同 input 和 nondeterministic events，从而保持相同执行。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

---

## 3. 本课最重要的 6 个 Mental Models

### 3.1 Primary / Backup

#### 它解决的问题

机器 crash 后，不希望服务从零恢复。

#### 一句话

> 一个 replica 对外服务，另一个 replica 跟踪它的状态，前者失败后后者接管。

```
Client
   |
   v
Primary
   |
   | replication
   v
Backup
```

关键不是：

```
Backup 有“一些”数据
```

而是：

> **Backup 必须至少拥有所有已经被 Primary 对外承诺的历史。**

---

## 3.2 Deterministic State Machine

假设：

```
State0
   |
 input A
   v
State1
   |
 input B
   v
State2
```

如果两个机器：

```
same initial state
+
same inputs
+
same order
+
deterministic execution
```

那么：

```
same final state
```

这是 Replication 最强大的思想之一。

#### 为什么 VM 比普通程序麻烦？

因为机器执行有 nondeterminism。

例如：

```
time.Now()
random()
interrupt arrival
network packet arrival
disk completion
thread scheduling
device input
```

例如：

```
if current_time % 2 == 0:
    x = 1
else:
    x = 2
```

Primary：

```
time = 10
x = 1
```

Backup：

```
time = 11
x = 2
```

即使两边执行相同代码：

```
state diverged
```

因此只复制“network request”还不够。

---

## 3.3 Deterministic Replay

VMware 的核心技术：

> **Primary 记录所有可能造成 nondeterministic execution 的信息，Backup replay 它。**

例如 Primary 收到 timer interrupt：

```
Primary instruction stream:

100
101
102
103
      ← interrupt
104
```

不能只告诉 Backup：

```
“发生过一个 interrupt”
```

必须告诉它类似：

```
在 execution point 103 发生 interrupt
```

于是 Backup：

```
100
101
102
103
      ← replay interrupt
104
```

MIT 以前甚至拿这个点出过考题：仅仅让 network switch 同时复制 packet 给 Backup 不够，因为 Backup 必须在与 Primary 相同的 execution point 处理这个 input，而不是“某个差不多的时间”。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q18-1-sol.pdf?utm_source=chatgpt.com)

所以这里要形成一个重要 mental model：

> Replication 经常不是复制“状态”，而是复制“导致状态变化的事件”。

这就是：

```
state replication

vs

operation / event replication
```

后者通常带宽低很多。

---

## 3.4 Logging Channel

架构现在变成：

```
             input
               |
               v
        +-------------+
        | Primary VM  |
        +-------------+
               |
               | log entries
               |
               v
        +-------------+
        | Backup VM   |
        +-------------+
```

Logging Channel 传：

```
network input
disk-read result
interrupt
timer
nondeterministic instruction result
...
```

Backup：

```
replay
replay
replay
```

因此 Backup 往往：

```
Primary execution
       ↓
Backup execution

有一点 lag
```

这是允许的。

关键不是：

> Backup 必须实时和 Primary 一模一样快。

而是：

> Primary 不允许让 Backup 尚未掌握的 execution history 产生不可撤销的 external output。

于是进入全课最重要的机制。

---

## 3.5 Output Rule

这是这篇 VMware FT paper 最值得记住的一条规则。

论文的规则可以压缩成：

> **Primary 在把一个 output 真正发到 external world 之前，必须确认 Backup 已经收到足够的 log，使 Backup 能够 replay 到产生该 output 的状态。**

论文明确要求 Primary 延迟 external output，直到 Backup ACK 对应的 log entry。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

这是整节课的灵魂。

不是：

```
Primary 每执行一条 instruction
都等 Backup
```

而是：

```
可以继续执行

但

external output
必须等待 Backup
```

区别极大。

---

## 3.6 Split Brain / Fencing

最后一个核心概念：

```
P ------X------ B
```

heartbeat timeout 只能说明：

```
“I haven't heard from the other side.”
```

不能说明：

```
“The other side is dead.”
```

这两个完全不是一回事。

因此：

> timeout 是 suspicion，不是 proof of death。

VMware FT 论文解决的方法是借助 shared storage：

```
Primary ----\
             \
              Shared Storage
             /
Backup  ----/
```

两边在决定“我要 go live”之前执行一个 atomic test-and-set。

只有一个能成功。

成功：

```
I become active.
```

失败：

```
other side already won
I halt myself.
```

论文明确描述了这一 split-brain 防护。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

这个机制本质就是：

> **Fencing**

你以后在：

```
Kubernetes leader election
distributed lock
database failover
storage fencing
STONITH
lease
fencing token
```

都会看到同一个思想。

---

## 4. System Model

现在严格说 assumptions。

### Node Model

论文主要处理：

> fail-stop failure。

即：

```
机器正常运行

or

机器停止
```

而不是：

```
机器偷偷返回错误结果
机器恶意伪造消息
memory 随机篡改
Byzantine behavior
```

论文明确说明它针对 fail-stop failure。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

所以：

```
Crash fault      ✓
Byzantine fault  ✗
```

---

### Crash-stop 还是 crash-recovery？

课堂抽象主要可以按：

```
Primary crashes
Backup takes over
```

来理解。

现实 VMware FT 在 failover 后会：

```
new Primary
     |
     +---- create another Backup
```

重新恢复冗余。论文认为“自动重新建立 redundancy”是完整 FT 系统的重要部分。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

所以工程系统不是简单永远 crash-stop，而是：

```
failed host disappears
service fails over
cluster later recreates redundancy
```

---

## 5. Network Model

必须允许：

```
delay
loss
partition
connection failure
```

系统不能因为：

```
heartbeat timeout
```

就直接断言对方死掉。

否则 partition：

```
      network partition
P ----------X---------- B
```

两边都会成为 Primary。

所以：

```
failure detection

≠

failure proof
```

这是 distributed systems 非常基础的区别。

---

## 6. Timing Model

从理论角度，你不要把它理解成：

```
网络延迟一定 < 100 ms
```

Timeout 是用于：

```
detect / suspect failure
```

而不是安全性的根基。

因此更接近实际工程中的：

> asynchronous / partially synchronous environment。

Safety 不能建立在：

```
“5 秒没回一定死了”
```

这种假设上。

VMware FT 的 safety 最终依赖：

```
shared-storage atomic arbitration
```

而不是 timeout。

---

## 7. Storage Model

论文默认配置大概是：

```
Primary VM
     \
      \
     Shared Disk
      /
     /
Backup VM
```

磁盘内容本身共享。

VM 内部其他执行状态则通过 deterministic replay 保持一致。

论文也讨论了：

```
separate disk
```

方案，其中 Primary 和 Backup 各自执行 disk write，使 disk 成为 replica state 的一部分。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

---

## 8. Happy Path：算法到底怎样执行？

假设 client 发：

```
Put(x=1)
```

为了教学，先把 VM 复杂性压成一个 server。

```
Client
   |
   v
Primary
   |
   v
Backup
```

#### Step 1

Primary 收到 request：

```
Put(x=1)
```

记录 input：

```
log:
[Put(x=1)]
```

并发送给 Backup。

---

#### Step 2

Primary 继续执行：

```
x = 1
```

Backup 收到 log：

```
Put(x=1)
```

也 replay：

```
x = 1
```

---

#### Step 3

Primary 想：

```
send OK to client
```

这是一个：

> external output。

不能立即发。

---

#### Step 4

Backup：

```
ACK(log)
```

此时 Primary 知道：

> 即使我现在瞬间消失，Backup 也已经拥有足够的信息重演到这个 output。

---

#### Step 5

Primary：

```
Client <- OK
```

于是：

```
Client 看到的历史
```

已经安全地“跨过”了 failover boundary。

---

## 9. 用 timeline 看最关键的因果关系

```
time →

Client:
    Put(x=1) ---------------------------------- OK
       |                                        ^
       |                                        |
Primary:
       receive
       execute x=1
       log ------------------->
       wait                    ACK
                                |
Backup:                         |
             receive log ------+
             replay x=1
```

真正重要的 happens-before：

```
Backup receives necessary log
            ↓
Backup ACK
            ↓
Primary receives ACK
            ↓
Primary externalizes output
```

也就是：

```
backup_has_replay_information
        happens-before
external_output
```

这就是 Output Rule 的本质。

---

## 10. Failure #1：Primary 在发送 replication 前 crash

```
Primary:
Put(x=1)
execute
X

Backup:
x=0
```

Backup 不知道 `x=1`。

可以吗？

可以，前提是：

```
Client 没有收到 OK
```

Client 的 observable history 中：

```
Put(x=1)
```

没有成功完成。

Client 之后可能 retry。

所以：

```
内部执行过
≠
外部已经 commit
```

这跟数据库非常像：

```
modify buffer
≠
transaction committed
```

---

## 11. Failure #2：Backup 已收到 log，但 Primary 尚未 output 就 crash

```
Primary:
execute
send log ---------->

Backup:
          receive
          ACK
             \
              \
Primary:       receive ACK
               X crash

Client:
      尚未收到 OK
```

Backup 接管。

它已经能够 replay：

```
x=1
```

于是 service 可以继续。

但是 client 不确定：

```
operation happened?
```

可能 retry。

所以又出现：

> duplicate request。

这个问题 Primary-Backup 自身并没有魔法般消除。

应用层仍需要：

```
request ID
client ID
sequence number
dedup table
idempotency
```

---

## 12. Failure #3：最微妙的情况

现在：

```
Backup 收到 log
↓
ACK
↓
Primary 输出 OK
↓
Primary crash
```

timeline：

```
Client       Primary          Backup

               log ------------>
                                  receive
               <------------- ACK

<------ OK

               X crash

                            take over
```

Backup 知道：

```
“我必须执行这个 operation。”
```

但它是否知道：

```
“Client 已经收到 Primary 的 OK？”
```

不知道。

这叫：

> output ambiguity。

假设 Backup 为确保 output 不丢，再发送：

```
OK
```

Client 可能收到两份：

```
OK
OK
```

这是不是 bug？

对于 TCP packet 来说，duplicate packet 可以由 TCP sequence numbers 去掉。

论文明确指出 failover 时不能保证所有 output exactly once，网络协议（包括 TCP）本来就需要处理 packet loss 和 duplication。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

这其实是一条非常深的分布式系统规律：

> **Crash 发生在 external side effect 附近时，exactly-once 通常非常难，甚至无法仅靠发送方知道。**

例如：

```
PaymentServer -> Bank
```

Bank 执行：

```
charge $100
```

回复丢了。

PaymentServer：

```
到底扣钱了吗？
```

不知道。

解决方法通常是：

```
idempotency key
transaction ID
deduplication
```

而不是“更聪明的 retry”。

---

## 13. Output Rule 最容易被误解的一点

Output Rule 不是：

```
Primary:
execute 1 instruction
wait Backup
execute 1 instruction
wait Backup
```

论文明确强调：

> Primary execution 本身可以继续；需要延迟的是 output。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

可以想象：

```
Primary:

compute A
compute B
compute C
produce network output   ← hold
compute D
compute E
```

所以：

```
execution synchronization

≠

output synchronization
```

这是一项重要性能优化。

---

## 14. 为什么 Backup 不能简单“同时运行同一个 VM”？

因为 nondeterminism。

假设两边：

```
Primary               Backup

thread A              thread A
thread B              thread B
```

Primary 调度：

```
A
A
B
A
```

Backup：

```
A
B
B
A
```

即使 input 相同：

```
shared-memory interleaving
```

也可能不同。

这就是为什么原论文当时生产版本主要支持 single-processor VM；论文指出，多处理器 VM 中 shared-memory access 带来的 nondeterminism 会显著增加 replay 难度。这个是论文发表时代的实现限制，不应理解成今天 VMware 产品仍有相同限制。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

---

## 15. 为什么“把 network packet 同时复制给 Primary 和 Backup”还不够？

看这个：

```
Network
   |
   +------ Primary
   |
   +------ Backup
```

Primary 收到 packet 时：

```
instruction #1000
```

Backup 可能收到时：

```
instruction #1050
```

于是：

```
machine state different
```

最终 execution 可以 divergent。

VMware 需要的不只是：

```
what input
```

还需要：

```
where in execution the input happens
```

这是 deterministic replay 和普通 network replication 最大区别之一。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q18-1-sol.pdf?utm_source=chatgpt.com)

---

## 16. Primary crash 时 Backup 怎么接管？

Backup 平时：

```
replay mode
```

它可能比 Primary 落后一点：

```
Primary:

A B C D E F G

Backup:

A B C D E
```

如果 Primary crash：

```
Primary:
A B C D E F G X

Backup:
A B C D E
```

但 Backup 已经收到了：

```
F G
```

log。

于是：

```
replay F
replay G
```

到：

```
same safe point
```

然后：

```
leave replay mode
become normal live VM
```

论文明确描述 Backup 会先消费掉已经 ACK、但尚未 replay 完的 log entries，然后才进入 live execution。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

---

## 17. Backup crash 又怎样？

假设：

```
Primary alive
Backup crash
```

不能让整个 application 立即停掉。

因此：

```
Primary -> normal execution
```

但状态变成：

```
available

but

not fault tolerant
```

然后 cluster：

```
Primary
   |
   +------ create New Backup
```

重新建立 redundancy。

这里是很重要的工程 distinction：

```
Service Availability
        vs
Replication Redundancy
```

可能出现：

```
Service available = true
Redundancy = false
```

这在现实基础设施非常常见。

比如：

```
EBS degraded
RDS Multi-AZ degraded
Kafka ISR shrink
etcd member down
```

服务可能还能工作，但 fault budget 已经减少。

---

## 18. Network Partition：真正进入 Distributed Systems

这是本课从“复制技术”跳进“分布式系统理论”的地方。

```
Primary P       Backup B
     |              |
     +------X-------+
```

B：

```
haven't received heartbeat
=> maybe P crashed
```

P：

```
haven't received ACK
=> maybe B crashed
```

两边都无法区分：

```
machine crash
```

和：

```
network partition
```

这就是：

> failure detector 的根本问题。

Timeout 只能产生：

```
suspect(P)
```

不能产生数学意义上的：

```
dead(P)
```

---

## 19. 为什么两个 Primary 是灾难？

如果：

```
P                          B
Primary                    Primary

Client A                    Client B

Put(x=1)                   Put(x=2)
```

partition 恢复以后：

```
P: x=1
B: x=2
```

谁对？

没有简单答案。

这叫：

> Split Brain。

特别是如果两边还访问同一块 disk：

```
P -----\
        Shared Disk
B -----/
```

两边同时写：

```
corruption
```

更惨。

---

## 20. VMware FT 怎样解决 Split Brain？

论文方法：

```
Primary --------\
                 \
                  Shared Storage
                 /
Backup ---------/
```

发生 failure suspicion 后：

```
atomic test-and-set
```

假设 P 赢：

```
TAS -> success
```

则：

```
P live
```

B：

```
TAS -> failure

=> P already owns live token

=> B halts itself
```

论文甚至使用了很形象的描述：失败的一边相当于主动停止自己。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

这里的本质不是 VMware。

本质是：

> **所有潜在 Primary 必须竞争一个不可同时获得的 authority。**

这就是 fencing。

---

## 21. 和 Lease / Fencing Token 联系起来

你做 Cloud / Kubernetes 时，可以把它翻译成：

```
Node A:
lease epoch = 10

Node B takeover:
lease epoch = 11
```

Storage：

```
reject write(epoch < current_epoch)
```

那么旧 Primary：

```
write(epoch=10)
```

即使它从 GC pause / network partition 中恢复：

```
REJECT
```

这就是：

> fencing token。

所以：

```
Heartbeat
```

负责：

```
detect something may be wrong
```

而：

```
Fencing
```

负责：

```
prevent stale leader doing damage
```

永远不要混淆这两个作用。

---

## 22. State：这个协议到底有哪些状态？

抽象 Primary-Backup 可以想成：

```
Primary:
    machine_state
    log
    log_position
    acked_position
    output_queue
    role = PRIMARY

Backup:
    machine_state
    received_log
    replay_position
    role = BACKUP
```

VMware FT 更具体的 machine state 包括：

```
CPU registers
memory
virtual devices
disk-related state
execution position
nondeterministic events
```

而 log 主要包含：

```
inputs
interrupt/event positions
nondeterministic results
output-producing events
```

---

## 23. 哪些状态必须 durable？

这里不要把 VMware FT 和 Raft 混在一起。

Raft 里你会特别关心：

```
currentTerm
votedFor
log[]
```

持久化到 disk。

VMware FT 的设计思想不同：

```
Backup 本身就是正在运行的 live copy
```

并使用 shared storage 保存 virtual disk。

核心安全条件不是：

> “所有 log 永远 durable。”

而是：

> **Primary 不得把 Backup 尚未掌握的执行历史 externalize。**

所以 durability 的边界和 Raft 不完全一样。

---

## 24. 最重要的 Invariants

### Invariant 1

```
Primary 对外产生过的所有 output
Backup 都拥有足够的信息 replay 到对应 execution point。
```

如果破坏：

```
Client 收到 x=1

Primary dies

Backup:
x=0
```

历史倒退。

---

### Invariant 2

```
同一时刻最多一个 VM 可以 go live。
```

如果破坏：

```
split brain
```

---

### Invariant 3

```
Backup replay 的 nondeterministic events
必须与 Primary 相同，
并发生在对应 execution point。
```

如果破坏：

```
Primary state != Backup state
```

---

### Invariant 4

Backup 可以落后：

```
Backup_state < Primary_internal_state
```

但不能落后于：

```
Primary externally committed history
```

这个 invariant 特别值得记住：

> **Replica 不需要知道 Primary 的每一个瞬间状态，只需要覆盖所有已经变成 external reality 的状态。**

---

## 25. Safety

Safety 问：

> 什么事情永远不能发生？

这套设计最核心的 Safety：

#### Safety 1

已经向 external world 输出的 execution history：

```
不能因为 Primary crash 而消失。
```

Output Rule 保证它。

---

#### Safety 2

Failover 时不能同时存在两个 active VM。

Shared-storage atomic arbitration 保证它。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

---

#### Safety 3

Backup replay 必须和 Primary execution 一致。

Deterministic replay 保证它。

---

## 26. Liveness

Liveness 问：

> 在合理条件恢复后，系统最终能不能继续工作？

假设：

```
Primary crashes
Backup healthy
shared storage reachable
```

则：

```
Backup replay remaining log
↓
obtain live authority
↓
become Primary
↓
continue service
```

所以具有 failover liveness。

但是：

```
P
 \

  X network

 /
B

shared arbitration inaccessible
```

系统宁可：

```
stop
```

也不能：

```
two primaries
```

这是经典：

```
Safety > Availability
```

选择。

---

## 27. Network Partition 对 Safety / Liveness 的影响

假设：

```
P | X | B
```

#### Safety

仍然必须：

```
at most one Primary
```

不能牺牲。

#### Liveness

其中一边可能无法继续。

所以：

```
partition
    ↓
choose consistency / single-writer
    ↓
some side unavailable
```

这里你已经能看出之后 CAP discussion 的味道了。

---

## 28. Failure Matrix

|Failure|系统行为|Safety|Availability|核心机制|
|---|---|---|---|---|
|Primary crash before Backup receives log|Backup 保持旧状态|✓，因为 output 尚未释放|短暂中断|Output Rule|
|Primary crash after Backup receives log|Backup replay 后接管|✓|✓|Deterministic replay|
|Primary crash after external output|Backup 可继续，但可能产生 duplicate output|✓ state history|✓|Output Rule + network dedup|
|Backup crash|Primary 单独继续|✓|✓|Primary go-live|
|Logging channel packet delay|output 被延迟|✓|性能下降|ACK gating|
|Logging channel failure|触发 failure handling|✓|可能降级|failure detector|
|Network partition|必须只允许一边 live|✓|部分不可用|shared-storage fencing|
|Duplicate external packet|TCP / upper layer dedup|✓ application-dependent|✓|sequence/idempotency|
|Both P+B fail|无运行 replica|无法保证连续服务|✗|超出 assumption|
|Shared storage unavailable|无法 safely arbitrate / use disk|通常保持 safety|✗|fail closed|

---

## 29. Primary-Backup 到底是不是 Linearizable？

不能直接说：

```
Primary-Backup = Linearizability
```

这是错的。

Primary-Backup 是：

> replication mechanism。

Linearizability 是：

> observable consistency specification。

如果原服务：

```
single server
+
每个 request 原子执行
```

本身提供 linearizable behavior，那么 Primary-Backup 可以努力让 failover：

```
不破坏这种 observable history。
```

但如果原 application 本身：

```
non-linearizable
```

VMware FT 不会把它 magically 变 linearizable。

所以：

```
Replication architecture
        ≠
Consistency model
```

这一点非常重要。

---

## 30. Primary-Backup vs Raft

这是你后面最需要区分的。

Primary-Backup：

```
Primary
   |
Backup
```

核心思路：

```
跟踪 Primary
Primary crash → Backup 接管
```

Raft：

```
      Leader
      /    \
Follower  Follower
```

核心问题：

```
多个节点
如何通过 quorum
决定唯一 leader
和唯一 replicated log
```

Raft 内部当然也有：

```
Leader -> Followers
```

所以外形很像 Primary-Backup。

但 Raft 多了最难的部分：

```
leader election
terms
quorum
log reconciliation
commit rule
membership
partition handling
```

所以可以这样理解：

```
Primary-Backup

让你发现：
“复制还不够，我们怎么安全换 Primary？”

                ↓

Raft / Paxos

解决：
“谁有资格成为 Primary，
以及历史到底是什么？”
```

---

## 31. Primary-Backup vs Consensus 的一个非常关键公式

如果只是：

> 我有一个 magical perfect failover coordinator。

要容忍：

```
f crashes
```

理论上保存：

\[ N \ge f+1 \]

份 replica 就能留下一个。

例如：

```
N = 2
f = 1
```

一个 Primary，一个 Backup。

但问题在于：

```
谁知道哪个 replica 该接管？
```

如果这个决定也必须 distributed：

Crash-fault majority consensus 通常需要：

\[ N \ge 2f+1 \]

例如：

```
f = 1
N = 3
```

为什么？

三个节点：

```
A B C
```

majority：

```
2
```

任意两个 majority：

```
{A,B}
{B,C}
```

至少交于一个节点。

这就是 quorum intersection。

而只有两个：

```
A B
```

一旦：

```
A ---X--- B
```

无法判断：

```
A dead?

还是

partition?
```

VMware FT 用 shared storage atomic operation 来提供这个 arbitration authority，所以它不需要自己在两个 VM 间实现完整 Paxos/Raft。

---

## 32. 和 2PC 的区别

Primary-Backup：

> 同一个 logical service 的 replicas 怎样保持 fault tolerance。

2PC：

> 多个不同 transaction participants 怎样共同决定 commit / abort。

例如：

```
Bank A
Bank B
```

transfer：

```
A -$100
B +$100
```

2PC 解决：

```
两边一起 commit
or
一起 abort
```

Primary-Backup 解决：

```
Bank A server crash
其 replica 怎么接管
```

现实中：

```
2PC participants
```

每个 participant 自己内部可能又由：

```
Raft / Paxos / Primary-Backup
```

复制。

---

## 33. 和 Chain Replication 的区别

Primary-Backup：

```
Client -> Primary -> Backup
```

Chain Replication：

```
Client
   |
   v
Head -> R2 -> R3 -> Tail
```

write 沿 chain 流动。

通常：

```
Tail
```

代表已传播到整个 chain 的最新 committed state。

思想上有共同点：

> external visibility 不能跑在 replication safety 前面。

但是 Chain Replication 将：

```
ordering
replication
read/write roles
```

组织成 pipeline，以提高 throughput / consistency。

---

## 34. 和 GFS 的区别

GFS 的主要 Replication：

```
chunk replica
```

关注：

```
large data
storage failure
chunk placement
mutation ordering
```

而 VMware FT：

```
整个 running computation
```

都被复制。

可以粗略想：

```
GFS:
replicate data

VMware FT:
replicate execution
```

---

## 35. 和 MapReduce / Spark 的区别

这特别能帮你建立 fault-tolerance taxonomy。

VMware FT：

```
failure
↓
backup already has execution
↓
continue
```

这是：

> redundancy-based fault tolerance。

Spark：

```
partition lost
↓
use lineage
↓
recompute
```

这是：

> recomputation-based fault tolerance。

所以：

```
Replication
vs
Recomputation
```

是两条完全不同的 fault tolerance 路线。

Replication：

```
higher steady-state cost
fast recovery
```

Recomputation：

```
lower replication cost
recovery may take longer
```

---

## 36. VMware FT Paper：完整阅读框架

### Paper Problem

目标：

> 不修改 guest OS 和 application，就让普通 VM 获得 transparent fault tolerance。

这很厉害。

应用：

```
MySQL
legacy Java app
Windows application
```

都不需要知道：

```
“下面还有 Backup。”
```

论文的核心设计是复制 Primary VM execution 到另一个 physical server 上的 Backup VM。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

---

### Previous Approach

一种简单办法：

```
不断复制所有 memory/state changes
```

问题：

```
memory write bandwidth enormous
```

论文提出更高效的：

```
state machine + deterministic replay
```

即：

```
不复制每一次 state mutation

而复制导致 mutation 的 nondeterministic inputs
```

论文明确把这两种方式作了对比。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

---

## 37. Paper Key Insight

整篇 paper 可以压缩成两个 insight：

#### Insight 1

> VM 是非常好的 deterministic state-machine boundary。

因为 hypervisor 可以观察：

```
network
disk
interrupt
clock
device
CPU execution
```

所以可以捕获 nondeterminism。

#### Insight 2

> 不必让 Backup 每时每刻和 Primary 同步；只需要保证 external output 永远不会超越 Backup 已掌握的 execution history。

也就是：

> Output Rule。

第二条尤其深。

---

## 38. Paper Design

```
                Network
                   |
                   v
             Primary VM
                 |
                 | Logging Channel
                 | inputs
                 | nondeterminism
                 | execution events
                 v
              Backup VM

             \         /
              \       /
              Shared
               Disk
```

只有 Primary 对外产生正常 output。

Backup output：

```
drop
```

直到：

```
takeover
```

论文描述 Backup 始终稍微落后于 Primary，以 virtual lockstep 的方式 replay；正常情况下 Backup 的 external output 会被 hypervisor 丢弃。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf?utm_source=chatgpt.com)

---

## 39. Paper Mechanisms

可以把完整机制压成：

```
Deterministic Replay
        ↓
Logging Channel
        ↓
Backup ACK
        ↓
Output Rule
        ↓
Failure Detection
        ↓
Shared-storage Fencing
        ↓
Failover
        ↓
Create New Backup
```

每一层都修复上一层遗漏的问题。

---

## 40. 为什么 shared disk 本身不是 Single Point of Failure？

严格来说：

> 从系统依赖关系看，它当然是 shared dependency。

但现实共享存储本身通常：

```
RAID
redundant controller
redundant paths
storage array replication
```

论文的抽象假设是：

```
shared storage has its own high availability
```

并且 virtual disks 本来就依赖它。

所以论文指出，如果 shared storage 本身不可访问，VM 本来也很难继续正常工作，因此用它做 split-brain arbitration 不额外增加很多可用性损失。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

---

## 41. Performance Trade-off

FT 最大 latency cost 来自：

```
external output
      ↓
wait until Backup receives log
      ↓
ACK
```

大约相当于：

```
额外 logging-channel RTT
```

所以 Backup 放得越远：

```
latency ↑
```

这就是同步 replication 永恒的 trade-off：

```
failure safety
        vs
write/output latency
```

论文当时测量的几个 workload 中，FT 性能下降低于约 10%，logging bandwidth 大约在 1.5–18 Mbit/s 范围。这个数字是该论文时代、特定 workload 和硬件下的实验结果，而不是现代系统通用常数。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/vm-ft.pdf)

---

## 42. Paper Limitations

这篇设计没有解决所有 failure。

例如：

```
software bug
```

如果 Primary：

```
kernel panic because bug
```

Backup replay：

```
same execution
```

可能：

```
same kernel panic
```

Replication 擅长：

```
independent hardware failure
```

不擅长：

```
correlated deterministic software bug
```

这是一条非常重要的 fault-tolerance 原理：

> Replica diversity 决定你能容忍什么 failure。

如果所有 replicas：

```
same software
same input
same bug
```

那么：

```
replication ≠ protection from software bug
```

---

## 43. What aged well？

今天仍然非常重要的思想：

```
State Machine Replication
Deterministic execution
External visibility boundary
Fencing
Failover
Redundancy restoration
Separate safety from failure detection
```

尤其：

> **不要让 external side effect 领先于可恢复状态。**

这基本是：

```
WAL
Raft commit
Kafka ISR acknowledgements
database commit
replicated storage
```

共同的设计思想。

---

## 44. What changed？

VM-level deterministic replay 并不是今天所有 HA 系统最主流的方式。

大量现代 distributed service 更倾向：

```
application-level replication
+
Raft/Paxos
+
replicated log
```

原因包括：

```
multi-core nondeterminism
scalability
fine-grained replication
cross-region latency
application-aware recovery
```

但 paper teaching value 仍然很高，因为它把：

```
Replication
Failure
Commit
Output
Fencing
```

之间的关系讲得非常干净。

---

## 45. Top 5 Misconceptions

### ❌ 1. Backup 和 Primary 必须每个瞬间 state 一模一样

不需要。

允许：

```
Primary ahead
Backup behind
```

前提：

```
Primary externally visible history
<=
Backup replayable history
```

---

### ❌ 2. Primary 发给 Backup 就算 replicated

不够。

Primary 必须知道：

```
Backup 收到了。
```

所以需要：

```
ACK
```

否则：

```
send
↓
packet lost
↓
Primary thinks safe
↓
crash
```

数据丢失。

---

### ❌ 3. Backup ACK 以后 operation 就一定只执行一次

错误。

Primary：

```
output
crash
```

Backup 无法总是知道 external world 是否已经收到 output。

所以 duplicates 仍可能存在。

---

### ❌ 4. Timeout 能证明 Primary 已死

完全错误。

```
timeout
```

只能说明：

```
I can't currently communicate with it.
```

原因可能是：

```
crash
partition
packet loss
GC pause
CPU stall
switch failure
```

---

### ❌ 5. 两台机器足够解决 HA 的所有问题

两份 state 可以容忍：

```
一个 copy 消失
```

但不能天然解决：

```
谁拥有 authority？
```

于是仍需要：

```
witness
shared storage
lease
quorum
consensus
```

---

## 46. 和 Kubernetes / etcd 联系

你可以把：

```
Kubernetes API state
```

想象成：

```
distributed state machine
```

真正复制它的是：

```
etcd Raft
```

结构：

```
kube-apiserver
      |
      v
etcd leader
   /       \
follower   follower
```

这里已经不是 VMware FT 式：

```
整个 VM deterministic replay
```

而是：

```
replicate logical commands/log entries
```

例如：

```
Put(/pods/p1, ...)
```

但核心 invariant 极其类似：

> 已经对 client 宣布 committed 的 state，leader crash 后不能消失。

现代 MIT Lab 也是沿这条路线：Raft 先维护 identical replicated log，再让 replicated KV state machine 应用 log。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)

---

## 47. Kubernetes Leader Election 和本课的关系

你应该非常熟悉：

```
controller replicas:

controller-1
controller-2
controller-3
```

但是通常：

```
only one leader
```

Lease：

```
coordination.k8s.io/Lease
```

解决：

```
Who should actively reconcile?
```

这和：

```
Primary
Backup
```

很像。

但区别是：

Controller leader election 通常没有在复制整个 process memory。

新 leader：

```
read desired state from API Server
reconstruct local state
resume reconciliation
```

所以：

```
VMware FT
= execution replication

Kubernetes controller
= state externalization + restart/reconciliation
```

这是非常好的对比。

---

## 48. Terraform 的类比

Terraform 更极端。

它基本不是：

```
Primary/Backup running execution
```

而是：

```
desired config
+
state
+
provider APIs
+
reconciliation-ish apply
```

执行进程 crash：

```
terraform apply dies
```

通常不是：

```
Backup process从同一 CPU instruction 接着跑
```

而是：

```
重新 read state
refresh remote resources
重新 plan/apply
```

因此它属于：

> recover by persistent state + idempotent reconciliation

而不是：

> recover by execution replication。

这个区别非常值得记住。

---

## 49. Cloud Control Plane 的联系

假设你设计：

```
Cloud Region Provisioner
```

如果只有一个 controller：

```
CreateVPC
CreateSubnet
CreateCluster
CreateNodePool
```

controller crash 后：

你通常不会复制 controller process memory。

而是持久化：

```
operation state
desired state
resource IDs
workflow step
```

然后另一 instance：

```
resume / reconcile
```

也就是说现代 control plane 常用：

```
durable state machine
```

代替：

```
whole-process replication
```

但背后问题完全一样：

> 哪些 action 已经 externalized？

例如：

```
CreateVPC API 成功
↓
controller crash
↓
response lost
```

新 controller：

```
到底 VPC 创建了吗？
```

这就是我们刚才讲的：

> output ambiguity。

解决：

```
idempotency token
resource lookup
operation ID
reconciliation
```

你实际上每天都在遇到 Primary-Backup 这节课里的同一个 fundamental problem。

---

## 50. Load Balancer Failover

假设：

```
VIP
 |
 v
LB1
 |
LB2
```

只有一个应该 announce VIP。

如果 network partition：

```
LB1: I'm active
LB2: I'm active
```

就有：

```
split brain
```

所以 Keepalived/VRRP、cloud HA mechanisms 等，本质也需要处理：

```
active ownership
failover
fencing
```

和 Primary-Backup 是同一类问题。

---

## 51. Redis 的联系

普通 Redis Primary/Replica：

```
Client
   |
Primary
   |
Replica
```

如果使用 asynchronous replication：

```
SET x 1
↓
Primary replies OK
↓
Primary crashes
↓
replication wasn't finished
↓
Replica promoted
↓
x missing
```

这就是我们本课开头的 counterexample。

所以：

> Async replication 通常没法保证“acknowledged write 绝不因 failover 丢失”这种强语义。

Redis 提供一些 replication acknowledgment 机制，但它不是简单等价于 Raft-style consensus commit。

---

## 52. Kafka 的联系

Kafka partition：

```
Leader
   |
   +---- follower
   +---- follower
```

producer 的：

```
acks
```

配置，本质也在问：

> 什么 replication condition 下，可以告诉 producer “成功”？

你会立刻看到与 Output Rule 的共性：

```
external ACK
```

不能跑得比：

```
recoverable replicated state
```

更前面。

当然 Kafka 的具体 durability、ISR、acks、leader election 语义比这个教学模型复杂。

---

## 53. Database WAL 的联系

数据库：

```
UPDATE x
```

可能先修改：

```
buffer pool
```

但不能因为 memory 改好了就告诉 client：

```
COMMIT
```

通常必须：

```
WAL durable
↓
COMMIT ACK
```

理由：

```
crash 后能 replay
```

所以：

```
WAL durability boundary
```

和：

```
VMware FT Output Rule
```

的 mental model 极其接近：

> **先保证 future recovery 能重现，再允许 external commitment。**

---

## 54. Lab 中的关系

旧版 6.824 曾经有非常直接的 Primary/Backup Lab：

```
View Server
Primary
Backup
```

旧考试甚至明确讨论过这种 Lab：Primary 必须把请求转发给 Backup，否则 Primary 后续 crash、Backup promotion 后 client 可能看到状态倒退。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/archive/6.824-2013/quizzes/q1-13-ans.pdf?utm_source=chatgpt.com)

你会遇到：

```
RPC retry
duplicate RPC
primary/backup role
view
state transfer
failure detection
```

其中一个经典 bug：

```
client operation executed
reply lost
client retry
operation executed again
```

所以通常需要：

```
ClientID
RequestID
lastResult
```

dedup。

MIT 旧考试也专门展示过 dedup state 没有正确复制导致 retry 被执行两次的错误。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q14-1-ans.pdf?utm_source=chatgpt.com)

现代 6.5840 则把核心 fault-tolerant KV Lab 建在 Raft 上，并明确要求 linearizable `Get/Put` 和 at-most-once `Put`。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-kvraft1.html?utm_source=chatgpt.com)

---

## 55. 你实现 Primary-Backup 时最应该 debug 什么？

不要先盯代码。

先画：

```
Client
Primary
Backup
View Server
```

然后给每条 RPC 标：

```
send
receive
execute
ack
reply
crash
```

尤其测试：

```
crash before forward
crash after forward
crash after Backup executes
crash before client reply
lost reply
duplicate request
partition Primary↔Backup
partition Primary↔Coordinator
Backup restart
role changes while RPC in flight
```

然后检查三个 invariant：

```
1. acknowledged client state
   必须存在于 future Primary

2. at most one authoritative Primary

3. retry 不能重复产生 non-idempotent effect
```

如果这三个先想清楚，代码反而不难。

---

## 56. Problem → Solution Chain

现在把整节课压缩成你要求的最重要形式。

```
Problem
单机 crash 会导致 service 消失
        ↓
Naive Solution
再放一台 Backup
        ↓
Failure
Backup state 可能落后
        ↓
Mechanism
把 Primary execution/input 复制给 Backup
        ↓
Failure
程序存在 nondeterminism
        ↓
Mechanism
Deterministic Replay
记录 input + nondeterministic event
        ↓
Failure
Primary 可能在 Backup 跟上之前
就向 Client 输出结果
        ↓
Mechanism
Output Rule
external output 必须等 Backup ACK
        ↓
Failure
Primary crash 时
Backup 不知道 output 是否已经真的送达
        ↓
Mechanism
允许 network duplicate
TCP / application idempotency 处理
        ↓
Failure
timeout 无法区分 crash 和 partition
        ↓
Problem
可能出现两个 Primary
        ↓
Mechanism
Shared-storage atomic test-and-set
/fencing
        ↓
Failure
Primary/Backup 任一个死后只剩一份
        ↓
Mechanism
自动创建新 Backup
        ↓
Final Design
Transparent fault-tolerant VM
```

如果你真正理解了这条链，这节课就已经掌握 80%。

---

## 57. 30 秒版本

如果面试官问：

> Primary-Backup Replication 这节课讲什么？

你可以回答：

> Primary-Backup 通过让 Backup 跟踪 Primary 的执行状态，在 Primary crash 后接管服务。真正困难的是保证已经对外可见的结果在 failover 后不会消失。VMware FT 使用 deterministic replay，把 inputs 和 nondeterministic events 从 Primary 发送给 Backup，并通过 Output Rule 要求 Primary 在 Backup 确认能够 replay 到相应状态前不能产生 external output。同时，因为 timeout 无法区分 crash 和 network partition，它使用 fencing 来避免 split brain。这个模型自然引出了后面的 Consensus、Raft 和 State Machine Replication。

---

## 58. 3 分钟版本

一个 fault-tolerant service 最简单的想法是：

```
Primary + Backup
```

但如果 Primary 在把最新 state 复制给 Backup 之前就回复 client，Primary crash 后 Backup 会恢复到旧状态，因此 client 会看到已经成功的操作消失。

VMware FT 的解决方式不是持续复制所有 memory，而是把整个 VM 看成 deterministic state machine。Primary 把 network inputs、disk input、interrupt 等会影响 execution 的 nondeterministic information 通过 Logging Channel 发送给 Backup，Backup deterministic replay。

但允许 Backup 有少量 lag。真正关键的是 Output Rule：

```
Primary externalizes output
```

之前必须确认：

```
Backup 已经拥有足够 log 来重现该 output。
```

这样 Primary 即使立即 crash，Backup 仍能恢复到与过去 external outputs 一致的状态。

另一个问题是 failure detection。Heartbeat timeout 无法区分：

```
Primary dead
```

与：

```
network partition
```

如果 Backup 错误接管，就可能产生 split brain。因此系统还需要 fencing；VMware FT 使用 shared storage 的 atomic test-and-set，确保只有一个 VM 能 go live。

所以整节课真正建立的是：

```
Replication
+
Commit / external visibility boundary
+
Failure detection
+
Fencing
+
Failover
```

随后 Raft/Paxos 会把“谁是 Primary、什么历史被 commit”变成真正 distributed consensus problem。

---

## 59. 深入版本

可以最终压成：

```
Problem
    │
    ├─ single machine crash
    └─ acknowledged state must survive
    │
    ▼
Model
    │
    ├─ Primary
    ├─ Backup
    ├─ fail-stop
    └─ unreliable / partitionable network
    │
    ▼
Algorithm
    │
    ├─ capture inputs
    ├─ capture nondeterminism
    ├─ send log
    ├─ deterministic replay
    ├─ Backup ACK
    └─ release output
    │
    ▼
Invariants
    │
    ├─ output ⇒ replayable by Backup
    └─ at most one live Primary
    │
    ▼
Safety
    │
    ├─ acknowledged history doesn't disappear
    └─ no split brain
    │
    ▼
Liveness
    │
    └─ surviving replica can take over
    │
    ▼
Failure Handling
    │
    ├─ crash
    ├─ lost messages
    ├─ duplicates
    └─ partition
    │
    ▼
Trade-offs
    │
    ├─ replication latency
    ├─ logging bandwidth
    ├─ correlated bugs
    └─ arbitration dependency
```

---

## 60. 最终知识网络

把今天的知识放进整个 Distributed Systems mental model：

```
                         Distributed Systems
                                |
              +-----------------+------------------+
              |                                    |
        Fault Tolerance                       Consistency
              |                                    |
          Replication                         Linearizability
              |
      +-------+----------------+
      |                        |
Primary-Backup        State Machine Replication
      |                        |
      |                 deterministic commands
      |                        |
      +-----------+------------+
                  |
             Leader / Primary
                  |
             Split Brain
                  |
         Failure Detection
                  |
          timeout ≠ death
                  |
              Fencing
                  |
                  v
              Consensus
            /           \
         Paxos          Raft
                          |
                    Replicated Log
                          |
                  State Machine Replication
                          |
                  +-------+-------+
                  |               |
                etcd          ZooKeeper-like
                  |
            Kubernetes state
```

再向课程后半段扩展：

```
Replication
    |
    +---- Primary-Backup
    |
    +---- Raft / Paxos
    |
    +---- Chain Replication
    |
    +---- Spanner replication
    |
    +---- distributed storage

另一条 Fault Tolerance 路径：

Failure
   |
   +---- Replication -------- VMware FT / Raft
   |
   +---- Re-execution ------- MapReduce / Spark
```

---

## 最后：这节课你真正应该带走的 4 句话

如果只让我要求你记住四句话，我会选：

```
① Replication 最难的不是复制，而是决定什么时候一个状态可以对外承诺。

② Backup 可以落后 Primary，
   但绝不能落后于 Primary 已经 externalized 的历史。

③ Timeout 只能怀疑节点失败，不能证明节点失败；
   因此 failover 必须配合 fencing / consensus 避免 split brain。

④ Fault tolerance 的核心不是“机器不挂”，
   而是 crash 发生在任意 instruction / RPC / output 边界时，
   系统仍然能给 external observer 一个合法的历史。
```

而 **④ 是这整节课最深的一层**。

因为下一步学 Raft 时，你会发现 Raft 本质上是在系统化解决同一个问题：

> **在 Primary 随时可能消失、网络随时可能断开的世界里，什么历史已经不可撤销？**

Primary-Backup 先让你看到这个问题；Raft/Paxos 才开始给出更一般的答案。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/labs/lab-raft1.html?utm_source=chatgpt.com)
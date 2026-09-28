## MIT 6.824 Lecture 17 — COPS / Causal Consistency

先把整节课压缩成一句话：

> **COPS 想解决的是：在 multi-region 系统里不愿意为每次操作支付跨地域 coordination 的代价时，能不能允许数据暂时不一致，但绝不能让用户看到违反“因果关系”的世界？**

COPS 的答案不是“把所有操作排成一个全局顺序”，而是：

```
只强制真正存在因果关系的操作保持顺序；
彼此无关的操作继续并发执行。
```

这就是这节课最值得形成的 Mental Model。

论文是 2011 年 SOSP 的 _Don't Settle for Eventual: Scalable Causal Consistency for Wide-Area Storage with COPS_。COPS 的目标是 geo-replicated KV store，在每个 datacenter 内本地完成操作，通过 WAN 异步复制，并在远端暴露一个 write 之前确认它的 causal dependencies 已经存在。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 1：这节课到底想解决什么问题？

### 1.1 从一个真实场景开始

假设你做一个照片网站，有两个 region：

```
                    WAN

        US ---------------------- EU
        |                         |
   Photo DB                  Photo DB
   Album DB                  Album DB
```

Alice 在 US：

```
1. upload Photo=P
2. update Album，把 P 的引用加入 album
```

应用逻辑显然是：

```
Photo=P
   ↓
Album contains P
```

因为第二个操作只有在第一个完成之后才发生。

把它写成：

```
Put(Photo, P) → Put(Album, "&Photo")
```

这里的 `→` 不是“网络先到”，而是：

> **逻辑上后者依赖前者。**

---

### 1.2 Eventual Consistency 会发生什么？

US 本地都已经完成：

```
US
Photo = P
Album = [&Photo]
```

然后异步复制：

```
Photo update:

US -----------------------------> EU
            500 ms


Album update:

US ---------> EU
      50 ms
```

于是某个瞬间 EU 是：

```
Photo = <不存在>

Album = [&Photo]
```

Bob 在 EU：

```
GET Album
→ "&Photo"

GET Photo
→ NOT FOUND
```

系统最终可能会 convergence，所以从 Eventual Consistency 来说完全可以接受。

但从 application semantics 来看很荒谬：

> 你看到了“结果”，却看不到导致这个结果存在的“原因”。

COPS 论文正是用“先上传照片，再把照片加入 album”的例子说明这个问题。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### 1.3 为什么单机没那么明显？

单机里：

```
put(Photo)
put(Album)
```

通常共享同一个存储系统或者 transaction/order 机制。

执行顺序自然就是：

```
Photo
  ↓
Album
```

不会出现：

```
Album 已更新
Photo 还没更新
```

但一旦变成：

```
US                    EU

Photo --------\
               \ 不同网络路径
Album ----------\
```

不同对象：

- 在不同 shard
- 被不同 server 管理
- replication latency 不同
- message 可能 reorder

于是：

```
local program order

      ≠

remote arrival order
```

这就是问题真正出现的地方。

---

### 1.4 最 naive 的解法 #1：全部同步复制

Alice 写 Photo：

```
US → EU
   ← ACK

才能返回
```

然后写 Album：

```
US → EU
   ← ACK
```

这样当然不会错。

但代价是：

```
write latency >= WAN RTT
```

而且：

```
US -------X------- EU
       partition
```

如果你坚持等 EU：

```
US 也无法完成 write
```

于是你牺牲：

```
low latency
availability during partition
```

COPS 论文把 Availability、low Latency、Partition tolerance、Scalability 概括为 ALPS，并试图在这些目标下提供比纯 eventual 更强的 consistency。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### 1.5 最 naive 的解法 #2：完全异步 Eventual Consistency

```
write local
↓
立即返回

background replicate
```

性能很好。

但现在：

```
cause   effect

Photo → Album
```

复制可能变成：

```
EU receives:

Album
  ↓
Photo
```

于是 application 必须自己处理大量 anomaly。

---

### 1.6 COPS 的核心想法

不要要求：

```
所有 write：

A < B < C < D < E
```

只要求：

```
如果存在 dependency：

A → B

那么任何 replica：

apply(A)
必须发生在
apply(B)
之前
```

而如果：

```
A     B

互不相关
```

那么：

```
DC1: A B

DC2: B A
```

都没关系。

这就是：

> **只支付因果关系所需要的 ordering cost。**

---

# Part 2：它在整个 6.824 知识地图中的位置

可以把前半学期理解为一直在问：

```
我们怎样让所有 replica
看起来像一台机器？
```

而 Lecture 17 开始问：

```
我们真的需要“像一台机器”那么强吗？
```

知识链是：

```
Replication
    ↓
replica 可能不同
    ↓
Consistency Model
    ↓
+-----------------------+
|                       |
强一致路线               弱一致路线
|                       |
Linearizability       Eventual
|                       |
Raft / Paxos             |
SMR                       |
|                         ↓
Spanner             anomaly 太多
                          |
                          ↓
                 Causal Consistency
                          |
                          ↓
                      Causal+
                          |
                          ↓
                        COPS
```

---

### Raft / Paxos vs COPS

#### Raft / Paxos

解决：

> 多个 replica 对一个 ordered log 达成 Consensus。

Mental model：

```
大家必须同意：

index 1 = A
index 2 = B
index 3 = C
```

---

#### COPS

完全不同：

```
A → C

B 与它们无关
```

允许：

```
DC1: A B C
DC2: B A C
DC3: A C B
```

只要：

```
A before C
```

所以：

> **Consensus 在找 total order；Causal Consistency 只保留 required partial order。**

这一区别是整节课的核心。

---

### Linearizability vs Causal Consistency

Linearizability 关心：

```
现实时间
```

如果：

```
write(x=1) 返回

之后

read(x)
```

后面的 read 不能看到旧值。

Causal Consistency 不要求所有 real-time ordering 都被保存。

它只要求：

```
真正形成 causality 的操作
不能被反过来看见。
```

论文把 consistency spectrum 表示为：

````
Linearizability
   >
Sequential
   >
Causal+
   >
Causal
   >
FIFO
   >
Per-key Sequential
   >
Eventual
``` :chatgpt-content-reference{index="3"}


---

## 2PC / Distributed Transactions vs COPS

2PC 解决：

```text
A shard
B shard
C shard

一个 transaction

要么全部 commit
要么全部不 commit
````

核心是：

> Atomic Commit。

COPS 解决：

> 不同独立 write 之间的 causal ordering。

例如：

```
write(Photo)
write(Album)
```

COPS 并没有说：

```
这两个必须一起 commit
```

Alice 完成 Photo 后 crash：

```
Photo exists
Album does not contain photo
```

没关系。

真正不能出现的是：

```
Album references photo
但 Photo 不存在
```

这是很重要的区别：

```
Atomicity:

A & B

要么都有
要么都没有


Causality:

如果 B 出现
A 必须已经出现

但只有 A 完全可以
```

---

### Spanner vs COPS

Spanner 的设计目标更强：

```
Distributed Transactions
+
External Consistency
```

代价是 coordination。

COPS 则明确选择：

```
Local operation
↓
立即返回

WAN replication
↓
asynchronous
```

论文中每个 COPS datacenter 是本地 linearizable KV cluster，而跨 datacenter replication 是 asynchronous。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

所以：

```
Spanner
strong semantics
↑
coordination cost


COPS
weaker semantics
↓
local latency
```

---

### Chain Replication vs COPS

这两个甚至可以同时存在。

COPS 原型本地 KV 建在 FAWN-KV 上，论文讨论了用 Chain Replication 在单个 datacenter 内 mask node failure。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

所以：

```
                 COPS
        causal replication
        between regions
              |
              v
      local KV cluster
              |
      Chain Replication
              |
          node failure
```

一个解决 WAN consistency semantics。

一个解决 local replication/fault tolerance。

---

# Part 3：核心 Mental Model

我建议记住 7 个概念。

---

## Concept 1：Causality

### 它解决的问题

哪些操作“必须有顺序”？

---

### 一句话定义

如果：

> B 的发生建立在 A 已经发生/被观察到的基础上，

那么：

```
A → B
```

A causally precedes B。

COPS 论文定义了三种 potential causality：同一 execution thread 的程序顺序、read-from 关系、以及 transitivity。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### 三条规则

#### 规则 1：Program / Execution Thread Order

```
Client:

put(x=1)
put(y=2)
```

于是：

```
put(x=1) → put(y=2)
```

---

#### 规则 2：Read-from

Client A：

```
put(x=1)
```

Client B：

```
get(x) = 1
put(y=2)
```

于是：

```
put(x=1)
    ↓
get(x)=1
    ↓
put(y=2)
```

因此：

```
put(x=1) → put(y=2)
```

注意：

> causality 可以跨 client 传播。

---

#### 规则 3：Transitivity

如果：

```
A → B
B → C
```

那么：

```
A → C
```

---

## Concept 2：Partial Order

这是理解 Causal Consistency 最重要的一点。

假设：

```
A → B → C

D → E
```

但两条链之间完全没关系。

那么系统不需要构造：

```
A < B < C < D < E
```

可以：

```
DC1:

A B D C E


DC2:

D A E B C
```

都正确。

只需要满足：

```
A < B < C
D < E
```

这就是：

```
Total Order
vs
Partial Order
```

为什么 Causal Consistency 能 scale：

> **它不试图对所有 unrelated operations 做无意义的 coordination。**

---

## Concept 3：Causal Consistency

一句话：

> **如果一个 replica 暴露某个 effect，那么导致这个 effect 的 causal history 必须已经在那里成立。**

例如：

```
Photo=P → Album references P
```

EU 不允许：

```
Album=P
Photo=missing
```

但允许：

```
Photo=P
Album=old
```

为什么？

因为这相当于只看到了较早的世界：

```
Photo 已存在
但 Alice 还没更新 Album
```

完全可能。

这是一个很好用的判断法：

> Causal Consistency 允许“落后”，不允许“穿越因果关系”。

---

## Concept 4：Concurrent Operations

如果：

```
A ↛ B
B ↛ A
```

两者就是 concurrent。

注意这里：

```
concurrent
≠
物理时间同时
```

例如：

```
12:00 A
12:05 B
```

如果 B 完全不知道 A：

```
A ↛ B
```

在 causal model 中仍然可能被视为 unordered。

论文明确指出 causal consistency 不要求 ordering concurrent operations。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

这也是：

```
Lamport happened-before
```

mental model 的实际应用。

---

## Concept 5：Causal+ Consistency

单纯 Causal Consistency 有一个坑。

假设两个 region 同时：

```
US:
put(event_time = 8pm)

EU:
put(event_time = 10pm)
```

彼此没有看到对方：

```
8pm || 10pm
```

它们是 concurrent。

Causal Consistency 并不规定：

```
谁赢
```

所以理论上：

```
US 永远认为 8pm
EU 永远认为 10pm
```

都不违反 causal consistency。

这显然很烦。

于是 Causal+：

```
Causal Consistency
        +
Convergent Conflict Handling
```

论文要求冲突处理最终在所有 replica 上产生相同结果；默认 COPS 使用 Last-Writer-Wins，也可使用 application-specific conflict handler。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

## Concept 6：Dependency

COPS 把：

```
抽象的 causality
```

变成具体 metadata：

```
(key, version)
```

例如：

```
Photo@17
```

然后：

```
Album@22 depends on Photo@17
```

表示：

```
Album@22.deps = {
    Photo >= 17
}
```

这样 remote server 不需要理解：

> “这是一张照片，所以 Album 应该等它。”

它只理解：

```
before exposing Album@22

ensure:
Photo@17 exists
```

这是 COPS 最大的工程 insight：

> **把 causality 显式编码成 data dependency。**

COPS 会在 remote cluster commit incoming version 前检查 dependencies。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

## Concept 7：Client Context

那么问题来了：

> Server 怎么知道 Album 依赖 Photo？

答案：

```
Client Library
```

维护一个：

```
Context
```

例如 Alice：

```
ctx = {}
```

执行：

```
put(Photo=P)
```

得到：

```
Photo@17
```

于是：

```
ctx = { Photo@17 }
```

下一次：

```
put(Album=...)
```

client library 自动产生：

```
Album@22
deps = { Photo@17 }
```

论文中的 client context 正是用于跟踪一个 logical execution thread 已经 read/write 的 causal history。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 4：System Model / Assumptions

这部分非常重要。

COPS 并不是“任意 distributed system 都成立”。

---

### 4.1 拓扑模型

典型：

```
         US Datacenter
     +--------------------+
     | shard A shard B    |
     | shard C shard D    |
     +--------------------+
               |
            async WAN
               |
     +--------------------+
     | shard A shard B    |
     | shard C shard D    |
     +--------------------+
         EU Datacenter
```

每个 datacenter 是一个 logical replica，并在论文模型中拥有完整 key space；数据在 datacenter 内被 shard 到不同机器上。论文实现使用 consistent hashing。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### 4.2 Local Storage Model

单个 datacenter 内：

```
linearizable KV store
```

所以：

```
local operation
```

可以有很强的一致性。

真正被弱化的是：

```
cross-datacenter replication
```

这点很容易误解：

> COPS 不是“系统所有地方都弱一致”。

而是：

````
inside DC       strong
across DC       causal+
``` :chatgpt-content-reference{index="12"}


---

## 4.3 Node Failure Model

论文明确假设：

```text
fail-stop
````

也就是节点：

```
正常工作
或者
停止工作
```

不会：

- malicious
- arbitrary response
- Byzantine

并且论文的 failure discussion 假设 failure 可检测。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### 4.4 Network Model

WAN 可以：

```
delay
partition
暂时无法通信
```

核心设计目的就是：

> partition 时一个 datacenter 仍然可以本地服务。

所以：

```
US  X------- EU

US writes continue
EU writes continue
```

但 replication queue 会累积。

---

### 4.5 Timing Model

COPS 并不是一个像经典 Consensus 论文那样严格围绕 synchronous/asynchronous model 展开的算法。

最合理的 mental model 是：

```
Safety:
不依赖固定 WAN latency bound

Liveness:
最终需要 dependency 到达，
或者 partition heal / failure reconfiguration
```

系统用 timeout/retry 处理 dependency check。论文描述 `dep_check` 不返回时会 timeout 后重新发起。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### 4.6 Byzantine？

不处理。

如果 server 伪造：

```
“Photo@17 已存在”
```

实际上没有：

COPS correctness 会直接被破坏。

---

# Part 5：算法一步一步执行

现在进入真正的 COPS。

---

## Stage 1：最简单的 Happy Path

Alice 位于 US：

```
Client
   |
   | put(Photo=P)
   v
US
```

US local store：

```
Photo@17 = P
```

返回给 client：

```
version=17
```

client context：

```
ctx = {
    Photo@17
}
```

---

然后：

```
put(Album="&Photo")
```

COPS client library 得到：

```
Album@22

depends:
Photo@17
```

US 本地：

```
Photo@17 已经存在
```

所以：

```
commit Album@22
```

论文要求单个 client 同时只保留一个 outstanding put，从而后续 write 能知道前一个 write 分配到的 version 并形成 dependency。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

## Stage 2：异步复制

US 后台分别发送：

```
Photo@17 ----------------> EU

Album@22 ----------------> EU
deps={Photo@17}
```

假设 Album 先到：

```
time →

US         EU

Album@22 -----> receive
                 |
                 | dependency check
                 |
                 v
             Photo@17 ?

             NO
```

EU 不能 commit Album。

---

然后：

```
Photo@17 ----------------> EU
```

现在：

```
dep_check(Photo@17)
→ true
```

于是：

```
commit Album@22
```

所以：

```
EU visibility:

Photo@17
   ↓
Album@22
```

即使：

```
network arrival:

Album
Photo
```

也没关系。

**COPS 把 network arrival order 与 visibility order 解耦了。**

论文的核心规则就是：remote node 只有在 locally satisfying incoming value 的 nearest dependencies 后才 commit 它。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 6：时间线看清楚

```
time ---------------------------------------------------------->

Alice:

put(Photo)
     |
     |<--------- Photo@17
     |
put(Album)
 deps={Photo@17}
     |
     |<--------- Album@22


US storage:

Photo@17 commit
       |
       +-------------- replication ----------\
                                              \
Album@22 commit ------------------------\      \
                                        \      \
                                         v      v

EU storage:

                              recv Album@22
                                   |
                             dep_check(Photo@17)
                                   |
                                  FAIL
                                   |
                                  wait
                                   |
                                   |    recv Photo@17
                                   |           |
                                   |       commit Photo
                                   |           |
                                   +-----------+
                                   |
                              commit Album@22
```

问自己：

> EU 收到 Album 时，它“知道”什么？

它知道：

```
Album@22 exists as an incoming update
```

但它还不能让 client 看见。

这是：

```
received
≠
visible/committed
```

非常重要。

---

# Part 7：为什么 per-key ordering 不够？

回到我们一开始的问题。

假设：

```
Photo:

v1 → v2 → v3


Album:

v1 → v2 → v3
```

每个 key 自己绝不乱序。

还是会有：

```
Photo@17

        ↓ causal

Album@22
```

这是：

```
cross-key dependency
```

Per-key ordering 只知道：

```
Photo@16 < Photo@17

Album@21 < Album@22
```

完全不知道：

```
Photo@17 < Album@22
```

所以：

```
Per-key consistency
```

无法解决：

```
cross-key causality
```

论文正是将 PNUTS 一类 per-key ordering 与 COPS 的 cross-key causal dependencies 区分开来。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 8：Causality 是怎么产生的？

非常值得慢下来。

假设：

```
Client A:
put(x=1)
```

产生：

```
x@10
```

Client B：

```
read x@10
```

这一步非常关键：

```
x@10
  ↓
进入 Client B context
```

然后 B：

```
put(y=2)
```

于是：

```
y@20 depends x@10
```

再有 Client C：

```
read y@20
put(z=3)
```

得到：

```
x@10
  ↓
y@20
  ↓
z@30
```

所以：

```
z@30
```

间接依赖：

```
x@10
```

这就是：

```
causality propagation
```

---

# Part 9：Nearest Dependencies

如果每次都携带整个 ancestor graph：

```
z depends:
x1
x2
x3
...
100000 items
```

metadata 会越来越大。

注意：

```
x → y → z
```

如果 remote 已经确认：

```
y exists
```

那么根据 COPS 的 invariant：

```
y visible
⇒
所有 y dependencies 已经存在
```

因此：

```
check y
```

就足够。

不用再次：

```
check x
```

所以 COPS 可以保留：

```
nearest dependencies
```

例如：

```
A → B → C
    ↘
      D → E
```

E 只需要检查：

```
D
```

因为：

```
D present
⇒
B/A 已满足
```

论文使用 transitivity 将 dependency checks 缩减到 nearest dependencies；basic COPS 只需要这些 nearest dependencies，而 COPS-GT 为 multi-key consistent reads 需要更多 metadata。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 10：Lamport Clock 到底在这里干嘛？

这里非常容易误解。

你已经学过 Lamport Clock，所以一定注意：

> **Lamport Timestamp 不是 COPS 用来“发现 causality”的核心机制。**

Causality 主要来自：

```
client context
+
read/write dependencies
```

Lamport timestamp 负责：

```
给 version 一个 globally comparable order
```

COPS 的 version 大体：

```
(Lamport timestamp, node ID)
```

使 version 唯一并可比较；论文用这个 global ordering 实现默认的 LWW conflict handling。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### 关键区别

Lamport 性质：

```
A → B

⇒

L(A) < L(B)
```

但：

```
L(A) < L(B)
```

不能推出：

```
A → B
```

所以绝不能写成：

```
timestamp 小
⇒
causally before
```

这是经典错误。

---

# Part 11：Concurrent Writes 和 Causal+

现在：

```
US:

put(x = A)


EU:

put(x = B)
```

两者发生时都没看见对方：

```
A || B
```

Causal Consistency：

```
不规定顺序
```

但这是同一个 key：

```
conflict
```

如果 US：

```
A wins
```

EU：

```
B wins
```

可能永远不同。

于是 Causal+ 增加：

```
Convergent Conflict Handling
```

默认 COPS：

```
Last Writer Wins
```

由 version order 决定 winner。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 12：为什么 conflict handler 要满足交换律之类？

论文讨论 handler：

```
h(a,b)
```

希望：

```
DC1 receives:
a,b,c

DC2 receives:
c,a,b
```

最终结果一样。

所以希望：

```
h(a,b) = h(b,a)
```

commutative。

以及：

```
h(a,h(b,c))
=
h(h(a,b),c)
```

associative。

这样 arrival order 不会导致 permanent divergence。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

你可以把它和 CRDT 的 mental model 联系起来：

```
network reorder
       ↓
operation order differs
       ↓
merge must still converge
```

但 COPS 本身不等于 CRDT。

---

# Part 13：COPS 仍然有一个坑 —— Multi-key Reads

这是 COPS-GT 出现的原因。

假设：

```
ACL = public
Album = harmless
```

Alice 修改：

```
1. ACL = friends-only
2. Album = private-photo
```

所以：

```
ACL_private → Album_private
```

一个 reader 想：

```
read ACL
read Album
```

因为两个独立 GET 不是 atomic snapshot：

```
time →

Reader:   GET ACL ----------------------- GET Album

Alice:             update ACL
                           update Album
```

Reader 可能看到：

```
ACL_old = public
Album_new = private
```

这非常危险。

注意：

> 每个 individual value 仍然可以是合法的 Causal+ value。

问题是：

```
两个 read 不是同一个 consistent snapshot
```

论文专门指出，没有一个简单的“先读 ACL 还是先读 album”的固定顺序可以解决这种 TOCTOU 问题。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 14：COPS-GT 的 Get Transaction

COPS-GT 提供：

```
get_trans([ACL, Album])
```

目标：

> 返回一组彼此 causally compatible 的 values。

它不是：

```
general read-write ACID transaction
```

重点是：

```
consistent multi-key reads
```

---

### Round 1

并行读取最新值：

```
GET ACL   ----\
               \
GET Album ------+--> local cluster
               /
GET X --------/
```

假设得到：

```
ACL@10 = public

Album@30 = private
deps:
ACL@20
```

client 一看：

```
Album@30 requires ACL>=20

but we got ACL@10
```

不合法。

---

### Round 2

于是：

```
get_by_version(ACL, 20)
```

而不是简单：

```
get latest ACL
```

为什么必须指定 version？

假设你再取 latest：

```
ACL@100
```

结果它又可能依赖：

```
OtherKey@200
```

你就可能不断追赶一个 moving target。

指定：

```
ACL@20
```

则 dependency closure 在第一轮已经能够推导出来。

COPS-GT 的算法最多进行两轮 local parallel reads；论文解释第二轮读取第一轮 dependency metadata 指定的具体版本，从而避免新读到更晚版本再引入一组新的 dependencies。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 15：一个特别容易误解的事实

> **即使底层每个 key 都是 Linearizable，也不意味着多 key read 自动是 atomic snapshot。**

例如：

```
x linearizable
y linearizable
```

你执行：

```
read(x)

        writer updates x/y

read(y)
```

两个 operation 各自完全 linearizable。

组合：

```
(x_old, y_new)
```

却未必是你想要的 consistent snapshot。

所以：

```
per-object Linearizability

≠

Multi-object Transactional Snapshot
```

COPS 论文也特别指出 get transactions 对 multi-key consistent view 是单独需要的，即使底层 storage 有强一致的单-key operations。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 16：State

现在从代码角度看。

---

### Client State

basic COPS 大致维护：

```
Context {
    (key, version)
}
```

例如：

```
{
    Photo@17,
    User@41,
    Comment@52
}
```

GET：

```
value, version = get(k)

ctx.add(k, version)
```

PUT：

```
deps = ctx

version = put_after(k, value, deps)

ctx = {(k, version)}
```

为什么可以把 context 清掉？

因为新 write：

```
Wnew
```

已经依赖所有旧 context：

```
A ─┐
B ─┼→ Wnew
C ─┘
```

后续只依赖：

```
Wnew
```

transitivity 就隐含了 A/B/C。

basic COPS 的 client library 正是这样利用 nearest dependency 压缩 context。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### COPS-GT Client State

更复杂：

```
<key, version, deps>
```

因为 get transaction 需要检查 dependency graph。

---

### Server State

概念上包括：

```
Lamport clock

key → version/value

replication queue

dependency metadata

old versions     // especially COPS-GT

global checkpoint / GC metadata
```

COPS-GT 需要保存 old versions，因为第二轮：

```
get_by_version(k, specific_version)
```

必须还能找到历史 version。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 17：核心 Invariants

这是整节课最值得背后的东西。

### Invariant #1

```
如果 version V 在一个 replica 可见，

那么 V 的所有 causal dependencies
都已经在该 replica 中满足。
```

即：

```
Visible(B)
⇒
Visible/available(all deps(B))
```

破坏它：

```
Album visible
Photo missing
```

---

### Invariant #2

```
A → B

不能在任何 replica 上看到：

B 已经生效
A 还没生效
```

---

### Invariant #3

Concurrent update 不需要 causal order。

```
A || B
```

可以：

```
DC1 A then B

DC2 B then A
```

但 conflict resolution 必须最终 convergence。

---

### Invariant #4

Causal dependency 是 transitive。

```
A → B
B → C

⇒

A → C
```

这是 nearest-dependency optimization 正确的基础。

---

### Invariant #5

COPS+ 的 progressing property：

一个 replica 一旦返回某个 version，之后不能退回 causally earlier 的状态。

论文称其为 causal+ 的 progressing property。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 18：Correctness

### Safety：永远不能发生什么？

核心：

```
不能让 effect 在 cause 之前可见。
```

形式化直觉：

如果：

```
x → y
```

那么每个 replica：

```
commit(x)
<
commit(y)
```

对于这个 dependency order。

---

### 为什么成立？

远端收到：

```
y(deps={x})
```

不会直接：

```
commit(y)
```

而是：

```
dep_check(x)
```

如果：

```
false
```

则：

```
y stays pending
```

直到：

```
x exists
```

才：

```
commit y
```

因此：

```
y visible
⇒
x exists
```

这就是 safety proof 的核心。

---

### 为什么只检查 nearest dep 就够？

假设：

```
A → B → C
```

C 只检查：

```
B
```

因为 Invariant：

```
B visible
⇒
A 已满足
```

所以：

```
B satisfied
⇒
A satisfied
```

通过 transitivity，C 不需要再次检查 A。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### Conflict Safety / Convergence

Concurrent same-key writes：

```
A || B
```

causal ordering 不处理。

Causal+ 通过 deterministic/convergent handler：

```
resolve(A,B)
```

让所有 replica 最终得到相同结果。

---

## Liveness

本地 operation 不需要等待 WAN：

```
Client
   ↓
Local DC
   ↓
return
```

所以：

```
US X EU
```

US 仍然可以服务。

但：

```
remote visibility
```

可能被 dependency 卡住：

```
B arrives
A delayed indefinitely
```

那么：

```
B
```

也不能 visible。

这就是 causal consistency 很重要的 trade-off：

> **它没有让 coordination 消失，而是把 coordination 从 synchronous client critical path 移到了 asynchronous dependency propagation。**

---

# Part 19：Failure Matrix

|Failure|系统如何处理|Safety|Availability|
|---|---|---|---|
|Client crash|已完成的单个 KV operation 保留；之后不再发请求|保持|其他 client 不影响|
|Local storage node crash|由 underlying fault-tolerant local KV / Chain Replication mask|保持|视 local replication 而定|
|Packet loss|retry replication / dep check|保持|可能延迟|
|Delayed dependency|dependent update 暂不 commit|保持|remote freshness 降低|
|WAN partition|各 DC 继续 local operation|保持 causal ordering|local 可用|
|Partition heal|backlog 继续 replication|恢复 convergence|恢复|
|Entire DC permanent failure|尚未复制出该 DC 的 local writes 可能丢失|已复制数据仍可保持 ordering|其余 DC 可继续|
|Concurrent same-key writes|LWW 或 conflict handler|causal+ convergence|可继续|
|Byzantine node|不在模型内|不保证|不保证|

论文明确指出一个很重要的代价：如果整个 datacenter 在 locally acknowledged write 被复制出去之前永久丢失，这个 write 也可能丢失；如果只是 partition 而 datacenter 未失败，writes 会积压并在连接恢复后传播。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 20：Network Partition 到底发生什么？

假设：

```
US          EU

 |    X      |
 | partition |
```

US：

```
put(A)
put(B depends A)
```

都可以：

```
return success locally
```

EU 继续服务自己的 clients。

所以从 client critical path：

```
no WAN coordination
```

但是：

```
US replication queue:

A
B
C
D
...
```

增长。

而 GC 也可能受到影响，因为系统不能确认某些 dependencies 已经传播到所有 replicas。论文明确讨论了 partition/DC failure 下 replication queue 和 dependency GC 的积压问题。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

所以：

> Partition tolerance ≠ partition 没代价。

只是代价变成：

```
staleness
queue growth
metadata retention
```

而不是：

```
user write blocked
```

---

# Part 21：为什么 COPS 比“global log”更 scalable？

更早的 causal systems 常见：

```
DC1 operations
    ↓
Single Log
A
B
C
D
E
```

然后：

```
DC1 log → DC2
```

很好理解。

但：

```
所有 shard
   ↓
一个 serialization point
```

最终：

```
throughput bottleneck
```

COPS 不这么干。

假设：

```
Shard 1     Shard 2      Shard 3

Users       Photos       Albums
```

dependencies 可以跨 shard：

```
User@5
   ↓
Photo@10
   ↓
Album@22
```

各 shard 自己 scale。

只有真正存在 dependency 时：

```
dep_check
```

论文把“消除 replica-wide single serialization point、显式携带 dependency metadata”作为 COPS scalability 的关键区别。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 22：一个非常重要的类比——DAG

你可以把整个 causal history 看成：

```
           A
          / \
         B   C
         |   |
         D   E
          \ /
           F
```

不是：

```
A → B → C → D → E → F
```

而是 DAG。

所以：

```
Raft mental model:

Ordered Log


COPS mental model:

Dependency DAG
```

这是非常值得记住的对比：

```
Consensus
   ↓
Global/replicated ordered history


Causal Consistency
   ↓
Dependency DAG
```

---

# Part 23：与 Vector Clock / Lamport Clock 的关系

你已经学过这两个，因此现在它们终于落到了实际系统。

### Vector Clock

擅长判断：

```
A → B

或者

A || B
```

因为：

```
VA < VB
```

能够表示 causality。

但 metadata：

```
O(number of participants)
```

可能很重。

---

### Lamport Clock

便宜：

```
scalar
```

保证：

```
A → B
⇒
L(A) < L(B)
```

但不能检测真正 concurrency：

```
L(A)<L(B)
```

不代表 A caused B。

---

### COPS

不是简单：

```
“用 Lamport Clock 实现 causal consistency”
```

而是：

```
Client Context
      ↓
explicit dependencies
      ↓
actual causality

Lamport timestamps
      ↓
versions / deterministic ordering
      ↓
conflict handling
```

这两个角色一定要分开。

---

# Part 24：Causal Consistency vs Session Guarantees

它们很像，但层次不同。

常见 session guarantees：

```
Read Your Writes
Monotonic Reads
Monotonic Writes
Writes Follow Reads
```

这些可以理解成：

> 从单个 session 视角限制 anomaly。

Causal Consistency 则更一般：

```
Client A write
      ↓
Client B reads A
      ↓
Client B writes B
      ↓
Client C reads B
```

causality 可以：

```
cross-client
```

因为 `read-from` 会传播 dependency。

所以它不只是：

```
“一个 client 不要看到自己旧数据”
```

---

# Part 25：外部 causality —— COPS 的硬限制

这是很多人真正开始理解 Causal Consistency 局限的地方。

Alice：

```
COPS:
put(x=1)
```

然后拿起电话：

```
Alice → Bob

“我已经把 x 改了，
你现在去写 y。”
```

Bob：

```
put(y=2)
```

从真实世界：

```
x → y
```

但 COPS 看到了：

```
Alice context: x

Bob context: {}
```

COPS 完全不知道电话发生过。

因此：

```
y
```

不会自动包含：

```
dep(x)
```

这叫：

```
out-of-band causality
```

Causal system 只能捕获：

> **它能够观察到的 causality。**

论文的 formal model 明确假设 execution threads 通过 data store 交互，而不是任意外部通信。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 26：与 Kubernetes / Control Plane 联系

这里可以做一个不错但有限的类比。

假设 controller：

```
Create VPC
   ↓
Create Subnet
   ↓
Create EKS Cluster
```

你实际上拥有一个 DAG：

```
VPC
 ↓
Subnet
 ↓
Cluster
```

如果某个 eventually replicated control plane 暴露：

```
Cluster exists
```

但另一个 region 看不到：

```
Subnet
```

应用可能出现类似 causal anomaly。

所以 COPS 的：

```
dependency metadata
```

很像你在 orchestration system 中明确表示：

```
Cluster dependsOn Subnet
Subnet dependsOn VPC
```

但注意不同点：

COPS 的 dependency 表示：

```
visibility ordering
```

Terraform DAG / Kubernetes owner/dependency 表示的是：

```
resource lifecycle / reconciliation ordering
```

不是同一个 consistency mechanism。

---

# Part 27：和 Terraform 的联系

Terraform：

```
resource "subnet"
depends_on = [vpc]
```

mental model：

```
VPC → Subnet
```

COPS：

```
Photo@17 → Album@22
```

两者共同思想都是：

> 不需要 globally serialize everything，只需要保存真实 dependency DAG。

这也是为什么 DAG 是 distributed systems / schedulers / build systems / orchestration 中如此强大的 abstraction。

---

# Part 28：和 Kafka 的联系

Kafka partition：

```
partition 1:
A1 A2 A3

partition 2:
B1 B2 B3
```

Kafka 很自然保证：

```
within partition ordering
```

但：

```
A2 → B3
```

这种 cross-partition causality：

Kafka 单靠 partition order 并不知道。

这与：

```
per-key sequential consistency
```

不足以实现 Causal Consistency 非常类似。

如果应用需要：

```
event B 必须在 event A 后消费
```

通常必须显式携带：

```
correlation/dependency/version
```

或者把相关 operation 放在同一个 ordering domain。

---

# Part 29：和 Redis Cache 联系

例如：

```
DB update user
        ↓
invalidate cache
```

这里其实存在应用级 causality：

```
DB write
→
cache invalidation
```

如果 invalidation 先后顺序错乱：

```
old fill
new invalidate
...
```

可能造成 stale cache。

但一般 cache consistency 问题比 COPS 更复杂，因为这里涉及：

```
DB truth
cache copies
race
TTL
CDC
```

不能简单说 COPS 就等于 Cache Aside。

共同 mental model 是：

> 哪些 operation 之间存在不可打破的 happens-before relation？

---

# Part 30：和 etcd / ZooKeeper 对比

etcd / ZooKeeper 倾向为 coordination workloads 提供更强 semantics。

例如：

```
Leader Election
Distributed Lock
Configuration
Membership
```

这些场景经常需要：

```
“所有 participant 对某个当前状态有清晰共识”
```

所以通常需要 Consensus / total ordering。

COPS 面向的是另一类 workload：

```
large-scale social/application data
geo replication
high write availability
```

你不愿意为：

```
每张照片
每条评论
每个 like
```

都支付跨 region consensus。

所以：

```
coordination metadata

→ Raft/ZooKeeper/etcd


high-volume causally-related application data

→ causal model may be enough
```

---

# Part 31：Top 5 Misconceptions

### ❌ 1. Causal Consistency = Eventual Consistency

不是。

Eventual：

```
以后最终一样
```

并不自动要求：

```
cause before effect
```

Causal：

```
dependent writes 必须按照 causal order 暴露
```

---

### ❌ 2. Causal Consistency = Linearizability

不是。

Linearizability：

```
尊重 real-time ordering
```

Causal：

```
只尊重 causality
```

所以：

```
write 已经在 US 返回

EU 某个没有 causal dependency 的 client
仍然可能看到旧值
```

这在 Causal Consistency 下可以合法。

---

### ❌ 3. Lamport Timestamp 本身实现了 Causal Consistency

不对。

Lamport clock：

```
A→B ⇒ L(A)<L(B)
```

却不能：

```
L(A)<L(B) ⇒ A→B
```

COPS 依赖 explicit dependency/context。

---

### ❌ 4. 每个 key 顺序正确就够了

不够。

问题恰恰是：

```
Photo key
    ↓
Album key
```

cross-key relation。

---

### ❌ 5. Causal+ 就等于 transaction

不是。

```
Causal:
B visible ⇒ A already happened
```

而 transaction：

```
A & B atomic
```

完全不同。

COPS-GT 的 get transaction 也只是 consistent multi-key reads，不等于 general multi-key read-write ACID transaction。论文明确把 COPS 与提供跨 key write transaction 的系统区分开。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 32：Safety vs Liveness 再压缩一次

### Safety

无论：

```
packet reorder
delay
partition
retry
```

都不能：

```
effect visible
cause missing
```

机制：

```
dependency metadata
+
dep_check before remote commit
```

---

### Liveness

正常情况下：

```
dependency eventually arrives
```

则：

```
dependent write eventually commits remotely
```

如果：

```
dependency 永远不会出现
```

dependent write 也不能安全地 visible。

所以：

```
Safety
优先于
remote freshness/liveness
```

但：

```
local writes
```

仍然保持 available。

---

# Part 33：COPS-GT 两轮为什么一定够？

这是一个很好的 correctness question。

Round 1：

```
x@10
y@30
```

发现：

```
y@30 depends x@20
```

所以 Round 2：

```
get x@20
```

问题：

> x@20 自己不是还可能依赖 z@40 吗？

是的。

但是：

```
y@30 depends x@20
```

而 `y@30` 已经能够在当前 replica visible。

根据 Causal+ invariant：

```
y@30 visible
⇒
x@20 以及 x@20 的 ancestor
都已经存在
```

因此：

```
x@20
```

一定能立即读到。

同时第二轮读的是：

```
specific version
```

不是 `LATEST`，所以不会追到一个新的 causality frontier。

这就是“两轮足够”的核心 reasoning。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

# Part 34：Paper Reading

### Paper Problem

作者想要：

```
Availability
Low latency
Partition tolerance
Scalability
```

同时又认为纯 Eventual Consistency 给 programmer 留下太多 anomaly。

因此问：

> 能不能找到一个明显强于 Eventual，但仍允许 local reads/writes 的 consistency model？ [CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### Previous Approach

两大类。

#### Strong systems

```
Paxos / synchronous replication
```

consistency 强，但 WAN coordination 成本高。

#### Weak systems

```
Dynamo-style asynchronous replication
```

快且 available，但 application 需要处理更多 inconsistent states。

还有较早的 causal systems 常通过 replica-wide serialized log/exchange 实现，限制了单个 logical replica 的 horizontal scalability。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### Key Insight

整篇论文最核心的一句话：

> **不要建立一个 global serialization point；让每个 write 显式携带它真正依赖的 versions，remote site 只需检查这些 dependencies。**

```
Global order
       X

Dependency DAG
       ✓
```

---

### Design

```
                  Client
                     |
               Client Library
                     |
                  Context
                     |
        +------------+------------+
        |                         |
      Shard A                   Shard B
        |                         |
        +------ local cluster ----+
                     |
                async replication
                     |
        +------------+------------+
        |                         |
      Shard A                   Shard B
                 remote DC
```

---

### Mechanisms

核心就这几个：

```
versions
dependencies
client context
put_after
dep_check
Lamport timestamps
convergent conflict handling

COPS-GT:
get_by_version
get_trans
old versions
dependency GC
```

---

### Evaluation

论文 microbenchmark 的单服务器实验中：

```
COPS get_by_version median:
≈ 0.37 ms

COPS put_after (1 dependency):
≈ 0.57 ms

COPS-GT put_after with 130 deps:
≈ 1.03 ms
```

对应 throughput 约为 52 Kops/s reads 和 30 Kops/s COPS puts；这些是论文当年的实验环境数字，不应理解为现代硬件 benchmark。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

扩展实验从 1 到 16 台 server/datacenter；论文报告服务器数量翻倍时 throughput 大致也翻倍，这是其“scalable causality”论点的主要实验证据。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### Limitations

#### 1. Dependency metadata

dependency graph 会长。

因此需要：

```
nearest deps
garbage collection
global checkpoint
```

---

#### 2. Causal delay amplification

如果：

```
A → B → C
```

A replication 非常慢：

```
B waiting
C indirectly waiting
```

causality 会传播 delay。

---

#### 3. External causality

电话、Slack、HTTP 到另一个完全不知道 context 的 system：

```
COPS 捕获不到
```

---

#### 4. Conflict resolution 很难

LWW：

```
简单
```

但可能：

```
丢失业务语义
```

例如购物车并发：

```
+ Apple
+ Banana
```

LWW 可能丢一个。

---

#### 5. COPS-GT ≠ General Transactions

没有给你：

```
multi-key atomic write
Serializable Isolation
full ACID transaction
```

---

### What aged well?

今天仍然非常重要的思想：

```
Causality as partial order
dependency metadata
session context
local-first operations
async geo replication
explicit conflict resolution
```

尤其：

> 用 dependency DAG 代替 unnecessary total ordering。

这个思想远远超出 COPS。

---

# Part 35：公式

COPS 论文通常写 potential causality 为类似：

\[ a \rightsquigarrow b \]

读作：

> a potentially causally precedes b。

---

### 三个规则

#### Execution order

如果同一 thread：

\[ a \text{ before } b \]

那么：

\[ a \rightsquigarrow b \]

---

#### Gets-from

如果：

```
a = put(x=1)

b = get(x)=1
```

则：

\[ a \rightsquigarrow b \]

---

#### Transitivity

\[ a \rightsquigarrow b \]

且：

\[ b \rightsquigarrow c \]

则：

\[ a \rightsquigarrow c \]

论文正是以这三条规则定义 potential causality。[CMU School of Computer Science](https://www.cs.cmu.edu/~dga/papers/cops-sosp2011.pdf)

---

### Concurrent

如果：

\[ a \not\rightsquigarrow b \]

且：

\[ b \not\rightsquigarrow a \]

则：

```
a || b
```

可以认为是 concurrent。

这就是 Partial Order。

---

# Part 36：为什么这节课没有像 Raft 一样的“多数派公式”？

因为 COPS 的核心不是：

```
N replicas
f failures

N >= 2f+1
```

它并没有通过 quorum intersection 来建立 global commit order。

它主要依赖：

```
local fault tolerant store
+
asynchronous remote replication
+
causal dependency ordering
```

因此它和 Raft/Paxos 的数学核心完全不同。

---

# Part 37：Lab 联系

2020 的 Lecture 17 并没有对应一个“实现完整 COPS”的主 Lab。

但你做 6.824 Lab 时已经学到很多组成它的 building blocks：

```
RPC
retry
timeout
replication
version
sharding
client state
failure handling
```

如果让你做一个简化版 COPS Lab，我会让你实现：

```
type Version struct {
    Clock uint64
    NodeID uint64
}

type Dependency struct {
    Key string
    Version Version
}

type Value struct {
    Data []byte
    Version Version
    Deps []Dependency
}
```

然后核心思路是：

```
receive replicated value

for dep in value.deps:
    wait/check local dep >= required version

commit(value)
```

真正难 debug 的不会是 KV，而是：

```
out-of-order replication
dependency never arrives
duplicate RPC
client context propagation
concurrent write
GC too early
```

---

# Part 38：工程师真正应该带走什么？

这节课真正不是要你以后去部署 COPS。

真正要训练的是：

> **看到“我们需要 consistency”时，不要条件反射地上 Consensus。**

先问：

```
到底哪些操作必须有 order？
```

假设：

```
A B C D E
```

只有：

```
A → C
B → D → E
```

那么你真正需要保护的是：

```
A < C

B < D < E
```

而不是：

```
A<B<C<D<E
```

这就是：

```
minimal coordination
```

的思想。

---

# Part 39：Problem → Solution Chain

整节课最重要的知识链：

```
Geo-replication
      ↓
想要 local low-latency operation
      ↓
采用 async replication
      ↓
不同 key replication reorder
      ↓
Eventual Consistency 出现 causal anomaly
      ↓
“那就同步所有 DC？”
      ↓
WAN latency + partition blocks availability
      ↓
只同步 required order
      ↓
定义 Causality
      ↓
Causal Consistency
      ↓
如何知道 dependencies？
      ↓
Client Context
      ↓
如何让 remote replica enforce？
      ↓
Explicit Dependency Metadata
      ↓
dep_check before visibility
      ↓
dependency graph 太大
      ↓
Nearest Dependencies + GC
      ↓
Concurrent same-key writes 无 order
      ↓
Convergent Conflict Handling
      ↓
Causal+
      ↓
单独 GET 多个 key 仍可能 snapshot 不一致
      ↓
COPS-GT get_trans
      ↓
Two-round consistent multi-key read
```

这基本就是整节课。

---

# Part 40：30 秒版本

如果面试官问：

> COPS / Causal Consistency 在解决什么？

可以回答：

> COPS 是一个 geo-replicated key-value store，目标是在不让 client write/read 等待跨数据中心 coordination 的情况下，比 eventual consistency 提供更强的语义。核心是 Causal+ Consistency：对于 causally related 的 writes，例如先创建 photo 再创建指向 photo 的 album entry，任何 replica 都必须先让 photo 可见再让 album entry 可见；但 unrelated concurrent operations 不需要 global ordering。COPS 通过 client context 捕获 causality，把 dependencies 随 write 传播，并在 remote datacenter commit write 前检查 dependencies。这样用 partial order 替代 global total order，换取 low latency 和 scalability。

---

# Part 41：3 分钟版本

完整一点：

```
COPS 的背景是 multi-datacenter storage。

如果像 Spanner 那样追求很强的 consistency，
write 往往需要跨 region coordination，
会增加 latency，并影响 partition 时的 availability。

如果完全使用 eventual consistency，
不同 key 的 async replication 可以 reorder。
例如先写 Photo，再写引用 Photo 的 Album，
EU 可能先看到 Album，却看不到 Photo。

Causal Consistency 的思想是：
不要求所有 operation globally ordered，
只要求 causally related 的 operation 保持 order。

COPS 用 client context 记录客户端已经 read/write 的 versions，
因此新的 write 可以携带 dependencies。

写入 local DC 后立即成功；
然后异步 replication。

remote DC 收到一个 update 时，
不会立即 expose，
而是先用 dep_check 确认所有 nearest dependencies 已经存在。
所以即使 network reorder，也不会让 effect 比 cause 先 visible。

普通 causal consistency 对 concurrent same-key writes 没规定，
所以 COPS 加 convergent conflict handling，
形成 Causal+ Consistency，默认用 LWW。

此外，单独读取多个 key 仍可能得到不一致 snapshot，
因此 COPS-GT 提供 get transaction：
第一轮并行获取 values，
根据 dependency metadata 检查版本，
必要时第二轮读取明确要求的 version。

本质上 COPS 的 insight 是：
不要使用一个 global total order；
只维护真实 causal dependency DAG。
```

---

# Part 42：深入版本

```
Problem
    ↓
Strong geo consistency costs WAN coordination
while eventual replication exposes anomalies

Model
    ↓
Multi-DC
local linearizable sharded KV
async cross-DC replication
fail-stop failures
partitionable WAN

Abstraction
    ↓
Causal dependency DAG

Algorithm
    ↓
client context
→ dependencies
→ local put
→ async replication
→ remote dep_check
→ expose only after deps satisfied

Conflict
    ↓
concurrent writes unordered
→ convergent conflict resolution
→ Causal+

Multi-key Read
    ↓
independent gets may mix versions
→ COPS-GT
→ 1st-round parallel reads
→ dependency analysis
→ optional 2nd-round explicit-version reads

Invariant
    ↓
Visible(B)
⇒
all Dependencies(B) already satisfied

Safety
    ↓
effect cannot be visible before cause

Liveness
    ↓
local ops avoid WAN
remote propagation waits for dependencies

Trade-offs
    ↓
stale reads allowed
dependency metadata
causal blocking
conflict resolution complexity
external causality not automatically captured
no general write transactions
```

---

# Part 43：最终知识网络

```
                         Consistency Models
                                |
             +------------------+------------------+
             |                                     |
       Strong Consistency                    Weak Consistency
             |                                     |
       Linearizability                        Eventual
             |                                     |
      Consensus / SMR                              |
       /          \                                |
    Raft         Paxos                             |
             |                                     |
             |                              Causal Consistency
             |                                     |
             |                        +------------+------------+
             |                        |                         |
             |                   Causality                 Concurrent Ops
             |                        |                         |
             |                 Happens-Before                 conflict
             |                        |                         |
             |                 Dependency DAG        Convergent Handling
             |                        |                         |
             |                        +------------+------------+
             |                                     |
             |                                  Causal+
             |                                     |
             |                                    COPS
             |                                     |
             |                    +----------------+---------------+
             |                    |                                |
             |              Client Context                    dep_check
             |                    |                                |
             |               Dependencies                  async replication
             |                                                     |
             |                                                  COPS-GT
             |                                                     |
             |                                               get_trans
             |
          Spanner
             |
 Distributed Transactions
 External Consistency
```

---

# 最后，把这一课真正压缩成一个 Mental Model

如果以后忘掉所有 API、`put_after`、`dep_check`，只记住下面这张图：

```
Total Order 思维：

A → B → C → D → E → F

所有事情都排队
         ↓
强语义
         ↓
大量 coordination



Causal 思维：

A ─────→ C
 \
  └────→ D

B ─────→ E

F

只约束真正存在 dependency 的边
         ↓
其他事情自由并发
         ↓
更少 coordination
```

**Raft 教你的一个重要问题是：**

> “如何让所有 replica 对顺序达成一致？”

而 **COPS 教你的下一层问题是：**

> **“等等，我们真的需要让所有东西都有顺序吗？”**

这其实是 Lecture 17 在整个 6.824 里面非常重要的一次思维转折。

你接下来最值得做的不是继续背 COPS API，而是训练下面这个判断：**给定一段 multi-region execution，自己画 causal DAG，然后判断某个 replica 当前看到的状态是否合法。**

# 5 道递进练习

**Level 1**

```
Alice:
put(x=1)
put(y=2)

EU:
y=2
x=0
```

是否合法？

**Level 2**

```
Alice:
put(x=1)

Bob:
get(x)=1
put(y=2)

EU:
y=2
x=0
```

是否合法？为什么 Bob 会把 Alice 的 causality “传染”给 y？

**Level 3**

```
Alice: put(x=1)
Bob:   put(y=2)
```

两人之间没有 read、通信或共享 context。

```
DC1: x then y
DC2: y then x
```

是否合法？

**Level 4**

```
A → B → C
```

COPS 让 C 只检查 B，而不检查 A。

请证明：

```
B exists
⇒
A exists
```

依赖了哪个 invariant？

**Level 5**

设计一个 multi-region feature-flag 系统：

```
CreateFlag
    ↓
CreateRule
    ↓
EnableRule
```

要求 region partition 时仍能本地工作。你会选择：

```
Linearizability
Causal Consistency
Eventual Consistency
```

中的哪种语义，以及哪些 dependency 必须显式追踪？

这 5 题如果都能自己推出来，你对 COPS 的理解就已经从“知道定义”进入真正的 distributed-systems reasoning 了。[DOI](https://doi.org/10.1145/2043556.2043593?utm_source=chatgpt.com)
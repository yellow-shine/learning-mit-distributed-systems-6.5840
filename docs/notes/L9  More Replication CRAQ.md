可以。下面把 **MIT 6.824 Lecture 9 — More Replication / CRAQ** 一次性串起来。

先给你整节课最重要的一句话：

> **CRAQ 要解决的不是“怎么复制数据”，而是：当数据已经复制到很多 replica 后，怎样让所有 replica 都参与读扩展，同时仍然知道“哪个版本现在可以安全地返回给客户端”。**

这句话其实也是理解 CRAQ、follower read、lease read、safe timestamp、closed timestamp 等一大类现代系统设计的钥匙。

---

# Part 1：这节课到底想解决什么问题？

### 只允许记住一个问题

> **如何同时获得 Strong Consistency 和 scalable reads？**

假设我们已经知道如何做 Replication：

```
             write
               |
               v
          +---------+
          | Node A  |
          +---------+
               |
               v
          +---------+
          | Node B  |
          +---------+
               |
               v
          +---------+
          | Node C  |
          +---------+
```

复制三个副本有两个直觉上的收益：

```
可靠性
Node A 挂了还有 B/C

扩展性
既然 A/B/C 都有数据，
理论上应该都能处理 read
```

第二件事却没有想象中简单。

因为一次 write 不可能瞬间同时出现在三个节点：

```
time →

A:   x=1 ---- x=2 ----------------------
B:   x=1 ----------- x=2 ---------------
C:   x=1 -------------------- x=2 -------
```

在传播期间，会暂时出现：

```
A = 2
B = 2
C = 1
```

此时如果：

```
Client1 -> B -> 2
Client2 -> C -> 1
```

你已经不能简单地说：

> “replica 有数据，所以 replica 可以直接读。”

问题变成：

> **一个 replica 如何判断自己的 local value 是否已经安全到可以对外暴露？**

传统 Chain Replication 用一个非常简单的答案解决：

```
只让 tail 读。
```

这样：

```
Head -> Replica -> Replica -> Tail
                               ^
                               |
                           all reads
```

只要 write 到达 tail 才算 commit，那么 tail 永远知道什么已经 commit。

非常漂亮。

但新的问题出现了：

> 我明明买了 7 台机器，为什么读吞吐还是只有 1 台机器？

这正是 CRAQ 被发明出来的原因。CRAQ 的目标是让任意 chain node 都可以处理 read，同时保留 Chain Replication 的 strong consistency。论文称这种设计为 **Chain Replication with Apportioned Queries**。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

# Part 2：放进 6.824 整个知识地图

这节课的位置大概是：

```
RPC / Concurrency
       ↓
机器会失败、消息会延迟
       ↓
Replication
       ↓
Primary / Backup
       ↓
如何让多个副本保持一致？
       ↓
Consensus / Raft
       ↓
State Machine Replication
       ↓
ZooKeeper
       ↓
────────────────────────────
现在我们进一步问：

Replication 已经正确了
       ↓
能不能把 replica 都利用起来？
       ↓
Chain Replication
       ↓
CRAQ
       ↓
Strong Consistency
+
Read Scalability
────────────────────────────
       ↓
后面：
Transactions
Spanner
Distributed DB
Follower Reads
Geo Replication
```

这里首先要把几个东西彻底分开。

### Replication vs Consensus

Replication：

> “我要保存多份数据。”

例如：

```
x=10

A: x=10
B: x=10
C: x=10
```

Consensus：

> “多个节点对一个决定达成一致。”

比如：

```
log index 100

大家到底决定：

PUT x=10

还是：

PUT x=20
```

Raft 的核心属于第二类。

---

### Consensus vs State Machine Replication

Consensus 通常解决：

```
这个 slot 应该放什么 command？
```

State Machine Replication：

```
Consensus
    ↓
得到完全相同的 ordered log
    ↓
每台机器 deterministic apply
    ↓
得到相同 state
```

即：

```
log:

1 PUT x=1
2 PUT y=2
3 DELETE z
```

所有 replica 按相同顺序 apply。

---

### Chain Replication 又是什么？

它也做 replication，但选择了非常特殊的数据流结构：

```
Head
 |
 v
A -> B -> C -> Tail
```

Write：

```
Head → ... → Tail
```

Read：

```
Tail
```

核心思想不是“大家 vote”。

而是：

> **利用固定的数据流方向，让谁知道什么变得特别容易推理。**

这是 CRAQ 的理论基础。

---

### ZooKeeper 和 CRAQ 的边界

ZooKeeper 在 CRAQ 论文里并不是用来保存 object data。

它负责：

```
谁在线？
chain 成员是谁？
谁是 predecessor？
谁是 successor？
chain 配置是什么？
```

即：

```
Data Plane
CRAQ chain

Control Plane
ZooKeeper
```

CRAQ prototype 使用 ZooKeeper 的 ephemeral nodes 和 watches 管理 membership 和 chain metadata。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

这个结构跟你熟悉的 infrastructure 世界很像：

```
Control Plane
    |
    | membership/config
    v
Data Plane
```

---

# Part 3：先理解 Chain Replication

在 CRAQ 之前必须先真正理解 Chain Replication。

假设：

```
          A           B           C
        Head                    Tail
          |           |           |
          +---------->+---------->+
```

初始：

```
A: x=1
B: x=1
C: x=1
```

Client：

```
Put(x=2)
```

### Happy Path

#### Step 1

Head A 接收到：

```
Put(x=2)
```

变成：

```
A: x=2
B: x=1
C: x=1
```

然后 forward：

```
A ---- x=2 ----> B
```

#### Step 2

B apply：

```
A: x=2
B: x=2
C: x=1
```

继续：

```
B ---- x=2 ----> C
```

#### Step 3

Tail C 收到：

```
A: x=2
B: x=2
C: x=2
```

现在：

```
x=2 committed
```

因为 write 已经经过所有 replicas。

Chain Replication 把 tail 到达点当成 update commitment 的关键位置。论文中的基本 CR 模型正是 head 处理 writes、tail 处理 reads，write 到达 tail 后才 committed。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

## 为什么 read 只能去 Tail？

因为传播过程中可能：

```
A = 2
B = 2
C = 1
```

但：

```
x=2 尚未 committed
```

如果你读 A：

```
Read(x) -> 2
```

然后 A/B crash，write 没到 C。

系统恢复成：

```
x=1
```

于是客户端观察：

```
2
↓
1
```

从已经“看到”的未来回到了过去。

所以 CR 采用：

```
所有 read -> tail
```

Tail 是非常特殊的位置：

> 如果 tail 看到了 V2，那么 V2 必然已经经过 chain 中所有前驱。

论文明确指出，这使 tail 可以自然地给读写操作建立一个 ordering，但代价是 read throughput 被限制为单节点。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

## Chain Replication 的第一个 Mental Model

你可以记：

```
Write ordering
      ↓
由 chain 的方向产生

Commit knowledge
      ↓
集中在 Tail

Read consistency
      ↓
通过 Tail 获得
```

这其实已经埋下 CRAQ 的问题：

```
数据：
A B C 都有

但是 commit knowledge：
主要在 C
```

---

# Part 4：为什么 naive “所有 replica 都读”不行？

现在我们希望：

```
Client -> A
Client -> B
Client -> C
```

达到：

```
3 replicas
≈
3 × read capacity
```

但考虑：

```
                Put(x=2)

time →

A: x=1 --- x=2 -------------------------
B: x=1 -------- x=2 --------------------
C: x=1 ----------------- x=2 ------------
```

在：

```
A=2
B=2
C=1
```

时：

```
Read(B) = 2
Read(C) = 1
```

错误不是“副本暂时不一样”。

**replica 暂时不一样本身完全正常。**

真正的问题是：

> 我们不知道 B 上的 `2` 到底已经 committed，还是仅仅是正在传播的 tentative value。

于是 CRAQ 的核心 insight 出现：

> **不要要求所有 replica 永远只有一个值。让它们保留多个版本，并明确区分 committed 与 uncommitted version。**

---

# Part 5：CRAQ 的三个核心机制

真正需要记住的概念实际上只有三个：

```
1. Version
2. Clean / Dirty
3. Tail Version Query
```

整个 CRAQ 都可以从这三件事推导出来。

论文规定每个 object version 带 monotonically increasing version number，并标记为 clean 或 dirty；非 tail 收到新版本时把它标成 dirty，tail 收到后将其变成 clean，并向前面的节点传播 acknowledgement。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

## Concept 1：Multiple Versions

不要：

```
x = V2
```

而是：

```
x:
  V1
  V2
  V3
```

例如：

```
Node B

x:
  version 1 = "alice"
  version 2 = "bob"
  version 3 = "charlie"
```

为什么？

因为有可能：

```
V1 = committed

V2 = 正在传播
V3 = 正在传播
```

如果只保留最新：

```
V3
```

你就失去了一个非常重要的能力：

> “如果 tail 告诉我当前 committed 是 V2，我还能不能返回 V2？”

因此 CRAQ 保留 outstanding versions。

---

## Concept 2：Clean vs Dirty

这是 CRAQ 的灵魂。

### Clean

直觉：

> **这个版本已经确定 committed。**

例如：

```
x:
V1 clean
```

### Dirty

直觉：

> **这个 replica 已经知道这个版本，但还不能确定它是不是当前可以对外返回的 committed version。**

例如：

```
x:

V1 clean
V2 dirty
```

注意：

```
dirty ≠ 错误
dirty ≠ 损坏
dirty ≠ replica 不一致
```

dirty 只是：

> **commit status 尚不能仅从本地状态确定。**

---

## Concept 3：Version Query

如果 node 上：

```
V1 clean
V2 dirty
```

现在来了：

```
Read(x)
```

不能返回 V2。

但直接返回 V1 也不一定是最好的决定。

为什么？

因为可能实际上：

```
Tail 已经 commit V2

但 commit ACK
还没有传播回这个 node
```

所以 node 问 tail：

```
Node B -------------------> Tail
       committed version?
```

Tail：

```
version = 1
```

或者：

```
version = 2
```

B 不需要从 tail 拉完整 object。

只需要：

```
一个很小的 version number
```

然后：

```
Tail: committed = V1

B:
V1 clean
V2 dirty

=> 返回本地 V1
```

这就是 CRAQ 的关键性能技巧：

> **把大数据留在 local replica，只向 authoritative node 查询极小的 freshness/commit metadata。**

论文称这个操作为 **version query**。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

# Part 6：完整 CRAQ Happy Path

现在完整走一遍。

初始：

```
       Head                     Tail
        A          B             C

x: V1 clean   V1 clean      V1 clean
```

### Write V2

Client：

```
Put(x=V2)
    |
    v
    A
```

#### A

收到 V2：

```
A:

V1 clean
V2 dirty
```

因为 A 不是 tail。

然后：

```
A ---- V2 ----> B
```

#### B

```
B:

V1 clean
V2 dirty
```

继续：

```
B ---- V2 ----> C
```

#### Tail C

收到 V2：

```
C:

V2 clean
```

此时：

```
V2 committed
```

然后：

```
C ---- ACK(V2) ----> B ----> A
```

#### B 收到 ACK

```
V2 clean
```

旧：

```
V1
```

可以删掉。

#### A 收到 ACK

同样：

```
V2 clean
```

最终：

```
A          B          C

V2 clean   V2 clean   V2 clean
```

于是之后任何：

```
Read(x)
```

都可以完全 local：

```
Client -> A
Client -> B
Client -> C
```

这就是性能来源。

论文特别指出，在 read-mostly workload 中，大部分请求成为这样的 clean local reads，因此吞吐可以随着 chain size 增长。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

# Part 7：真正困难的 Dirty Read

现在停在：

```
A               B               C

V1 clean        V1 clean        V1 clean
V2 dirty        V2 dirty
                                ↑
                                tail还没收到
```

此时：

```
Read -> B
```

B 发现：

```
latest = V2 dirty
```

所以：

```
B -------- version query --------> C

                                  committed?
```

C：

```
V1
```

B 本地刚好：

```
V1
V2
```

于是：

```
return V1
```

注意这里的一个美妙点：

> **B 不需要把 read 转发给 C。**

它只问：

```
哪个 version？
```

真正的 value：

```
本地取。
```

对于大 object，区别尤其明显：

```
普通 Chain Replication：

Client -------- 5MB ---------- Tail


CRAQ dirty read：

Node ---- 8-byte version? ---- Tail

Node 本地读取 5MB
```

这就是为什么即使出现 dirty read，CRAQ 依然可能比所有 read 去 tail 更便宜。

---

# Part 8：这里是最重要的正确性推理

假设：

```
B:
V1 clean
V2 dirty
```

B 问 tail：

```
current committed version?
```

Tail 回：

```
V1
```

就在 Tail 回答以后：

```
V2 arrives
Tail commits V2
```

然后 B 才：

```
return V1 to client
```

时间线：

```
time →

B:       Read starts
             |
             +-------- query -------->

Tail:                     answer V1
                              |
                              | commit V2
                              v

B:                                   return V1
```

乍一看：

> “返回的时候 V2 已经 committed 了，为什么还能返回 V1？”

答案就是 Linearizability 的真正含义。

Linearizability 并不要求：

> response 发出去那一纳秒必须读取最新内存。

它要求：

> 一个 operation 可以被想象成在 invocation 和 response 之间某个瞬间原子发生。

于是我们把 read 的 **linearization point** 放在：

```
Tail 回答 version query 的那个瞬间
```

当时：

```
V1 确实是 latest committed
```

所以合法顺序是：

```
Read(V1)
↓
Write(V2)
```

虽然真正网络执行发生了 overlap。

论文明确讨论了这一情况：tail 可以在回复 version query 后、intermediate node 回答 client 之前 commit 新版本，这仍然不破坏其 strong consistency，因为该 read 可以按照 tail 的 version query 时刻序列化。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

这非常值得记住。

---

# Part 9：Clean Read 为什么甚至不需要问 Tail？

这是 CRAQ 最巧妙的地方。

假设：

```
A -> B -> C -> D(Tail)
```

B 当前：

```
V5 clean
```

而且：

```
latest == V5
```

为什么 B 可以确定：

```
Tail 不可能已经 commit V6？
```

因为 write 的传播方向固定：

```
A → B → C → D
```

如果 V6 已经到达 D：

```
V6 必须先经过 B。
```

但 B 没看见 V6。

矛盾。

因此：

```
B 尚未看见 V6
        ↓
D 不可能看见 V6
        ↓
V6 不可能 commit
```

所以：

```
latest local version = clean
```

蕴含：

```
它就是全局当前 latest committed version
```

这是 CRAQ 最重要的 invariant 之一。

---

# Part 10：把关键 Invariants 写出来

### Invariant 1：Write Prefix Property

一个 write 总是：

```
Head → ... → Tail
```

因此对于某个 version `V`：

```
收到 V 的节点集合
```

永远形成 chain 的一个 prefix：

```
[A B C] D E

而不会：

[A _ C] D E
```

---

### Invariant 2：Tail Commit Property

只有：

```
V reaches Tail
```

才：

```
V committed
```

所以：

```
committed(V)
   ⇒
V 已经经过所有 chain nodes
```

---

### Invariant 3：Clean Local Read Safety

如果 node：

```
latest version = clean V
```

那么不存在：

```
Tail 已 commit V'>V
但 node 不知道
```

因为更大的 version 必须先经过这个 node。

---

### Invariant 4：Dirty Node Has Enough History

假设：

```
Node:

V1 clean
V2 dirty
V3 dirty
V4 dirty
```

Tail 回答：

```
committed = V3
```

Node 必须还能返回：

```
V3
```

所以 outstanding versions 不能随便覆盖掉。

---

### Invariant 5：Writes Reach Tail in Order

例如：

```
V2
V3
V4
```

在同一 chain 中按顺序传播。

Tail 不会产生：

```
commit V4
commit V2
commit V3
```

版本号因此形成一个明确的 commit frontier。

---

# Part 11：用 Multiple Outstanding Writes 看 CRAQ

这是理解 multiple versions 最好的例子。

```
Head A          B          Tail C

V1 clean        V1         V1

V2 dirty        V2 dirty
V3 dirty
```

再过一会：

```
A               B               C

V1 clean        V1 clean        V1 clean
V2 dirty        V2 dirty        V2 clean
V3 dirty        V3 dirty
```

现在 B：

```
Read(x)
```

问 Tail：

```
last committed?
```

回答：

```
V2
```

所以：

```
B return V2
```

即使：

```
latest local = V3
```

也绝不能返回 V3。

因此：

```
latest known version
```

和：

```
latest committed version
```

是两个完全不同的概念。

这是 distributed storage 中非常重要的 mental model。

---

# Part 12：它为什么可以理解成 Linearizability？

论文自己的术语主要是：

> **Strong Consistency**

它描述的是 per-object read/write ordering。对 6.824 学习来说，最有帮助的 mental model 是把 strong mode 看成一个 **per-object linearizable register**。论文也特别指出 consistency 是针对 individual object，而不是默认整个 store 上所有 objects 的全局 transaction ordering。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

我们可以给操作找 linearization point：

|Operation|可以理解的 Linearization Point|
|---|---|
|Write|Tail 接受/commit version|
|Dirty Read|Tail 回答 version query 的时刻|
|Clean Read|local read 时刻|

最难的是 Clean Read。

它之所以能 local linearize：

```
没有看到 newer version
+
write 必须经过自己才能到 tail
```

所以：

```
tail 不可能秘密 commit 一个自己没看到的 newer version
```

这不是 cache 一般天然拥有的性质。

是 **chain topology + ordered propagation** 给你的。

---

# Part 13：CRAQ 最核心的 abstraction

如果让我把整篇 paper 压缩成一句设计原则：

> **把“data availability”和“commit knowledge”分开。**

所有节点：

```
拥有 data
```

但某些时候：

```
不知道哪一版本已经 commit
```

怎么办？

不用：

```
把 read 转发给 authoritative node
```

而是：

```
只向 authoritative node 获取
“commit frontier”
```

例如：

```
Tail:

committed_version = 42
```

Node：

```
我本地已经有：

39
40
41
42
43
44

所以直接返回本地 42。
```

你之后看到很多现代 distributed database 的设计，会不断看到这个思想的变体：

```
我已经有数据

关键问题不是：
“我有没有？”

而是：
“我怎么证明自己读这个版本是安全的？”
```

---

# Part 14：System Model / Assumptions

这里必须严格区分：

```
Chain Replication 理论模型
CRAQ 设计
CRAQ prototype
```

### Node Model

原始 Chain Replication 面向 **fail-stop servers**。CRAQ 延续这一基本 fault model，并使用 coordination service 管 membership/reconfiguration。[Cornell CS](https://www.cs.cornell.edu/fbs/publications/ChainReplicOSDI.html?utm_source=chatgpt.com)

也就是说主要考虑：

```
node 正常
或
node failure
```

不是：

```
node 恶意返回假数据
```

所以：

> **不提供 Byzantine Fault Tolerance。**

---

### Storage Model

一个很容易漏掉的事实：

CRAQ 论文中的 prototype：

```
object data = memory only
```

作者提到 storage abstraction 后续可接 BerkeleyDB。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

所以 prototype 中机器 crash：

```
local object state 丢失
```

重新加入后通过：

```
state transfer
```

恢复。

ZooKeeper 自己则负责 replicated coordination metadata。

---

## Network Model

prototype 使用：

```
TCP
RPC
```

节点维护到：

```
predecessor
successor
tail
```

的连接。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

因此：

```
packet reorder
packet loss
```

大量交给 TCP。

但必须注意：

```
TCP reliability
≠
distributed operation exactly once
```

客户端：

```
Put(x++)
```

发出去以后 connection 断了：

```
到底成功了没？
```

仍然可能不知道。

需要 request ID / idempotency / transaction semantics 另外解决。

---

## Timing Model

CRAQ 并不是依赖：

```
消息 10ms 内一定到达
```

这样的 synchronous assumption。

但实际 failure detection 依赖：

```
ZooKeeper session / timeout
```

因此工程上更接近：

```
asynchronous network
+
eventual failure detection / partial synchrony assumption
```

即：

> timeout 不是证明机器死亡，只是 reconfiguration 的触发依据。

---

## Network Partition

这是一个极重要的 trade-off。

CRAQ 的 strong-consistency write path **不会为了 partition availability 而允许两个独立 chain fragment 随便同时写**。

论文讨论的多数据中心配置中，可以指定 master datacenter；或者在没有 master 时，只让包含多数 chain nodes 的 partition 继续写，其余 partition 转成 read-only。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

注意这里的 majority 很容易跟 Raft 混淆。

它并不意味着：

```
CRAQ 每次 write 都是 quorum commit
```

不是。

正常 CRAQ 写：

```
Head → ... → Tail
```

仍然通过整个当前 chain。

这里 majority 更多用于：

> partition/reconfiguration 时决定哪边有权继续成为 write chain。

---

# Part 15：它最多容忍多少失败？

不要套：

```
N >= 2f + 1
```

那个经典关系主要来自 majority quorum / consensus。

CRAQ 不一样。

假设：

```
N = 5
```

正常 write commit 要：

```
通过当前 chain 中所有 live members
```

如果其中一个挂掉：

```
write 会暂时卡住
```

直到：

```
failure detected
↓
chain reconfigured
↓
剩余节点重新连接
↓
继续
```

论文实验里也观察到：节点 failure 到被 ZooKeeper 判定并从 chain 移除期间，write 无法 commit；reads 受到的影响小得多，因为失败 replica 的读取可以转去其他 replica。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

所以 CRAQ 的 fault tolerance 不是：

```
每次请求只要多数派活着立即成功
```

而更像：

```
failure
↓
temporary interruption
↓
reconfiguration
↓
resume
```

最终如果保存该 object 的所有 replicas 都永久丢失：

```
数据当然也丢失
```

任何 replication protocol 都无法突破这一点。

---

# Part 16：Failure #1 — Head Crash

假设：

```
A -> B -> C
^
Head
```

A crash。

CRAQ：

```
B becomes new Head
```

论文明确说明 head failure 时其 immediate successor 接任。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

现在最有意思的是：

```
Client:
Put(V2)

A 收到了
但还没发给 B

A crash
```

V2：

```
没有 commit
```

可以消失。

这是安全的，因为 strong write 尚未成功返回。

如果 client 不知道 write 是否成功：

```
retry
```

就需要考虑：

```
idempotency
request ID
```

CRAQ 本身并不 magically 给所有 arbitrary operations exactly-once semantics。

---

# Part 17：Failure #2 — Tail Crash

```
A -> B -> C
          ^
         Tail
```

C crash：

````
B becomes Tail
``` :chatgpt-content-reference{index="15"}


考虑更有意思的情况：

```text
A: V2 dirty
B: V2 dirty
C: V2 committed
````

然后 C crash，ACK 还没回来。

V2 是否丢了？

不会，因为：

```
C 能收到 V2
⇒
V2 已经经过 A 和 B
```

所以：

```
A 有 V2
B 有 V2
```

新的 tail 可以通过 recovery/reconciliation 重建正确状态。

这就是 chain topology 很漂亮的性质：

> **Tail 是最后得到 write 的节点，因此 Tail 上存在的 write，在所有前驱上已经存在。**

---

# Part 18：Failure #3 — Middle Node Crash

```
A -> B -> C -> D
         X
```

假设 B crash：

```
A ------> C -> D
```

但不能只改 TCP 连接。

因为存在：

```
A 已经发给 B：
V7

但 B crash 前没有发给 C
```

所以新的 predecessor/successor 必须：

```
state reconciliation
```

CRAQ recovery 会传播需要的 object state；full-state recovery 包含 latest clean version 以及 outstanding dirty versions。论文中特别强调这点，因为新节点以后可能还需要处理 ACK。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

例如：

```
A:

V5 clean
V6 dirty
V7 dirty

C:

V5 clean
```

不能只说：

```
“最新 V7 给你”
```

因为之后可能：

```
Tail says committed = V6
```

C 必须能够返回：

```
V6
```

所以：

```
V5
V6
V7
```

的状态可能都很重要。

---

# Part 19：为什么新节点恢复期间不能立刻读？

假设一个新 B 插入：

```
A -> B -> C
```

B 刚启动：

```
B:
部分数据
```

如果马上接受：

```
Read(x)
```

它根本不知道：

```
自己缺不缺 version
```

所以 CRAQ 在 recovery 时要求新节点先与相邻节点 reconcile；论文指出新 node 在与 successor 达成一致前会拒绝相应 object 的 client reads。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

这和你做 Kubernetes controller 时的一个思想很像：

```
process started
≠
state synchronized
≠
ready
```

因此常有：

```
liveness
readiness
```

两个概念。

Distributed storage 同样如此：

```
process alive
≠
replica safe to serve
```

---

# Part 20：Safety 为什么成立？

把 CRAQ safety 分成三个 case 最容易证明。

---

### Case A：Clean Read

Node：

```
latest local = V5 clean
```

如果 Tail 已 commit V6：

```
V6 必须从 Head → ... → Node → ... → Tail
```

那么 Node 必须已经看到 V6。

但 Node 没看到。

矛盾。

因此：

```
Tail latest committed = V5
```

local read safe。

---

### Case B：Dirty Read

Node：

```
V5 clean
V6 dirty
V7 dirty
```

问 Tail：

```
committed = V6
```

Node 返回本地 V6。

Tail 是 commit authority。

所以 safe。

---

### Case C：Tail 在 query 后 commit 新 write

```
Tail query:
committed=V6

随后 commit V7
```

read 返回 V6。

把 read linearize 在：

```
version query
```

即可：

```
Read(V6)
Write(V7)
```

仍合法。

---

## Safety ≠ Liveness

这节课非常适合再次强化这个区别。

### Safety

永远不能：

```
一个已经完成的 V2 write
之后
read 却返回 V1
```

也不能：

```
暴露一个最后永远没有 commit 的 dirty V3
```

---

### Liveness

希望：

```
只要系统恢复到足够稳定状态
read/write 最终能够完成
```

比如：

```
B crash
```

Safety 可以通过：

```
先暂停 writes
```

保住。

然后：

```
detect failure
reconfigure
```

恢复 liveness。

这正是很多 distributed protocol 的模式：

```
不确定时
宁愿停止进度
也不要破坏 safety
```

---

# Part 21：Network Partition

考虑：

```
A -> B   X   C -> D
```

两边互相看不到。

最危险的 naive design：

```
A-B:
自己选一个 Tail，继续写

C-D:
自己选一个 Head，继续写
```

然后：

```
left:  x=10
right: x=20
```

网络恢复：

```
哪个是真的？
```

这就是 split brain。

CRAQ 的 strong mode 不允许这么随便干。

可以：

```
authoritative partition
       ↓
继续写

non-authoritative partition
       ↓
read-only
```

或者应用选择弱 consistency semantics。

论文明确指出 strong-consistency protocol 不支持在 partition 两侧任意继续 writes，但 partitioned segment 可以退化为 read-only 的弱一致性服务。[USENIX](https://www.usenix.org/legacy/event/usenix09/tech/full_papers/terrace/terrace.pdf?utm_source=chatgpt.com)

---

# Part 22：CRAQ 甚至支持不同 Read Consistency

CRAQ 论文定义了三种 read 模式。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

### Strong Consistency

```
dirty?
    ↓
ask tail
```

保证读取 latest committed version。

---

### Eventual Consistency

不问 tail：

```
return latest local version
```

所以：

```
Node A: V5
Node B: V6
```

客户端换 node：

```
read V6
read V5
```

可能发生。

---

### Maximum-Bounded Inconsistency

介于两者之间：

```
最多 stale 多少时间
或者
最多落后多少 versions
```

例如：

```
允许：

max 2 versions behind
```

或者：

```
max 500ms
```

这已经非常接近今天常见的：

```
bounded staleness
```

设计思想。

---

# Part 23：Failure Matrix

|Failure|CRAQ 行为|Safety|Availability|核心原因|
|---|---|---|---|---|
|Head crash|successor 成新 Head|保持|短暂受影响|未 commit write 可丢|
|Tail crash|predecessor 成新 Tail|保持|短暂受影响|Tail 上的 write 已经过所有前驱|
|Middle crash|绕过失败节点并 reconcile|保持|writes 暂停到 reconfig|必须恢复 missing dirty state|
|Packet loss|prototype 主要依赖 TCP retry|保持|latency ↑|transport reliability|
|Delayed ACK|node 更久保持 dirty|保持|read 变慢|dirty read 查询 Tail|
|Duplicate client RPC|需要更高层 dedup/idempotency|取决于 op|通常可继续|RPC retry ≠ exactly once|
|Partition|authoritative side 才能强写|保持|部分 write availability 丢失|防 split brain|
|Node restart|重新 join + state transfer|保持|recovery 期间受影响|prototype object 是 memory state|

最值得注意的一行是：

```
Delayed ACK
```

系统不会因此错误。

只会：

```
本来可以 clean local read
↓
继续认为 dirty
↓
多问一次 tail
```

即：

> 很多 CRAQ metadata 延迟首先伤害的是 performance，而不是 safety。

这是一个非常好的系统设计性质。

---

# Part 24：为什么不是直接等 ACK？

你可能会想到：

```
Node B dirty
↓
那就 block
↓
等 B 收到 ACK
↓
再读
```

当然可以。

但是假设：

```
ACK delayed 500ms
```

实际上 Tail 可能早就 commit。

这时：

```
version query
```

可能只要：

```
1ms
```

于是 CRAQ 选择：

```
dirty
↓
主动获取 commit information
```

而不是：

```
被动等待 replication metadata
```

这会降低 read latency。

---

# Part 25：CRAQ 与 Primary/Backup

Primary/Backup：

```
          Primary
         /   |   \
       B1   B2   B3
```

通常：

```
Primary 排序 writes
```

如果 strong reads 也都 primary：

```
read hotspot
```

要允许 backup read，需要证明 backup 足够新。

CRAQ：

```
Head -> B -> C -> Tail
```

把：

```
write ingress
```

和：

```
commit authority
```

分到了 chain 两端。

并利用传播方向获得额外 invariant。

所以 CRAQ 的创新并不是：

```
“replica 可以读”
```

真正创新是：

```
replica 如何证明它可以安全地读
```

---

# Part 26：CRAQ vs Quorum Replication

Quorum 常见 mental model：

```
N replicas

write W
read R

R + W > N
```

例如：

```
N = 5
W = 3
R = 3
```

read quorum 和 write quorum 必有交集。

CRAQ 完全不是这个思路。

CRAQ：

```
Write:

A → B → C → D → E

Read:
local
or
local data + Tail metadata
```

因此：

```
Quorum:
靠 set intersection 找新数据

CRAQ:
靠 ordered topology + commit frontier 判断新数据
```

为什么必须区分？

因为 failure behavior 非常不同。

Raft / quorum：

```
5 个节点挂 2 个
多数派还活着
仍可能立即继续
```

CRAQ：

```
chain 中间一个节点挂了
当前 write path broken
需要先 reconfigure
```

所以不要看到 replication 就自动套：

```
2f+1
```

---

# Part 27：CRAQ vs Raft

这是整节课最值得掌握的比较之一。

||Raft|CRAQ|
|---|---|---|
|核心问题|Consensus / replicated log|scalable replicated object storage|
|Ordering authority|Leader + log|Head→Tail chain|
|Commit|Majority replication|Reach Tail / entire current chain|
|Normal write|Leader→followers|Head→…→Tail|
|Strong read|常需 leader/ReadIndex/lease 等机制|clean local 或 tail version query|
|Failure progress|majority alive 即可选 leader|通常需要 chain reconfiguration|
|Membership|consensus configuration|ZooKeeper/control-plane coordination|
|默认 abstraction|ordered command log|object read/write|

最容易犯的错误：

> “CRAQ 就是另一种 Raft。”

不是。

Raft：

```
谁决定 log 里的顺序？
```

CRAQ：

```
在已定义 chain ordering 的 object replication 中，
如何优化 data flow 与 reads？
```

---

# Part 28：为什么 CRAQ 没有“投票”？

因为：

```
Head
↓
所有 write 已经被串行进入同一条 path
↓
Tail
```

写入不存在：

```
A proposes x=1
B proposes x=2
C proposes x=3
```

然后大家决定。

它的 topology 本身强制：

```
one ordered stream
```

当然这意味着：

> **谁是 Head / Tail、chain 是什么，本身必须被可靠管理。**

于是又需要：

```
ZooKeeper
```

这样的 coordination layer。

这正展示了一个分布式系统常见模式：

```
复杂问题没有消失
只是被放到了另一个 layer。
```

---

# Part 29：CRAQ vs 2PC

2PC：

> 多个不同 participants 的 transaction，要么一起 commit，要么一起 abort。

例如：

```
Account A -100
Account B +100
```

要求 atomic。

CRAQ：

> 同一个 object 的 replicas 如何保持一致并支持 scalable reads。

所以：

```
CRAQ
≠
Distributed Transaction protocol
```

如果你要：

```
x=10
y=20

必须 atomic update
```

CRAQ 的 basic object protocol 不够。

论文甚至单独讨论了 mini-transactions / multi-object operations，这正说明：

```
per-object replication
```

和：

```
cross-object atomicity
```

是不同问题。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

# Part 30：CRAQ vs Spanner

CRAQ：

```
主要考虑：
per-object versions
+
replication
+
strong read
```

Spanner：

```
Replication
+
Consensus
+
MVCC
+
distributed transactions
+
timestamps
+
TrueTime
```

所以 Spanner 解决的问题更广。

但它们有一个非常值得建立的共同 mental model：

```
Replica 本地有数据
       ↓
能不能安全读？
       ↓
需要证明某个 version / timestamp
已经是安全 frontier
```

CRAQ：

```
Tail:
committed version = V42
```

现代 MVCC database 可能是：

```
safe timestamp = T
closed timestamp = T
applied index = I
```

本质问题极其相似。

---

# Part 31：一个现代类比：CockroachDB Follower Reads

这不是说 CockroachDB 实现 CRAQ。

机制不同。

但 mental model 极像。

CockroachDB follower 想回答某个过去 timestamp 的 read，需要知道：

```
1. 我已经 apply 到足够远
2. 不会再冒出 timestamp <= T 的新 write
```

它使用 **closed timestamps** 等机制给 follower 一个“这个 timestamp 之前已经安全封闭”的证明。[Cockroach Labs](https://www.cockroachlabs.com/blog/follower-reads-stale-data/?utm_source=chatgpt.com)

你可以看到共同问题：

```
CRAQ:

data local
+
Tail 告诉我 safe version


CockroachDB:

data local
+
closed timestamp 告诉我 safe time
```

抽象以后：

```
Local Data
    +
Proof of Freshness / Safety
    =
Safe Replica Read
```

这可能是整节 Lecture 最值得带走的现代 mental model。

---

# Part 32：etcd 为什么不能随便从 follower local read？

etcd 默认 linearizable operations 需要经过 Raft 的一致性路径；它也提供允许 stale data 的 serializable read 模式，以换取更低成本。[etcd](https://etcd.io/docs/v3.4/learning/api_guarantees/?utm_source=chatgpt.com)

这直接说明：

```
有 replica
```

不意味着：

```
replica local read 自动 linearizable
```

你必须有某种证明：

```
leader authority
ReadIndex
lease
safe timestamp
closed timestamp
CRAQ version query
...
```

不同系统机制不同，但问题相同。

---

# Part 33：与 Kubernetes / etcd 的联系

你可以把 Kubernetes control plane 想成：

```
Controllers
     |
     v
API Server
     |
     v
etcd
     |
     v
Raft Replicas
```

etcd 的 replicas 有数据。

但如果随便：

```
GET -> 任意 follower local state
```

这个 follower 可能：

```
apply lag
```

所以你会再次碰到：

> **“Replica 有数据”和“Replica 有资格提供 fresh read”不是同一件事。**

这就是 CRAQ 对基础设施工程师最大的学习价值。

---

# Part 34：和 Kubernetes Controller 的一个有限类比

不要把 Controller 直接等同于 CRAQ。

但两者有一个共同思维：

```
Observed State
≠
Authoritative State
```

Controller：

```
cache/informer 中看到的对象
```

可能 lag behind API server。

CRAQ node：

```
local version
```

可能不知道 commit status。

所以工程设计不能只问：

```
我本地看到什么？
```

还要问：

```
我如何证明这个观察足够新？
```

不过两者有明显区别：

```
K8s Controller:
eventually reconcile

CRAQ Strong Read:
需要同步保证
```

所以不能把二者 consistency semantics 混在一起。

---

# Part 35：和 Redis Cache Aside 的关系

假设：

```
DB
 |
 +--> Redis cache
```

Cache：

```
x=1
```

DB：

```
x=2
```

Redis 没有类似 CRAQ 的天然 property：

```
如果我没看见 V3
⇒
DB 肯定也没 commit V3
```

完全不成立。

因为：

```
DB commit
```

并不需要经过 Redis。

这正解释了为什么 cache invalidation 如此麻烦。

CRAQ 能 local clean read 的一个根本原因恰恰是：

> **所有 write 在 commit 前必须经过这个 replica。**

Cache-aside 通常没有这个 topology invariant。

这是两类系统很本质的差别。

---

# Part 36：为什么 Chain 的方向这么重要？

看：

```
Head -> A -> B -> C -> Tail
```

对于 B：

如果 B 没看到 V100：

```
Tail 不可能看到 V100
```

因为 V100 去 Tail 必须过 B。

这叫一种 information-ordering property。

如果换成：

```
          Head
       /    |    \
      A     B     C
             \
             Tail?
```

B 没看到 V100：

```
完全不能推出 C 没看到。
```

CRAQ 的局部 freshness inference 就没那么简单了。

所以：

> Chain topology 不只是网络路径优化，它实际上给 correctness proof 提供了结构。

这是一个非常重要的 distributed systems 思维。

---

# Part 37：Read-Mostly 为什么特别适合 CRAQ？

如果：

```
99% reads
1% writes
```

绝大多数时间：

```
A: V100 clean
B: V100 clean
C: V100 clean
D: V100 clean
```

于是：

```
100% local read
```

4 台机器：

```
≈ 4 台 read capacity
```

反过来，如果：

```
大量 writes
```

节点经常：

```
V100 clean
V101 dirty
V102 dirty
...
```

那么 read 经常：

```
Node -> Tail version query
```

Tail 又开始成为热点。

所以 CRAQ 的收益与：

```
dirty probability
```

高度相关。

---

# Part 38：性能直觉

设：

```
C = chain length
```

在理想 read-only workload：

```
Read throughput ≈ C × single-node throughput
```

这里不是精确性能公式，而是设计上的 scaling intuition。

论文实验也观察到 read throughput 随 chain length 近似线性增长；作者报告相对于基本 CR，3-node chain 大约提升 200%，7-node 大约提升 600%。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

这正好符合：

```
CR:
1 read server

CRAQ C-node:
≈ C read servers
```

---

# Part 39：Write-Heavy 时发生什么？

大量 write：

```
Head:
dirty dirty dirty

Node2:
dirty dirty

Node3:
dirty
```

于是：

```
read
 ↓
version query
 ↓
Tail
```

Tail 又承担大量 metadata request。

但 metadata query：

```
“version number?”
```

比：

```
“把完整 5KB / 1MB object 发给我”
```

便宜。

论文的一个实验中，3-node chain 的 500B object 在 read-only 时约：

```
CRAQ 59,882 reads/s
CR   20,552 reads/s
```

在 write rate 很高使非-tail replicas 长期 dirty 时，CRAQ read throughput 仍约：

```
39,873 reads/s
```

对比 CR：

```
20,430 reads/s
```

作者将差异归因于 Tail 处理小 version query 比完整 read response 更便宜。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

注意：

> 这些是 2009 年 prototype 的具体实验数字，不应该当成今天硬件上的性能预期。

真正重要的是趋势。

---

# Part 40：Latency

Clean read：

```
Client → local replica
```

Dirty read：

```
Client → replica
          |
          +→ Tail
          ←
```

所以：

```
Dirty latency
≈
Clean latency
+
one Tail RTT
```

论文实验也观察到 dirty reads 比 clean reads 慢，write latency 则随 chain length 增长，因为 update 需要沿 chain 传播。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

# Part 41：Multi-Region 为什么 CRAQ 特别有意思？

假设：

```
US-West -> US-East -> Europe
                        ^
                       Tail
```

传统 CR：

```
US-West client
    |
    +------------------------> Europe Tail
```

每个 read：

```
WAN RTT
```

很惨。

CRAQ：

```
US-West local replica clean
           ↓
local read
```

只有 dirty 时：

```
small version query
          ↓
remote Tail
```

论文正是把 locality 作为 CRAQ 的重要收益之一，并在模拟约 80ms RTT 的多数据中心环境下展示 clean read 可避免远端 tail round trip，而 dirty read 才需要远程 metadata query。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

# Part 42：CRAQ 最大的 Trade-offs

CRAQ 没有免费午餐。

### 1. Read scalability ↑，state complexity ↑

原来：

```
x = value
```

现在：

```
x = [
  V10 clean,
  V11 dirty,
  V12 dirty,
  ...
]
```

需要：

```
GC
version management
ack tracking
recovery
```

---

### 2. Read scalability ↑，write latency 随 chain length ↑

```
Head -> N2 -> N3 -> N4 -> Tail
```

chain 越长：

```
write path 越长
```

论文的 evaluation 也观察到 write latency 随 chain length 增加。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

### 3. Strong consistency ↑，partition write availability ↓

如果无法安全确定 authoritative chain：

```
停止 strong writes
```

这体现典型：

```
partition 下
Consistency
vs
Availability
```

trade-off。

---

### 4. Read-heavy 非常好，write-heavy 收益下降

因为：

```
dirty reads ↑
Tail queries ↑
```

---

# Part 43：Top 5 Misconceptions

### ❌ 1. Dirty 表示数据错了

不是。

Dirty：

```
我知道这个版本
但不知道它是不是当前 committed version
```

---

### ❌ 2. Clean 表示整个系统现在没有 write

不是。

可能：

```
Node C = V1 clean
```

同时 Head 刚刚收到 V2。

关键在：

```
V2 还没经过 C
```

因此 Tail 也还不可能 commit V2。

所以 C 返回 V1 仍正确。

---

### ❌ 3. CRAQ 让所有读都完全 local

不对。

只有：

```
latest = clean
```

才完全 local。

Dirty：

```
需要 Tail version query
```

---

### ❌ 4. CRAQ 就是 quorum replication

不是。

CRAQ 的主要正确性来源是：

```
ordered chain
+
Tail commit
```

不是：

```
R + W > N
```

---

### ❌ 5. Tail 回复 V1 后马上 commit V2，就意味着 read V1 错了

不对。

因为：

```
read 与 write overlap
```

read 可以 linearize 在：

```
Tail 回复 version query 的时刻
```

所以：

```
Read V1
→
Write V2
```

是合法的。

这通常是第一次学 CRAQ 最容易卡住的地方。

---

# Part 44：一个更深的误解：为什么不永远返回 last local clean？

假设：

```
Node:

V1 clean
V2 dirty
```

一个 naive optimization：

```
别问 Tail
直接返回 V1
```

很多时候确实不会出错。

但它缺少 CRAQ 想要的一个重要保证：

```
到底 Tail 已经 commit 到哪里？
```

特别是在：

```
commit notification 延迟
```

以及协议实现允许 Tail commit 与其他节点 knowledge 不同步时，local clean frontier 可能落后于 global commit frontier。

CRAQ 的 version query 让 read 明确在：

```
Tail 的 commit ordering
```

中找到位置。

因此它不是单纯：

```
“返回旧一点也没关系”
```

而是：

```
我要知道一个可以合法 linearize 的 committed version。
```

---

# Part 45：Paper Problem

论文：

**Object Storage on CRAQ: High-Throughput Chain Replication for Read-Mostly Workloads**

Jeff Terrace 与 Michael Freedman，USENIX ATC 2009。[USENIX](https://www.usenix.org/conference/usenix-09/object-storage-craq-high-throughput-chain-replication-read-mostly-workloads?utm_source=chatgpt.com)

作者看到的核心问题：

```
Replication
+
Strong Consistency

通常导致：
read coordination / bottleneck
```

基本 Chain Replication：

```
all reads -> Tail
```

因此：

```
一个 hot object
```

可以把 Tail 打爆。

---

# Part 46：Previous Approach

Chain Replication：

```
WRITE:
Head → ... → Tail

READ:
             Tail
```

优点：

```
非常好推理
strong consistency
pipeline writes
failure recovery relatively simple
```

缺点：

```
read throughput ≈ Tail throughput
```

尤其 multi-DC：

```
Tail 可能离 client 很远
```

论文指出这是基本 Chain Replication 的主要限制之一。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

# Part 47：Paper Key Insight

整个 paper 最重要的一句话：

> **允许所有 replicas 保存 speculative/uncommitted versions，但只有在知道正确 committed version 时才把相应 version 暴露给 strong read。**

于是把：

```
data replication
```

和：

```
commit metadata
```

解耦。

Clean：

```
完全 local
```

Dirty：

```
Tail 只提供版本 metadata
```

---

# Part 48：Paper Design

系统大致：

```
              ZooKeeper
           membership/config
                 |
        +--------+--------+
        |                 |
        v                 v

Client ---> Head -> Node -> Node -> Tail
             |       |      |       |
           objects  objects objects objects
```

多个 objects 又通过：

```
chains
+
DHT/consistent placement
```

分布到集群。

论文讨论了 single-DC、multi-DC、不同 replication layout 等配置。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

# Part 49：Evaluation

论文最核心的结论可以压成四条：

```
Read-mostly
→ read throughput 近似随 chain size scale

Write contention ↑
→ dirty read ↑
→ read throughput 下降
→ 但 metadata query 仍比完整 Tail read 轻

Failure
→ read 影响较小
→ write 会等待 membership reconfiguration

Multi-DC
→ clean local read 可以显著避免 WAN latency
```

实验中节点故障后需要等待 ZooKeeper timeout 才重建 chain，随后吞吐恢复；失败期间 write latency 会显著增加。[USENIX](https://www.usenix.org/event/usenix09/tech/full_papers/terrace/terrace.pdf)

---

# Part 50：Paper Limitations

这篇 paper 并没有解决“所有 distributed storage 问题”。

它主要不是：

```
Byzantine system
```

也不是完整：

```
distributed SQL
```

也不是：

```
general serializable multi-object transactions
```

也没有解决 CAP 意义上：

```
partition 两边都继续强一致写
```

Basic CRAQ 的主要 sweet spot 是：

> **replicated object store + read-mostly workload + per-object strong consistency。**

---

# Part 51：What Aged Well?

我认为最值得今天继续记住的是四个思想：

```
Replica read 需要 freshness proof

Data 与 commit metadata 可以分离

MVCC / versions 可以帮助安全暴露 replica state

Read scalability 的关键不是“复制更多”
而是“证明副本可以读”
```

你今天看到：

```
safe timestamp
closed timestamp
lease
ReadIndex
applied index
resolved timestamp
```

虽然实现完全不同，但大部分都在回答同一个问题：

> **“这个 replica 怎么知道自己的状态已经足够安全？”**

---

# Part 52：What Changed?

今天很多 distributed databases 更常见的架构是：

```
每个 shard/range
      ↓
Raft/Paxos replication group
      ↓
leader/leaseholder
      ↓
MVCC
      ↓
safe follower reads
```

例如：

```
CockroachDB
TiKV
Spanner-like systems
```

而不是直接用一条 CRAQ chain。

原因包括：

```
consensus reconfiguration 更成熟
transactions
MVCC
multi-key semantics
dynamic sharding
modern failure handling
```

但 CRAQ 的核心思想并没有“过时”。

相反，它非常适合训练：

> replica read 的正确性究竟依赖什么。

---

# Part 53：Lab 对应关系

CRAQ 本身通常不是 6.824 的主要编程 Lab。

但它和你写 Raft KV 时一个非常重要的问题直接相关。

最保守设计：

```
Read
 ↓
Leader
 ↓
确认 leadership / commit
 ↓
read state
```

如果你想：

```
Follower read
```

不能直接：

```
return kv[key]
```

你必须证明：

```
这个 follower 没落后
并且没有 future leader 能让这个 read 失效
```

所以会引出：

```
ReadIndex
leader lease
appliedIndex
safe timestamp
```

CRAQ 本质上是在训练你问正确的问题：

```
这个 read 的 linearization point 在哪里？
```

---

# Part 54：如果自己实现 CRAQ，核心 State 是什么？

简化成：

```
Node:

predecessor
successor

isHead
isTail

objects:
  key ->
    versions:
      versionNumber
      value
      clean/dirty
```

例如：

```
x:

version=100
value="A"
clean=true

version=101
value="B"
clean=false

version=102
value="C"
clean=false
```

以及：

```
membership/config generation
```

现实系统还需要：

```
request IDs
timeouts
retry
state transfer progress
connection state
GC frontier
```

---

# Part 55：一个伪代码 Mental Model

不是完整实现，只看核心逻辑。

```
WRITE(key, value):

    if not HEAD:
        reject/redirect

    v = next_version()

    store(key, v, value, DIRTY)

    send successor WRITE(key, v, value)
```

中间节点：

```
on WRITE(key, v, value):

    if TAIL:
        store(key, v, value, CLEAN)

        send ACK(v) backward

    else:
        store(key, v, value, DIRTY)

        forward WRITE(...)
```

ACK：

```
on ACK(key, v):

    mark v CLEAN
    GC versions < v

    send ACK backward
```

Read：

```
READ(key):

    latest = latest_local_version(key)

    if latest is CLEAN:
        return latest.value

    committedVersion =
        query_tail(key)

    return local[key][committedVersion]
```

整个 CRAQ 的 algorithmic essence 基本就在这里。

---

# Part 56：为什么 GC 不能太早？

假设：

```
V1 clean
V2 dirty
V3 dirty
```

如果你看到 V3 就删：

```
V2
```

然后 Tail 回：

```
committed = V2
```

你：

```
？？？
```

所以：

> outstanding versions 必须保留到系统知道它们已不再可能成为需要返回的 committed version。

ACK 正好提供 GC frontier。

这跟 MVCC garbage collection 的思想有很强联系：

```
不能因为版本旧
就说明没人再需要它
```

你必须知道：

```
safe GC frontier
```

---

# Part 57：Problem → Solution Chain

这是最值得复习的一张图：

```
Problem:
数据需要 Fault Tolerance
        ↓
Replication
        ↓
Problem:
多个 replica 怎样保持一致？
        ↓
Chain Replication
        ↓
Writes: Head → Tail
Reads: Tail
        ↓
Problem:
Tail 成为 read bottleneck
        ↓
Naive:
所有 replica local read
        ↓
Failure:
replicas 处于不同 propagation state
可能暴露 uncommitted version
        ↓
Mechanism:
Multiple Versions
        ↓
Mechanism:
Clean / Dirty
        ↓
Problem:
Dirty node 不知道哪个 version committed
        ↓
Mechanism:
Tail Version Query
        ↓
Result:
Clean read = local
Dirty read = local data + Tail metadata
        ↓
Problem:
Node failure / membership change
        ↓
Mechanism:
ZooKeeper membership
+
state reconciliation
+
back propagation
        ↓
Problem:
Network partition
        ↓
Strong mode:
只允许 authoritative partition 写
        ↓
Optional weaker consistency:
read-only / stale reads
        ↓
Final:
Strong Consistency
+
Read Scalability
+
Failure Recovery
```

---

# Part 58：30 秒版本

如果面试官问：

> CRAQ 是什么？

你可以说：

> CRAQ 是 Chain Replication 的一种扩展。传统 Chain Replication 让 writes 从 head 沿 chain 到 tail，并要求所有 strong reads 从 tail 读取，因此 read throughput 受单个 tail 限制。CRAQ 让每个 replica 保存多个 version，并把版本标成 clean 或 dirty。Clean 表示已知 committed，可以直接 local read；如果 latest version 是 dirty，replica 会向 tail 查询当前 committed version number，然后返回本地对应版本。这样在 read-mostly workload 下，大多数 reads 可以在任意 replica 本地完成，同时保持 per-object strong consistency。

---

# Part 59：3 分钟版本

可以讲：

> Chain Replication 利用固定链路 `Head → replicas → Tail` 保证强一致性。一个 write 只有到达 Tail 才 committed，所有 reads 也从 Tail 读取，因此 Tail 自然定义读写顺序。但这种方法不能利用所有 replicas 扩展 read throughput。
> 
> CRAQ 的关键是让 replica 保存 multiple versions。新 write 到达非 Tail node 时标记为 dirty，到达 Tail 后变成 clean 并 commit，然后 ACK 向 chain 前面传播。读取时，如果 node 的 latest version 是 clean，它可以直接返回，因为任何已经 commit 的更新都必须先经过这个 node 才能到达 Tail，所以不存在 Tail 已 commit、而该 node 完全没见过的更新。如果 latest version 是 dirty，node 不知道当前 commit frontier，于是问 Tail 当前 committed version number，再从本地返回那个 version。
> 
> Dirty read 查询 Tail 只是查询很小的 metadata，而不用从 Tail 获取整个 object。Read-mostly workload 下绝大多数对象是 clean，因此 read throughput 可以随着 replica 数量扩展。它的代价是多版本状态、recovery complexity、write latency 随 chain length 增长，而且 partition 时不能让多个 fragment 都继续 strong writes。

---

# Part 60：深入版本

你可以压成：

```
Problem
↓
Tail-only read causes bottleneck

Model
↓
Ordered Head→Tail propagation
Fail-stop replicas
Per-object versions

Algorithm
↓
non-tail receives write => dirty
tail receives write => clean/commit
ACK propagates backward

Read
↓
latest clean => local
latest dirty => ask Tail committed version

Invariant
↓
Any write committed at Tail
must previously pass every replica

Safety
↓
Clean replica cannot unknowingly lag behind
a committed version it never saw

Dirty read gets authoritative commit frontier

Liveness
↓
Failures may temporarily stall writes
until membership reconfiguration

Failure Handling
↓
successor becomes Head
predecessor becomes Tail
middle failure triggers reconciliation

Trade-off
↓
Read scalability ↑
State/recovery complexity ↑
Write latency ↑ with chain length
Partition write availability ↓
```

---

# Part 61：最终知识网络

把这节课挂在这里：

```
                     Distributed Systems
                              |
             +----------------+----------------+
             |                                 |
      Fault Tolerance                      Consistency
             |                                 |
        Replication                      Linearizability
             |                                 |
      +------+-------+                         |
      |              |                         |
 Primary/Backup   Consensus                    |
                    |                          |
                 Raft/Paxos                    |
                    |                          |
          State Machine Replication -----------+
                    |
                 ZooKeeper
                    |
        membership / coordination
                    |
                    v
             Chain Replication
                    |
        Head → replicas → Tail
                    |
           reads all at Tail
                    |
              read bottleneck
                    |
                    v
                  CRAQ
                    |
          +---------+---------+
          |                   |
     Multiple Versions   Clean / Dirty
                              |
                       Version Query
                              |
                              v
                Safe Replica Local Reads
                              |
               +--------------+-------------+
               |                            |
        Read Scalability             Geo-local Reads
               |
               v
         Modern analogues
               |
       follower reads / leases
       safe timestamp / closed timestamp
       applied index / ReadIndex
```

---

## 最后，用一句真正值得长期记住的话

CRAQ 表面是在讲：

```
Chain Replication
```

但更深的一层是在教：

> **在 replicated system 里，“副本上有数据”从来不等于“副本有资格返回这份数据”。**

真正的问题始终是：

```
Replica has data
        +
How does it know this version is safe?
        ↓
Correct Read
```

CRAQ 的答案是：

```
ordered chain
+
versions
+
clean/dirty
+
Tail commit frontier
```

Raft follower read、etcd、Spanner、CockroachDB 等现代系统虽然答案不同，但你以后都可以用同一个问题去拆：

> **“这个 replica 凭什么知道自己现在读到的 version 不会违反 Linearizability？”**

一旦开始这样问，Replication、Consensus、MVCC、Lease、ReadIndex、safe timestamp、closed timestamp 这些原本看起来分散的知识就开始连起来了。
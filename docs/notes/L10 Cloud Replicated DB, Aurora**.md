下面完整讲完 **MIT 6.824 Lecture 10 — Cloud Replicated DB, Aurora**。我会以 2020 年课程讨论的 **SIGMOD 2017 Aurora paper** 为主体；涉及今天 Aurora 的地方，我会明确标成“现代实现”，避免把后来的功能倒灌进论文。

整节课最值得记住的一条主线是：

> **Aurora 的创新不是发明了一种新的 SQL、事务或 Consensus，而是重新划分了 Database Compute 和 Distributed Storage 的边界：数据库只把 redo log 送入一个 replicated storage service，由 storage 负责 replication、page materialization、repair、backup 和大部分 recovery。**

原论文明确把 cloud database 的主要瓶颈从单机 compute/disk 转移到了 **database tier ↔ storage tier 的 network**，并把 redo processing 下推到 scale-out storage。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

# Part 1：这节课到底想解决什么问题？

### 如果只允许记住一个问题

> **怎样在 Cloud 中构造一个高吞吐、高可用、跨 AZ replicated relational database，而又不让 replication 带来的 network amplification 和 synchronous I/O 把数据库拖死？**

传统数据库大致是：

```
              MySQL
        ┌────────────────┐
        │ SQL            │
        │ Transactions   │
        │ Lock Manager   │
        │ Buffer Pool    │
        │ WAL / Redo     │
        │ Recovery       │
        └───────┬────────┘
                │
               Disk
```

执行：

```
UPDATE account
SET balance = balance - 10
WHERE id = 1;
```

可能发生：

```
修改 Buffer Pool 中的 page

       ↓

生成 redo log

       ↓

写 WAL

       ↓

以后写 dirty page

       ↓

checkpoint
```

单机时代这很好理解。

但现在把数据库搬进 Cloud：

```
                 AZ1
             Storage A
             Storage B

                 AZ2
DB Writer →   Storage C
             Storage D

                 AZ3
             Storage E
             Storage F
```

为了 durability，你开始复制。

问题来了：

一个逻辑 UPDATE 可能导致：

```
redo
data page
double-write page
binlog
checkpoint writes
backup writes
...
```

而这些 I/O 又可能继续被复制多份。

于是：

```
1 application write

      ↓

many database writes

      ↓

× replication

      ↓

huge network amplification
```

论文里的 mirrored MySQL 示例正是在说明这一点：传统架构不仅写 redo，还会产生 data page、double-write、binlog 等不同 I/O，而且其中多个步骤处在同步路径上。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

### 最 naive 的办法

最简单：

> 把传统 MySQL 原封不动放到 replicated network storage 上。

例如：

```
MySQL
  |
  | WAL
  | data page
  | double-write
  | binlog
  v
Network Storage
  |
  +---- replica
  +---- replica
  +---- replica
```

逻辑上没有问题。

但是 performance 很差。

原因有两个。

第一：

```
Write Amplification
```

同样的信息被：

```
redo representation
page representation
double-write representation
backup representation
...
```

重复传输。

第二：

```
Synchronization Amplification
```

很多 I/O：

```
send
 ↓
wait
 ↓
send
 ↓
wait
```

分布式环境里一个慢 storage node、一次 network jitter，就会放大整个 transaction latency。

Aurora 的思考是：

> **数据库修改 page 时，最小、最本质的信息是什么？**

不是完整 page。

而是：

```
redo record
```

例如：

```
Page P
offset 128
old = 100
new = 90
```

于是：

> **为什么不只复制 redo？**

这就是 Aurora 真正开始的地方。

---

# Part 2：放进整个 6.824 知识地图

到这里课程路线大致是：

```
RPC / Threads
      ↓
GFS
      ↓
Primary / Backup
      ↓
Raft
      ↓
ZooKeeper
      ↓
CRAQ / More Replication
      ↓
============================
Aurora
Cloud Replicated Database
============================
      ↓
Frangipani / Cache Consistency
      ↓
Distributed Transactions
      ↓
Spanner
```

Aurora 是一座很重要的桥：

```
Replication Protocol

        ↓

Distributed Storage

        ↓

Real Relational Database
```

---

### Aurora vs Raft

Raft 的问题：

> 一群 replica 怎样对一串 operation 的顺序达成一致？

```
Leader:

index 1: Put(x,1)
index 2: Put(y,2)
index 3: Put(x,3)
```

Raft 重点是：

```
Leader Election
Log Agreement
Commit
State Machine Replication
```

Aurora 的问题不同：

> **数据库应该复制什么？谁生成 page？谁负责 repair？谁负责 crash recovery？什么时候 transaction 可以 ACK？**

Aurora 已经有一个 single writer 来产生有序 LSN。

所以正常 I/O 路径不需要让六个 storage replica 对：

```
“下一个操作到底是什么？”
```

运行一遍 Raft。

Writer 已经决定了顺序：

```
LSN 100
LSN 101
LSN 102
```

storage 的主要任务变成：

```
把这些已经排序的 redo
可靠保存到 quorum
```

这一点极其重要。

2018 年 Aurora follow-up paper 更明确地说：Aurora 在正常 I/O、commit 等场景中尽量利用 quorum、monotonic log ordering 和 local transient state，避免传统分布式 consensus 协议的大量额外协调。[Amazon Science](https://www.amazon.science/publications/amazon-aurora-on-avoiding-distributed-consensus-for-i-os-commits-and-membership-changes?utm_source=chatgpt.com)

所以：

```
❌ Aurora = storage 层跑 Raft

更接近：

Single Writer orders redo
        ↓
Quorum persists redo
        ↓
Storage replicas repair asynchronously
```

当然，**single writer 必须被严格保证**。failover 时必须 fencing 旧 writer；否则 split brain 一样会破坏系统。

---

### Aurora vs 2PC

这是另一个特别容易混淆的点。

2PC：

> 一个 transaction 涉及多个独立 participant，大家如何一起 Commit / Abort？

例如：

```
Transaction
   |
   +--- Shard A
   |
   +--- Shard B
```

Aurora 这里：

```
redo record
   |
   +--- storage copy 1
   +--- storage copy 2
   +--- storage copy 3
   ...
```

这是：

```
Replication / Durability
```

不是：

```
Distributed Transaction Atomic Commit
```

所以：

> **4/6 storage ACK ≠ 2PC prepare/commit。**

后面的 Lecture 12 才真正进入 distributed transaction。

---

### Aurora vs CRAQ

CRAQ：

```
Head → A → B → C → Tail
```

主要问：

> replicated object 怎么既能写得一致，又能扩展读？

Aurora：

```
          → Storage 1
          → Storage 2
Writer    → Storage 3
          → Storage 4
          → Storage 5
          → Storage 6
```

采用的是：

```
parallel fan-out
+
quorum
```

不是 chain。

为什么？

因为 cloud storage 最大问题之一就是：

```
tail latency / jitter
```

如果：

```
A → B → C → D
```

D 慢，整个 chain 受影响。

Aurora：

```
6 个里面最快 4 个 ACK
```

慢节点可以暂时落后。

---

### Aurora vs Spanner

Aurora：

```
single logical DB
single writer
shared distributed storage
multi-AZ replication
```

Spanner：

```
many shards
many Paxos groups
distributed transactions
multi-region
TrueTime
external consistency
```

简单说：

```
Aurora:
怎样把一个关系数据库的 storage 做成 cloud-native？

Spanner:
怎样让一个全球分片数据库提供强事务？
```

---

# Part 3：六个核心 Mental Models

---

### Concept 1：Compute / Storage Separation

#### 它解决什么

Compute instance 会：

```
crash
resize
replace
upgrade
failover
```

数据库数据不能跟 compute 生命周期绑死。

于是：

```
        DB Compute
      SQL / Txn / Cache
             |
             |
             v
    Distributed Storage
```

Compute 可以死。

Storage 独立存在。

---

#### 最容易误解

> ❌ Aurora compute 是 stateless。

不是。

writer 仍有大量 runtime state：

```
buffer cache
lock table
transaction state
LSN allocation state
read points
...
```

只是：

> **durable database state 不依赖 compute 本地磁盘。**

---

## Concept 2：The Log Is the Database

这是本课第一核心思想。

论文直接把一个 section 命名为：

> **THE LOG IS THE DATABASE**

Aurora DB tier 不把 dirty pages 写入 storage。

它主要发：

```
redo log records
```

Storage：

```
redo
 ↓
redo
 ↓
redo
 ↓
materialize page
```

论文甚至把 storage materialized page 看成由 log 推导出的缓存：为了性能，storage 会持续在后台把 redo coalesce 成 page，但从 correctness 的角度，log 才是 authoritative history。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

Mental model：

```
                 source of truth
                       |
                       v
              ordered redo stream
                       |
               +-------+-------+
               |               |
               v               v
         Page materialize    Backup
               |
               v
             Reads
```

---

#### 不是 Event Sourcing

很像：

```
Event Sourcing:

events → state
```

但不能完全等同。

Aurora redo 更接近：

```
physical / physiological DB modification
```

比如：

```
modify B+Tree page
```

而不是：

```
UserTransferredMoney
OrderPlaced
```

---

## Concept 3：Quorum

Aurora 原 paper：

```
3 Availability Zones

AZ1          AZ2          AZ3

A B          C D          E F
```

每个 segment：

```
6 copies
```

write quorum：

```
Vw = 4
```

read quorum：

```
Vr = 3
```

论文设定这一布局，是为了在一个 AZ 整体故障的同时还能考虑大规模 fleet 中持续存在的单盘/单节点/background failures。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

# Part 4：为什么是 6 / 4 / 3？

先从公式开始。

定义：

```
V  = total replicas
Vw = write quorum
Vr = read quorum
```

必须满足：

\[ V_r + V_w > V \]

为什么？

保证：

```
任何 read quorum
        ∩
任何 write quorum
        ≠ ∅
```

Aurora：

```
V = 6
Vw = 4
Vr = 3
```

所以：

\[ 4 + 3 = 7 > 6 \]

一定 overlap。

---

另一个条件：

\[ V_w > V/2 \]

这里：

\[ 4 > 3 \]

所以任意两个 write quorum 也一定 overlap。

---

### 为什么不是经典 3 replicas？

通常：

```
AZ1       AZ2       AZ3

 A         B         C
```

2/3 write。

看起来能容忍一个 AZ failure。

但是 cloud fleet 永远存在：

```
disk failure
machine maintenance
temporary network issue
SSD replacement
software upgrade
...
```

假设：

```
AZ1      AZ2      AZ3

 A        B        C
 X                 X ← AZ3 down
```

A 恰好处于 maintenance。

AZ3 又整体失败。

现在只有 B。

quorum 丢了。

Aurora 的模型：

```
AZ1        AZ2        AZ3

A B        C D        E F
```

AZ3 全挂：

```
A B        C D        X X
```

仍然：

```
4 replicas
```

write 可继续。

如果再挂一个：

```
X B        C D        X X

3 alive
```

此时：

```
不能 write：4/6 不够
仍能形成 3/6 read quorum
```

因此原论文设计目标是：

```
loss of one AZ:
    write continues

loss of one AZ + one more copy:
    data remains readable/recoverable
```

这就是为什么不是简单地问：

> “majority 是多少？”

而是问：

> **我的 failure domain 是什么？**

这是非常重要的 distributed-system engineering mental model：

```
Replica Count

不能只由：

f = number of random node failures

决定


还必须考虑：

rack
AZ
network
power
deployment
operator error

这些 correlated failure domains
```

---

## Concept 4：Segmentation

如果整个 10 TB volume 是一个 replication unit：

```
10 TB disk failure

→ repair 10 TB
```

repair 很慢。

问题不是：

```
MTTF
Mean Time To Failure
```

能否无限提高。

真正可以显著优化的是：

```
MTTR
Mean Time To Repair
```

所以 Aurora 把 volume 切小。

论文时期：

```
Volume
 |
 +--- 10 GB segment
 |
 +--- 10 GB segment
 |
 +--- 10 GB segment
```

每个 segment 有：

```
Protection Group

       AZ1       AZ2       AZ3
       A B       C D       E F
```

论文报告的设计中，10 GB segment 在 10 Gbps link 上可以很快 repair，从而显著缩小第二次故障撞进来的 vulnerable window。[College of Science](https://www.cs.usfca.edu/~mmalensek/cs677/schedule/papers/verbitski2017aurora.pdf)

现代 AWS 文档仍然描述 Aurora volume 为 **10 GiB segments、six copies、three AZs**。[AWS Documentation](https://docs.aws.amazon.com/rds/latest/auroraextendedcontent/aurora-faq-availability-and-durability.html?utm_source=chatgpt.com)

这其实与你做 Kubernetes / Cloud Infra 很类似：

> **Failure recovery 单位越小，blast radius 和 MTTR 通常越容易控制。**

---

## Concept 5：LSN 与 Durable Frontier

Aurora 不是简单：

```
4 ACK
→ transaction committed
```

真正设计精彩的地方在这里。

首先每条 redo 有：

```
LSN
Log Sequence Number
```

例如：

```
LSN 100
LSN 101
LSN 102
LSN 103
...
```

是单调递增的。

但 storage 网络可能：

```
receive 100
receive 101
receive 103

102 missing
```

所以：

```
最高看到的 LSN
```

和：

```
之前所有 redo 都完整存在
```

不是一个概念。

---

### SCL — Segment Complete LSN

某个 segment replica：

```
100 ✓
101 ✓
102 ✓
103 ✓
104 missing
105 ✓
```

那么即使它看到了 105：

```
SCL = 103
```

意思是：

> 到 103 为止，这个 PG 所需的 log 没有 hole。

Storage peers 会 gossip，找缺失记录并补洞。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

### VCL — Volume Complete LSN

提升到整个 volume：

```
VCL
=
最大的、可以保证之前所有必要 log 都 available 的 LSN
```

例如：

```
100 ... 1007 全部完整
1008 有 hole

VCL = 1007
```

---

### CPL — Consistency Point LSN

这里又有一个问题。

假设一个 mini-transaction 修改 B+Tree：

```
LSN 1005: update leaf
LSN 1006: split page
LSN 1007: update parent
```

不能只保存：

```
1005
1006
```

否则 B+Tree 处于半操作状态。

所以 Aurora 给 MTR 标一个：

```
Consistency Point
```

MTR 的最后一个 log record 是一个 CPL。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

### VDL — Volume Durable LSN

定义：

> **最高的、同时满足 complete + consistency boundary 的 LSN。**

近似：

```
VDL =
highest CPL <= VCL
```

例如：

```
CPL:

1000
1005
1010

但

VCL = 1007
```

那么：

```
VDL = 1005
```

虽然：

```
1006
1007
```

可能也存在，

但它们不构成完整 atomic mini-transaction boundary。

所以 recovery 时只能承认：

```
<= 1005
```

这就是：

```
complete
        ≠
durable-consistent
```

论文正是通过 VCL、CPL、VDL 区分这些状态。[College of Science](https://www.cs.usfca.edu/~mmalensek/cs677/schedule/papers/verbitski2017aurora.pdf)

---

# Part 5：真正的 Write Happy Path

假设：

```
BEGIN;

UPDATE account
SET balance = 90
WHERE id = 1;

COMMIT;
```

---

### Step 1：DB Engine 修改 Buffer Pool

```
Writer

Buffer Page:

balance = 100

       ↓

balance = 90
```

---

### Step 2：产生 redo

例如：

```
LSN 2001
Page 42
offset X
100 → 90
```

不是发送整个：

```
16 KB page
```

而是发送 redo。

---

### Step 3：根据 page 所属 PG 路由

```
LSN 2001

      ↓

Protection Group 17

AZ1          AZ2          AZ3

S1 S2        S3 S4        S5 S6
```

---

### Step 4：并行发送六份

```
              S1 ← redo
              S2 ← redo
Writer ─────→ S3 ← redo
              S4 ← redo
              S5 ← redo
              S6 ← redo
```

Storage node 的 foreground path 主要是：

```
receive log
    ↓
persist log
    ↓
ACK
```

其它事情：

```
find log holes
peer gossip
coalesce page
backup
GC
scrub
```

都是异步 background work。论文明确指出 storage pipeline 中真正位于 foreground latency path 的主要是接收和 durable persist 两步。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

### Step 5：4 个 ACK

```
S1 ✓
S2 ✓
S3 ✓
S4 ✓
S5 ...
S6 ...
```

达到：

```
4/6
```

这个 log 可以认为：

```
hardened / durable
```

原 paper 的 write path 就是把 batch 发给全部六份，然后等待 4/6 ACK。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

# Part 6：但是 4 ACK ≠ Transaction COMMIT

这是很多人第一次学 Aurora 最容易混乱的地方。

假设：

```
T1:

redo A  LSN=100
redo B  LSN=101
redo C  LSN=102
commit LSN = 102
```

现在：

```
100 已经 quorum ✓
101 已经 quorum ✓
102 还没 quorum
```

VDL：

```
101
```

那么：

```
T1 不能 ACK commit
```

---

### Transaction commit 条件

论文给出的逻辑是：

```
VDL >= transaction.commitLSN
```

才能：

````
Client ← COMMIT OK
``` :chatgpt-content-reference{index="12"}


---

## Timeline

```text
time ─────────────────────────────────────────→

Client:
      COMMIT ------------------------------------------ wait

Writer:
      redo 100
      redo 101
      redo 102(commit LSN)
          │
          ├──────────────→ Storage
          │
          │     100 quorum ✓
          │     101 quorum ✓
          │     102 quorum ✓
          │
          │     VDL → 102
          │
          └────────────────────────→ COMMIT OK

Client:
                                         ← OK
````

注意 worker thread 不必在那里同步睡死。

它可以：

```
register waiting transaction
        ↓
go process other request
```

专门的 commit handling 在 VDL 前进后 ACK 客户端。

这就是 paper 所谓 asynchronous commit processing。

---

# Part 7：如果这里 crash 呢？

现在逐个制造 failure。

---

### Failure #1：只有两个 storage 收到 redo

```
S1 ✓
S2 ✓
S3 ✗
S4 ✗
S5 ✗
S6 ✗
```

没有 4/6。

所以：

```
not durable
not committed
```

writer crash 后可以丢。

正确。

---

### Failure #2：4 copies 已经收到，但 client 还没收到 COMMIT OK

```
storage quorum ✓
VDL advanced ✓

Writer
   |
   X crash

Client didn't receive ACK
```

这是什么状态？

答案：

> Transaction **可能已经 commit**。

这是 RPC 世界经典的：

```
unknown outcome
```

Client 看到：

```
timeout
```

不能推出：

```
transaction aborted
```

这是 RPC lecture 中的：

```
at-least-once
duplicate request
ambiguous outcome
```

在数据库 API 中重新出现了。

应用如果盲目：

```
retry INSERT/payment
```

就可能重复执行业务操作。

所以 application-level：

```
idempotency key
transaction ID
unique constraint
deduplication
```

仍然非常重要。

---

# Part 8：Storage Node 到底在干什么？

Aurora Storage Node pipeline：

```
         redo records
              |
              v
      +---------------+
      | Incoming Queue|
      +-------+-------+
              |
              v
        persist log
              |
              +--------→ ACK writer
              |
              v
        sort / group
              |
              v
        detect holes
              |
       peer gossip
              |
              v
       coalesce redo
              |
              v
        materialize
         data page
              |
        +-----+-----+
        |           |
        v           v
      Backup       GC
       S3         old log
```

此外还有：

```
checksum scrub
repair
```

论文描述的八个 storage-node stage 正是这个流程。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

非常重要：

```
Foreground:

receive
persist
ACK


Background:

repair
page generation
backup
GC
scrub
```

这是一种你在 infra 系统中经常看到的原则：

> **把 correctness-critical 最小工作放进 synchronous path，其它工作尽量转成 asynchronous reconciliation。**

---

# Part 9：为什么 page 可以是“缓存”？

假设：

```
Base Page P0
```

然后：

```
redo 1
redo 2
redo 3
redo 4
```

理论上：

```
P4 =
apply(
  redo4,
  apply(
    redo3,
    apply(redo2,
      apply(redo1, P0))))
```

所以：

```
base page + redo chain
```

可以构造当前 page。

但每次从远古 page 重放百万条 redo 显然不行。

所以 storage 定期：

```
P0
 + redo1
 + redo2
 + redo3
 + redo4
     ↓
    P4
```

materialize 一个新 page。

随后：

```
old redo
```

在确认没有 reader 再需要它之后可以 GC。

所以：

> **“The log is the database” 不代表 storage 永远不保存 page。**

真正意思是：

> page 是 log 的派生 materialization，page write 不再由 DB compute 的 foreground path 驱动。

---

# Part 10：Read Path

这里 Aurora 又有一个漂亮设计：

> **正常 read 不需要每次做 3/6 quorum read。**

---

### Case 1：Buffer Pool 命中

```
Client
   ↓
Writer
   ↓
Buffer Cache
   ↓
return
```

Storage 完全不参与。

---

### Case 2：Buffer miss

Writer 知道当前：

```
VDL = 5000
```

它建立：

```
read-point = 5000
```

然后 writer 又知道各 storage segment 的：

```
SCL
```

比如：

```
S1: SCL=5010
S2: SCL=4990
S3: SCL=5030
S4: SCL=5004
...
```

它可以选：

```
S1
```

因为：

```
SCL >= read-point
```

于是：

```
Writer → S1:
give me page P as of 5000
```

不需要：

```
request S1
request S2
request S3
compare versions
```

paper 明确指出正常运行时 database 利用自身掌握的 runtime completeness information，从一个足够新的 storage segment 读取；只有 runtime state 丢失、例如 recovery 时，才需要 quorum read 来重建知识。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

这是 Aurora 很漂亮的一条原则：

> **用 writer 已经拥有的 transient knowledge 避免每次 read 都做 distributed coordination。**

---

## 关于论文里 Page LSN 的一个坑

2017 paper 在 eviction 那段写的是：

```
page LSN >= VDL
```

但这段文字长期以来被许多读者认为方向写反了。

从直觉和 durability invariant 看，应理解为：

```
page 上最新 modification
已经被 durable storage 覆盖

即：

pageLSN <= durable frontier
```

才可以放心丢掉 compute 中唯一较新的 page。

所以学习这一段时，核心不要背不等号，而要记 invariant：

> **只有确认 storage 能重新构造出至少同样新的 durable page 后，compute 才可以放心 eviction。**

论文原文确实打印成了 `page LSN >= VDL`。[UW Computer Sciences User Pages](https://pages.cs.wisc.edu/~yxy/cs839-s20/papers/aurora-sigmod-17.pdf?utm_source=chatgpt.com)

---

# Part 11：Garbage Collection 为什么又需要一个 LSN？

假设 long-running reader：

```
Reader A snapshot @ LSN 1000
```

当前数据库已经：

```
VDL = 5000
```

storage 能不能删除 1000 附近旧版本？

不能。

Reader A 还可能需要。

所以 Aurora 追踪：

```
Minimum Read Point
```

每个 Protection Group 的全局低水位：

```
PGMRPL
Protection Group Minimum Read Point LSN
```

直觉：

```
        still needed
             |
             v
--- old -----1000-----------------------5000----→

            ^
          oldest
        active read
```

只有：

```
LSN < PGMRPL
```

才知道已经没有 reader 需要那些旧版本。

于是：

```
materialize
GC old redo
GC old versions
```

这个 mental model 跟很多系统完全一致：

```
MVCC oldest active transaction
Hazard Pointer
Epoch Based Reclamation
Kafka low watermark
LSM snapshot pinning
```

思想都是：

> **在删除历史前，必须知道没有活跃消费者还可能访问历史。**

---

# Part 12：Read Replica

论文架构：

```
                  Shared Storage
                       |
             +---------+---------+
             |                   |
             v                   v
          Writer              Reader
        Buffer Pool         Buffer Pool
```

Reader 不维护自己的另一整套 storage copy。

它和 writer：

```
mount same shared storage volume
```

论文时期最多描述 15 个 reader，这一点现代 Aurora 文档仍然保留。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

但这里有个 latency 问题：

如果 Reader 每次 cache miss 才去 storage：

```
writer update
       ↓
storage redo
       ↓
page materialization
       ↓
reader fetch
```

可能慢。

所以 writer 同时向 read replica stream redo。

---

### Reader cache update

假设：

```
Reader cache 中有 Page 42
```

writer 发来：

```
redo:
Page 42
100 → 90
```

reader：

```
apply redo directly
```

如果 Page 42 不在 cache：

```
discard redo
```

以后 cache miss：

```
从 shared storage 获取最新 durable page
```

漂亮之处就在这里：

> Reader 不是完整重放整个数据库，它只对自己 cache 中存在的 page apply redo。

---

### Reader 是否 Strongly Consistent？

不一定。

Reader 异步消费 redo。

所以：

```
Writer:

COMMIT x=10 ✓


Reader:

可能暂时仍然看到 x=9
```

论文中的 replicas 是 asynchronous relative to writer；writer commit 不等待 reader。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

因此：

```
Durable on storage
        ≠
Visible on every read replica
```

这条必须记住。

---

# Part 13：Linearizability vs Transaction Isolation

特别容易混。

Aurora storage replication 主要回答：

```
durability
replication
recovery
```

不是：

```
transaction isolation
```

并发控制仍然在：

```
Database Engine
```

paper 里的 Aurora InnoDB 继续支持 MySQL 对应的 transaction isolation / snapshot behavior。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

所以至少有三个独立轴：

```
               transaction isolation
                       ↑
                       |
                       |
durability ←-----------+----------→ replication
                       |
                       |
                       ↓
                read freshness
```

例如一个 transaction 可以：

```
durably committed
```

但一个 asynchronous reader：

```
暂时看不到它
```

这完全不矛盾。

---

# Part 14：Crash Recovery —— Aurora 最漂亮的结果之一

传统 DB：

```
              crash
                X
                |
                v
         read checkpoint
                |
                v
        replay redo log
                |
                v
       rebuild dirty pages
                |
                v
        undo incomplete txn
                |
                v
             ONLINE
```

如果：

```
checkpoint 很旧
```

recovery 要 replay 很久。

于是传统数据库面临：

```
checkpoint frequently
       ↓
foreground interference

vs

checkpoint rarely
       ↓
long recovery
```

---

## Aurora 改变了什么？

Redo application 早就在：

```
storage nodes
```

不停进行：

```
redo arrives
    ↓
materialize page
    ↓
redo arrives
    ↓
materialize
```

所以 crash 后不是：

> “现在才开始 replay 最近半小时 redo。”

而是：

> “storage 一直都在做 recovery-style work。”

论文总结得非常好：

```
Traditional DB:

normal operation
       ↓
crash
       ↓
expensive special recovery phase


Aurora:

normal operation
       ↓
continuous asynchronous recovery work
       ↓
crash
       ↓
recover metadata/frontier
```

原论文报告其设计能在很高的 write rate 下做到一般低于约 10 秒的数据库 recovery；这是 2017 paper 的实验/生产经验，不应理解成今天每种 Aurora failover 都固定是 10 秒。[College of Science](https://www.cs.usfca.edu/~mmalensek/cs677/schedule/papers/verbitski2017aurora.pdf)

---

# Part 15：Writer Crash 的完整 Recovery

假设 crash 前：

```
Writer thinks:

VDL = 1000
```

但一些请求已经出去：

```
1001
1002
1003
1004
```

由于网络并发：

```
some reached quorum
some didn't
```

writer 死了。

它的 RAM runtime knowledge 也丢了。

---

### Recovery Step 1：每个 PG 做 read quorum

现在不能相信单个 storage。

于是：

```
PG 1 → read 3/6
PG 2 → read 3/6
PG 3 → read 3/6
...
```

为什么 3 可以发现任何 durable write？

因为 durable write：

```
4 nodes
```

read quorum：

```
3 nodes
```

而：

\[ 4 + 3 > 6 \]

所以：

```
read quorum
∩
write quorum

至少一个
```

一定会碰到 durable copy。

论文 recovery 正是利用每个 PG 的 read quorum 去发现任何可能已经达到 write quorum 的数据。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

### Step 2：重建 VCL / VDL

例如发现：

```
complete through 1007
```

于是：

```
VCL = 1007
```

CPL：

```
1000
1005
1010
```

于是：

```
VDL = 1005
```

---

### Step 3：truncate > VDL

```
1006
1007
...
```

可能是：

```
partial MTR
uncommitted
ambiguous tail
```

所以：

```
truncate
```

---

### Step 4：数据库可以启动

不用：

```
scan entire DB
replay huge redo interval
```

---

### Step 5：Undo incomplete transactions

注意：

> Aurora 不是完全没有 Undo。

transaction rollback 还是 DB transaction engine 的事情。

区别是：

```
redo recovery
```

大部分已经被 storage continuously amortized。

论文还指出 undo work 可以在数据库重新 online 后进行。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

# Part 16：System Model

现在正式写模型。

### Node Model

主要考虑：

```
crash / restart
disk failure
machine failure
storage node unavailable
AZ failure
```

不是 Byzantine：

```
❌ storage node 恶意伪造 redo
❌ arbitrary malicious writer
```

不在 paper failure model 内。

---

### Writer Model

核心前提：

```
one active writer
```

Writer：

```
allocates monotonically increasing LSNs
orders redo
```

这大幅简化 storage protocol。

如果同时有：

```
Writer A
Writer B
```

都无协调地生成历史：

```
A: LSN100 = x
B: LSN100 = y
```

Aurora 这套 reasoning 会直接坏掉。

所以 failover 必须 fencing old writer。

后续 2018 Aurora paper 进一步讨论了如何利用 epoch、quorum membership 等 invariant 避免 normal-path distributed consensus。[Amazon Science](https://www.amazon.science/publications/amazon-aurora-on-avoiding-distributed-consensus-for-i-os-commits-and-membership-changes?utm_source=chatgpt.com)

---

### Network Model

实际上必须容忍：

```
delay
loss
temporary disconnection
slow node
reordering
network partition
```

redo hole：

```
100
101
103
```

可以靠：

```
SCL + peer gossip
```

识别和修复。

没有假设一个严格 latency bound。

---

### Timing Model

它不是一篇形式化算法论文，因此不会像 FLP paper 一样严格定义：

```
asynchronous model
```

但正确的 mental model 是：

```
safety:
不依赖已知 message delay 上界

liveness:
需要最终有足够 quorum 能通信
```

也就是很接近实际 distributed systems 中：

```
asynchronous network
+
eventual recovery / eventual connectivity assumption
```

---

### Storage Model

redo 到 quorum 后：

```
persistent
```

storage node 本地使用持久化 SSD。

同时：

```
pages
logs
snapshots/backups
```

进一步异步处理。

---

# Part 17：最核心 State

你不需要背 Aurora 所有内部字段。

必须理解下面这些。

|State|谁维护|含义|
|---|---|---|
|LSN|Writer|每条 redo 的全局顺序|
|SCL|Segment|本 segment 的完整 redo 前缀|
|VCL|Volume recovery state|整个 volume complete 到哪里|
|CPL|DB Engine|atomic mini-transaction boundary|
|VDL|DB + storage knowledge|安全 durable consistent frontier|
|Commit LSN|Transaction|transaction 要等到哪里才能 ACK|
|PGMRPL|DB/readers|GC 的 low watermark|
|Epoch/fencing state|Control/failover machinery|防旧 writer 重新写入|

---

# Part 18：最重要的 Invariants

如果只背 Aurora 五条，我建议背下面这些。

#### Invariant 1

```
Client 收到 COMMIT OK

⇒

commitLSN <= VDL
```

否则：

```
ACK 过的事务
可能 recovery 后消失
```

灾难性 violation。

---

#### Invariant 2

```
VDL 必须落在 Consistency Point
```

否则：

```
B+Tree split 做了一半
```

也被称为 durable。

---

#### Invariant 3

```
任何 durable write quorum
必须与 recovery read quorum 相交
```

也就是：

\[ V_w + V_r > V \]

否则：

```
write 曾经 durable
```

recovery 却：

```
完全看不到它
```

会导致 committed data loss。

---

#### Invariant 4

```
At most one active writer
```

否则：

```
ordered redo history
```

不再唯一。

---

#### Invariant 5

```
GC 不能越过 oldest active read
```

否则：

```
active snapshot
```

需要的数据被删掉。

---

# Part 19：Safety 为什么成立？

现在把 proof intuition 串起来。

---

### Safety 1：已 ACK transaction 不会因为一个普通 failure 消失

Client ACK：

```
commitLSN <= VDL
```

意味着相关 redo 已进入 durable consistent prefix。

相关 redo 已满足 write quorum：

```
4/6
```

recovery：

```
3/6 read quorum
```

因为：

\[ 4 + 3 > 6 \]

所以 recovery 一定能碰到 durable history。

因此不会出现：

```
Client:
COMMIT OK

...

crash

...

Database:
“咦，这笔 transaction 从来没存在过。”
```

在系统支持的 failure envelope 内不能发生。

---

### Safety 2：不会恢复到半个 MTR

因为：

```
VDL
```

必须是：

```
CPL
```

所以：

```
Page A updated
Page B not updated
```

这种半截 structural change 不会被选成恢复边界。

---

### Safety 3：replica 不应用未 durable tail

Read replica 只 apply：

```
LSN <= VDL
```

且一个 MTR：

```
atomically apply
```

因此 replica 不应该暴露半个 mini-transaction。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

# Part 20：Liveness 为什么成立？

Safety：

> 错事不发生。

Liveness：

> 好事最终发生。

只要：

```
至少 4 / 6 reachable
```

write 可以继续。

某节点慢：

```
S1 ✓
S2 ✓
S3 ✓
S4 ✓
S5 slow
S6 slow
```

writer 不需要等最慢。

---

如果一个 AZ 挂：

```
AZ1         AZ2         AZ3
A B         C D         X X
```

仍然：

```
4
```

write 可继续。

---

如果只剩 3：

```
A B C
```

write：

```
停止
```

这是正确行为。

Aurora 选择：

```
Safety > Availability
```

而不是随便接受 3-copy write。

---

# Part 21：Network Partition

这是最典型的题。

假设 writer 只能连：

```
S1
S2
S3
```

另一侧：

```
S4
S5
S6
```

那么：

```
3 < 4
```

所以：

```
不能 commit new writes
```

安全性仍在。

---

如果另一台 DB instance 被提升成 writer 怎么办？

这是：

```
split brain
```

问题。

系统必须确保旧 writer 被 fencing。

否则：

```
Old Writer
    ↓
S1 S2 S3 S4

New Writer
    ↓
S3 S4 S5 S6
```

两个 write quorum 都可能存在。

虽然 quorum overlap：

```
S3 S4
```

但如果 storage 允许两个 writer 不受约束写不同 history，单靠 quorum 并不能自动给你一个正确数据库。

所以：

> **Quorum 不等于 Consensus。**

这也是为什么必须明确：

```
single-writer invariant
+
fencing/epochs
```

---

# Part 22：Quorum 和 Consensus 到底什么关系？

这是本课非常值得掌握的高级区别。

#### Quorum

解决：

> 我要访问多少 replica，才能保证集合有 overlap？

它是一种：

```
intersection property
```

---

#### Consensus

解决：

> 多个参与者对一个 value / order 达成唯一决定。

例如：

```
谁是 Leader？
index 10 是 A 还是 B？
```

---

Aurora normal write path 中：

```
谁决定 redo order？

Writer。
```

storage 不需要决定：

```
100 应该是什么？
```

它收到：

```
LSN=100, redo=X
```

主要回答：

> 我有没有 durable store 它？

所以 Aurora 可以大量使用：

```
single-writer ordering
+
quorum durability
+
local metadata
```

而不是每条 redo 都跑 Paxos/Raft。

---

# Part 23：Failure Matrix

|Failure|系统行为|数据 Safety|Availability|为什么|
|---|---|---|---|---|
|1 storage node crash|继续写|保持|保持|仍有 ≥4|
|2 storage copies unavailable|继续写|保持|保持|仍能 4/6|
|1 AZ failure|失去 2 copies|保持|write 保持|剩 4|
|AZ + 1 copy|剩 3|保持|write 停止；read/recovery 尚可|只有 3/6 read quorum|
|slow storage|不等它|保持|通常保持|quorum 避免 tail node|
|packet loss|出现 log hole|保持|可降速|SCL + peer repair|
|Writer crash|重建 durable frontier|保持|短暂中断|quorum recovery + VDL|
|Reader crash|重启/替换|保持|writer 不受影响|shared storage|
|partition，writer 只有 3 storage|不 commit|保持|write unavailable|4/6 requirement|
|old writer 与 new writer 同时有效|危险|可能破坏|—|必须 fencing|
|>3 relevant copies unavailable|quorum 可能丢失|超出目标 envelope|unavailable|无法可靠确定 durable history|

现代 AWS 文档仍描述 Aurora 可以在丢失两份 storage copy 时维持 write availability、丢失三份时维持 read availability。[AWS Documentation](https://docs.aws.amazon.com/rds/latest/auroraextendedcontent/aurora-faq-availability-and-durability.html?utm_source=chatgpt.com)

---

# Part 24：为什么 Aurora Recovery 比传统数据库快？

把两者并排：

```
Traditional DB
===========================

Normal:
write WAL
write pages
checkpoint

Crash:
find checkpoint
   ↓
redo
   ↓
redo
   ↓
redo
   ↓
undo
   ↓
online


Aurora
===========================

Normal:
send redo
   ↓
storage continuously:
   materialize
   repair
   backup

Crash:
reconstruct VDL
   ↓
truncate unsafe tail
   ↓
online
   ↓
undo remaining txn
```

核心思想不是：

> Aurora 找到了一个特别快的 redo algorithm。

而是：

> **Aurora 把 crash 后的一次性 recovery work，搬成了平时一直在做的 asynchronous work。**

这是非常漂亮的 systems principle：

```
one giant recovery event

          ↓

continuous background reconciliation
```

---

# Part 25：这与你熟悉的 Kubernetes Controller 很像在哪里？

不能说它们是同一个东西，但有一条设计哲学很像。

Kubernetes：

```
Desired State
     ↓
Controller
     ↓
observe
     ↓
repair continuously
```

Aurora storage：

```
Expected replicated log/page state
             ↓
          observe
             ↓
      gaps / bad block
             ↓
     gossip / rebuild
             ↓
         repaired
```

共同思想：

> **不要等 disaster 才运行一个巨大的 repair protocol；平时一直 reconcile drift。**

但是差异也很明显：

```
Kubernetes controller

主要关心 eventually reaching desired resource state


Aurora

还必须维护严格 durability / transaction invariants
```

所以不能直接把 Aurora storage 称为 Kubernetes-style eventual consistency。

---

# Part 26：与你熟悉的 Cloud Control Plane 的联系

这个架构你应该会很有感觉：

```
            Control Plane
                 |
       membership / failover
       health / replacement
                 |
                 v

              Data Plane

DB Writer → Distributed Storage
              |
              + repair
              + replication
              + backup
```

很像 cloud infra：

```
control plane:
what should exist?

data plane:
serve actual traffic
```

Aurora 同样把：

```
foreground request path
```

和：

```
fleet management
repair
backup
scrubbing
```

尽可能解耦。

---

# Part 27：与 Kafka 的联系

有一个很好的 mental model：

```
Aurora:
ordered redo log → materialized pages


Kafka ecosystem:
ordered event log → materialized state
```

但区别：

Kafka log 通常本身就是面向 application record 的 append log。

Aurora：

```
redo log
```

是 database storage engine 的内部 modification history。

而且 Aurora page materialization 直接服务 OLTP random reads。

---

# Part 28：与 Terraform 的联系

这里只能做很弱的类比。

Terraform：

```
desired state
+
current state
→ actions
```

Aurora：

```
redo history
→ current pages
```

共同点：

```
compact representation
→ derived materialized state
```

但 Terraform 不是 replicated database protocol，所以不要继续类比 correctness。

---

# Part 29：Top 5 Misconceptions

### ❌ 1. Aurora = MySQL + 6-way replication

错。

真正的创新正是在：

```
traditional MySQL storage architecture

被改变了
```

compute 不再负责：

```
write dirty page to durable disk
checkpoint-based redo recovery
storage replication
```

这些责任被大量下推。

---

### ❌ 2. 4/6 ACK 就等于 transaction commit

不完全对。

首先表示相应 redo：

```
durable/hardened
```

Transaction ACK 还依赖：

```
VDL >= commitLSN
```

---

### ❌ 3. Aurora 不需要 Consensus，所以不需要协调

错。

更准确：

> normal data I/O 可以利用 single writer + monotonic LSN + quorum + transient state 避免对每个 operation 运行完整 distributed consensus。

但：

```
writer failover
fencing
membership
control plane
```

依然需要协调。

不能把它理解为：

```
Consensus is useless.
```

---

### ❌ 4. 六份数据意味着读必须读六份

错。

正常情况：

```
read one sufficiently complete segment
```

只有 recovery 等 runtime knowledge 丢失的情况才需要 quorum read。

---

### ❌ 5. Read Replica 使用另一套 replicated disk

错。

同 Region Aurora reader：

```
共享同一个 cluster storage volume
```

reader 主要增加：

```
compute + buffer cache
```

而不是复制完整另一套 database storage。现代 Aurora 文档仍然如此描述。[AWS Documentation](https://docs.aws.amazon.com/AmazonRDS/latest/AuroraUserGuide/Aurora.Overview.StorageReliability.html?utm_source=chatgpt.com)

---

# Part 30：一个非常关键的时间线

假设：

```
T1 modifies x
T2 modifies y
```

可能：

```
time ---------------------------------------------------->

Writer:
 T1 redo: 100 ------+
                    +------→ storage
 T2 redo: 101 --------+
                      +----→ storage
 T1 redo: 102 ----------+
                        +--→ storage


Storage:

100: S1 S2 S3 S4 ACK
101: S1 S2 S3      ← only 3
102: S1 S2 S3 S4 ACK


Highest quorum LSN:
102

BUT:

101 has a hole
```

能不能说：

```
everything <= 102 durable
```

不能。

这就是为什么 Aurora 不能只维护：

```
max acknowledged LSN
```

它需要：

```
completeness frontier
```

你可以把它类比 TCP：

```
received sequence 100
received sequence 102

不意味着 cumulative ACK 可以前进到 102
```

因为：

```
101 missing
```

这是一个非常好的网络 ↔ database mental connection。

Aurora 的：

```
Complete LSN
```

和 TCP cumulative contiguous prefix 有相似直觉。

---

# Part 31：Backpressure

如果 writer 无限制产生：

```
LSN:
1
2
3
...
100000000000
```

而 storage 卡住：

```
VDL = 100
```

那么 RAM 中 outstanding state 会无限增长。

所以 Aurora 对：

```
allocated LSN - VDL
```

设置上限。

2017 paper 当时给出的：

```
LSN Allocation Limit = 10 million
```

当 storage/network 跟不上：

````
writer throttle
``` :chatgpt-content-reference{index="26"}


这其实也是你做 infra 经常遇到的：

```text
Producer
     ↓
Queue
     ↓
Consumer

Producer > Consumer

     ↓

unbounded backlog

     ↓

OOM / latency explosion
````

正确设计必须：

```
backpressure
```

---

# Part 32：Aurora 的架构全景

现在可以理解完整图了：

```
                       Client
                         |
                         v
                +-----------------+
                |   DB Writer     |
                |-----------------|
                | SQL             |
                | Query Executor  |
                | Transactions    |
                | Locking         |
                | Buffer Pool     |
                | Undo            |
                | LSN ordering    |
                +--------+--------+
                         |
                    redo only
                         |
      +------------------+------------------+
      |                  |                  |
      v                  v                  v
     AZ1                AZ2                AZ3

   +------+           +------+           +------+
   | S1   |           | S3   |           | S5   |
   +------+           +------+           +------+

   +------+           +------+           +------+
   | S2   |           | S4   |           | S6   |
   +------+           +------+           +------+

                  4 / 6 write

Storage:

redo persistence
      ↓
hole detection
      ↓
peer repair
      ↓
page materialization
      ↓
backup
      ↓
GC
      ↓
scrubbing


      Shared Storage Volume
             ↑       ↑
             |       |
          Writer   Readers
```

论文 Figure 5 的 bird's-eye architecture 正体现了 DB compute、RDS/control functions、storage fleet 与 backup 的解耦。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

# Part 33：Paper Problem

论文：

**Amazon Aurora: Design Considerations for High Throughput Cloud-Native Relational Databases**, SIGMOD 2017。[Amazon Science](https://www.amazon.science/publications/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases?trk=article-ssr-frontend-pulse_little-text-block)

作者面对的核心观察：

> Cloud 中 compute 和 storage 解耦以后，高吞吐数据库的主要 bottleneck 可能从 disk/compute 转移到 network。

于是问题：

```
怎样既获得：

high durability
multi-AZ
replication
repairability

又避免：

network amplification
sync stalls
tail latency
slow recovery
```

---

## Previous Approach

近似：

```
Traditional MySQL
       +
network block storage
       +
mirroring / standby
       +
backup
```

结果：

```
same information
written many ways

×

replication
```

---

## Key Insight

整篇论文压缩成一句：

> **Don't replicate database pages and all their physical I/O; replicate the minimal redo stream and make the storage system understand how to turn redo back into pages.**

也就是：

```
smart database
+
dumb block storage
```

改成：

```
DB compute
+
smart database-aware storage
```

---

# Part 34：Evaluation

论文 Table 1 的 SysBench write-only 实验中：

```
Mirrored MySQL:
7.4 DB-node I/Os / transaction

Aurora:
0.95
```

同一组论文实验 30 分钟中报告：

```
780,000 transactions

vs

27,378,000 transactions
```

作者把这总结为该特定实验设置下约 35 倍 transaction throughput，以及约 7.7 倍更少的 DB-node I/O。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

必须正确理解：

> 这不是“任何 workload Aurora 都比 MySQL 快 35 倍”。

这是：

```
paper benchmark
+
specific mirrored architecture
+
specific hardware/workload
```

主要是用来证明：

> **减少 network/I/O amplification 确实释放了原来的瓶颈。**

论文还展示了在它的 SysBench 实验里，随着 connection 增长 Aurora write throughput 的扩展性，以及其 shared-storage reader 的低 replica lag。[Amazon Science](https://cdn.amazon.science/dc/2b/4ef2b89649f9a393d37d3e042f4e/amazon-aurora-design-considerations-for-high-throughput-cloud-native-relational-databases.pdf)

---

# Part 35：Trade-offs / Limitations

Aurora 没有免费午餐。

### 1. Storage 变得很复杂

你把：

```
redo processing
recovery
repair
page materialization
backup
GC
scrub
```

都推到 distributed storage。

DB compute 简单了一部分。

Storage infrastructure 复杂了很多。

---

### 2. Single writer 是关键 simplifying assumption

这让：

```
ordering
```

非常简单。

但也意味着：

```
write scaling
```

不是简单：

```
add 100 writer nodes
```

就完成。

---

### 3. 六份 storage 很贵

收益：

```
durability
availability
AZ failure tolerance
low tail latency
```

代价：

```
storage/network infrastructure
```

---

### 4. Reader 可以 stale

所以：

```
read scaling
```

和：

```
strong freshest read
```

并不是同一问题。

---

### 5. 不是 Byzantine Fault Tolerance

如果 storage node / writer：

```
maliciously fabricates data
```

这套 quorum proof 不成立。

---

### 6. 不是 multi-region transaction protocol

三 AZ：

```
within one Region
```

和全球：

```
US
Europe
Asia
```

完全不是同一个 latency/failure problem。

---

# Part 36：What aged well？

到 2026 年，从 AWS 当前文档看，Aurora 的几个核心架构思想依然非常清晰地存在：

````
compute / storage separation
shared cluster volume
3 AZ
six storage copies
10 GiB segment / protection groups
up to two-copy loss without write outage
up to three-copy loss without read outage
storage self-healing
up to 15 reader instances
``` :chatgpt-content-reference{index="31"}


也就是说，论文最核心的 insight 没有过时：

> **database-aware disaggregated storage。**

---

# What changed？

现代 Aurora 已经加入很多 2017 paper 没有讨论的东西，例如：

```text
Aurora PostgreSQL
Aurora Global Database
Serverless
newer failover/cache mechanisms
cross-region replication
````

例如现在的 Aurora Global Database 使用 storage-based cross-Region replication 来提供全球读和 DR。[AWS Documentation](https://docs.aws.amazon.com/rds/latest/auroraextendedcontent/aurora-features-availability.html?utm_source=chatgpt.com)

但学习 Lecture 10 时，先全部忽略。

你应该理解的是：

```
SIGMOD'17 architecture
```

而不是背 AWS 产品功能清单。

---

# Part 37：它与 6.824 Lab 的关系

没有一个：

```
“实现 Aurora”
```

的专门 Lab。

但它其实在训练你重新组合前面 Lab 的 mental model。

Raft Lab：

```
log index
commitIndex
leader
replication
recovery
```

Aurora：

```
LSN
VDL
single writer
quorum persistence
recovery
```

最值得对比：

```
Raft commitIndex
```

和：

```
Aurora VDL
```

它们绝对不是一个协议变量，但概念上都在回答：

> **ordered history 中，到哪里已经可以安全地当成稳定前缀？**

区别是：

```
Raft commitIndex:
consensus-driven replicated state machine prefix


Aurora VDL:
single-writer redo history 的 durable/consistent frontier
```

这个区别非常值得记住。

---

# Part 38：五道理解题

先自己想，再看下面答案。

#### Level 1

Aurora 6 copies：

```
AZ1 A B
AZ2 C D
AZ3 E F
```

AZ3 整体挂。

还能 write 吗？

答案：

```
能。

A B C D = 4
```

---

#### Level 2

只剩：

```
A B C
```

为什么不能让 write quorum 临时降成 3？

因为你会改变原来 quorum intersection/safety assumptions。

尤其系统不能在 partition 两边各自随意降 quorum，否则可能得到两个各自“合法”的历史。

---

#### Level 3

Transaction 的最后 redo 已经被四个 storage ACK。

是不是一定可以立即：

```
COMMIT OK
```

不是简单由这一条决定。

要看：

```
durable consistent frontier VDL
```

是否已经：

```
>= commit LSN
```

包括之前可能存在的 holes / consistency boundaries。

---

#### Level 4

如果删掉 CPL，只用：

```
VCL
```

会怎样？

可能恢复到一个：

```
mini-transaction 做了一半
```

的位置。

例如 B+Tree：

```
child split 完成
parent update 未完成
```

结构损坏。

---

#### Level 5

为什么 Aurora 正常 read 可以只读一个 storage replica，而 recovery 要读 quorum？

因为正常运行的 writer：

```
还拥有 transient runtime knowledge
```

知道：

```
哪个 storage complete 到哪个 LSN
```

crash 后这些 RAM state 消失。

新 writer 必须：

```
重新从 distributed replicas 推导 truth
```

所以做 quorum read。

这是整篇 Aurora paper 最漂亮的 ideas 之一：

> **正常路径用 transient knowledge 换 performance；crash 后再付 recovery cost 重建 knowledge。**

---

# Part 39：Problem → Solution Chain

这是本 Lecture 最值得保存的一张图：

```
Cloud DB 需要 high availability
            ↓
       Replicate storage
            ↓
       传统 DB I/O × replication
            ↓
     Network amplification 太大
            ↓
       只发送 redo log
            ↓
   Storage 自己 materialize page
            ↓
      ==================
       THE LOG IS DB
      ==================
            ↓
   但 storage node 会失败 / 变慢
            ↓
      6 copies / 3 AZ
            ↓
        4/6 write
        3/6 read
            ↓
   但 cloud 有 correlated failure
            ↓
      AZ-aware placement
            ↓
   但坏一大块 repair 太慢
            ↓
       10 GB segments
            ↓
         reduce MTTR
            ↓
 redo 可能 lost / reordered / holes
            ↓
       LSN + SCL + gossip
            ↓
  “看到最新 LSN”不等于完整
            ↓
            VCL
            ↓
   complete 不等于 atomic
            ↓
       CPL / mini-txn
            ↓
            VDL
            ↓
 transaction 什么时候 commit？
            ↓
      commitLSN <= VDL
            ↓
传统 crash recovery replay 很慢
            ↓
 storage 持续 background apply redo
            ↓
     crash 只重建 frontier
            ↓
          fast recovery
```

如果这一整条链你能自己讲出来：

> Lecture 10 基本就真的理解了。

---

# Part 40：30 秒版本

面试官问：

> Aurora 这篇论文主要讲什么？

你可以回答：

> Aurora 重新设计了 cloud relational database 的 storage boundary。传统数据库把 WAL、dirty pages、double writes 等 I/O 都放在 DB engine，然后底层再 replication，会产生严重 network amplification。Aurora 的核心思想是让 compute tier 只发送 ordered redo log，把 redo processing、page materialization、replication、repair、backup 和大部分 recovery 下推到 distributed storage。它把数据分成小 segment，每个 segment 六副本跨三个 AZ，使用 4/6 write quorum 和 3/6 read quorum。通过 LSN、completeness frontier 和 durable consistency frontier，writer 能异步判断 transaction 何时 durable，而 storage 持续在后台 apply redo，因此 crash 时不需要传统数据库那种大规模 redo recovery。

---

# Part 41：3 分钟版本

完整一点：

```
Aurora 的起点不是 Consensus，
而是 cloud database 的 network bottleneck。

传统 MySQL 一个 transaction 会产生：
redo、page write、double write、binlog、
checkpoint 等多种 I/O。

一旦 storage 又做 replication，
这些 I/O 被进一步放大。

Aurora 因此只让 DB writer
向 storage 发送 redo。

Storage 理解 redo，
并在后台：
materialize pages、
repair gaps、
backup、
GC、
scrub。

为了容错，每个 10GB segment
被复制 6 份，
2 copies × 3 AZ。

Writes 等 4/6，
recovery/read quorum 是 3/6。

4+3>6，
所以 recovery quorum
一定能碰到任何 durable write。

Writer 给 redo 分配单调 LSN，
storage 使用 complete frontier
判断有没有 holes。

因为数据库内部 MTR 不能被切半，
又加入 consistency point，
因此形成 VDL，
也就是安全 durable prefix。

只有：

commitLSN <= VDL

才能 ACK transaction。

normal read 时，
writer 知道哪个 storage
已经 complete 到 read point，
因此通常读一个 replica 就够，
不必每次 quorum read。

crash 后 runtime knowledge 丢失，
才通过 read quorum 重建 VDL。

因为 storage 平时就在不断
apply redo 和 materialize page，
crash 后不需要传统 checkpoint
之后的大量 foreground redo recovery。

这就是 Aurora 的核心。
```

---

# Part 42：深入版本压缩

```
Problem
  ↓
Cloud replication 放大传统数据库 I/O，
network 成为瓶颈

Model
  ↓
single writer
crash/recovery failures
unreliable/variable-latency network
persistent distributed storage
3 AZ

Architecture
  ↓
DB compute
    |
    | ordered redo
    v
distributed smart storage

Replication
  ↓
6 replicas
2 × 3 AZ
4-write / 3-read

Storage unit
  ↓
small segment
Protection Group
fast repair / low MTTR

Ordering
  ↓
LSN

Completeness
  ↓
SCL
VCL

Atomicity boundary
  ↓
CPL

Durable consistent prefix
  ↓
VDL

Commit
  ↓
commitLSN <= VDL

Read
  ↓
buffer cache
or one sufficiently complete
storage replica

Replica
  ↓
same shared volume
async redo into cache

Recovery
  ↓
read quorum
rebuild VDL
truncate unsafe tail
online
undo incomplete txn

Safety
  ↓
quorum intersection
single writer
consistent durable frontier
fencing
safe GC

Liveness
  ↓
4/6 reachable → writes
3/6 reachable → reads/recovery
slow replicas bypassed
async repair

Trade-off
  ↓
storage complexity
6-copy cost
single-writer design
reader staleness
control-plane/fencing complexity
```

---

# Part 43：最终知识网络

把 Aurora 挂到你的 Distributed Systems Mental Model 上：

```
                       Distributed Systems
                              |
             +----------------+----------------+
             |                                 |
        Replication                       Transactions
             |                                 |
     +-------+--------+                    2PC / OCC
     |                |                         |
    Raft             CRAQ                    Spanner
     |                |
     |                |
Consensus       Replicated Storage
     |                |
     |                v
State Machine       Aurora
Replication           |
                      |
          +-----------+-----------+
          |           |           |
       Quorum      Redo Log    Segmentation
          |           |           |
          |           |           |
       4W / 3R       LSN         MTTR
                      |
          +-----------+-----------+
          |                       |
         VCL                     CPL
          |                       |
          +-----------+-----------+
                      |
                     VDL
                      |
              Durable Commit
                      |
                Fast Recovery
```

把它和 Raft 放在脑子里时，可以最后压缩成这一组区别：

```
Raft
================================================
谁决定操作顺序？
→ distributed replicas 通过 consensus

安全稳定前缀是什么？
→ committed log prefix

replicas 做什么？
→ apply same ordered state-machine commands


Aurora
================================================
谁决定 redo 顺序？
→ single DB writer

安全稳定前缀是什么？
→ VDL

storage replicas 做什么？
→ persist redo quorum
   repair gaps
   materialize pages

为什么不用每次 consensus？
→ write ordering 已由 single writer 提供
```

**如果要把这一 Lecture 再压成一句真正值得长期记住的话：**

> **Aurora 展示了一个很重要的 distributed-systems design principle：不要机械地把单机数据库复制六遍；重新寻找真正的 source of truth，把同步路径缩减到最小的 durable information——redo log——再让分布式 storage 在后台异步完成 materialization、repair、backup 和 recovery。**

这正是 Lecture 10 从“学一个 AWS 产品”上升到“学 distributed system architecture”的地方。
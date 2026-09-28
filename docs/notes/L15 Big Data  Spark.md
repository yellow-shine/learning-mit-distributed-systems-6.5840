## MIT 6.824 Lecture 15 — Big Data: Spark

先把整节课压缩成一句话：

> **Spark 的核心问题不是“怎么把计算分到很多机器”，而是：当大规模计算需要反复复用中间结果时，怎样既把数据留在内存里获得高性能，又在 Worker 随时可能 crash 的情况下提供 Fault Tolerance，而不必把所有中间结果不断复制或写入 Distributed Filesystem。**

Spark 给出的关键答案是：

```
Immutable Partitioned Data
        +
      Lineage
        ↓
丢数据时重新计算
而不是总是复制数据
```

这就是理解这节 Lecture 的主轴。

原始 Spark/RDD 论文把目标明确放在 **iterative algorithms** 和 **interactive data mining** 上，因为传统 MapReduce 跨 job 复用数据通常要依赖 stable distributed storage，产生大量 I/O、serialization 和 replication 成本；RDD 则记录 coarse-grained transformations，也就是 lineage，用重算换取更低的正常运行成本。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf?utm_source=chatgpt.com)

---

# Part 1：这节课到底想解决什么问题？

### 1.1 如果只能记住一个问题

假设你有 1 TB 训练数据，要运行 Logistic Regression：

```
Training Data
      |
      v
Iteration 1
      |
      v
Iteration 2
      |
      v
Iteration 3
      |
     ...
      |
Iteration 100
```

每一轮都要扫描基本相同的 Training Data。

在单机上，这其实很好解决：

```
Disk
 |
 | read once
 v
RAM
 |
 +--> iteration 1
 +--> iteration 2
 +--> iteration 3
 +--> ...
```

只要：

```
data = load()
for i in range(100):
    train(data)
```

数据一直在 RAM。

---

### 1.2 分布式环境为什么变难？

现在数据大到一台机器放不下：

```
                    Dataset
                       |
       +---------------+---------------+
       |               |               |
       v               v               v
    Worker A         Worker B        Worker C

     RAM A             RAM B           RAM C
```

很自然：

```
Iteration 1
Iteration 2
Iteration 3
```

继续重用这些内存数据。

性能很好。

但是突然：

```
Worker B
   X
 crash
```

B 内存里的：

```
partition #17
partition #18
partition #19
```

全没了。

这正是 distributed memory 最大的问题：

> **RAM 很快，但 RAM 所在机器不是可靠存储。**

---

### 1.3 最 naive 的方案

最容易想到：

```
每完成一步
   ↓
写 HDFS
   ↓
下一步再读
```

于是：

```
HDFS
 |
 v
Job 1
 |
 v
HDFS
 |
 v
Job 2
 |
 v
HDFS
 |
 v
Job 3
```

这正是经典 MapReduce 很强的一点：

```
每个 Job 是相对独立的
中间结果已经 durable
Worker 坏掉很好恢复
```

但对 iterative computation 太昂贵：

```
RAM
 ↓
serialize
 ↓
network
 ↓
distributed filesystem
 ↓
replication
 ↓
disk / memory
 ↓
network
 ↓
deserialize
 ↓
RAM
```

下一轮又来一次。

原始 Spark paper 就是针对这种数据复用问题提出 RDD；论文实验中，100 GB、100-node 的 Logistic Regression 后续迭代，Spark 报告了相对 Hadoop 25.3× 的加速，其重点不是“CPU 算得更快”，而是避免反复 I/O、serialization/deserialization 和 framework overhead。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

---

### 1.4 另一个 naive 方案：replicate RAM

你可能说：

```
partition P

Worker A: P
Worker B: P replica
Worker C: P replica
```

这当然可以。

但是意味着：

```
100 TB working set
replication factor = 2

→ 200 TB memory
```

而且每生成新 intermediate result：

```
CPU
 |
 v
new result
 |
 +----network----> replica
```

network bandwidth 和 memory capacity 都非常昂贵。

Spark 的问题因此变成：

> 有没有一种方式，不保存第二份数据，却仍然能恢复第一份？

答案非常漂亮：

```
不保存：

P 的第二份 copy

而保存：

P = map(filter(parent_partition))
```

如果 P 丢失：

```
parent_partition
      |
    filter
      |
     map
      |
      v
     P
```

重新算。

这就是 **Lineage-based Fault Tolerance**。

---

# Part 2：放进整个 6.824 知识地图

你之前大量课程都在研究：

```
                         Fault Tolerance
                               |
                +--------------+-------------+
                |                            |
           保存 State                   重建 State
                |                            |
       Replication / Log                  Lineage
                |                            |
       Raft / Paxos / GFS                 Spark
```

这是这一 Lecture 最值得加入你的 Distributed Systems Mental Model 的部分。

---

### Raft 和 Spark 到底有什么区别？

Raft 问：

> “如果一个重要 state 改变了，我怎么确保它不会因为机器 crash 而消失？”

比如：

```
bank_balance = $100
```

这是 externally generated state。

你不能 crash 后说：

```
重新算一次用户以前所有行为好了。
```

因此：

```
command
   ↓
replicated log
   ↓
majority
   ↓
commit
```

---

Spark 则大量处理 derived state：

```
raw data
   |
 filter
   |
   v
 A
   |
 map
   |
   v
 B
```

B 本身不是不可替代的事实。

只要：

```
raw data
+
filter definition
+
map definition
```

还存在，

B 就可以重新产生。

所以：

```
Raft:
    preserve the result

Spark:
    preserve the recipe
```

这是一个非常重要的区别。

---

### GFS/HDFS 与 Spark

它们也不是竞争关系。

更准确：

```
          HDFS
     durable source data
            |
            v
           RDD
            |
       transformation
            |
            v
           RDD
            |
       transformation
            |
            v
           RDD
```

HDFS：

> **可靠地保存 bytes。**

Spark：

> **可靠地执行 computation。**

HDFS 使用 replication 保存 block。

Spark 可以使用 lineage 重建 derived partitions。

因此：

```
Distributed Storage
        +
Distributed Computation
```

是上下层关系。

---

### MapReduce 与 Spark

MapReduce：

```
Map
 ↓
Shuffle
 ↓
Reduce
 ↓
Durable output

下一 Job
 ↓
重新读
```

Spark：

```
Transformation
      ↓
Transformation
      ↓
Transformation
      ↓
Action
```

并且：

```
intermediate RDD
      |
    persist()
      |
      v
    memory
```

最大的 conceptual difference 不只是 API 更丰富。

而是：

> **Spark 把一个多阶段 computation 表达成一个 dependency DAG，并允许中间 dataset 在 memory 中被复用。**

---

### Spark 与 Sharding

Sharding 回答：

> 一个 dataset 怎么横向拆？

Spark RDD 同样：

```
RDD
 |
 +--- partition 0
 +--- partition 1
 +--- partition 2
 +--- partition 3
```

但是：

```
Database Shard
```

通常是长期拥有 mutable authoritative state。

而：

```
RDD Partition
```

通常是 immutable computation result。

两者都 partition，但语义完全不同。

---

### Spark 与 Distributed Transactions

2PC / Spanner：

```
Transaction modifies:
 A
 B
 C

必须保证：
all commit
or
all abort
```

Spark：

```
RDD A
 ↓ map
RDD B
 ↓ filter
RDD C
```

核心问题不是 atomic commit，而是：

```
如何调度
如何重算
如何处理 data dependency
```

所以不要把：

```
DAG atomicity
```

误认为 transaction atomicity。

Spark 并没有因为 RDD 是 fault tolerant，就自动提供 database ACID semantics。

---

# Part 3：建立核心 Mental Model

这一课我建议你只抓 7 个概念：

```
1. RDD
2. Partition
3. Lineage
4. Transformation vs Action
5. Lazy Evaluation
6. Narrow vs Wide Dependency
7. Stage / Task / Shuffle
```

把这 7 个搞懂，Spark 核心已经掌握了大半。

---

## Concept 1：RDD

### 它解决什么？

我们需要一种 distributed in-memory abstraction：

```
既能 parallel compute
又能 recover from worker failures
```

---

### 一句话

**RDD = Resilient Distributed Dataset，是一个 immutable、partitioned、可通过 lineage 重建的数据集合。**

注意：

> RDD ≠ 一大块真的一直驻留在 RAM 的数据。

这是最常见误解之一。

当前 Spark 文档仍然把 RDD 描述为 partitioned、parallel-operable、fault-tolerant collection，并允许显式 `persist()` 来复用。[Apache Spark](https://spark.apache.org/docs/latest/rdd-programming-guide?utm_source=chatgpt.com)

---

### 例子

```
RDD users

partition 0:
Alice
Bob

partition 1:
Charlie
David

partition 2:
Eve
Frank
```

你执行：

```
adults = users.filter(lambda u: u.age >= 18)
```

逻辑上：

```
users
  |
 filter(age >= 18)
  |
  v
adults
```

Spark 不一定现在就真的计算 `adults`。

它首先建立：

```
adults depends on users
using filter(age >= 18)
```

---

## Concept 2：Partition

RDD 真正 fault recovery 的单位，不是“整个 dataset”，而通常是：

> **Partition**

假设：

```
RDD X

P0  P1  P2  P3
```

分别位于：

```
W1: P0
W2: P1
W3: P2
W4: P3
```

如果 W3 crash：

```
P2 lost
```

Spark 不必：

```
recompute whole RDD
```

理想情况下只需要：

```
recompute P2
```

这也是为什么 partition-level dependency 如此重要。

---

## Concept 3：Lineage

这是整篇 paper 的灵魂。

假设：

```
A = textFile(...)
B = A.filter(...)
C = B.map(...)
D = C.reduceByKey(...)
```

Lineage：

```
 A
 |
filter
 |
 B
 |
 map
 |
 C
 |
reduceByKey
 |
 D
```

Spark 不只是知道：

```
C exists
```

还知道：

```
C 是如何产生的。
```

于是：

```
C partition 7 lost
```

可以沿 dependency graph 倒推：

```
C7
 ↑
B7
 ↑
A7
```

然后：

```
A7
 ↓ filter
B7
 ↓ map
C7
```

---

### Lineage 和 Replication 的根本区别

Replication：

```
保存 value：

C7 = [......]
```

Lineage：

```
保存 recipe：

C7 = map(filter(A7))
```

可以理解成：

```
Replication:
    store state

Lineage:
    store provenance
```

---

## Concept 4：Transformation vs Action

Spark operation 基本分成两类。

#### Transformation

例如：

```
map
filter
flatMap
join
groupByKey
```

产生新的 RDD：

```
RDD A
   |
  map
   |
RDD B
```

---

#### Action

例如：

```
count
collect
save
reduce
```

需要产生真正结果，因此触发 computation。

原始 paper 描述 scheduler 在 Action 发生时检查目标 RDD 的 lineage graph，并据此构造 stages 执行。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

---

## Concept 5：Lazy Evaluation

代码：

```
a = sc.textFile(...)
b = a.filter(...)
c = b.map(...)
```

这里通常没有立即把全部：

```
a → b → c
```

算出来。

更像：

```
Driver:

"I know how to calculate c."
```

然后：

```
c.count()
```

才说：

```
"I need c now."
```

Scheduler 再从：

```
c
↑
b
↑
a
```

生成 execution plan。

---

### 为什么 Lazy 很重要？

因为这样 Spark 可以看到：

```
整个 dependency graph
```

并：

```
pipeline transformations
avoid unnecessary materialization
schedule by locality
divide stages
```

而不是每写一行 transformation 就启动 cluster job。

---

## Concept 6：Narrow Dependency

这是整节课另一个核心。

假设：

```
Parent RDD

P0    P1    P2
 |     |     |
map   map   map
 |     |     |
C0    C1    C2
```

这是典型 narrow dependency。

直觉：

> **一个 child partition 只需要少量特定 parent partitions；更关键地，从 paper 的定义看，一个 parent partition 不会被多个 child partitions 广泛共享。**

Spark 当前 `NarrowDependency` API 也把它描述为 child partition 依赖少量 parent partitions，并指出它支持 pipelined execution。[Apache Spark](https://spark.apache.org/docs/latest/api/java/org/apache/spark/NarrowDependency.html?utm_source=chatgpt.com)

---

比如：

```
map
filter
```

可以直接：

```
read P0
 ↓
map
 ↓
filter
 ↓
result
```

都在一台机器上一条 pipeline 做。

不需要中间全局同步。

---

## Concept 7：Wide Dependency

现在：

```
RDD:

P0:
(a,1)
(b,1)

P1:
(a,1)
(c,1)

P2:
(b,1)
(c,1)
```

执行：

```
groupByKey
```

目标：

```
a → all a
b → all b
c → all c
```

那么：

```
         P0
       / | \
      /  |  \
     v   v   v
    A    B    C

         P1
       / | \
      /  |  \
     v   v   v
    A    B    C
```

一个 parent partition 的数据可能发送到多个 child partitions。

这就是：

```
Wide Dependency
```

需要：

> **Shuffle**

原始 paper 明确把 `map/filter` 列为 narrow，而 `groupByKey`、未 co-partition 的 join 列为 wide；wide dependency 需要跨节点 shuffle。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

---

## Narrow vs Wide 为什么这么重要？

不是为了分类考试。

它直接决定三个事情：

```
                Narrow             Wide
                   |                 |
                   v                 v
Execution       pipeline          shuffle
                   |
Recovery       local-ish        potentially broad
                   |
Stage          same stage       stage boundary
```

这是 Spark scheduler 的核心。

---

# Part 4：System Model / Assumptions

Spark 和 Raft paper 很不同。

它不是在给：

```
asynchronous message-passing system
+ formal consensus protocol
```

因此不要硬套 Raft 那套 system model。

但我们仍然可以把 assumptions 说清楚。

---

### Node Model

主要考虑：

```
crash failure
```

例如：

```
Worker dies
process crashes
machine disappears
```

不是 Byzantine model。

它不试图处理：

```
Worker maliciously returns wrong answer
RAM silently fabricates arbitrary records
```

---

### Worker State

Worker 上可能有：

```
cached RDD partitions
shuffle outputs
running tasks
```

很多都是：

```
ephemeral / reconstructable
```

Worker crash 后通常不要求：

```
recover exact in-memory process state
```

而是：

```
rerun computation
```

---

### Driver State

原始 RDD paper 的 Spark scheduler/driver 保存：

```
RDD lineage
stage metadata
task metadata
partition locations
```

原论文明确指出，当时实现**并不容忍 scheduler failure**；作者认为复制 RDD lineage graph 可以扩展来解决，但当时系统没有实现。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

这非常重要。

所以：

```
Worker Fault Tolerance
≠
entire Spark application magically fault tolerant
```

---

### Network Model

实践上 Spark 必须容忍：

```
packet loss
connection reset
worker unreachable
temporary delays
```

通过：

```
task retry
re-fetch
recompute
```

恢复。

但 paper 没有像 Raft 那样定义：

```
messages may arbitrarily reorder
network asynchronous
majority eventually connected
```

因为它不是 consensus protocol。

---

### Timing Model

更准确的描述是：

> **Spark 不依赖已知网络 latency bound 来证明结果正确。**

Scheduler 会实际使用 timeout / failure detection 等工程机制。

但：

```
timeout
```

更多决定：

```
什么时候 retry
```

而不是像 consensus safety proof 那样参与 correctness。

---

### Storage Model

可以分三层：

```
1. Reliable external storage
      HDFS / object storage
            |
            v
2. Reconstructable distributed data
      RDD partitions
            |
            v
3. Temporary computation state
      task / shuffle / buffers
```

Spark lineage 最终必须有一个 anchor。

比如：

```
HDFS input
```

如果：

```
source data itself permanently disappeared
```

lineage 当然救不了。

---

### Failure Assumption

核心假设可以写成：

> **只要 authoritative input 和 lineage 仍存在，并且有足够计算资源重新执行 lost computation，Spark 就可以重新生成丢失的 derived partitions。**

超出 assumption：

```
source storage corrupt
driver + lineage permanently lost
user function nondeterministically produces different result
all required resources permanently unavailable
```

则无法保证恢复到相同 logical dataset。

---

# Part 5：算法一步一步执行

我们做一个 Word Count。

```
lines = sc.textFile("logs")
words = lines.flatMap(split)
pairs = words.map(lambda w: (w, 1))
counts = pairs.reduceByKey(add)
counts.count()
```

先不要看 API。

看 computation graph：

```
      HDFS
        |
     textFile
        |
      lines
        |
     flatMap
        |
      words
        |
       map
        |
      pairs
        |
   reduceByKey
        |
      counts
```

---

## Happy Path

假设 HDFS 数据有三个 partitions：

```
A0   A1   A2
```

执行：

```
flatMap
+
map
```

可以：

```
A0 → words0 → pairs0
A1 → words1 → pairs1
A2 → words2 → pairs2
```

于是：

```
Worker 1:
A0 → flatMap → map

Worker 2:
A1 → flatMap → map

Worker 3:
A2 → flatMap → map
```

这些 transformations 可以 pipeline。

---

然后来了：

```
reduceByKey
```

假设：

```
hash(word) % 3
```

决定 reducer partition。

那么：

```
pairs0 ─┬──> R0
        ├──> R1
        └──> R2

pairs1 ─┬──> R0
        ├──> R1
        └──> R2

pairs2 ─┬──> R0
        ├──> R1
        └──> R2
```

这就是：

```
Shuffle
```

因此：

```
Stage 1
------------------
textFile
flatMap
map

       |
       | SHUFFLE
       v

Stage 2
------------------
reduceByKey
```

原始 scheduler 就是这样利用 lineage：尽量把 narrow transformations pipeline 在一个 stage 内，而 wide/shuffle dependency 成为 stage boundary。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

---

# Part 6：用 Timeline 看 Spark

假设：

```
Driver
Worker A
Worker B
Worker C
```

执行：

```
counts.collect()
```

timeline：

```
time →

Driver:
 build DAG
    |
    |-------- Stage1 Task P0 ------> Worker A
    |-------- Stage1 Task P1 ------------> Worker B
    |-------- Stage1 Task P2 ------------------> Worker C

Worker A:
 read A0 → flatMap → map → shuffle blocks

Worker B:
 read A1 → flatMap → map → shuffle blocks

Worker C:
 read A2 → flatMap → map → shuffle blocks

Driver:
             wait Stage1
                    |
                    v
             launch Stage2

Worker A:
 fetch blocks from A/B/C → reduce partition 0

Worker B:
 fetch blocks from A/B/C → reduce partition 1

Worker C:
 fetch blocks from A/B/C → reduce partition 2
```

注意：

> Spark 的 execution synchronization 经常发生在 shuffle boundary。

不是：

```
每个 map 都 barrier 一次
每个 filter 都 barrier 一次
```

---

# Part 7：Failure Scenario

现在逐个制造 failure。

---

### Failure #1：运行 map 时 Worker crash

```
Worker B:

A1
 ↓
flatMap
 ↓
map
 ↓

CRASH
```

Task 没完成。

怎么办？

```
Driver
  |
  | retry Task(A1)
  v
Worker D
```

重新执行。

非常简单。

---

## Failure #2：cached partition 丢失

假设：

```
A
|
map
|
B.persist()
|
filter
|
C
```

B：

```
Worker 1: B0
Worker 2: B1
Worker 3: B2
```

Worker 2 crash：

```
B1 gone
```

Spark：

```
A1
 ↓ map
B1
```

重新生成。

这里不需要复制：

```
B1
```

因为它是 derived state。

---

## Failure #3：Shuffle output 丢失

这是复杂点。

```
Stage 1

P0 → shuffle0
P1 → shuffle1
P2 → shuffle2

            ↓

Stage 2
```

假设：

```
Worker B:
shuffle1

X crash
```

Stage 2 需要的：

```
shuffle1
```

消失。

怎么办？

Spark 必须回到 producing stage：

```
P1
 ↓
rerun map-side task
 ↓
regenerate shuffle1
```

然后 downstream task 再 fetch。

原始 paper 特别指出，wide dependency 的 intermediate records 会 materialize 在 parent nodes 上，类似 MapReduce 的 map outputs；如果这些 shuffle outputs 丢了，对应 upstream partitions 会被重新提交计算。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

这就是为什么：

> **wide dependency 的 recovery 更贵。**

---

## Failure #4：Lineage 太长

假设：

```
A
↓
B
↓
C
↓
D
↓
E
↓
...
↓
Z
```

现在：

```
Z17 lost
```

如果 reconstruction 需要：

```
A17
 ↓
...
 ↓
Z17
```

可能非常昂贵。

于是引入：

```
Checkpoint
```

比如：

```
A
↓
B
↓
C
↓
D
↓
E  <--- checkpoint
↓
F
↓
...
↓
Z
```

恢复：

```
checkpoint E
     ↓
     F
     ↓
     ...
     ↓
     Z
```

而不用一直回到 A。

原始论文特别指出，long lineage、尤其带 wide dependencies 的 lineage 更值得 checkpoint；简单 narrow lineage 从 reliable storage 重算可能比复制整个 RDD 更便宜。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

---

# Part 8：RDD 内部到底记录什么？

原始 RDD paper 给出的 abstraction 非常值得记住。

一个 RDD 核心上可以回答：

```
partitions()
preferredLocations()
dependencies()
iterator()
partitioner()
```

分别是：

```
partitions
    我有哪些 partition？

preferredLocations
    这些 partition 最好在哪运行？

dependencies
    我依赖哪些 parent RDD？

iterator
    给我 parent，我怎么产生自己的 records？

partitioner
    我的数据怎么 partition？
```

这五项直接出现在原论文的 RDD representation interface。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

从 distributed-systems 角度看，这非常漂亮。

因为它同时描述了：

```
Data
+
Computation
+
Dependency
+
Placement
```

---

# Part 9：State / Invariants

Spark 不是 Raft，因此没有：

```
currentTerm
votedFor
commitIndex
```

这种 protocol state。

但它仍然有重要 state。

---

### Driver Logical State

概念上：

```
RDD graph
RDD dependencies
partition metadata
partitioner metadata
stage graph
task state
cached partition locations
shuffle metadata
```

这是 control plane。

---

### Executor / Worker State

```
cached partitions
shuffle blocks
task execution state
temporary spill files
```

这是 data plane。

你可以类比自己熟悉的：

```
Kubernetes control plane
        |
        v
desired/execution metadata

worker node
        |
        v
actual workloads / local data
```

但注意 Spark Driver 不是 Kubernetes API Server，也没有默认用 Raft replication 保存整个 execution state。

---

## 最重要的 Invariants

### Invariant 1

```
RDD 本身逻辑上 immutable。
```

不是：

```
RDD[3] = new_value
```

而是：

```
RDD2 = transform(RDD1)
```

为什么重要？

因为如果同一个 dataset 可以被任意 remote mutation：

```
T1 modifies P1
T2 modifies P2
T3 reads midway
```

恢复时就需要记录：

```
mutation ordering
write conflicts
concurrent updates
```

很快就变成：

```
distributed shared memory
database
replication/logging
```

而不是 lineage。

---

### Invariant 2

对可重算的 deterministic transformation：

```
same parent partitions
+
same transformation
→
same logical child partition
```

这是 lineage recovery 能成立的关键。

---

### Invariant 3

某个 lost partition 的恢复可以由 dependency graph 确定它所需的 ancestors。

即：

```
lost C7
```

不应该需要：

```
整个 cluster memory snapshot
```

才能知道怎么恢复。

---

## 如果 Immutable 被去掉会怎样？

假设：

```
RDD partition P

time 1:
x = 10

Client A:
x = 20

Client B:
x = 30
```

Worker crash。

恢复时：

```
P = ?
```

仅仅知道：

```
P derived from parent
```

已经不够。

你必须知道：

```
A update happened?
B update happened?
order?
atomic?
durable?
```

也就是说：

> **Spark 用限制 programming model 换取简单高效的 fault tolerance。**

这正是 RDD paper 的关键设计哲学：相比 general distributed shared memory 的 fine-grained mutable updates，RDD 使用 coarse-grained transformations，使 lineage-based recovery 成为可能。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf?utm_source=chatgpt.com)

---

# Part 10：Correctness

Spark 的 correctness 不像 Raft：

```
Election Safety
Leader Completeness
State Machine Safety
```

这里更适合从 computation semantics 看。

---

## Safety

理想情况下要保证：

> **Worker failure 和 retry 不应该改变一个 deterministic Spark computation 的 logical output。**

假设：

```
C7 = map(B7)
```

C7 丢失：

```
B7
 ↓ same map
C7'
```

要求：

```
C7' logically equals C7
```

为什么？

因为：

```
B7 unchanged
map unchanged
```

---

### 真正危险的地方：side effect

假设：

```
rdd.foreach(lambda x: charge_credit_card(x))
```

Task 做了一半：

```
charge Alice ✓
charge Bob   ✓
```

然后 Worker crash。

Spark retry：

```
charge Alice AGAIN
charge Bob AGAIN
```

RDD output 可以重算。

但是 external side effect：

```
不是天然 exactly-once
```

这是一个非常重要的工程边界：

> **Spark 的 recomputation fault tolerance 针对 computation result，不自动让外部 side effects exactly-once。**

---

## Nondeterminism 也危险

例如 transformation 使用：

```
current_time()
random()
external mutable database lookup
```

第一次：

```
P7 = [A, B]
```

失败后重算：

```
P7 = [A, C]
```

Lineage 的：

```
same recipe
```

不再意味着：

```
same result
```

原论文甚至在 `sample` transformation 中记录 per-partition random seed，使重算保持 deterministic sampling。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

这很能说明设计哲学。

---

## Liveness

Spark 的 liveness 更像：

> 如果 scheduler 仍然工作、source data 可以访问、cluster 最终有可用资源，而且失败不是无限持续的，那么失败 task 可以被重新执行，job 最终继续推进。

如果：

```
Worker A dies
Worker B dies
Worker C dies
```

但：

```
Worker D available
```

可以 retry。

如果：

```
所有 Worker 永久不可用
```

当然没有 liveness。

---

## Network Partition

例如：

```
Driver -------- X -------- Worker A
                  partition
```

Driver 可能把 A 当成 unavailable，然后：

```
rerun task elsewhere
```

Safety：

```
只要 transformation deterministic
重复执行本身通常没有问题
```

Liveness：

```
只要 eventually 有 reachable resources
可以继续
```

这和 Raft 非常不同。

Raft partition 时要问：

```
哪边有 majority？
```

Spark worker partition 时通常问：

```
这个 task 能否在别处重跑？
```

---

# Part 11：Failure Matrix

|Failure|Spark 怎么处理|Logical data 安全？|Job 是否继续？|关键机制|
|---|---|---|---|---|
|Task crash|retry task|通常是|是|deterministic recomputation|
|Executor crash|丢失其 cache/shuffle；重新计算|通常是|是|lineage|
|Cached RDD partition lost|从 parent 重建|是|是|lineage|
|Shuffle block lost|重跑 producing task|是|通常是|stage recomputation|
|Packet loss|RPC/fetch/task retry|是|通常是|retry|
|Temporary partition|worker 被视作 unavailable，工作重调度|是|通常是|recomputation|
|Source HDFS replica failure|依赖 storage 层恢复|取决于 HDFS|取决于 storage|storage replication|
|所有 source data 丢失|无法 lineage recovery|否|否|无法解决|
|Driver/scheduler crash（原论文）|当时不支持完整恢复|不一定|否|paper limitation|
|nondeterministic transformation|重算可能不同|不一定|可运行但语义危险|application responsibility|
|external side effect 后 task retry|side effect 可能重复|外部状态不一定|是|需要应用级 idempotency|

---

# Part 12：最容易混淆的三个层级

Spark 初学最容易把下面三个东西混在一起：

```
RDD
Stage
Task
```

可以这样记。

---

### RDD

是：

```
logical dataset abstraction
```

例如：

```
users
adults
countryCounts
```

---

### Stage

是：

```
execution DAG 的一段
```

通常由 shuffle boundary 分隔：

```
Stage 1
map
filter
map

------- shuffle -------

Stage 2
reduceByKey
map
```

---

### Task

是：

```
Stage × Partition
```

例如 Stage 1 有 100 partitions：

```
Task 0
Task 1
...
Task 99
```

可以并发跑在很多 Executors 上。

Mental model：

```
Job
 |
 +----- Stage 1
 |       |
 |       +---- Task(P0)
 |       +---- Task(P1)
 |       +---- Task(P2)
 |
 +----- Stage 2
         |
         +---- Task(P0)
         +---- Task(P1)
```

---

# Part 13：Job 又是什么？

一个 Action 通常触发一个 Job。

例如：

```
rdd.count()
```

：

```
Action
  ↓
Job
  ↓
Stages
  ↓
Tasks
```

因此：

```
Application
   |
   +--- Job
   |     |
   |     +--- Stage
   |           |
   |           +--- Task
   |
   +--- Job
```

不要把：

```
Transformation = Job
```

一一对应。

不是。

---

# Part 14：为什么 Shuffle 是 Spark 的“危险地带”？

如果你今后做 Spark performance troubleshooting，只记一句：

> **先找 Shuffle。**

为什么？

因为 narrow pipeline：

```
P0
 ↓
map
 ↓
filter
 ↓
map
```

可能完全在同一个 executor 流式完成。

而 shuffle：

```
Worker A ─┬────────> Worker D
          ├────────> Worker E
          └────────> Worker F

Worker B ─┬────────> Worker D
          ├────────> Worker E
          └────────> Worker F
```

需要：

```
serialization
network
buffer
sort/hash
disk spill
fetch
deserialization
```

当前 Spark 文档也直接把 shuffle 描述为昂贵操作，涉及 network、serialization、disk I/O，并在内存不足时 spill。[Apache Spark](https://spark.apache.org/docs/latest/rdd-programming-guide?utm_source=chatgpt.com)

---

## 一个关键例子：groupByKey vs reduceByKey

假设：

```
(a, 1)
(a, 1)
(a, 1)
...
```

`groupByKey`：

```
所有 values
     |
   network
     |
同一个 reducer
```

而能做 map-side aggregation 的：

```
reduceByKey
```

可以先：

```
Worker 1:
(a,1) + (a,1) + ...
       ↓
     (a,100)
```

再 shuffle：

```
(a,100)
```

而不是 100 个 records。

所以 Spark performance 很大程度上其实是：

> **减少跨 partition 数据移动。**

---

# Part 15：Partitioner 为什么重要？

假设你有：

```
Users:
(userID, profile)

Orders:
(userID, order)
```

执行：

```
join by userID
```

如果：

```
Users partitioning:
hash(userID)

Orders partitioning:
random
```

就得 shuffle。

但是如果两边：

```
P0 = hash(userID) % N == 0
P1 = hash(userID) % N == 1
...
```

采用相同 partitioner：

```
Users P0  + Orders P0
Users P1  + Orders P1
```

可以 local join。

因此 join 并不是永远 wide。

原论文特别指出：如果两个 parent 已经使用相同 hash/range partitioner，join 可以形成 narrow dependencies；否则一般需要 wide dependency。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

这个知识点在 MIT 考题里也出现过：join 是否 wide，关键之一就是 inputs 是否已经按 join key 使用相同 partitioning。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q22-2.pdf?utm_source=chatgpt.com)

---

# Part 16：为什么 Spark 不直接做 Distributed Shared Memory？

想象 API：

```
shared_array[71239] = 5
shared_array[88123] += 2
```

1000 台机器都可以 fine-grained mutation。

现在 Fault Tolerance 怎么做？

你需要记录：

```
write #1
write #2
write #3
...
```

或者 replication：

```
every write
 ↓
send replica
```

还要处理：

```
Concurrent writes
ordering
atomicity
consistency
locks
recovery
```

你突然又回到了：

```
Distributed Database
Distributed Shared Memory
Replication Protocol
```

RDD 的设计选择是：

```
不要支持 arbitrary mutable shared state。
```

而支持：

```
map entire dataset
filter entire dataset
join datasets
group datasets
```

这就是论文所谓：

> coarse-grained transformations。

限制能力：

```
fine-grained mutation ✗
```

换来：

```
compact lineage
cheap fault recovery
simple parallelism
```

这其实是一个很经典的系统设计原则：

> **强大的 abstraction 不一定更好；限制 abstraction 有时可以大幅简化 distributed-system semantics。**

---

# Part 17：Cache / Persist 到底在做什么？

假设：

```
data = sc.textFile(...)
features = data.map(expensiveFunction)

for model in models:
    evaluate(features, model)
```

没有 persist：

```
model 1:
data → expensiveFunction → evaluate

model 2:
data → expensiveFunction → evaluate

model 3:
data → expensiveFunction → evaluate
```

重复计算。

如果：

```
features.persist()
```

第一次：

```
data
 ↓
expensiveFunction
 ↓
features
 ↓
CACHE
```

后面：

```
CACHE → model 2
CACHE → model 3
```

---

但注意：

```
persist()
```

不等于：

```
make this durable forever
```

Partition 仍可能因 executor loss 被丢掉。

区别是：

```
persist:
    optimization

lineage:
    fault recovery
```

这是必须分开的两个概念。

---

## Persist 与 Checkpoint 的区别

非常重要。

### Persist

目的：

```
避免重复计算
```

存的通常是：

```
fast reusable copy
```

它丢了可以 lineage recompute。

---

### Checkpoint

目的之一：

```
cut lineage
```

原来：

```
A
↓
B
↓
C
↓
D
↓
E
```

checkpoint C：

```
A
↓
B
↓
C === durable checkpoint

C
↓
D
↓
E
```

恢复 E 时可以从 C 开始。

Mental model：

```
persist
=
performance optimization

checkpoint
=
recovery boundary / lineage truncation
```

---

# Part 18：MapReduce → Spark 的 Problem → Solution Chain

这是这节课最值得记住的 problem chain。

```
Big Data
   ↓
单机内存/CPU 不够
   ↓
Partition across workers
   ↓
Worker 会 failure
   ↓
MapReduce:
materialize intermediate results
to stable distributed storage
   ↓
Fault tolerance 很强
   ↓
但是 iterative workloads
反复读写 stable storage
   ↓
太慢
   ↓
把 data cache 在 RAM
   ↓
Worker crash → partition lost
   ↓
Replication?
   ↓
memory/network cost 高
   ↓
关键观察：
intermediate data 是 derived state
   ↓
Immutable RDD
   ↓
记录 Lineage 而不是复制全部数据
   ↓
Partition lost
   ↓
recompute only lost partition
   ↓
但 dependency 有不同性质
   ↓
Narrow Dependency
→ pipeline / cheap recovery
   ↓
Wide Dependency
→ shuffle / expensive recovery
   ↓
Scheduler 根据 DAG 分 Stage
   ↓
lineage 过长
   ↓
Checkpoint
```

这就是 Spark 这篇 paper 的完整逻辑。

---

# Part 19：Paper Reading

对应的经典论文是：

**Resilient Distributed Datasets: A Fault-Tolerant Abstraction for In-Memory Cluster Computing**，Zaharia 等人，NSDI 2012。[USENIX](https://www.usenix.org/conference/nsdi12/technical-sessions/presentation/zaharia?utm_source=chatgpt.com)

---

### Paper Problem

已有 distributed computing frameworks 很擅长：

```
one-pass batch jobs
```

但是不擅长：

```
iterative machine learning
interactive querying
```

因为这些 workload 会不断：

```
reuse intermediate datasets
```

而传统框架需要把数据经 stable storage 传递。

---

## Previous Approach

大致两个方向：

#### MapReduce

```
fault tolerant
general
but repeated stable-storage I/O
```

#### Specialized iterative systems

例如 Pregel 等：

```
fast for specific patterns
but programming model specialized
```

论文希望：

> 找一个足够 general，又能高效利用 distributed memory 的 abstraction。

---

## Key Insight

整篇论文可以压缩成：

> **Derived data 不一定需要 replication；如果它由 deterministic coarse-grained transformations 产生，那么记录 lineage 往往就足以 fault recover。**

这个 insight 比 Spark API 本身重要得多。

---

## Design

```
                Driver
                  |
              RDD DAG
                  |
             Scheduler
                  |
       +----------+----------+
       |          |          |
     Worker     Worker     Worker
       |          |          |
   partition   partition   partition
```

RDD 描述：

```
partition
dependency
compute function
preferred location
partitioner
```

Scheduler 把 DAG 转成：

```
Stages
  ↓
Tasks
```

---

## Evaluation

原始论文重点展示：

```
Iterative Logistic Regression
K-Means
PageRank
Interactive Query
Fault Recovery
```

论文的 headline 结果包括 iterative workloads 相对 Hadoop 的显著加速，以及 worker failure 后只重建 lost RDD partitions。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

但你看论文 evaluation 时不要只记：

```
20x
25x
40x
```

真正应该得出的结论是：

```
如果 workload:
    computation light
    reuse high
    serialization/I/O heavy

那么 keeping reusable representation in memory
价值特别大。
```

反之如果：

```
每条 record 要算 10 秒 CPU
```

磁盘那几百毫秒的成本比例可能就不那么大。

---

## Limitations

原始 RDD abstraction 不特别适合：

```
fine-grained mutable shared state
```

例如：

```
一个 distributed KV store
每秒随机修改亿万个 keys
```

这里你更可能需要：

```
database
key/value store
log
replication
```

而不是：

```
每次创建一个新 RDD
```

论文自己也明确把 RDD 定位成 restricted shared-memory abstraction，而不是 general mutable distributed shared memory。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf?utm_source=chatgpt.com)

---

## What aged well?

到今天依然很重要的是：

```
DAG execution
partitioned datasets
lineage/provenance
lazy transformations
shuffle boundaries
data locality
cache/persist
retry/recompute
```

现代 Spark 上层更多使用：

```
DataFrame
Dataset
SQL
Catalyst
```

但底层 distributed-dataflow mental model 依然非常有价值。

---

## What changed?

你现在生产环境用 Spark，通常不会主要写：

```
RDD API
```

而会更多写：

```
Spark SQL
DataFrame
Dataset
```

并依赖更高级 optimizer。

所以：

> **这篇论文最值得学的不是 2012 年的 API，而是“restricted dataflow + lineage 如何构造 fault tolerance”的系统设计思想。**

---

# Part 20：和 Raft / Replication 做一次真正比较

这个表非常重要：

|问题|Raft|Spark|
|---|---|---|
|保护什么|authoritative mutable state|derived computation state|
|核心手段|Replication|Recompute|
|保存什么|command/log/data copies|lineage + source data|
|Majority|核心|通常不需要|
|Consensus|核心|Worker recovery 不需要|
|Mutable state|是|RDD immutable|
|Worker lost data|follower catch-up|recompute partition|
|Partition|minority cannot commit|task may run elsewhere|
|核心 correctness|replicated state consistency|deterministic computation|

最值得你记的是：

```
不可重新产生的 state
        ↓
replicate

可以便宜重新产生的 state
        ↓
recompute
```

实际系统当然经常混合两者。

---

# Part 21：和 Kubernetes 联系

你做 Platform/Infra，可以这样看 Spark on Kubernetes：

```
Kubernetes Control Plane
        |
      etcd
        |
       Raft
        |
  authoritative cluster state


Spark Driver
        |
      DAG
        |
  Executors / Pods
        |
   derived partitions
```

这是非常漂亮的一组对比。

---

### Kubernetes 为什么不能像 Spark 一样简单？

比如：

```
Secret
Deployment spec
Lease
CRD state
```

这些是 externally supplied authoritative state。

Pod 死后可以重建：

```
Pod
```

但不能随便重建：

```
用户到底提交了什么 Deployment spec？
```

所以：

```
etcd → replication
```

---

而一个 Spark Executor：

```
Pod
 |
 +--- RDD P3
 +--- RDD P17
```

Pod 死了：

```
RDD partition
```

经常可以 lineage recompute。

所以：

```
Spark Executor resembles disposable compute
```

非常契合 Kubernetes。

---

## 一个更深的类比：Kubernetes Controller

Kubernetes 也有一点：

```
desired state
      ↓
reconciliation
      ↓
recreate derived resources
```

比如：

```
Deployment
   ↓
ReplicaSet
   ↓
Pod
```

Pod 丢了：

```
Controller recreates Pod
```

这跟 Spark 的思想有一点共同性：

> **有些 state 不值得直接保护，因为可以从更 authoritative 的 state 重建。**

不过区别也很明显：

```
Kubernetes:
desired-state reconciliation

Spark:
deterministic data computation lineage
```

不要把它们当成同一机制。

---

# Part 22：和 Terraform 联系

Terraform 也有一个很好的 mental connection：

```
Terraform configuration
       +
current cloud state
       ↓
      plan
       ↓
 desired mutations
```

资源对象不是由 Terraform lineage 像 RDD 那样 deterministic compute 出来，所以它不是 Spark。

但共同的系统设计思想是：

> **区分 authoritative description 与 reconstructable/executable consequences。**

例如你做 region onboarding 时：

```
Terraform code
       ↓
VPC
subnets
route tables
```

如果本地 temporary computation 消失，你不会复制：

```
terraform plan 的每个中间结构
```

因为可以重新生成。

但真正的：

```
RDS data
customer metadata
```

不能简单靠重新执行 Terraform 恢复。

这其实就是：

```
derived state
vs
authoritative state
```

---

# Part 23：和 Kafka 联系

Kafka 的 log：

```
P0
P1
P2
```

也是 partitioned。

但 Kafka partition：

```
authoritative event history
```

所以：

```
replication
ISR
leader/follower
```

Spark partition：

```
derived dataset fragment
```

所以：

```
lineage recomputation
```

两者都叫 partition，但千万别混为一谈。

---

# Part 24：和 Redis Cache 联系

Redis Cache：

```
DB
 |
 v
Cache
```

cache 丢了：

```
DB
 ↓
reload
```

这个思想其实和 Spark 非常接近：

```
authoritative source
       ↓
derived fast representation
```

因此：

```
Cache:
miss → recompute/refetch

Spark:
partition lost → recompute
```

都在利用：

> **derived state 可以牺牲 durability 换 performance。**

但 Redis cache 通常没有 Spark 那样完整的：

```
DAG lineage
```

---

# Part 25：Data Locality

假设 HDFS：

```
Block 7 lives on Worker B
```

Task：

```
process Block 7
```

方案 A：

```
run task on Worker A

Worker A ← network ← Worker B data
```

方案 B：

```
run computation on Worker B
```

于是：

```
move compute to data
```

往往比：

```
move data to compute
```

便宜。

RDD 的：

```
preferredLocations(partition)
```

正是为 scheduler 提供这种 locality information。原 paper scheduler 会优先把 task 发到缓存 partition 所在节点，或者 source data 的 preferred location。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

---

# Part 26：为什么 Lineage metadata 很便宜？

假设：

```
RDD = 10 TB
```

Replication：

```
≈ another 10 TB
```

Lineage 可能只是：

```
parent RDD id
map function
dependency metadata
partition info
```

逻辑上可能 KB/MB 级 metadata，而不是复制 TB 级 values。

这就是：

```
Data size
>>
Recipe size
```

时 lineage 特别划算的原因。

但反过来：

```
recipe extremely expensive to replay
```

就应该 checkpoint / persist / replicate。

于是你可以得到一个更一般的系统设计公式：

```
Recovery strategy
≈
compare:

Cost(replicate now)

vs

Probability(failure)
×
Cost(recompute later)
```

不是严格 Spark 公式，但这是非常有用的工程直觉。

---

# Part 27：Checkpoint 的设计本质

于是 checkpoint 其实是在这个 continuum 上找平衡：

```
Replication -------------------------------- Recompute
     |                                          |
higher steady cost                         higher recovery cost

                         ^
                         |
                    checkpoint
```

如果：

```
recompute cost low
failure rare
```

偏右：

```
lineage
```

如果：

```
recompute extremely expensive
lineage very long
```

往左：

```
checkpoint
```

这不只是 Spark。

你以后看到：

```
database snapshot
Raft snapshot
stream checkpoint
ML checkpoint
VM snapshot
```

都可以从：

```
recovery work vs normal-operation overhead
```

这个维度思考。

虽然这些 checkpoint 的 semantics 并不相同。

---

# Part 28：Top 7 Misconceptions

### ❌ 1. RDD = distributed memory

不准确。

更好：

```
RDD = logical fault-tolerant distributed dataset abstraction
```

它可能：

```
在 memory
在 disk
尚未 materialize
丢了等待 recompute
```

---

### ❌ 2. Spark 的 Fault Tolerance = Replication

不是核心机制。

Spark RDD 的关键是：

```
Lineage + recomputation
```

虽然 Spark 也可以使用 replicated storage levels，current docs 也提供诸如 `_2` storage levels。[Apache Spark](https://spark.apache.org/docs/latest/rdd-programming-guide?utm_source=chatgpt.com)

---

### ❌ 3. persist() 才让 RDD fault tolerant

错。

```
Lineage
```

让它具有主要 recovery capability。

`persist()` 更多是：

```
performance
```

---

### ❌ 4. 每个 Transformation 都产生一个 Stage

错。

```
map
filter
map
```

往往：

```
one stage
```

因为是 narrow dependencies。

Stage boundary 通常来自：

```
shuffle / wide dependency
```

---

### ❌ 5. Spark 全部 computation 都在 RAM

错。

Shuffle 可以：

```
spill to disk
materialize local intermediate data
```

Current Spark 文档明确说明 shuffle 涉及内存、网络和 disk spill。[Apache Spark](https://spark.apache.org/docs/latest/rdd-programming-guide?utm_source=chatgpt.com)

---

### ❌ 6. Lineage 可以解决任意 failure

错。

例如：

```
source data permanently lost
driver metadata lost in original implementation
nondeterministic function
external side-effect duplication
Byzantine worker
```

都不是简单 lineage 能解决。

---

### ❌ 7. Spark 是 Database

不是。

Spark 很擅长：

```
distributed data processing
```

而 Database 还承担：

```
transaction
concurrent mutation
durability
indexes
constraint
isolation
query serving
```

完全不同的问题集合。

---

# Part 29：为什么这篇论文对 Distributed Systems 很重要？

因为它给你的 mental model 增加了一种新的 fault-tolerance technique：

你以前可能觉得：

```
机器会坏
↓
必须复制
```

Spark 告诉你：

```
不一定。
```

更完整应该是：

```
机器会坏
   ↓
这个 state 是否可重建？
   |
   +--- NO ---> Replicate / Persist / Log
   |
   +--- YES --> Recompute
```

再问：

```
重建贵吗？
```

如果贵：

```
checkpoint
```

于是得到：

```
authoritative state
derived state
ephemeral state
```

这种分类。

这是 Staff-level system design 非常常用的思考方式。

---

# Part 30：一个实际 Spark execution 完整走一遍

代码：

```
logs = sc.textFile("s3://logs")

errors = logs.filter(is_error)

pairs = errors.map(
    lambda x: (x.service, 1)
)

counts = pairs.reduceByKey(add)

hot = counts.filter(
    lambda x: x[1] > 1000
)

hot.collect()
```

首先 Driver 得到 logical DAG：

```
        logs
          |
       filter
          |
       errors
          |
         map
          |
        pairs
          |
    reduceByKey
          |
       counts
          |
       filter
          |
         hot
          |
       collect
```

dependency：

```
logs
  |
 narrow
  |
errors
  |
 narrow
  |
pairs
  |
 WIDE
  |
counts
  |
 narrow
  |
hot
```

生成：

```
Stage 1
===========
read logs
filter
map

     |
   shuffle
     v

Stage 2
===========
reduceByKey
filter
```

再拆 Tasks：

```
Stage 1

Task P0
Task P1
Task P2
Task P3
```

执行：

```
P0 → filter → map → shuffle blocks
P1 → filter → map → shuffle blocks
P2 → filter → map → shuffle blocks
P3 → filter → map → shuffle blocks
```

Stage 2：

```
Q0 fetch from P0/P1/P2/P3
Q1 fetch from P0/P1/P2/P3
Q2 fetch from P0/P1/P2/P3
```

如果 P2 machine crash：

```
P2 shuffle outputs lost
```

重新：

```
source partition P2
 ↓
filter
 ↓
map
 ↓
shuffle P2
```

而不是：

```
rerun P0
rerun P1
rerun P2
rerun P3
```

除非 dependency/recovery 情况要求更广泛重算。

这就是整套 machinery 最终服务的东西。

---

# Part 31：为什么 Narrow Dependency 恢复特别漂亮？

假设：

```
A0 → B0 → C0
A1 → B1 → C1
A2 → B2 → C2
```

C1 丢失。

只需要：

```
A1
 ↓
B1
 ↓
C1
```

恢复范围：

```
one dependency chain
```

而且可以并行恢复多个 lost partitions。

---

## Wide Dependency 为什么复杂？

```
A0 ─┬→ B0
    ├→ B1
    └→ B2

A1 ─┬→ B0
    ├→ B1
    └→ B2

A2 ─┬→ B0
    ├→ B1
    └→ B2
```

现在某个 map output 丢失：

```
A1-produced shuffle blocks
```

可能同时影响：

```
B0
B1
B2
```

因此 failure propagation 范围大得多。

这就是论文把 narrow/wide dependency 同时用于：

```
execution scheduling
AND
failure recovery analysis
```

的原因。[USENIX](https://www.usenix.org/system/files/conference/nsdi12/nsdi12-final138.pdf)

---

# Part 32：公式——Spark 为什么适合“重算”？

Spark 没有什么像：

```
N ≥ 2f + 1
```

这种核心公式。

但我们可以建立一个工程 cost model：

设：

```
D = dataset size
R = replication cost per byte
C = recompute cost
p = failure probability
```

Replication 的大致 expected cost：

```
Cost_rep ≈ D × R
```

它不管失败发生没有，都要支付。

Lineage：

```
Cost_lineage ≈ metadata + p × C
```

比如：

```
D = 10 TB

复制成本 = 10 TB network + 10 TB extra storage

p = 1%
recompute lost fraction ≈ 100 GB
```

这时可能：

```
lineage much cheaper
```

但如果：

```
recompute takes 8 hours
```

那么 checkpoint 就变得值得。

注意这只是教学 cost model，不是 Spark paper 的 formal equation。

---

# Part 33：和你熟悉的 Platform 系统做最终分类

你可以以后把 state 都问一遍：

```
这个 state 是 authoritative 的吗？
还是 derived？
```

例如：

```
etcd Deployment object
    authoritative
    → replicate

Kafka committed message
    authoritative
    → replicate

PostgreSQL row
    authoritative
    → WAL + replication

Redis cache entry
    derived
    → reload

Spark RDD partition
    derived
    → recompute

Kubernetes Pod
    often reconstructable from controller desired state
    → recreate

Prometheus local temporary query result
    derived
    → recompute
```

这已经超出 Spark 了。

这是这节课真正应该留在你脑子里的 Distributed Systems Mental Model。

---

# Part 34：与 6.824 Lab 的关系

Spark 没有像 Raft Lecture 那样直接对应你必须实现：

```
currentTerm
AppendEntries
commitIndex
```

但它和早期 MapReduce Lab 的思想非常直接。

---

### MapReduce Lab

你实现过类似：

```
Coordinator
Workers
Tasks
Timeout
Retry
```

核心：

```
Worker lost
    ↓
task reassigned
```

为什么 task 可以重跑？

因为：

```
Map/Reduce task
```

通常应该是 deterministic computation。

这其实已经在为 Spark 做铺垫。

---

### Spark 多走了一步

MapReduce：

```
Task graph relatively rigid
+
intermediate files
```

Spark：

```
general DAG
+
lineage
+
memory reuse
+
narrow/wide dependency
```

所以你可以把 Spark 看成把：

```
MapReduce fault-tolerant task execution
```

推广成：

```
fault-tolerant distributed dataflow
```

---

# Part 35：Debugging Strategy——生产中怎么想 Spark 问题？

如果一个 Spark Job 慢，可以按下面顺序想：

```
1. DAG 长什么样？
      ↓
2. 有多少 Stage？
      ↓
3. Stage boundary 为什么产生？
      ↓
4. 哪些 Shuffle？
      ↓
5. Shuffle data 多大？
      ↓
6. Partition 是否均衡？
      ↓
7. 有 data skew 吗？
      ↓
8. Executor memory 是否够？
      ↓
9. 是否 spill？
      ↓
10. 是否大量 recomputation？
      ↓
11. 应不应该 persist？
```

不要一上来只问：

```
CPU 不够？
```

Big Data 系统常见瓶颈可能是：

```
network
disk
serialization
skew
GC
scheduler
```

而不是 arithmetic。

---

# Part 36：一个 Data Skew 例子

比如：

```
groupBy(country)
```

数据：

```
US       90%
China     5%
Japan     2%
others    3%
```

Partition：

```
P0 US:      900 GB
P1 China:    50 GB
P2 Japan:    20 GB
P3 others:   30 GB
```

于是：

```
Task P0 ============================= 30 min
Task P1 ===                           2 min
Task P2 =                             1 min
Task P3 ==                            1 min
```

整个 Stage 必须等：

```
P0
```

所以：

> 增加机器不一定解决 skew。

这是 Spark、MapReduce、distributed SQL 都非常经典的问题。

---

# Part 37：Straggler 也是 Distributed Systems 问题

假设：

```
999 tasks = 1 minute
1 task    = 20 minutes
```

整个 job：

```
≈20 minutes
```

而不是：

```
≈1 minute
```

这说明 distributed parallel computation 的 latency 常常取决于：

```
max(task latency)
```

而不是 average。

因此：

```
straggler
skew
speculative execution
```

都是大数据系统的重要主题。

这也和你学过的：

```
tail latency
```

是一脉相承的。

---

# Part 38：Spark 和 Consensus 的边界

这是必须说清楚的。

有人可能问：

> “Spark Worker 都分布式运行，为什么不用 Raft？”

因为 Worker 并没有共同维护：

```
one replicated authoritative state machine
```

它们是在运行：

```
independent partition computations
```

假设：

```
Worker A calculates P0
Worker B calculates P1
Worker C calculates P2
```

没有需求：

```
A/B/C 必须 consensus 决定 P0 的 value
```

P0 的 value 已经由：

```
input + deterministic function
```

定义。

因此：

```
Consensus:
agree on value/history

Spark:
compute predetermined value
```

这是非常不同的问题。

---

# Part 39：Spark 和 Linearizability 的边界

RDD 没有：

```
Get(x)
Put(x)
```

这种 mutable concurrent object API。

因此通常不问：

```
这个 map 是 linearizable 吗？
```

Linearizability 适用于：

```
concurrent operations on shared object
```

RDD 通过 immutable transformations，绕开了大量这种 shared mutable-state concurrency problem。

这恰恰体现了一条很重要的系统设计原则：

> **有时候最好的 distributed consistency algorithm，是设计一个 abstraction，让你根本不需要那个 consistency problem。**

---

# Part 40：30 秒版本

面试官：

> Spark 这节 Lecture 主要讲什么？

你可以说：

> Spark 解决的是 MapReduce 对 iterative 和 interactive workloads 数据复用效率差的问题。核心 abstraction 是 immutable、partitioned 的 RDD。RDD 不必通过复制每个中间结果来获得 fault tolerance，而是记录构造它的 lineage；Worker 丢失某个 partition 时，可以根据 lineage 从 parent partitions 重算。Spark 根据 RDD dependency DAG 调度 execution，narrow dependencies 可以 pipeline，而 wide dependencies 通常需要 shuffle 并形成 stage boundary。这个设计本质上是用受限的 coarse-grained immutable programming model 换取高效的 distributed memory reuse 和 fault recovery。

---

# Part 41：3 分钟版本

可以这样讲：

> MapReduce 很适合 one-pass batch processing，因为 job 之间通过 durable distributed filesystem 传递结果，fault recovery 很简单。但 iterative ML、PageRank 或 interactive analytics 会反复使用相同 intermediate dataset，如果每轮都写 HDFS，I/O、serialization 和 replication 成本很高。
> 
> Spark 引入 RDD，也就是 immutable partitioned distributed datasets。RDD 可以 cache 在 worker memory 中，但真正让它 resilient 的不是 memory replication，而是 lineage：系统记录某个 RDD 是由哪些 parent RDD 通过什么 transformations 构造出来的。如果 executor crash 导致一个 cached partition 丢失，Spark 可以重新执行对应 computation 来恢复这个 partition。
> 
> Lineage dependency 分成 narrow 和 wide。像 map/filter 通常是 narrow，可以在同一个 worker pipeline，并且 failure recovery 只需要相关 partition；groupByKey 或没有 co-partition 的 join 通常是 wide，需要 shuffle，一个 parent partition 的数据可能流向很多 child partitions。因此 wide dependency 通常形成 Stage boundary，也是 network、disk I/O 和 failure recovery 成本的重要来源。
> 
> 当 action 发生时，Spark Driver 根据 lineage DAG 建立 Job，把 DAG 根据 shuffle boundary 分为 Stages，每个 Stage 再按 partition 创建 Tasks。Lineage 太长或恢复成本太高时，可以 checkpoint。
> 
> 从 distributed-systems 角度，Spark 最重要的 insight 是：fault tolerance 不只有 replication。一类 derived state 可以通过保存“怎么计算它”的 metadata，在失败后 recompute。这个思想的前提是 immutable、最好 deterministic 的 computation，而且 authoritative input 仍然可靠存在。

---

# Part 42：深入版本

完整 mental model：

```
Problem
  |
  v
Big Data requires distributed computation
  |
  v
Intermediate data reused across computations
  |
  v
MapReduce writes it to stable storage
  |
  v
Fault tolerant but expensive
  |
  v
Keep data in distributed RAM
  |
  v
Worker crash loses partitions
  |
  v
Could replicate everything
  |
  v
High memory/network overhead
  |
  v
Observation:
intermediate data is derived
  |
  v
RDD
Immutable + Partitioned
  |
  v
Lineage
record transformations
  |
  v
Failure
recompute lost partition
  |
  +-----------------------------+
  |                             |
Narrow                        Wide
  |                             |
pipeline                       shuffle
cheap recovery                 expensive
  |                             |
same Stage                  Stage boundary
  +-------------+---------------+
                |
                v
             Scheduler
                |
          Stage → Tasks
                |
                v
       distributed execution
                |
       long expensive lineage?
                |
                v
            Checkpoint
```

---

# Part 43：最终知识网络

把今天的知识挂在这里：

```
                       Distributed Systems
                              |
        +---------------------+----------------------+
        |                                            |
   State Services                              Data Processing
        |                                            |
   +----+------+                          +----------+---------+
   |           |                          |                    |
Replication  Transactions              MapReduce             Spark
   |           |                          |                    |
Raft/Paxos    2PC                    batch jobs              RDD
   |                                      |                    |
State Machine                         durable                Lineage
Replication                         intermediates             |
                                                           DAG
                                                            |
                                                  +---------+--------+
                                                  |                  |
                                               Narrow              Wide
                                                  |                  |
                                               Pipeline            Shuffle
                                                  |                  |
                                             cheap recovery       Stage boundary
                                                                     |
                                                                 Checkpoint
```

再把 storage 接进来：

```
HDFS / S3
durable source
    |
    v
   RDD
    |
Transformation
    |
    v
   RDD
    |
Transformation
    |
    v
   RDD
```

最终你应该把 Spark 挂在：

> **Distributed Dataflow + Recomputable State Fault Tolerance**

而不是挂在：

```
Consensus
```

下面。

---

# Part 44：五个理解检查题

你可以先遮住下面的答案自己推理。

#### Level 1

有：

```
A → map → B → filter → C
```

`C3` 丢失，A 在 HDFS。

需要重算整个 C 吗？

---

#### Level 2

有：

```
A → map → B → groupByKey → C → map → D
```

一共至少有几个 Stage？为什么？

---

#### Level 3

`B.persist()`，B 的一个 partition 因 executor crash 丢失。

是否意味着：

```
persist 失效
job 必须失败
```

？

---

#### Level 4

假设：

```
B = A.map(lambda x: (x, random()))
```

B7 丢失。

为什么 lineage reconstruction 可能不能恢复“原来的 B7”？

---

#### Level 5

现在让你设计一个类似 Spark 的系统。

有两种 state：

```
1. 用户上传的原始照片
2. 从照片生成的 thumbnail
```

你分别会优先选择：

```
Replication
还是
Lineage/recompute
```

为什么？

---

## 参考答案

#### 1

不需要。

如果 dependency 是 narrow：

```
A3
 ↓ map
B3
 ↓ filter
C3
```

通常只需要恢复相关 partition chain。

---

#### 2

通常至少：

```
Stage 1:
A → map → B

shuffle

Stage 2:
groupByKey → C → map → D
```

因为：

```
groupByKey
```

引入 wide/shuffle dependency。

---

#### 3

不是。

`persist()` 是 reuse optimization。

B partition 丢失后：

```
Lineage
```

允许从 A 重新生成。

---

#### 4

因为：

```
random()
```

是 nondeterministic。

第一次：

```
x → 0.123
```

第二次：

```
x → 0.873
```

所以：

```
same lineage
```

不意味着：

```
same value
```

除非你控制 random seed 等 nondeterministic input。

---

#### 5

原始照片：

```
authoritative / irreproducible data
→ durable storage + replication
```

Thumbnail：

```
derived state
→ usually recompute from original
```

除非 thumbnail 的：

```
recompute frequency × computation cost
```

已经大到值得 cache / persist。

---

## 最后：这节课真正应该改变你的一个直觉

在 Raft、GFS、Spanner 学多了以后，很容易形成：

```
Fault Tolerance
=
Replication
```

Spark 是一次非常重要的纠正。

更完整的模型是：

```
                 State
                   |
        +----------+-----------+
        |                      |
Authoritative              Derived
        |                      |
cannot cheaply recreate    can recreate
        |                      |
        v                      v
Replication / Log          Recompute
        |                      |
Raft / DB / Kafka           Spark / Cache
                               |
                         expensive replay?
                               |
                               v
                           Checkpoint
```

所以以后做任何 distributed system，可以先问三个问题：

```
这个 state 是谁创造的？

它是 authoritative，还是 derived？

如果丢了，是复制回来更便宜，
还是重新算出来更便宜？
```

如果你真正吸收了这三个问题，**你学到的就已经不只是 Spark，而是一种通用的 Distributed Systems 设计方法。**
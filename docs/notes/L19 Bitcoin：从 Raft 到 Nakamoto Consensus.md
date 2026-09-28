## MIT 6.824 — Bitcoin：从 Raft 到 Nakamoto Consensus

这节课最值得记住的不是 Bitcoin 的货币属性，而是一个 Distributed Systems 问题：

> **在没有可信 Leader、没有固定 membership、节点可能 Byzantine、任何人都能创建任意数量 identity 的 Internet 中，怎样让大家最终收敛到同一份 transaction history？**

Bitcoin 原论文就是从 double-spending 出发：数字签名可以证明“这笔钱确实由 owner 授权花掉”，但不能证明“owner 没有同时把它花给另外一个人”。传统系统让一个中央机构观察全部交易并决定顺序；Bitcoin 要解决的是如何去掉这个中央机构。[pdos.csail.mit.edu](https://pdos.csail.mit.edu/6.824/papers/bitcoin.pdf?utm_source=chatgpt.com)

你可以把整节课压缩成这条链：

```
Double Spend
    ↓
需要一个全局 transaction order
    ↓
没有可信中央 sequencer
    ↓
普通 majority voting 遇到 Sybil attack
    ↓
Proof of Work
    ↓
按 computation 权重竞争 block
    ↓
Hash-linked blocks
    ↓
选择 cumulative work 最大的 chain
    ↓
短暂 fork 可以存在
    ↓
随着 block 越来越深，reorg 概率指数下降
    ↓
Probabilistic Consensus / Probabilistic Finality
```

---

# Part 1：这节课到底想解决什么问题？

### 1.1 单机时代其实没有这个困难

假设一个银行数据库：

```
                   Bank DB
                     |
Alice account = $10 |
                     |
                +----+-----+
                |          |
               Bob       Charlie
```

Alice 同时发送：

```
T1: Alice -> Bob     $10
T2: Alice -> Charlie $10
```

数据库可以简单做：

```
BEGIN

check balance
deduct balance
commit T1

T2:
balance = 0
reject

COMMIT
```

这里存在一个天然的权威：

```
Database
```

它决定：

```
T1 before T2
```

还是：

```
T2 before T1
```

所以 double spending 最终其实是一个：

> **ordering problem。**

---

### 1.2 数字签名不能解决 double-spend

Alice 可以签两个完全合法的 transaction：

```
               Alice

          /               \
         /                 \
        v                   v

Tx1: Alice -> Bob     Tx2: Alice -> Charlie
Signature: valid      Signature: valid
```

Bob 验证：

```
Verify(PubAlice, Tx1.signature) == true
```

Charlie 验证：

```
Verify(PubAlice, Tx2.signature) == true
```

两个都是真的。

Cryptography 只能告诉我们：

> Alice 的确创建了 Tx1。

却不能告诉我们：

> Tx1 应该排在 Tx2 前面。

原论文正是指出：payee 可以验证 ownership signature，却无法仅凭签名确认之前没有发生 double-spend。[pdos.csail.mit.edu](https://pdos.csail.mit.edu/6.824/papers/bitcoin.pdf?utm_source=chatgpt.com)

---

### 1.3 Distributed System 为什么困难？

因为不存在全局的：

```
“谁先收到”
```

例如：

```
time →

Alice:       Tx1 created
                \
                 \-----------------> Node A

Alice:          Tx2 created
                   \
                    -----> Node B
```

结果：

```
Node A sees:

Tx1
Tx2


Node B sees:

Tx2
Tx1
```

那么：

```
first_received(NodeA) != first_received(NodeB)
```

这是你之前学 Lamport 时已经遇到的问题：

> 网络中的两个 concurrent event 并没有天然的 global order。

但是货币系统偏偏必须决定：

```
Tx1 valid
Tx2 invalid
```

或者：

```
Tx2 valid
Tx1 invalid
```

不能：

```
Node A:
Alice的钱属于Bob

Node B:
Alice的钱属于Charlie
```

永久持续下去。

---

### 1.4 最 naive 方法：大家投票

可能首先想到：

```
100 个节点

Tx1: 60票
Tx2: 40票

=> Tx1 wins
```

在 Raft 中，这很正常。

但是 Bitcoin 是 permissionless system。

攻击者 Mallory：

```
Mallory
   |
   +-- Mallory1
   +-- Mallory2
   +-- Mallory3
   ...
   +-- Mallory1,000,000
```

IP address、public key、process 都可以廉价创建。

于是：

```
one identity = one vote
```

没有意义。

这就是 **Sybil problem**。

因此 Bitcoin 必须回答：

> 如果 identity 不值钱，我们拿什么作为 vote 的权重？

答案就是：

> **Proof of Work。**

但注意：

> **PoW 的第一直觉不是“浪费算力保护 Blockchain”，而是“为一个开放 membership 的 Consensus 系统制造不可廉价伪造的 voting weight”。**

这是理解整节课最重要的一步。

---

# Part 2：它在整个 6.824 知识地图中的位置

不同年份 6.824/6.5840 的 lecture 编号有所变化。例如 2026 课程表把 SUNDR 放在 Lecture 19、Bitcoin 放在 Lecture 20；核心课程位置没有变化：Bitcoin 出现在传统 Consensus、Transactions、Replication 等主题之后，并紧邻 Byzantine Fault Tolerance。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

整体知识链可以画成：

```
RPC / Threads
      ↓
机器之间如何通信

Replication
      ↓
多台机器维护相同 state

Primary/Backup
      ↓
谁决定 operation order？

Raft / Paxos
      ↓
Crash Fault 下 Consensus

State Machine Replication
      ↓
所有 replica 执行相同 ordered commands

ZooKeeper
      ↓
构造 Coordination Service

2PC / Distributed Transactions
      ↓
跨多个 state machine 原子更新

Spanner
      ↓
Replication + Transactions + Time

SUNDR / Fork Consistency
      ↓
服务器本身可能恶意怎么办？

Bitcoin
      ↓
甚至连 replica membership 都不存在，
节点可能 Byzantine，
如何产生公共历史？

PBFT / BFT
      ↓
另一种 Byzantine Consensus 模型
```

---

## Bitcoin 和 Raft 到底是什么关系？

它们都试图解决：

```
Node A
Node B
Node C
...
```

最终应对 operation 的顺序形成共同观点。

比如：

```
Tx1
Tx2
Tx3
```

但是 system model 差别巨大。

||Raft|Bitcoin|
|---|---|---|
|membership|固定/已知|open|
|identity|一个 server 一票|identity 没价值|
|fault|crash|Byzantine|
|leader|明确 Leader|临时 PoW winner|
|vote weight|replica count|hash power|
|agreement|majority quorum|chain work|
|fork|protocol 尽量隐藏/修复|正常现象|
|finality|deterministic|probabilistic|
|commit|majority 后确定|confirmations 越多越可信|

因此你可以理解：

```
Raft:
谁有 majority replica votes？

Bitcoin:
谁代表 majority computational work？
```

但这个类比只能到这里。

Bitcoin 并不是 Raft 的 PoW 版本。

---

## Bitcoin 和 State Machine Replication

Raft：

```
        Raft Log

index 1: Put(x,1)
index 2: Put(y,2)
index 3: Put(x,3)

           ↓

Replica A state
Replica B state
Replica C state
```

Bitcoin：

```
       Blockchain

Block 100
    Tx A

Block 101
    Tx B
    Tx C

Block 102
    Tx D

        ↓

UTXO state
```

所以 Bitcoin 也可以看成：

```
ordered transactions
        ↓
deterministic validation
        ↓
ledger state
```

具有 State Machine Replication 的味道。

真正困难的是：

> 谁决定 order？

---

## Bitcoin 和 2PC

完全不同。

2PC 问：

> 已经知道参与 transaction 的几个数据库，怎样保证它们一起 commit 或 abort？

Bitcoin 问：

> 整个开放网络应该接受哪份 transaction history？

所以：

```
2PC
= atomic commitment

Bitcoin
= Byzantine/open-membership ledger consensus
```

不要把两个都看到 `commit` 就认为是同一层问题。

---

# Part 3：六个核心 Mental Models

---

## Concept 1：Double Spending 本质上是 Ordering Problem

### 它解决的问题

```
Alice has one coin.

T1: Alice -> Bob
T2: Alice -> Charlie
```

究竟哪个合法？

### 一句话定义

> Double-spend 是同一个 spendable input 被两个 conflicting transactions 消费。

真正需要决定的不是：

```
signature valid?
```

而是：

```
which transaction came first in accepted history?
```

因此 Bitcoin 最核心的数据其实不是：

```
balance
```

而是：

```
history
```

---

## Concept 2：Blockchain 本身不是 Consensus

一个 block：

```
Block N

+--------------------+
| prev_block_hash    |
| transactions       |
| timestamp-ish      |
| nonce              |
| ...                |
+--------------------+

        |
        v hash

Block N+1
```

形成：

```
B100
  ↓ hash
B101
  ↓ hash
B102
  ↓ hash
B103
```

如果修改 B100：

```
B100'
```

那么：

```
hash(B100') != hash(B100)
```

于是后面所有链接都失效。

#### Hash Chain 解决什么？

解决：

> **tamper evidence。**

你不能偷偷改过去而不被发现。

#### 它没有解决什么？

假设：

```
        B100
        /  \
       /    \
    B101A  B101B
```

两个 block 都：

```
✓ hash valid
✓ transaction valid
✓ PoW valid
```

Hash chain 无法告诉你：

> A 还是 B？

所以：

```
Blockchain
≠
Consensus
```

Consensus 还需要：

```
Proof of Work
+
fork choice
+
network propagation
```

---

## Concept 3：Proof of Work = Sybil-resistant Leader Lottery

这是整节课核心。

假设 hash：

```
SHA256(block_header)
```

要求：

```
hash < Target
```

直觉上等价于要求 hash 前面有很多 0：

```
00000000xxxxxxxxxxxxxxxx...
```

Miner 不断试：

```
nonce = 1
hash(...)

nonce = 2
hash(...)

nonce = 3
hash(...)

...
```

直到：

```
Hash(block) < target
```

因为 cryptographic hash 类似随机输出，所以没有捷径。

谁每秒能试更多 hash：

```
更大概率先找到 block
```

因此：

```
hash power
      ↓
probability of winning next block
```

这相当于：

```
Leader Election
```

但每一轮 leader 都是随机的。

---

### 为什么这解决 Sybil？

Mallory 可以创建：

```
1,000,000 public keys
```

但是如果总计算能力还是：

```
100 TH/s
```

那么不会 magically 变成：

```
1,000,000 × 100 TH/s
```

所以：

```
identity is free

computation is not free
```

PoW 把：

```
one identity one vote
```

替换成：

```
one unit computational work
≈
one unit influence
```

---

## Concept 4：Fork 是正常状态，不是异常状态

假设两个 miner 几乎同时找到 block：

```
                 B100
                /    \
               /      \
           B101-A    B101-B
```

一部分网络先看到 A：

```
Node1 -> A
Node2 -> A
```

另一部分先看到 B：

```
Node3 -> B
Node4 -> B
```

Bitcoin 不要求：

> 现在立刻选出绝对正确的 block。

而是允许：

```
temporary disagreement
```

接下来：

```
                 B100
                /    \
               /      \
           B101-A    B101-B
              |
           B102-A
```

于是 A branch accumulated more work。

节点逐渐切换到：

```
A branch
```

因此 Bitcoin 的一个巨大思想变化是：

```
Raft:
避免 committed histories 分叉

Bitcoin:
允许短暂 fork
然后概率收敛
```

---

## Concept 5：Longest Chain 实际上是 Most-work Chain

原论文通常叫：

> longest chain。

但现代 Bitcoin 更准确的 mental model 应该是：

> **chain with the greatest cumulative Proof of Work。**

不是简单数：

```
谁 block 更多
```

而是：

```
哪条 valid chain 累积 work 最大
```

因为 difficulty 可能变化。

所以我们之后统一写：

```
most-work chain
```

---

## Concept 6：Confirmation = Probabilistic Finality

Raft：

```
entry committed
```

之后安全模型满足时：

```
永远不会改变
```

Bitcoin 不是。

假设 transaction T 在：

```
B100
```

之后又出现：

```
B101
B102
B103
B104
B105
B106
```

称作越来越多 confirmations。

攻击者如果想撤销 T，需要生成：

```
             B99
             / \
            /   \
       honest   attacker
          |        |
        B100      B100'
          |        |
        B101      B101'
          |        |
         ...       ...
```

并最终让 attacker branch 累积 work 超过 honest branch。

如果 attacker hash power 小于 honest hash power：

> 落后越多，追上的概率越低。

论文明确给出了这种概率随落后 block 数指数下降的分析。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/bitcoin.pdf?utm_source=chatgpt.com)

所以 Bitcoin 没有：

```
100% final at t
```

而是：

```
1 confirmation
    ↓
some confidence

3 confirmations
    ↓
higher confidence

6 confirmations
    ↓
even higher confidence
```

这就是：

> **Probabilistic Finality。**

---

# Part 4：System Model

这一部分很重要，因为 Bitcoin 和 Raft 最大差异就是假设。

---

### Node Model

节点可能：

```
honest
Byzantine
crash
restart
leave
join
```

Byzantine 节点可以：

```
发送 conflicting transactions
隐藏 block
创建 fork
选择性发送消息
联合其他 attacker
尝试 double-spend
```

但假设不能：

```
伪造别人的 signature

找到 SHA-256 shortcut

任意制造符合 consensus rules 的钱
```

---

### Membership Model

没有：

```
members = {A,B,C,D,E}
```

任何人都可以参与。

所以：

```
N
```

甚至不需要被所有人知道。

这与：

```
Raft:
5 replicas
```

根本不同。

---

## Network Model

消息可能：

```
delay
drop
duplicate
reorder
```

节点通过 peer-to-peer 网络传播：

```
transactions
blocks
```

原论文给出的网络流程基本是：

```
new transaction broadcast
       ↓
nodes collect transactions
       ↓
miners search PoW
       ↓
winner broadcasts block
       ↓
nodes verify
       ↓
miners build next block on accepted chain
```

---

## Timing Model

这里不要生硬地说：

> Bitcoin = asynchronous system。

这不够准确。

Bitcoin 不依赖：

```
message 必须 50ms 到达
```

这种同步 bound。

但是 Consensus 能够稳定收敛隐含需要：

> honest nodes 之间的信息传播速度，相对于 block production 足够快。

如果：

```
block interval << network propagation delay
```

就会大量：

```
fork
fork
fork
```

因此现代形式化研究通常要引入某种 network-delay / synchrony assumption 才能证明 ledger 的 persistence 和 liveness；Bitcoin Backbone 一类工作将这些性质形式化为 common prefix、chain growth、chain quality。[ePrint IACR](https://eprint.iacr.org/2015/1019?utm_source=chatgpt.com)

最好的 mental model 是：

> **网络可以异步一段时间，但想获得最终稳定性和 progress，需要 eventual connectivity / sufficiently timely propagation。**

类似：

```
partially synchronous flavor
```

但原始 2008 paper 本身没有像 PBFT/Raft paper 那样严格形式化 timing model。

---

## Storage Model

Full node 会维护 persistent state，例如概念上：

```
blocks
block headers
best-chain metadata
UTXO set
```

另外通常还有 volatile：

```
mempool
peer state
in-flight messages
```

Miner 维护：

```
candidate block
nonce/search state
```

nonce search 丢了没有 correctness 问题，重新开始即可。

---

## 最关键 Failure Assumption

原论文最核心的安全假设：

```
honest computational power
>
attacker computational power
```

也就是：

```
p > q
```

其中：

```
p = honest fraction
q = attacker fraction
```

原论文明确基于 honest CPU power 超过 cooperating attacker 的假设。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/bitcoin.pdf?utm_source=chatgpt.com)

现代语境通常说：

```
attacker hash power < 50%
```

但千万不要把它误解成：

```
<50% 就绝对安全
```

不是。

而是：

> attacker < 50% 时，深度增加会让成功 reorg 的概率迅速降低。

---

# Part 5：算法从 Happy Path 一步步运行

现在真正走一次。

---

## Step 1：Alice 拥有一个 spendable output

现代 Bitcoin 更适合用 UTXO mental model：

```
UTXO X:

value = 1 BTC
owner condition = Alice's public key
```

Alice 想支付给 Bob。

---

## Step 2：Alice 创建 transaction

```
Transaction T

Input:
    UTXO X

Output:
    1 BTC -> BobPublicKey

Signature:
    Sign(AlicePrivateKey, transaction)
```

广播：

```
Alice
   |
   +-------> Node A
   +-------> Node B
   +-------> Node C
```

---

## Step 3：节点验证 transaction

例如检查：

```
signature valid?
input exists?
input unspent?
outputs valid?
transaction obeys consensus rules?
```

注意：

> Miner 不是想放什么 transaction 就放什么。

Full node 会独立验证。

---

## Step 4：transaction 进入 mempool

例如：

```
Node A mempool:

T1
T2
T3
Alice->Bob
T5
```

此时：

> transaction 还没有 final。

甚至还没有进入 blockchain。

---

## Step 5：miner 构造 candidate block

```
Block candidate

prev_hash = hash(B100)

transactions:
    T1
    T2
    Alice -> Bob
    T3

nonce = ?
```

然后：

```
while true:
    h = SHA256(header)

    if h < target:
        success

    change nonce / header
```

---

## Step 6：某个 miner 赢得 PoW

假设 Miner A 找到：

```
B101
```

广播：

```
Miner A
     |
     +------ B101 ------> nodes
```

---

## Step 7：其他节点验证 block

它们检查：

```
prev hash valid?
PoW valid?
transactions valid?
no double-spend?
block obey rules?
```

如果全部正确：

```
accept B101
```

然后 miner 开始：

```
mine B102 on top of B101
```

---

## Step 8：后续 block 增加 confidence

```
B101 [Alice -> Bob]
 |
B102
 |
B103
 |
B104
```

Bob 等得越久：

```
reorg cost ↑
attacker catch-up probability ↓
```

---

# Part 6：并发问题——为什么会产生 fork？

来看 timeline：

```
time →

Miner A:   mine B101A
                  |
                  +----------broadcast-------->

Miner B:      mine B101B
                     |
                     +---------broadcast------>

Node X:        sees A first

Node Y:             sees B first
```

于是：

```
                   B100
                  /    \
                 /      \
             B101A     B101B
```

此时：

```
Node X best chain = A
Node Y best chain = B
```

Bitcoin 允许这一刻存在 inconsistency。

然后：

```
Miner C finds B102A
```

变成：

```
                   B100
                  /    \
                 /      \
             B101A     B101B
               |
             B102A
```

B branch 上的 node 收到 B102A：

```
比较 cumulative work
        ↓
A branch wins
        ↓
switch/reorg
```

最终：

```
Node X -> A
Node Y -> A
```

---

## 这里最反直觉

在 Raft 中你经常问：

> 什么时候 committed？

Bitcoin 中你必须换问题：

> **这个 transaction 被 reorg 掉的概率现在有多低？**

这是 mental model 的巨大变化。

---

## Failure：Double Spend

Alice 给 Bob：

```
TBob
```

同时秘密创建：

```
TAlice
```

把钱重新支付给自己。

Public chain：

```
B100
 |
B101 [Alice -> Bob]
 |
B102
 |
B103
```

Attacker secret fork：

```
B100
 |
B101' [Alice -> Alice]
 |
B102'
```

如果攻击者最终做到：

```
attacker cumulative work
>
honest cumulative work
```

并公布：

```
B100 -> B101' -> B102' -> B103' -> B104'
```

网络可能 reorg：

```
Alice->Bob transaction disappears
```

这就是为什么商家不能仅看到：

```
transaction broadcast
```

就认为支付 finalized。

---

# Part 7：State 和 Invariants

Bitcoin 不像 Raft paper 那样给：

```
currentTerm
votedFor
log[]
commitIndex
```

但我们仍可以从 protocol 角度整理。

---

## Full Node 重要状态

```
Blockchain / block DAG
```

知道收到过哪些 valid blocks。

```
best chain
```

当前 cumulative work 最大的 valid branch。

```
UTXO Set
```

当前 accepted history 下还有哪些 output 未花费。

```
mempool
```

知道但尚未进入 accepted chain 的 transactions。

---

## Miner 额外状态

```
candidate block
previous block hash
transactions
nonce/search space
```

---

## Invariant 1：Accepted transaction 必须被授权

没有 Alice 的 private key：

```
attacker
```

不能合法消费 Alice 的 output。

PoW 再多也不能让：

```
invalid signature
```

变成：

```
valid signature
```

这是非常重要的区分。

---

## Invariant 2：一条 valid chain 中一个 UTXO 只能被消费一次

不能：

```
B100:
UTXO X

B101:
X -> Bob

B102:
X -> Charlie
```

节点会认为后者 invalid。

---

## Invariant 3：每个 block 都链接到 predecessor

```
B(n).prev_hash
=
hash(B(n-1))
```

所以修改历史需要重做后续 Proof of Work。

---

## Invariant 4：节点只考虑 valid chains

即使攻击者生成：

```
100000 blocks
```

但里面存在：

```
Alice signature forged
```

full node 不应该接受。

因此：

```
most work
```

完整说法其实是：

> **most-work valid chain。**

---

# Part 8：Correctness

这里需要放弃 Raft 式：

```
Safety = absolute
```

的思考。

Bitcoin 的核心保证是 **probabilistic**。

后续 formal analysis 通常用类似：

```
Common Prefix
Chain Growth
Chain Quality
```

来刻画。[ePrint IACR](https://eprint.iacr.org/2016/1048.pdf?utm_source=chatgpt.com)

---

## Safety / Persistence

我们希望：

> 一个 sufficiently deep 的 transaction 被 honest node 接受以后，不会再被另一个 conflicting transaction 替代。

但不是：

```
P(reorg) = 0
```

而是：

```
P(reorg)
→ very small
```

随着 confirmations 增加。

---

## Liveness

希望：

> 一个持续被广播的 valid transaction，只要网络工作、honest miners 持续产生 blocks，最终能够进入稳定 ledger。

对应：

```
chain keeps growing
```

---

## Network Partition 对 Safety / Liveness

假设：

```
              Internet partition

Group A                        Group B
60% hash                       40% hash

B100                           B100
 |                              |
B101A                          B101B
 |                              |
B102A                          B102B
```

两边都能继续产生 blocks。

所以局部：

```
liveness = yes
```

但是形成两个 histories。

恢复以后：

```
A chain work > B chain work
```

通常：

```
B side reorg
```

那么 B partition 中用户以为已经确认的 transaction 可能消失。

所以 Bitcoin 在 partition 时并不像 Raft minority partition 那样：

```
stop serving writes to preserve deterministic safety
```

Bitcoin 的哲学更接近：

```
keep progressing
+
resolve conflicts later
+
finality is probabilistic
```

---

# Part 9：Failure Matrix

|Failure|会怎样|Ledger 安全性|Availability|恢复|
|---|---|---|---|---|
|普通 node crash|其他节点继续|通常不受影响|高|restart / sync|
|Miner crash|少一点 hash power|通常不影响|基本继续|其他 miners|
|Packet loss|tx/block 重传或从其他 peer 得到|通常安全|可能延迟|gossip|
|Duplicate tx|tx 已知或 input 已 spent|不会重复花|正常|deterministic validation|
|Duplicate block|已知 block|无影响|正常|ignore|
|两 miner 同时出块|fork|暂时 disagreement|继续|more-work branch|
|Delayed block|stale fork|可能短暂 reorg|正常|chain selection|
|Network partition|两边形成 chain|confirmation 可能被撤销|两边可继续|partition heal + reorg|
|Byzantine transaction|validation reject|安全|正常|ignore|
|Invalid block|full nodes reject|安全|正常|ignore|
|<50% hash attacker|可尝试 reorg|概率风险|通常可用|confirmations|
|>50% hash attacker|可持续 dominate fork/censor|ordering 保证严重破坏|可被攻击|assumption broken|
|Signature crypto broken|ownership 失效|灾难|—|system assumption broken|

---

## 一个非常重要的问题：51% 能不能偷 Alice 的 Bitcoin？

很多工程师会答：

> 可以。

严格说：

> **不能仅凭 51% hash power 伪造 Alice 的 signature。**

51% attacker 可以：

```
reorder transactions
censor transactions
double-spend自己的 outputs
reorg recent history
outpace honest chain
```

但它不能因为拥有很多 hashpower 就制造：

```
Alice -> Attacker
```

的合法 signature。

原论文的 security analysis 也明确指出，即使 attacker 赶上 chain，也不是因此获得修改所有规则、偷任意资金的能力；节点仍然验证 transactions。[bitcoin.org](https://bitcoin.org/bitcoin.pdf?utm_source=chatgpt.com)

---

# Part 10：Top 5 Misconceptions

### ❌ 1. Blockchain 天然保证 Consensus

错。

```
hash chain
```

只告诉你：

> 历史有没有被修改。

它没告诉你：

```
fork A
vs
fork B
```

谁是真历史。

还需要：

```
PoW
+
fork choice
+
network protocol
```

---

### ❌ 2. Proof of Work 用来验证 transaction

错。

Transaction validity 主要依赖：

```
signatures
consensus rules
UTXO state
```

PoW 解决的是：

```
谁有权给 history 增加 weight
```

它主要服务于：

```
Sybil resistance
+
ordering
+
fork resolution
```

---

### ❌ 3. Bitcoin 达成 Consensus 后就永远不会改变

错。

Bitcoin 没有 Raft 那种：

```
commitIndex <= X
=> 永不回滚
```

的 deterministic finality。

而是：

```
depth ↑
reorg probability ↓
```

---

### ❌ 4. 51% 指 51% 的节点

错。

不是：

```
51% IP addresses
51% machines
51% public keys
```

而是近似：

```
majority hash power
```

---

### ❌ 5. 最长 chain 就是 block 数量最多

原论文的表述经常叫 longest chain，但工程 mental model 应该用：

```
greatest cumulative Proof of Work
```

Bitcoin Developer Guide 也以 cumulative work/difficulty 来解释 chain selection 和重写历史的成本。[Bitcoin Developer Documentation](https://developer.bitcoin.org/devguide/block_chain.html?utm_source=chatgpt.com)

---

## 另外两个特别容易混淆的概念

### Authentication vs Consensus

```
Digital Signature
       ↓
who authorized this transaction?


Consensus
       ↓
which valid transaction/history wins?
```

这是完全不同的层。

---

### Validation vs Ordering

假设：

```
Tx1: Alice -> Bob
Tx2: Alice -> Charlie
```

单独看：

```
valid(Tx1) = true
valid(Tx2) = true
```

但是放进 history：

```
Tx1 first
=> Tx2 invalid
```

反过来：

```
Tx2 first
=> Tx1 invalid
```

因此：

> **Transaction validity 有时依赖于之前已经 accepted 的 history。**

这正是 Consensus 为什么重要。

---

# Part 11：和真实 Distributed Systems 联系

Bitcoin 有一个很漂亮的抽象：

```
          transaction
               ↓
         P2P dissemination
               ↓
       ordering mechanism
               ↓
      replicated history
               ↓
 deterministic validation
               ↓
         replicated state
```

这和传统 State Machine Replication：

```
Client Command
      ↓
Consensus Log
      ↓
Replicated State Machine
```

非常相似。

差别主要在：

```
Consensus Log 怎么产生？
```

---

## etcd / Raft

```
Kubernetes API Server
        ↓
       etcd
        ↓
       Raft
```

Raft：

```
known members
leader
quorum
crash fault
deterministic commit
```

Bitcoin：

```
unknown/open participants
no stable leader
PoW lottery
Byzantine fault
probabilistic finality
```

---

## Kafka

Kafka partition 也需要一个 ordered log：

```
offset 10
offset 11
offset 12
```

但 Kafka 有：

```
known brokers
partition leader
controller
ISR
```

它完全不需要 Proof of Work。

为什么？

因为：

```
Kafka broker identity
```

由 cluster administration 决定。

不存在：

```
Mallory 自己启动一百万 broker，
然后获得一百万票
```

的问题。

---

## PostgreSQL / MySQL

它们本质上可以靠：

```
central authority / primary
```

决定：

```
transaction serialization order
```

因此不需要 Nakamoto Consensus。

---

# Part 12：与你熟悉的 Infra 系统联系

这个部分应该非常有帮助。

---

## Kubernetes Controller

Controller：

```
Desired State
    ↓
Observe
    ↓
Diff
    ↓
Reconcile
```

Consensus 不在 Controller 自己里面。

真正 shared source of truth：

```
API Server
    ↓
etcd
    ↓
Raft
```

所以：

```
Controller
```

可以在一个已经具有强一致性的 substrate 上工作。

---

Bitcoin 没有：

```
etcd
```

也没有：

```
API Server Leader
```

它必须先自己解决：

```
什么是 canonical state？
```

所以 Bitcoin 所解决的问题其实是在 Kubernetes stack 更下面一层。

---

## Terraform

Terraform 里：

```
state
```

通常由一个受信 backend 保存：

```
S3
Terraform Cloud
GCS
```

配合：

```
locking
IAM
```

Terraform 不试图解决：

> 全球不互相信任的人怎样对 Terraform state 达成 Byzantine Consensus。

所以千万别因为二者都有：

```
desired/current state
```

就强行类比。

---

## Multi-region database

你熟悉的 Multi-region DB 通常：

```
Region A
Region B
Region C
```

membership 是 operator 配好的。

例如：

```
3 replicas
```

因此可以：

```
2/3 quorum
```

Bitcoin 如果也这么做：

```
2/3 nodes
```

立刻碰到：

```
谁算 node？
```

这恰好是它需要 PoW 的原因。

---

# Part 13：Bitcoin Paper 怎么读

论文：

**Bitcoin: A Peer-to-Peer Electronic Cash System — Satoshi Nakamoto, 2008**。MIT 6.824/6.5840 也直接使用原论文作为 Bitcoin 课程阅读材料。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)

---

### Paper Problem

传统网络支付需要 trusted third party。

作者想要：

```
electronic payment
+
no trusted central authority
+
prevent double spending
```

---

## Previous Approach

传统：

```
Alice
   |
   v
Bank / Mint
   |
   v
Bob
```

Mint 保存：

```
coin spent?
```

所有人相信：

```
Mint's order
```

原论文明确描述 central mint 是一种简单 double-spend solution，但代价是所有交易和整个系统都依赖该 central authority。[pdos.csail.mit.edu](https://pdos.csail.mit.edu/6.824/papers/bitcoin.pdf?utm_source=chatgpt.com)

---

## Key Insight

整篇论文最值得记住的一句话不是：

> 使用 Blockchain。

而是：

> **通过 Proof of Work 把历史变成一条需要真实计算资源才能重写的公共 chain，并让 honest computational majority 的 chain 随时间超过 attacker chain。**

---

## Design

```
             Users
               |
        signed transaction
               |
               v
           P2P Network
               |
               v
          transaction pool
               |
               v
             Miners
               |
           Proof of Work
               |
               v
              Block
               |
             gossip
               |
               v
        validating nodes
               |
               v
          best valid chain
```

---

## Mechanisms

Paper 的核心机制大致是：

```
Digital Signatures
        ↓
ownership

Timestamp / Hash Chain
        ↓
history linkage

Proof of Work
        ↓
scarce voting resource

Network Gossip
        ↓
dissemination

Longest/Most-work Chain
        ↓
fork resolution

Block Reward / Fees
        ↓
incentive

Merkle Tree
        ↓
efficient commitment / SPV support

Confirmations
        ↓
probabilistic finality
```

---

## Evaluation

这篇 2008 paper 并不是现在常见的：

```
100-node benchmark
99p latency graphs
millions ops/s
```

式系统论文。

核心 evaluation 更偏：

```
analytical security argument
```

尤其分析：

> attacker 落后 z blocks 后追上 honest chain 的概率。 [MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/bitcoin.pdf?utm_source=chatgpt.com)

---

## Limitations

从 Distributed Systems 角度特别值得看到：

#### 1. Timing/network model 不够形式化

后续论文才更严格建立：

```
Common Prefix
Chain Growth
Chain Quality
```

等性质。[ePrint IACR](https://eprint.iacr.org/2016/1048.pdf?utm_source=chatgpt.com)

#### 2. Finality 不是 deterministic

永远存在 non-zero probability。

#### 3. Proof of Work 成本巨大

安全来自真实 resource expenditure。

#### 4. Throughput/latency 和安全存在 trade-off

不能简单地：

```
block every 1 ms
```

否则：

```
network propagation delay
```

造成大量 fork。

后续研究专门形式化分析了 block generation speed 与安全性的 trade-off。[ePrint IACR](https://eprint.iacr.org/2015/1019?utm_source=chatgpt.com)

---

## What aged well?

最经得起时间的思想：

```
Hash-linked append-only history
resource-based Sybil resistance
probabilistic finality
open-membership consensus
economic incentives + distributed protocol
```

特别是：

> **Consensus 的 membership 问题本身也是 protocol design 的一部分。**

这是传统 Raft/Paxos 课程很容易让人忽略的地方。

---

# Part 14：三个关键公式

---

## 公式 1：PoW 成功概率

假设要求 hash 前：

```
d
```

个 bit 必须为 0。

每 bit：

```
P(0) = 1/2
```

所以：

\[ P(\text{success}) = \frac{1}{2^d} \]

例如：

```
d = 7
```

那么：

\[ P = \frac1{128} \]

MIT 的 Bitcoin exam 也直接考过这个关系：SHA-256 hash 的前 7 bits 都为 0 的概率为 1/128。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/quizzes/q25-2-sol.pdf?utm_source=chatgpt.com)

因此期望尝试次数：

\[ E[\text{attempts}] = 2^d \]

---

## 公式 2：Hash Power 和赢下一块概率

假设：

```
Alice miner = 30 TH/s
whole network = 100 TH/s
```

那么粗略：

\[ P(\text{Alice finds next block}) \approx \frac{30}{100} =0.3 \]

所以 PoW 本质形成：

```
weighted random leader election
```

---

## 公式 3：Attacker catch-up

论文设：

```
p = honest 找到下一个 block 的概率
q = attacker 找到下一个 block 的概率
z = attacker 落后的 blocks
```

若：

\[ p > q \]

从正好落后 \(z\) blocks 开始最终追平的经典 random-walk 概率：

\[ P_{\text{catchup}} = \left(\frac qp\right)^z \]

如果：

\[ p \le q \]

则长期追上的概率趋近：

\[ 1 \]

原论文就是用 Gambler's Ruin 的思路推导这个关系。[bitcoin.org](https://bitcoin.org/bitcoin.pdf?utm_source=chatgpt.com)

---

### 数字例子

攻击者：

```
q = 0.1
```

honest：

```
p = 0.9
```

落后：

```
z = 6
```

于是仅从“此刻正好落后六块”开始：

\[ \left(\frac{0.1}{0.9}\right)^6 = \left(\frac19\right)^6 \]

已经非常小。

论文进一步考虑：

> 在 honest network 挖出这 z 块的同时，攻击者也可能已经偷偷挖出了一些 block。

所以用 Poisson distribution 给出了更完整的 double-spend success probability；原论文的公式和代码都用于计算这个值，并说明当 \(p>q\) 时，风险随确认深度快速下降。[MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/papers/bitcoin.pdf?utm_source=chatgpt.com)

重点不用背公式。

记住：

```
attacker < honest
+
depth increases
        ↓
catch-up probability falls rapidly
```

---

# Part 15：和 6.824 Lab 的关系

Bitcoin Lecture 通常不像 Raft 那样直接对应一个 Bitcoin Lab。

但它实际上是在训练你重新审视 Lab 中很多假设。

---

### Raft Lab 中你拥有

```
peers[]
```

这一个字段已经偷偷给了你一个巨大假设：

> membership 已经解决。

所以你可以说：

```
majority = N/2 + 1
```

Bitcoin 没有这个奢侈条件。

---

### Raft 里的 term

Raft：

```
term
↓
leader epoch
```

帮助系统建立一个：

```
logical authority
```

Bitcoin 没有全局：

```
currentTerm
```

你更像拥有：

```
competing histories
```

由累积 PoW 决定 preferred branch。

---

### commitIndex

Raft：

```
commitIndex = 100
```

具有很强的语义：

```
<=100 不应再改变
```

Bitcoin：

没有完全对应的：

```
commitIndex
```

更像：

```
confirmation depth
```

因此：

```
Raft commitIndex
≠
Bitcoin confirmation count
```

---

# Part 16：理解检查

先别看答案，自己推一下。

---

### 问题 1

两个 miner 同时找到：

```
        B10
       /   \
     B11A B11B
```

那么此时 Bitcoin 是否“Consensus 失败”？

---

### 问题 2

攻击者拥有 1% hash power，但是创建了：

```
10,000,000 public keys
```

他的 mining influence 是否变成很大？

---

### 问题 3

一个 merchant 看到交易：

```
Alice -> Merchant
```

已经进入当前 tip block。

为什么还不应该立刻认为：

```
100% final
```

？

---

答案分别是：

1. 不一定；temporary fork 是协议预期行为，关键是后续能否收敛。
2. 不会，仅增加 identity 不增加 PoW resource。
3. 因为存在 competing branch，当前 block 仍可能被 reorg。

---

# Part 17：逐级练习

### Level 1 — Concept

为什么数字签名无法单独解决 double spending？

核心答案：

```
signature proves authorization
not global ordering
```

---

## Level 2 — Execution

有：

```
         B100
        /    \
      B101A B101B
        |
      B102A
```

若所有 difficulty 相同：

> 新节点更应该在哪条 branch 上继续？

A，因为 A branch 累积 work 更多。

---

## Level 3 — Failure

网络分区：

```
Partition A: 70% hash
Partition B: 30% hash
```

持续一天。

两边 merchant 都接受了 payment。

恢复后可能发生什么？

关键不是：

```
两边的数据 merge
```

Bitcoin ledger 不是 CRDT。

而是：

```
one branch becomes canonical
other branch reorged
```

因此 losing partition 的一些 payment 可能消失。

---

## Level 4 — Counterexample

假设我们取消 PoW，只规定：

```
每个 public key 一票
```

攻击方法？

```
generate unlimited keys
      ↓
Sybil attack
      ↓
manufacture majority
```

---

## Level 5 — System Design

假设让你设计公司内部：

```
replicated metadata service
```

你会不会使用 Bitcoin PoW？

通常不会。

因为你已经知道：

```
Node A
Node B
Node C
```

由公司控制。

直接：

```
Raft
```

会更：

```
低延迟
高吞吐
节能
确定 finality
```

Bitcoin 的复杂机制，是为它独特的：

```
permissionless
Byzantine
open-membership
```

model 服务的。

这也是非常重要的 system-design lesson：

> **不要复制 mechanism；先比较 assumptions。**

---

# Part 18：Problem → Solution Chain

这是最值得你记忆的一张图。

```
需要电子货币
        ↓

用户必须证明自己拥有钱
        ↓
Digital Signature
        ↓

但是一个 owner 可以签两个 payment
        ↓
Double Spending
        ↓

需要决定哪个 payment first
        ↓
需要 global transaction history
        ↓

Naive:
central server / bank
        ↓
Single Trusted Authority
        ↓

想去掉 central authority
        ↓
Naive:
nodes vote
        ↓

问题:
Sybil attack
        ↓

需要不可免费复制的 voting power
        ↓
Proof of Work
        ↓

PoW 随机选出 block producer
        ↓

但两个 miners 可能同时成功
        ↓
Fork
        ↓

允许 temporary forks
        ↓
Most cumulative work rule
        ↓

网络最终偏向某个 branch
        ↓

但是以前的 block 仍然可能 reorg
        ↓
Confirmation depth
        ↓

深度越大
attacker 重写历史越困难
        ↓

Probabilistic Finality
```

整节 Bitcoin Lecture 本质上就是这一条链。

---

# Part 19：三个粒度总结

### 30 秒版本

面试官问：

> Bitcoin 从 Distributed Systems 角度解决什么？

你可以回答：

> Bitcoin 解决的是 permissionless Byzantine 环境下的 shared ledger consensus。数字签名只能证明 transaction 的 authorization，无法防止 double spending，因此系统必须让参与者对 transaction order 形成共同历史。由于开放网络不能使用 one-node-one-vote，否则会遭到 Sybil attack，Bitcoin 用 Proof of Work 把 voting influence 与计算资源绑定，让 miners 竞争产生 hash-linked blocks，并让节点跟随 cumulative work 最大的 valid chain。短暂 forks 是允许的，所以 Bitcoin 不是 deterministic finality，而是随着 confirmation depth 增加，历史被重写的概率不断下降。

---

## 3 分钟版本

可以这样组织：

```
Problem
↓
double spending

Root Cause
↓
no trusted global ordering authority

Why voting doesn't work
↓
open membership → Sybil

Key mechanism
↓
Proof of Work

What PoW does
↓
resource-weighted leader lottery

History structure
↓
hash-linked blocks

Concurrent proposals
↓
fork

Resolution
↓
greatest cumulative work valid chain

Correctness model
↓
honest hash power > attacker hash power
+ sufficiently effective communication

Safety
↓
deep history becomes exponentially difficult
to replace, but not mathematically final

Liveness
↓
honest mining keeps chain growing

Big contrast with Raft
↓
Raft = fixed membership + crash fault
      + deterministic quorum finality

Bitcoin = open membership + Byzantine fault
        + Sybil resistance
        + probabilistic finality
```

---

## 深入版本

```
Problem
    ↓
prevent double-spending without trusted authority

Model
    ↓
permissionless nodes
Byzantine participants
P2P network
cryptographic assumptions
honest hash-power majority

Algorithm
    ↓
broadcast transaction
validate transaction
miners assemble blocks
perform PoW
broadcast winner
validate block
build on greatest-work valid chain

Invariant
    ↓
signatures must validate
UTXO can't be spent twice in one valid history
blocks hash-link to predecessors
invalid chains aren't accepted merely due to PoW

Safety
    ↓
deep common history becomes increasingly
unlikely to be reverted

Liveness
    ↓
honest mining + usable network
causes chain growth

Failures
    ↓
temporary fork
packet loss
miner crash
partition
Byzantine blocks
double-spend attempts

Resolution
    ↓
gossip
validation
fork choice
additional PoW
confirmations

Trade-offs
    ↓
energy
latency
throughput
probabilistic finality

Key Insight
    ↓
replace trusted identity/quorum voting
with scarce computational resource
```

---

# Part 20：最后建立知识网络

把 Bitcoin 挂在你已经学过的知识树上：

```
                           Distributed Systems
                                  |
            +---------------------+----------------------+
            |                                            |
        Replication                                  Security
            |                                            |
     Ordered History                              Byzantine Faults
            |                                            |
        Consensus <--------------------------------------+
            |
      +-----+--------------------+
      |                          |
 Raft / Paxos               Nakamoto Consensus
      |                          |
fixed membership            open membership
crash faults                Byzantine nodes
quorum votes                Proof of Work
      |                          |
      v                          v
State Machine              Blockchain Ledger
Replication                     |
      |                     Temporary Forks
      |                          |
deterministic               Most-work Chain
 finality                        |
                             confirmations
                                  |
                         probabilistic finality
```

再把它和上一节 SUNDR 放在一起，会更漂亮：

```
Trusted replicas
      ↓
Raft

server may be malicious
      ↓
SUNDR / Fork Consistency

everyone may be malicious
+
membership is open
+
we still want shared history
      ↓
Bitcoin
      ↓
Proof of Work
      ↓
Probabilistic Byzantine Consensus
```

---

## 最后，只留下三个真正重要的 Mental Models

第一：

```
Bitcoin 的根本问题不是 cryptocurrency。

而是：

Who decides the global order
when nobody is trusted?
```

第二：

```
Proof of Work
不是 transaction validator。

它主要解决：

open membership
     ↓
Sybil resistance
     ↓
resource-weighted consensus
```

第三，也是整个 Lecture 最重要的：

```
Raft 的世界：

commit
   ↓
final


Bitcoin 的世界：

included
   ↓
1 confirmation
   ↓
2 confirmations
   ↓
3 confirmations
   ↓
...
   ↓
越来越难逆转
```

所以你学完 Bitcoin 后，Distributed Systems 的 Consensus mental model 应该从：

```
Consensus
=
Leader + Majority + Replicated Log
```

升级成：

```
                 Consensus
                     |
        +------------+-------------+
        |                          |
Known Membership             Open Membership
        |                          |
Raft / Paxos                  Bitcoin
        |                          |
Quorum                       Proof of Work
        |                          |
Crash Faults                 Byzantine + Sybil
        |                          |
Deterministic                Probabilistic
Finality                     Finality
```

**这才是 MIT 6.824 把 Bitcoin 放进 Distributed Systems 课程里真正想让你理解的东西。** [MIT CSAIL PDOS](https://pdos.csail.mit.edu/6.824/schedule.html?utm_source=chatgpt.com)
## MIT 6.824 Lecture 20 — Blockstack

先把整节课压成一句话：

> **Blockstack 想解决的核心问题是：在没有中央可信机构的情况下，怎样让全世界对“一个人类可读的名字属于谁、它对应哪个公钥、数据在哪里”形成统一且可验证的认识，同时又不把所有数据都塞进昂贵而缓慢的 blockchain。**

理解 Blockstack 最好的方式，不是把它看成“又一个 blockchain 项目”，而是把它看成：

```
Bitcoin / Blockchain
        ↓
提供一个 decentralized + tamper-resistant
+ roughly globally ordered log
        ↓
Virtualchain
        ↓
在这个 log 上跑 BNS State Machine
        ↓
alice.id → Alice's public key → data location
        ↓
Atlas：发现 metadata
        ↓
Gaia：存真正的数据
```

你已经学过 Raft、State Machine Replication、Bitcoin 和 Fork Consistency，所以这一课其实正好把前面的几个知识点连起来。

---

# Part 1：这节课到底想解决什么问题？

传统 Internet 上，假设 Alice 有：

```
alice.com
```

你访问它时，背后其实依赖很多中央 authority：

```
alice.com
   │
   ▼
DNS
   │
   ▼
IP address

同时：

alice.com
   │
   ▼
Certificate Authority
   │
   ▼
Alice's public key

应用数据：
Alice
   │
   ▼
Facebook / Google / Dropbox / ...
```

因此你实际上同时信任了：

```
DNS operator
Certificate Authority
application provider
storage provider
```

Blockstack 2017 whitepaper 的目标就是把这些 application-layer trust points 尽可能移走：Blockchain 负责可信的 name/key binding，Atlas 负责 discovery，Gaia 负责实际数据。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

### 为什么单机没这个问题？

如果只有一台机器：

```
users.db

alice -> PK_A
bob   -> PK_B
```

你只需要：

```
UNIQUE(name)
```

就能保证：

```
alice 只有一个 owner
```

因为存在一个天然的 authority：

```
数据库
```

它决定：

```
INSERT alice -> PK_A
INSERT alice -> PK_E
```

谁先成功。

---

### 一旦 decentralized，就出现了真正的问题

现在我们把数据库删掉：

```
Alice node: alice -> PK_A
Bob node:   ...
Eve node:   alice -> PK_E
Carol node: ...
```

Alice 宣布：

```
alice.id belongs to PK_A
```

Eve 同时宣布：

```
alice.id belongs to PK_E
```

问题来了：

> **全世界根据什么规则判断谁赢？**

这不是 storage 问题。

它本质上是：

```
Global Ordering
+
Global Agreement
+
Ownership
```

问题。

---

## 一个非常重要的三角：Zooko's Triangle

理想名字希望同时具有：

```
              Human-readable
                   /\
                  /  \
                 /    \
                /      \
               /        \
              /          \
   Unique ---------------- Decentralized
```

三个性质：

```
Human-readable
alice.id

Unique
全世界不能同时有两个 alice.id

Decentralized
不存在 Twitter / ICANN / Google
决定谁能拥有 alice.id
```

传统系统通常容易得到两个，但很难同时得到三个。Blockstack whitepaper把这作为 BNS 的根本动机。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

比如：

```
Public key

1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa
```

可以：

```
unique ✓
decentralized ✓
human-readable ✗
```

Twitter handle：

```
@alice
```

可以：

```
unique ✓
human-readable ✓
decentralized ✗
```

昵称：

```
"Alice"
```

可以：

```
human-readable ✓
decentralized ✓
unique ✗
```

所以你现在应该看到 Blockstack 为什么需要 blockchain 了。

它不是为了：

> “把网页存在 blockchain 上。”

而是为了：

> **让互不信任的参与者获得一个 canonical ordering，从而决定 alice.id 究竟属于谁。**

---

## 最 naive 的方案为什么不行？

#### 方案一：中央数据库

```
         Naming Server
              |
     +--------+--------+
     |                 |
 alice→PK_A         bob→PK_B
```

性能很好。

但是 Naming Server 变成：

```
Single Point of Trust
Single Point of Control
```

即使你用：

```
            Raft
              |
      +-------+-------+
      |       |       |
      A       B       C
```

也只是解决：

```
machine crash
```

没有解决：

```
administrator malicious
```

如果 NameCorp 决定：

```
alice -> attacker
```

三个 Raft replicas 会非常可靠地、一致地执行这个决定。

所以：

> **Replication ≠ Decentralization。**

---

#### 方案二：直接使用 public key 当名字

这其实非常干净：

```
PK_Alice
```

Alice 可以自己生成。

不用问任何服务器。

但没人愿意输入：

```
02a1b99d84...
```

这就是 human-readable problem。

---

#### 方案三：DHT

可以做：

```
hash(alice)
       ↓
DHT node
       ↓
alice -> PK_A
```

但 DHT 主要解决的是：

> **Where can I find the value?**

它不自然解决：

> **Who legitimately owns the key `alice`?**

尤其：

```
Alice: PUT(alice, PK_A)
Eve:   PUT(alice, PK_E)
```

你仍然需要一个 globally agreed conflict-resolution rule。

---

#### 方案四：所有东西放 blockchain

这样确实可以：

```
Blockchain:
alice -> key
profile
photos
documents
videos
...
```

但是 blockchain 是：

```
slow
expensive
globally replicated
low throughput
```

Blockstack因此做了一个非常关键的设计：

> **只把必须全局 consensus 的小量 control metadata 放在 blockchain，bulk data 放链外。**

这就是整篇设计的核心。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

# Part 2：放到整个 6.824 知识地图里

你可以把课程脉络理解成：

```
RPC / Threads
     ↓
多个节点如何协作？

Primary / Backup
     ↓
如何 Replication？

Raft
     ↓
可信的 crash-fault replicas
如何形成统一 ordered log？

ZooKeeper
     ↓
如何把 consensus 暴露成 coordination service？

Transactions / 2PC
     ↓
多个数据分片如何 atomic commit？

Spanner
     ↓
geo-distributed database + transaction

COPS / Cache / Spark
     ↓
不同 consistency / scale trade-off

Bitcoin
     ↓
参与者互不信任时，
如何产生 decentralized global history？

Blockstack
     ↓
能不能利用 Bitcoin 的 global history
构造 decentralized naming / identity / storage？
```

这是 Blockstack 真正的位置。

---

### Blockstack vs Raft

Raft：

```
known servers
A B C D E

假设：
crash / network partition
但服务器不会恶意伪造协议
```

目标：

```
replicated log
+
strong consensus
```

Blockstack：

```
open membership
unknown participants
miners
possible attackers
```

它自己并没有重新发明 Raft。

它说：

> Bitcoin 已经给我一个 global ordered log 了，我在这个 log 上再构造自己的 State Machine。

因此：

```
Raft:

commands
  ↓
Raft log
  ↓
State Machine


Blockstack:

BNS commands
  ↓
Bitcoin blockchain
  ↓
Virtualchain
  ↓
BNS State Machine
```

它们的结构非常像。

但底层 consensus model 完全不同。

---

## Blockstack vs Bitcoin

Bitcoin 的 state machine 可以粗略理解为：

```
UTXO state

transaction:
A → B 10 BTC
```

Blockstack 想做：

```
Naming state

transaction:
REGISTER alice.id

UPDATE alice.id zonefile_hash=H1

TRANSFER alice.id PK_A → PK_B
```

关键 Insight 是：

> **我不需要修改 Bitcoin，让 Bitcoin 理解 REGISTER/UPDATE/TRANSFER。**

Bitcoin 只看到：

```
ordinary Bitcoin transaction
+
OP_RETURN metadata
```

真正理解这些 operation 的是：

```
Virtualchain
```

Bitcoin 对 Blockstack 来说只是：

> **tamper-resistant total-order-ish communication channel。**

论文称这个 ordered-operation abstraction 是架构的 “narrow waist”。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

## Blockstack vs State Machine Replication

这层关系非常重要。

Raft：

```
              ordered log
                   |
                   v
        deterministic state machine
                   |
                   v
                  state
```

Blockstack：

```
           Bitcoin blocks
                |
                v
     extract BNS transactions
                |
                v
           Virtualchain
                |
                v
        deterministic BNS FSM
                |
                v
 name -> owner -> zonefile hash
```

因此 Virtualchain 本质就是：

> **从 blockchain transaction log 派生 application state。**

Whitepaper明确把 Virtualchain 描述为建立在 blockchain 上、可定义 arbitrary state machines 的 logical layer。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

# Part 3：核心 Mental Model

这一课我建议你真正记住 7 个东西：

```
BNS
Blockchain as ordered log
Virtualchain
Commit-Reveal registration
Control Plane vs Data Plane
Atlas
Gaia
```

---

## Concept 1：BNS — Blockchain Name System

#### 它解决的问题

```
alice.id 到底属于谁？
```

#### 一句话定义

> **BNS 是一个 blockchain-backed naming state machine，把 human-readable names 绑定到 cryptographic owner 和 discovery metadata。**

名字 owner 最终由 private key 控制。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

#### 直觉

可以把它理解成一个巨大：

```
Map<String, NameRecord>
```

例如：

```
alice.id:
    owner = PK_A
    zonefile_hash = H123
```

但这个 Map 不存于一台数据库。

每个 BNS node：

```
Bitcoin blockchain
       ↓
replay
       ↓
自己算出同一个 Map
```

---

#### 一个重要误解

BNS 证明的是：

```
alice.id
属于拥有 private key K 的人
```

它不自动证明：

```
这个人现实中真的叫 Alice
```

论文也明确指出，BNS name 是 memorable identifier，名字本身不意味着现实身份；第三方可以另外提供 attestations。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

## Concept 2：Blockchain as an Ordered Log

这是整节课最重要的抽象之一。

不要想：

```
Blockchain = database
```

而应该想：

```
Blockchain = global append-only event log
```

例如：

```
Block 100:
    PREORDER ...

Block 102:
    REGISTER alice.id

Block 150:
    UPDATE alice.id

Block 300:
    TRANSFER alice.id
```

然后：

```
State = fold(log)
```

如果用函数表达：

```
S0 = empty

S1 = Apply(S0, op1)
S2 = Apply(S1, op2)
S3 = Apply(S2, op3)
...
```

非常接近：

```
Event Sourcing
+
State Machine Replication
```

---

## Concept 3：Virtualchain

#### 问题

假设 Bitcoin 根本不懂：

```
REGISTER_NAME
UPDATE_NAME
TRANSFER_NAME
```

怎么办？

最 naive：

> fork Bitcoin，实现新的 opcode。

这就是 Namecoin 一类路径。

问题是：

```
新 blockchain
   ↓
hash power 少
   ↓
更容易 51% attack
```

Blockstack团队此前运行 Namecoin 时观察到单一 mining pool 一度控制过半甚至更高比例的算力，这也是他们转向利用 Bitcoin 作为底层的重要动机。[USENIX](https://www.usenix.org/system/files/conference/atc16/atc16_paper-ali.pdf)

---

#### Blockstack 的答案

```
Bitcoin
  |
  | doesn't understand BNS
  v
transactions + OP_RETURN
  |
  v
Virtualchain
  |
  | understands BNS opcodes
  v
BNS State Machine
```

Virtualchain：

> **从底层 blockchain 中挑出属于自己的 operation，然后 deterministic replay。**

所以 Bitcoin miners 不需要知道：

```
REGISTER
TRANSFER
REVOKE
```

的语义。 [SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

## Concept 4：Control Plane vs Data Plane

这是一个你做 infrastructure 特别应该记住的设计。

如果把所有东西写入 blockchain：

```
alice.id
profile.json
photo.jpg
video.mp4
...
```

全网每个 full node 都得复制。

非常不合理。

Blockstack拆成：

```
              Control Plane
                   |
                   v
               Blockchain
                   |
       name -> zonefile hash
                   |
                   v

              Data Plane
          +--------+--------+
          |                 |
        Atlas              Gaia
     discovery data      user data
```

论文把 blockchain/Virtualchain 用于 trust bootstrap，而把 discovery 和 bulk storage 放在 data plane。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

这跟你熟悉的云基础设施 mental model 很像：

```
Kubernetes API
    control metadata

Object Storage / Data Plane
    actual bulk data
```

只是 trust model 完全不同。

---

## Concept 5：Commit-Reveal Name Registration

如果 Alice 直接发：

```
REGISTER alice.id
```

Bitcoin mempool 中 transaction 尚未确认。

Eve 看到了：

```
alice.id
```

于是：

```
Alice:
REGISTER alice.id fee=1

Eve:
REGISTER alice.id fee=10
```

矿工可能先打包 Eve。

这叫：

```
front-running
```

所以 BNS 采用两个阶段：

```
PREORDER
   ↓
REGISTER
```

先提交 commitment，再 reveal name。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

注意：

> **这里 paper 有时称 two-phase commit，但它不是你学过的 distributed transaction 2PC。**

这是极其容易混淆的地方。

数据库 2PC：

```
Coordinator

PREPARE
  ↓
participants vote

COMMIT / ABORT
```

BNS：

```
commitment
   ↓
reveal
```

更准确地说是：

```
commit-reveal protocol
```

---

## Concept 6：Atlas

Blockchain 只保存：

```
alice.id -> hash(zonefile)
```

但：

```
zonefile
```

实际在哪里？

这就是 Atlas。

2017 Blockstack从早期 Kademlia DHT 转向 Atlas。Atlas 中节点保存全部 zone files 的 replica，而不是像典型 DHT 那样每个节点只保存一小部分。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

结构大概：

```
       Atlas peer
       /   |   \
      /    |    \
 Atlas   Atlas   Atlas
   |       |       |
 full    full    full
copy     copy     copy
```

这里是一个非常有趣的工程选择：

```
传统直觉：

规模大
→ shard

Atlas：

metadata 足够小
→ 不 shard
→ full replicate
```

为什么？

因为它换来了：

```
simpler recovery
harder censorship
no DHT key-placement attack
```

这就是典型的：

> **利用 workload 特征换取简单性。**

---

## Concept 7：Gaia

Gaia 不承担：

```
Global Consensus
```

它承担的是：

```
Actual User Storage
```

例如：

```
Alice

profile.json
documents/
photos/
```

可以实际存在：

```
S3
Dropbox
Google Drive
FreeNAS
...
```

但 provider 只是：

```
"dumb drive"
```

数据可以：

```
signed
encrypted
replicated
```

关键链条是：

```
Blockchain
   ↓
hash(zonefile)
   ↓
Atlas
   ↓
zonefile
   ↓
URI
   ↓
Gaia / S3
   ↓
signed/encrypted user data
```

论文设计里，storage provider 不需要被信任来保证 **integrity**，因为客户端可以验证 hash/signature；但是 provider 仍然能够影响 **availability**。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

这两个性质必须严格分开：

```
Integrity ≠ Availability
```

---

# Part 4：System Model / Assumptions

Blockstack 和 Raft 最大的不同就是：它没有一个简单的：

```
N = 5
f = 2
```

模型。

---

### Node Model

#### Blockchain nodes / miners

允许：

```
malicious behavior
censorship
forks
competing blocks
```

因此模型比 crash-stop 强得多。

但不是说：

> arbitrary number of Byzantine miners 都无所谓。

安全依赖底层 blockchain 的 consensus assumptions。

对于当时使用的 Bitcoin：

```
majority hashing power
not controlled by attacker
+
no persistent severe network partition
```

非常重要。论文也强调底层 blockchain 的 security/reliability 会直接传递给上层。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

### Blockstack / Virtualchain Node

本质：

```
Blockchain log
   ↓
deterministic replay
   ↓
local derived state
```

节点可以 crash。

恢复后可以重新：

```
download / read blockchain
        ↓
replay
        ↓
rebuild database
```

所以很多 local state 是：

```
derived state
```

而不是唯一 truth。

---

## Network Model

应该认为：

```
messages can delay
messages can disappear
peers can disconnect
network may partition
```

不存在固定 latency upper bound。

因此不要把它想成：

```
synchronous system
```

更适合的 mental model 是：

> **asynchronous network + eventually sufficiently well-connected 的实际假设。**

长期 partition 会直接影响 Nakamoto consensus 的 security/liveness。

---

## Timing Model

Bitcoin-style consensus 没有类似：

```
message arrives within 500ms
```

的 guarantee。

所以：

```
strict synchronous ✗
```

更接近：

```
eventual / partially synchronous operational assumption
```

尤其 security analysis依赖：

```
fork eventually resolves
network eventually communicates
```

---

## Storage Model

Blockstack非常漂亮地把 storage 分层：

```
Blockchain:
persistent
globally replicated
control metadata

Virtualchain DB:
local derived persistent cache
reconstructible

Atlas:
fully replicated zone files

Gaia:
external persistent data
possibly replicated

User private key:
必须由用户自己可靠保存
```

Private key 尤其关键。

如果 Alice 丢掉：

```
SK_Alice
```

那 cryptographic ownership 本身可能就丢了。

---

## Failure Model

不能说：

```
最多容忍 f 个节点 crash
```

因为它不是固定 membership。

而应该分层看：

```
Blockchain failure
Atlas failure
Gaia failure
client/key failure
```

底层 blockchain 若失去 security：

```
majority attacker
deep reorg
persistent censorship
```

上层 naming 的 safety/liveness 都可能受到影响。

Atlas 即使大量 peer crash，只要还有可访问副本就可能恢复 zone files；Gaia的数据可用性则依赖实际 storage replicas。论文还描述了 cross-chain migration 以处理底层 chain failure。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

# Part 5：BNS 算法——从 Happy Path 开始

假设 Alice 要注册：

```
alice.id
```

---

### Happy Path：Step 1 PREORDER

Alice 先构造一个隐藏 name 的 commitment。

概念上：

```
C = H(alice.id, secret, owner information, ...)
```

然后：

```
Alice
  |
  | PREORDER(C)
  v
Bitcoin network
```

此时 Eve 看到：

```
C = 91ab3f...
```

但不知道 Alice 抢的是：

```
alice.id
```

---

### Step 2 等待确认

```
time →

Alice:   PREORDER ---------->
Bitcoin:      block N
                 |
                 | confirmations
                 v
             block N+k
```

Alice不应该看到 mempool 接收就认为完成。

因为：

```
unconfirmed transaction
```

还可能消失。

---

### Step 3 REGISTER / Reveal

然后 Alice 发：

```
REGISTER(
    name = alice.id,
    commitment-secret = ...,
    owner = PK_A
)
```

Virtualchain 检查：

```
有没有对应 preorder？
是否合法？
name 是否仍然 available？
```

如果合法：

```
absent
   ↓
preordered
   ↓
registered
```

---

## 如果 Alice 和 Eve 都想抢 alice.id 呢？

假设：

```
Alice preorder C_A
Eve   preorder C_E
```

后来都 reveal：

```
Block 100:
Alice REGISTER alice.id

Block 101:
Eve REGISTER alice.id
```

Virtualchain deterministic replay：

```
Block 100
alice.id = Alice

Block 101
Eve's REGISTER invalid
```

所以真正决定 conflict 的是：

```
canonical blockchain order
```

不是：

```
wall clock
```

也不是：

```
谁最先告诉我
```

---

## UPDATE

注册后：

```
alice.id
owner = PK_A
```

Alice 可以：

```
UPDATE alice.id
```

例如更新：

```
zonefile_hash = H2
```

只有合法 owner 才能发有效 update。

---

## TRANSFER

如果 Alice 转让给 Bob：

```
before:

alice.id -> PK_A

TRANSFER

after:

alice.id -> PK_B
```

从此 Alice 的旧 key：

```
PK_A
```

不能继续合法修改。

---

## REVOKE

还有：

```
REVOKE
```

使 name 进入 revoked 状态。

2016/2017设计中的核心 name lifecycle 包括 preorder、register、update、renew、transfer、revoke、expire。[USENIX](https://www.usenix.org/system/files/conference/atc16/atc16_paper-ali.pdf)

---

# Part 6：完整 Read Path

现在 Bob 想读取：

```
alice.id
```

这时真正有意思。

系统不是：

```
Bob -> blockchain -> Alice's 10 MB profile
```

而是：

```
Bob
 |
 v
Virtualchain / BNS
 |
 | alice.id
 v
hash(zonefile)
 |
 v
Atlas
 |
 | fetch zonefile
 v
verify hash(zonefile)
 |
 v
zonefile
 |
 | contains Gaia URI
 v
Gaia / S3 / Dropbox
 |
 v
Alice's actual data
 |
 v
verify signature / decrypt
```

论文给出的 Gaia lookup flow 正是这四层：先从 virtualchain 得 name/hash，再从 Atlas 获得 zone file，再取得 storage URI，最后读取并验证数据。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

## 为什么 Atlas 可以是不可信的？

假设恶意 Atlas peer 返回：

```
fake-zonefile
```

客户端已经从 blockchain 得到：

```
expected = H_real
```

然后：

```
H(fake-zonefile) != H_real
```

直接拒绝。

所以：

```
Atlas
```

可以影响：

```
availability
```

但不能轻易篡改：

```
integrity
```

这是一种非常重要的 distributed-systems design pattern：

> **Make remote storage untrusted but verifiable.**

---

## Gaia 为什么也可以“不可信”？

Alice 的数据：

```
profile.json
```

可能有：

```
signature = Sign(SK_Alice, data)
```

storage provider 修改：

```
Alice engineer
```

变成：

```
Alice attacker
```

signature verification：

```
Verify(PK_Alice, modified_data, signature)
            ↓
           false
```

所以：

```
S3 cannot forge authentic Alice data
```

但是它可以：

```
DELETE data
```

因此再次强调：

```
Integrity ✓
Availability 不一定 ✓
```

---

## 一个非常重要的 timeline

考虑 Alice 更新：

```
alice.id -> new zone file
```

```
time →

Alice:
    create zonefile Z2
       |
       | publish Z2
       v
Atlas: ---------------- Z2 propagated ---------------->

Alice:
       submit UPDATE(hash(Z2))
                |
                v
Bitcoin:
                    block 100
                       |
                    confirmation
                       |
                    confirmation
                       |
                       v

Bob:
                                       Read alice.id
```

这里实际上存在多个状态：

```
Z2 created
≠
Z2 available in Atlas
≠
UPDATE included in blockchain
≠
UPDATE sufficiently confirmed
```

所以千万不要把：

```
write submitted
```

和：

```
globally accepted
```

混为一谈。

这和 Raft 里：

```
leader append
≠
replicated
≠
committed
≠
applied
```

是同一种思维训练。

---

# Part 7：State / Invariants

把 BNS 简化成一个 state machine：

```
NameState {
    status
    owner
    zonefile_hash
    namespace
    expiration
}
```

状态大致：

```
             PREORDER
 absent ----------------> preordered
   ^                          |
   |                          | REGISTER
   |                          v
   |                     registered
   |                     /   |   \
   |               UPDATE    |   TRANSFER
   |                    \     |   /
   |                     \    |  /
   |                     registered
   |                          |
   |                       REVOKE
   |                          v
   +------ EXPIRE -------- revoked
```

---

## Invariant 1：Single Owner

在一个确定的 canonical history 上：

```
∀ name:
at most one current owner
```

例如不能同时：

```
alice.id -> PK_A
alice.id -> PK_E
```

都有效。

否则 human-readable unique name 就没有意义了。

---

## Invariant 2：Only Owner Can Mutate Ownership State

如果：

```
owner(alice.id) = PK_A
```

合法：

```
UPDATE signed by SK_A
TRANSFER signed by SK_A
```

非法：

```
UPDATE signed by SK_E
```

---

## Invariant 3：Derived State Is Deterministic

如果 Node A 和 Node B：

```
看到同一 canonical blockchain history
+
运行同一 virtualchain rules
```

则应该：

```
State_A == State_B
```

这就是 State Machine Replication 的灵魂。

---

## Invariant 4：On-chain Hash Commits to Off-chain Metadata

如果 blockchain 是：

```
alice.id -> H(Z)
```

那么接受：

```
Z'
```

必须满足：

```
H(Z') == H(Z)
```

这就是 control/data separation 能工作的关键。

---

# Part 8：Virtualchain 最值得理解的地方

现在把 Bitcoin 想成：

```
Block 100
  tx X
  tx Y
  tx Z

Block 101
  tx A
  tx B
```

Virtualchain 遍历：

```
tx X → irrelevant → ignore

tx Y → BNS REGISTER → process

tx Z → irrelevant → ignore

tx A → BNS UPDATE → process
```

产生：

```
Virtual Block 100:
REGISTER alice.id

Virtual Block 101:
UPDATE alice.id
```

这其实非常像：

```
Kafka topic
     ↓
consumer filters events
     ↓
materialized view
```

或者更准确地说：

```
Bitcoin
   =
shared immutable event log

Virtualchain
   =
deterministic consumer

BNS DB
   =
materialized state
```

这个类比对做 infrastructure 的人非常有帮助。

---

## Consensus Hash

Virtualchain还需要知道：

> 两个 node 是不是真的 replay 出了同一段 history？

于是引入：

```
Consensus Hash
```

简化理解：

```
block n operations
       +
previous history fingerprints
       ↓
      Hash
       ↓
    CH(n)
```

论文中的结构是：

\[ V_n = Merkle(\text{virtualchain transactions in block }n) \]

以及：

\[ CH(n)=Hash(V_n + P_n) \]

其中：

```
CH(n)
=
block n 时的 consensus hash

Vn
=
本 block 接受的 Virtualchain operations 的 Merkle root

Pn
=
过去一系列 consensus hash
```

而这系列不是：

```
n-1
n-2
n-3
n-4
...
```

而是大致：

```
n-1
n-2
n-4
n-8
n-16
...
```

也就是指数间隔。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

## 为什么用：

```
1, 2, 4, 8, 16...
```

因为这跟：

```
skip list
```

思路类似。

如果：

```
current block = 1024
```

要验证 block 10：

你不需要：

```
1024
1023
1022
...
10
```

一步一步走。

可以：

```
1024
 → 512
 → 256
 → 128
 → ...
```

所以 query complexity 可以接近：

\[ O(\log N) \]

这帮助 lightweight client / SNV 做历史验证。论文描述了利用 trusted consensus hash 从 untrusted full node 进行 logarithmic historical verification。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

# Part 9：Blockchain Fork 是怎么处理的？

这里是和 Raft 最大的区别之一。

Raft：

```
committed entry
```

具有非常强的“不回滚”含义。

Bitcoin：

```
Block N
```

刚出现时：

```
可能发生 fork
```

例如：

```
             B1
           /
A ---- B0
           \
             B2
```

Alice 的 REGISTER 在：

```
B1
```

Eve 的 REGISTER 在：

```
B2
```

于是短时间内：

```
Node A:
alice = Alice

Node B:
alice = Eve
```

这在 blockchain systems 中并不奇怪。

---

## Confirmation

Blockstack 的办法之一：

```
不要立即相信最新 block。
```

等：

```
1 confirmation
2
3
...
```

2017 whitepaper 描述的 Blockstack implementation 使用了 10 confirmations 来降低短 fork 导致的 loss/reordering 风险。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

所以：

```
latency
↑

reorg risk
↓
```

是直接 trade-off。

---

## Deep Reorg

如果发生：

```
非常深的 chain reorganization
```

事情就复杂了。

例如原来：

```
100 Alice REGISTER
101 ...
102 ...
...
200
```

然后一个更重的 alternate history 出现：

```
100 Eve REGISTER
101 ...
...
201
```

canonical history 改了。

于是：

```
过去认为 finalized 的 application state
```

可能不再一致。

Virtualchain consensus hashes 可以帮助发现这种 divergence，但 paper明确承认，对于 deep reorg 后已发生的 irreversible application effects，最终 reconciliation 可能需要 human intervention。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

这是非常重要的现实主义设计。

---

## 因此 Blockstack 是 Linearizable 吗？

不要简单回答：

```
是
```

或：

```
不是
```

更准确的是：

Bitcoin-style systems 有：

```
probabilistic finality
```

论文自己的表述是，在没有长期 partition 且多数算力诚实时，随着 confirmations 增加，较长 fork 概率快速下降，因此 transaction 在足够 confirmations 后“非常可能” durable，并被上层当作稳定顺序使用。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

所以它和 Raft 的：

```
deterministic safety
```

不是一回事。

记住：

```
Raft committed
       ≠
Bitcoin 10 confirmations
```

前者是协议 safety property。

后者依赖概率性 chain settlement assumption。

---

# Part 10：Atlas 为什么不用 DHT？

这是论文特别值得看的工程经验。

最开始他们用：

```
Kademlia DHT
```

优点：

```
O(log N) lookup
distributed storage
```

听起来很漂亮。

但生产环境遇到了：

```
churn
partition
Sybil/eclipse risk
reliability issues
```

论文报告，2015–2016 年间其 DHT deployment 出现过多次大的 overlay partition；后来切换到 Atlas full-replica architecture。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

### 为什么 full replication 反而合理？

关键观察：

```
zonefile 很小
```

它不是用户照片。

只是：

```
metadata
URI
keys
service pointers
```

因此：

```
Metadata size << User data size
```

既然 metadata 小：

```
每个 Atlas node
保存全部 metadata
```

反而简化很多东西。

论文当时测得约 70,000 个 BNS domain 的全部 zone files 只需要约 300 MB；这是 2017 时点的数据，不应理解为今天的容量结论。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

## DHT vs Atlas

```
DHT:

key1 -> nodes A/B/C
key2 -> nodes D/E/F

攻击某个 key
只需要攻击负责该 key 的节点。


Atlas:

Node A: all metadata
Node B: all metadata
Node C: all metadata
...
```

所以攻击者要完全 censor 某个 zonefile：

```
需要挡住大量 replica
```

论文把这种成本描述成大致从针对 DHT 某 key 的少数节点，提升到需要针对整个 O(N) replica population。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

## 一个需要修正的论文措辞

Whitepaper有一句很强：

> Atlas 中 “there is no concept of a network partition”，因为 unstructured 且每个节点 full replica。

教学上不要字面理解为：

```
Atlas impossible to partition
```

这是不对的。

物理网络当然仍然能：

```
US -----X----- Europe
```

真正的意思更接近：

> **Atlas避免了 DHT 那种“routing/key-space topology 自己裂成互不一致的 overlay partition”的特殊问题。**

IP/network partition 仍然可能发生。

这是读论文时应该保留的批判性思维。

---

# Part 11：Gaia 的 consistency 问题

Gaia里：

```
Blockchain
```

并不参与每一次：

```
save profile
save note
update file
```

否则写一次文件等十几分钟：

```
根本不可用。
```

所以：

```
app
 ↓
sign/encrypt
 ↓
PUT Gaia
```

可以直接完成。

这带来巨大 performance benefit。

但也带来一个问题：

```
stale data
```

例如：

```
version 1
version 2
```

某个 replica 仍返回：

```
version 1
```

signature 完全合法。

所以：

```
signature
```

只能告诉你：

> Alice 写过这个。

不能告诉你：

> 这是 Alice 最新写的。

论文也明确指出，mutable Gaia storage 需要额外 data-versioning scheme 来避免 stale reads。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

这是非常好的 distributed systems lesson：

> **Authenticity ≠ Freshness。**

同样：

```
Integrity ≠ Consistency
```

---

# Part 12：Correctness

现在正式回答：

> Blockstack 为什么是正确的？

要拆开来。

---

### Safety 1：Name Uniqueness

我们希望：

```
不可能在同一 canonical history 上
同时得到：

alice.id -> Alice
alice.id -> Eve
```

机制是：

```
canonical blockchain ordering
       +
deterministic Virtualchain transition rules
```

假设：

```
REGISTER Alice
```

先成为有效操作。

则：

```
REGISTER Eve
```

看到 state 已经：

```
registered
```

所以被拒绝。

---

### Safety 2：Ownership

攻击者不能简单：

```
UPDATE alice.id
```

因为 operation authorization 取决于 current owner key。

因此：

```
private key
```

代表 ownership capability。

---

### Safety 3：Off-chain Data Integrity

```
Blockchain:
H(zonefile)
```

Atlas 返回：

```
zonefile'
```

验证：

```
H(zonefile') == H(zonefile)?
```

Gaia data 则可以：

```
Verify(PK_A, data, signature)
```

因此 untrusted storage 无法悄悄改内容而不被发现。

---

## 但是 Safety 的前提是什么？

一个非常重要的条件：

```
canonical blockchain history
```

如果底层 Bitcoin consensus 被破坏：

```
51% attacker
deep reorg
persistent fork
```

BNS 的 global order 也就不再可信。

所以 Blockstack 没有凭空创造 security。

它做的是：

```
Bitcoin security
        ↓
inherit
        ↓
BNS security
```

---

## Liveness

希望：

```
合法 Alice REGISTER
```

最终能够进入 canonical blockchain。

这依赖：

```
miners继续产生 blocks
transaction能传播
miners愿意 include transaction
network eventually connected
```

如果攻击者 censorship：

```
Alice REGISTER
     ↓
miners continually ignore
```

那么：

```
Safety 可能仍然保持
Liveness 失败
```

这正是：

```
Safety ≠ Liveness
```

---

## Availability 与 Integrity 再分一次

假设 S3 删除 Alice 数据：

```
GET -> 404
```

系统不会返回：

```
fake Alice data
```

所以 integrity 仍然成立。

但是：

```
availability = broken
```

Blockstack没有魔法般让 S3 不能宕机。

只能通过：

```
replication
multiple storage backends
peer replication
```

改善。

---

# Part 13：Failure Matrix

|Failure|系统发生什么|Safety / Integrity|Availability|恢复|
|---|---|---|---|---|
|Blockstack node crash|本地服务中断|blockchain truth 不丢|单节点不可用|重启并 replay/rebuild|
|Atlas peer crash|少一个 metadata replica|hash 可验证|通常仍可从其他 replica 读|从 peers 重新同步|
|Atlas 返回伪造 zonefile|hash mismatch|保持|换 peer|从其他 replica fetch|
|Gaia provider 修改数据|signature/hash mismatch|保持|取决于其他 replica|从其他 storage 获取|
|Gaia provider 删除数据|数据不可读|integrity 没被破坏|降低/失败|replica / backup|
|Packet loss|request retry|通常保持|暂时下降|retry|
|Bitcoin short fork|暂时可能出现两个 history|confirmation 前不稳定|写延迟增加|等 canonical fork|
|Deep reorg|历史 state 可能改变|可检测，但应用后果复杂|可能暂停|paper允许 human reconciliation|
|Majority-hash attacker|可 reorg/censor|可能破坏|可能破坏|换 chain / migration 等|
|Alice private key lost|无人能继续合法操作|ownership机制仍工作|Alice失去控制|取决于预设 recovery 机制|

其中 deep-reorg detection、10-confirmation strategy 和 cross-chain migration 都是 Virtualchain 论文设计的一部分。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

# Part 14：Cross-chain Migration

Blockstack还有一个很大胆的观点：

> **底层 blockchain 本身也应该被当作可能失败的 dependency。**

这点特别像你做 cloud platform 时：

```
不要假设 AWS region 永远存在
```

于是：

```
Blockchain A
     ↓
BNS state
     ↓
migration
     ↓
Blockchain B
```

大致：

```
1. 宣布 A 在 future block H 停止
2. 在 A/B 都发 migration marker
3. freeze / lock transition
4. 把 current application state anchor 到 B
5. 验证 consensus hash
6. 开始接受 B 上的新 operation
```

2017 whitepaper确实描述了这样一种两阶段 cross-chain migration framework。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

这里有个非常深的 distributed-systems insight：

> **把 consensus substrate 当 abstraction，而不是把应用永远绑定到某个 implementation。**

---

# Part 15：Blockstack vs 你学过的 Fork Consistency

前一课你学 Certificate Transparency / Fork Consistency，这里又出现：

```
fork*-consistency
```

不要直接当成同一概念。

SUNDR 中重点是：

```
malicious server
```

可以 fork 两个 client：

```
Alice sees history H1
Bob sees history H2
```

但是一旦两个 client 的 history divergence：

```
不能再悄无声息 merge
```

Blockstack Virtualchain则借助：

```
blockchain fork
+
consensus hash
+
application log
```

来检测和约束应用 history divergence。

Whitepaper称其模型为 `fork*-consistency`，并要求新 transaction携带 latest known consensus hash，使 application 能识别 stale/unknown branch。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

你重点记：

```
SUNDR:
不可信文件服务器造成 equivocation

Blockstack:
底层 blockchain 本身可能 fork，
上层 state machine 如何识别 history branch
```

问题相似，但 threat source 和 mechanism 不一样。

---

# Part 16：Top 5 Misconceptions

### ❌ 1. Blockstack = 把文件存在 Bitcoin 上

错。

核心正相反：

```
blockchain
只放小型 commitment / operations

Atlas
放 discovery metadata

Gaia
放 bulk user data
```

---

### ❌ 2. PREORDER + REGISTER 就是数据库 2PC

错。

它是：

```
commit-reveal
```

目的主要是防：

```
front-running
```

没有：

```
Coordinator
Prepare
Vote
Atomic commit across participants
```

---

### ❌ 3. 有 signature 就一定是最新数据

错。

Signature只能证明：

```
Alice signed it
```

不能证明：

```
这是 Alice 最新版本
```

所以 Gaia mutable storage 仍然需要 versioning。

---

### ❌ 4. Blockchain consensus = Raft consensus

错。

Raft：

```
fixed membership
crash fault
quorum
deterministic safety
```

Bitcoin：

```
permissionless
adversarial
Proof of Work
temporary forks
probabilistic settlement
```

---

### ❌ 5. Decentralized storage = 数据必须存在 P2P disk 上

错。

Gaia甚至可以使用：

```
Amazon S3
Dropbox
Google Drive
```

Blockstack追求的是：

```
decentralized control
+
cryptographic ownership
+
provider portability
```

而不一定：

```
每一个 disk 都 decentralized
```

这是一个非常现代的设计思想。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

# Part 17：和真实系统的 Mental Model 联系

### etcd / Kubernetes

Kubernetes：

```
API request
   ↓
etcd / Raft
   ↓
canonical desired state
   ↓
controllers
```

Blockstack：

```
BNS operation
   ↓
Bitcoin
   ↓
canonical-ish ordered history
   ↓
Virtualchain
   ↓
canonical BNS state
```

共同模式：

```
Ordered Log
    ↓
Deterministic State Machine
    ↓
Replicated State
```

不同的是底层 trust model。

---

## Kubernetes Controller

Controller经常：

```
Watch event
   ↓
reconcile
   ↓
derive external state
```

Virtualchain：

```
Watch blockchain blocks
       ↓
extract operations
       ↓
validate
       ↓
apply
       ↓
derive BNS state
```

非常像：

```
event source
+
reconciler/materializer
```

但不要把 controller 本身误认为 consensus protocol。

---

## Terraform

Terraform state 说：

```
resource X
should exist like this
```

Blockstack blockchain log 则更像：

```
authoritative sequence of state transitions
```

一个有趣的共同思想是：

```
remote systems 不一定可信
local deterministic computation
可以重新 derive state
```

但 Terraform 并不解决 Byzantine consensus，所以类比到这里就停止。

---

## Kafka

这反而是一个非常好的数据流类比：

```
Kafka log
  ↓
consumer replay
  ↓
materialized view
```

对应：

```
Bitcoin log
  ↓
Virtualchain replay
  ↓
BNS DB
```

当然：

```
Kafka broker trust model
```

和：

```
Bitcoin
```

完全不同。

---

## Cloud Control Plane

这一课对做 Cloud Infra 特别值得记住：

```
small critical state
        ↓
strongly protected control plane

bulk user data
        ↓
scalable data plane
```

这是 Blockstack 的核心设计原则之一。

你做 Kubernetes / BYOC / control plane 时也经常这么做：

```
Control Plane:
metadata
ownership
policy
desired state

Data Plane:
actual workloads
traffic
bulk storage
```

---

# Part 18：Paper Problem

2017 whitepaper叫：

> **Blockstack: A New Internet for Decentralized Applications**

它把问题定义得比 2016 ATC paper 更大：

```
DNS trust
+
PKI trust
+
centralized application storage
```

然后提出三部分：

````
Blockchain / Virtualchain
    naming + trust bootstrap

Atlas
    discovery

Gaia
    storage
``` :chatgpt-content-reference{index="30"}


---

# Previous Approach：Namecoin

早期思路：

```text
需要新的 naming feature
       ↓
fork Bitcoin
       ↓
Namecoin
````

问题是新 chain：

```
smaller mining ecosystem
       ↓
less security
       ↓
majority concentration
```

Blockstack团队生产运行 Namecoin 后观察到了实际 security/reliability issues，于是得出一个很重要的经验：

> 不要轻易为了 application feature 自己启动一条弱 chain。 [USENIX](https://www.usenix.org/system/files/conference/atc16/atc16_paper-ali.pdf)

---

## Key Insight

整篇 paper 最值得记的不是：

```
Blockchain naming
```

而是：

> **把已经存在的强 blockchain 当作一个极小的 global consensus/control substrate，在它上面构造 application-specific State Machine，再把可扩展的数据路径全部移到链外。**

也就是：

```
                   Blockchain
                       |
                 minimal trust
                       |
                Total Ordered Ops
                       |
                       v
                  Virtualchain
                       |
                 BNS State
                       |
         +-------------+--------------+
         |                            |
       Atlas                         Gaia
    discovery metadata             bulk data
```

这实际上是一种：

```
Thin Consensus Layer
+
Rich Off-chain Data Plane
```

架构。

---

## Paper Evaluation

2017 whitepaper报告当时系统已有约 74,000 个 registered domains。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

Atlas部分报告：

```
之前 Kademlia deployment
出现过多个 major partition incidents

迁移 Atlas 后
在论文观察窗口内没有报告同类 outage

故意删除 Atlas local data 后
实验节点能够重新恢复
```

这是 paper 中的生产经验数据，不是说 Atlas 在数学上不会发生 partition。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

Gaia benchmark 中，论文报告 encryption/compression 带来的文件空间开销大约为几个百分点，对 100 MB 测试文件的 CPU overhead 约为秒级，wide-area场景主要仍受网络影响。再次强调，这些是 2017 implementation 的 measurements，而不是现代云性能结论。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

---

## Limitations

Blockstack没有解决几个非常重要的问题。

第一：

```
Blockchain scalability
```

每次重要 naming change 仍然受：

```
Bitcoin throughput
fees
confirmation latency
```

限制。

论文自己就把 underlying blockchain 视为 registration scalability bottleneck。[SEC](https://www.sec.gov/Archives/edgar/data/1719379/000110465919020748/a18-15736_1ex1a13tstwtrsd2.htm)

第二：

```
Name squatting
```

只要名字有价值：

```
alice.id
google.id
apple.id
```

就存在抢注。

所以需要：

```
pricing
renewal
economic mechanism
```

但这不是纯 distributed-consensus 技术能够解决的。

第三：

```
Key management
```

把 ownership 给用户意味着：

```
lose key
```

的代价也给用户。

第四：

```
Availability
```

Cryptographic verification不能让：

```
offline storage
```

神奇地上线。

第五：

```
probabilistic finality
```

底层 blockchain reorg 仍然存在。

---

## What aged well?

虽然 2017 的 Blockstack 具体 architecture 已经属于历史系统，但几个设计思想非常耐久：

```
1. on-chain commitments, off-chain bulk data

2. content hashes allow untrusted storage

3. signatures separate authenticity from storage trust

4. ordered log → deterministic materialized state

5. minimize consensus surface

6. control plane / data plane separation

7. user-owned identity/key rather than provider account
```

尤其：

> **只把真正需要全球 agreement 的东西放进昂贵 consensus path。**

这是很值得带回现代 distributed system design 的原则。

---

## What changed?

这里需要区分：

```
MIT Lecture 中的 Blockstack
```

和今天的 ecosystem。

原来的 Blockstack / Stacks 1.0 架构后来演化为 Stacks ecosystem；legacy Stacks 1.0 在 2021 年进入最终冻结/迁移阶段，`Blockstack` 也不再是该 ecosystem 的主要品牌名称。[Stacks Forum](https://forum.stacks.org/t/proposal-for-stacks-github-changes/12454?utm_source=chatgpt.com)

所以今天看到：

```
Stacks
Clarity
STX
Proof of Transfer
```

不要反过来混进 2017 Lecture 的 mental model。

学习这一课，应以：

```
BNS
Virtualchain
Atlas
Gaia
```

这套架构为准。

---

# Part 19：和 Lab 的关系

Blockstack没有像 Raft 那样一个：

```
Lab X = implement Blockstack
```

的直接对应。

但它高度依赖你在 Lab 里建立的 mental model：

```
Raft Lab
ordered log
→ deterministic apply
→ replicated state
```

对应：

```
Virtualchain
blockchain log
→ deterministic apply
→ BNS state
```

你真正应该从 Lab 带过来的 invariant 是：

> **所有正确 replica 如果处理相同的 operation sequence，就必须到达相同 state。**

---

# Part 20：Problem → Solution Chain

现在把整节课压成你要求的形式：

```
Problem
人类需要 human-readable global names
        ↓
Naive
中央 naming server
        ↓
Failure
central point of trust/control
        ↓
Idea
去中心化，让所有人自己发布 name binding
        ↓
Failure
两个用户可以同时声称 alice.id
        ↓
Mechanism
Blockchain global ordering
        ↓
Problem
Bitcoin 不认识 naming operations
        ↓
Mechanism
Virtualchain
        ↓
Problem
REGISTER 暴露在 mempool，可被 front-run
        ↓
Mechanism
PREORDER → REGISTER commit-reveal
        ↓
Problem
Blockchain 太慢、太小、太贵
        ↓
Mechanism
Control Plane / Data Plane separation
        ↓
Problem
zone files 去哪里？
        ↓
Naive
DHT
        ↓
Failure
churn / partition / eclipse / reliability
        ↓
Mechanism
Atlas full-replica peer network
        ↓
Problem
真正用户数据太大
        ↓
Mechanism
Gaia external storage
        ↓
Problem
storage provider 不可信
        ↓
Mechanism
hash + signature + encryption
        ↓
Problem
Blockchain 会 fork
        ↓
Mechanism
confirmations + consensus hashes
        ↓
Problem
deep chain failure
        ↓
Mechanism
detection + migration + possible human recovery
        ↓
Final Design

Blockchain / Virtualchain
      ↓
BNS ownership
      ↓
Atlas discovery
      ↓
Gaia data
```

这就是这节课真正的“问题演化链”。

---

## 30 秒版本

如果面试官问：

> Blockstack paper 讲了什么？

你可以说：

> Blockstack研究如何用 blockchain 构建 decentralized naming、identity 和 storage。核心思想不是把所有数据上链，而是把 Bitcoin 当作一个 tamper-resistant ordered log，在它上面通过 Virtualchain 构造 BNS State Machine，让 human-readable names 可以拥有 globally agreed cryptographic ownership。链上只保存关键 name bindings 和 hashes，Atlas负责分发 zone-file discovery metadata，Gaia负责真正的用户数据。这样把昂贵的 consensus control plane 和 scalable data plane 分离，同时通过 hashes 和 signatures 让链外 storage 可以是不可信的。

---

## 3 分钟版本

可以这样说：

> Blockstack首先面对的是 Zooko's Triangle：一个 naming system 很难同时做到 human-readable、globally unique 和 decentralized。传统 DNS 通过 central authority 得到 uniqueness，public key 则 decentralized 但不可读。Blockstack利用 Bitcoin 的 blockchain 给 naming operations 一个 global order。
> 
> 它没有修改 Bitcoin，而是在 Bitcoin transaction 上编码 BNS operations，由 Virtualchain 读取 canonical blockchain log，过滤自己的 operations，然后 deterministic replay 出全局 naming state。这跟 State Machine Replication 的思想非常类似。
> 
> 注册名字使用 PREORDER/REGISTER commit-reveal，防止攻击者从 mempool 看到名字后 front-run。名字注册、transfer、update 等 critical metadata 属于 control plane。
> 
> 由于 blockchain storage 很昂贵，Blockstack不会把真正的数据上链。Blockchain只保存 zone-file hash；Atlas复制 zone files，提供 discovery；zone file 指向 Gaia storage。Gaia可以实际用 S3、Dropbox 等，但用户数据被签名或加密，所以 storage provider不需要被信任来保证 integrity。
> 
> 最大 trade-off 是 blockchain 的 throughput、latency 和 probabilistic finality仍然向上传递。短 fork 用 confirmation 处理，deep reorg 可以检测但可能需要人为 reconciliation。因此 Blockstack最重要的 architecture lesson 是：把昂贵的 global consensus 只用于最小 trust-critical metadata，把 bulk data 放到可验证的链外 data plane。

---

## 深入版本

```
Problem
↓
Global human-readable decentralized naming

Model
↓
Untrusted/open participants
Unreliable network
Bitcoin/Nakamoto consensus
Untrusted peer/storage layers

Ordered Log
↓
Blockchain

Application State Machine
↓
Virtualchain

Naming
↓
BNS

Concurrent Registration
↓
PREORDER / REGISTER

Control State
↓
name
owner key
zonefile hash

Discovery
↓
Atlas

Bulk Storage
↓
Gaia

Integrity
↓
Hash + Signature

Safety
↓
deterministic ordering
authorization
hash commitments

Liveness
↓
depends on blockchain progress
transaction inclusion
network recovery
storage availability

Failure
↓
fork
censorship
peer loss
storage loss
deep reorg

Trade-offs
↓
decentralization/security
vs
latency/throughput/complexity
```

---

## 最后建立整张知识网络

```
                         Distributed Systems
                                |
              +-----------------+------------------+
              |                                    |
         Replication                         Decentralization
              |                                    |
          Primary/Backup                         Bitcoin
              |                                    |
             Raft                          Nakamoto Consensus
              |                                    |
       Replicated Log                     Global Ordered Log
              |                                    |
    State Machine Replication                      |
              |                                    v
              |                              Virtualchain
              |                                    |
              |                              BNS State Machine
              |                                    |
              |                     +--------------+-------------+
              |                     |                            |
              |                   Atlas                         Gaia
              |                 Discovery                     Storage
              |                     |                            |
              +---------------------+----------------------------+
                                    |
                           Control/Data Separation
                                    |
                         Verifiable Untrusted Storage
                                    |
                             hash + signature
```

如果只让我从这节课替你保存 **三个 mental hooks**，我会选择：

```
① Blockchain 在 Blockstack 中首先是 Ordered Log，
   不是大容量 Database。

② Virtualchain = 在别人的 Consensus Log 上，
   deterministic replay 自己的 State Machine。

③ Consensus 只保护最小 Control Plane；
   Atlas/Gaia 承担可扩展 Data Plane，
   用 hash/signature 把“可信”变成“可验证”。
```

其中 **② 是这节课和你前面学的 Raft / State Machine Replication 联系最深的地方，③ 则是对真实系统设计最有价值的地方。**

最后给你 5 个适合继续推理的题，不是背定义：

#### Level 1

假设 Alice 已经成功注册 `alice.id`，Eve 从 Atlas 返回了一个伪造 zone file。为什么 Eve 通常骗不了 Bob，但却仍然可以攻击 Bob 的 availability？

#### Level 2

Alice 的 Gaia storage 中同时存在：

```
profile-v1  signed by Alice
profile-v2  signed by Alice
```

恶意 storage provider 永远给 Bob 返回 `v1`。为什么 signature 完全不能解决这个问题？

#### Level 3

Alice 的 `REGISTER alice.id` 出现在 Bitcoin fork A，Eve 的 registration 出现在 fork B。此时为什么不能简单说“BNS 保证 alice.id 永远只有一个 owner”？

#### Level 4

假设删除 Virtualchain，让 Bitcoin miners 原生理解所有 BNS rules。你获得了什么，又失去了什么？

#### Level 5

假设让你今天设计一个 **multi-region globally unique customer-name service**，但参与者全属于同一家公司。你会使用 Blockstack/Bitcoin，还是 Raft-backed metadata service？关键不是回答哪个好，而是说明这两个方案的 **failure model 和 trust model 为什么完全不同**。
# 15 — 不变量，以及实现时会写错的地方

## Raft Invariants

每条都写：为什么重要，课上哪段代码或哪条规则在维持。没写出来的标成论文。

### currentTerm 永不减少

重要：旧 leader 必须能被认出来。Term 回退会让过期消息重新有效。

谁维持：见到更大的 term 就跳过去；开始选举只加不减。D5 把它放进 `handle_message` 前后的断言 (D5 1_part029)。

### 每个服务器每个 term 最多投一票

重要：否则同一 term 可能有两个多数派，两个 leader。

谁维持：`votedFor`。课上讲了规则，没落盘。论文要求在回复前写入。同一候选人的重试应再次投给他，而不是把“只回复一次”当成这条规定。

### Leader 不删除自己的日志条目

重要：已提交前缀在 leader 上必须还在，follower 才有可对齐的源头。

谁维持：leader 的追加函数总是成功，不走冲突删除。删除只在 follower 接受 AppendEntries 时发生 (D3 1_part001，1_part008)。

### commitIndex 永不减少

重要：执行许可不能收回。收回意味着你可能已经告诉客户端的事情不再算数。

谁维持：只在新值更大时赋值。D5 断言。Follower 的 min 是在和 leaderCommit 比，不是把已有 commitIndex 降下去。

### lastApplied 永不减少，且不超过 commitIndex

重要：不重复执行，也不执行未提交命令。

谁维持：server 里的 while 循环只加一，上限是 commitIndex (D4 2_part006)。论文的那一句是唯一的规格。

### 已提交条目出现在之后所有 leader 中

重要：这是“成功不会被推翻”的协议版。

谁维持：多数派交集、选举时的 up-to-date、以及只提交当前 term。缺最后一条就是 Figure 8。他的口语是“没有已提交条目就不可能赢”(D5 1_part004)。

### 相同 index + term ⇒ 相同前缀

重要：不必传输整份前缀，也能拒绝分叉。

谁维持：prevLogIndex/prevLogTerm 检查，加上 leader 在一个位置只写一次。名字 Log Matching 是论文的。

### 未提交可以消失，已提交不能

重要：划清客户端听到和没听到的边界。

谁维持：不提交就不回复成功；冲突删除只打在未提交后缀上。检测手段是“最后一条已提交条目不得变化”。没有这个检测，规则被写错你也看不见 (D5 2_part007)。

### 少数派不能提交

重要：分区两侧不能各写一份成功历史。

谁维持：中位数或等价的多数判断，含 leader 自己。3 要 2，5 要 3。etcd 死两台后超时是外部形式。

### 更高 term 的任何 RPC 都会把接收者变成 follower

重要：旧 leader 不靠自觉。

谁维持：Figure 2 的全局规则，D5 他在图上找到并读过。请求和响应都算。

## 课上真正撞见或故意制造的 bug

| 现象 | 原因 | 他怎么处理 |
| --- | --- | --- |
| 重试后 `setX42` 出现两次 | 响应丢失后 `append` | 改成按位置写，prev 相同则不变。要单测，不要等集群 (D2 2_part022) |
| index 怪，follower 停过再回来 | 把复制当成一对一的保存/确认，没有 per-follower 光标 | 不逐行修。要求 Figure 7 收敛 (D4 1_part001) |
| 控制台在演示 update 时崩了 | 未知 | 放弃那场 demo (D4 1_part006) |
| 上午选举什么都做就是选不出 | 未知。他提到重置超时和当选后立刻 AppendEntries 这两个细节 | 下午三节点看起来通了，没讲根因 (D5 1_part028) |
| 杀 follower 后 leader 异常 | socket broken pipe / connection refused；Rust `unwrap` 杀线程 | 丢掉这次发送；不要 unwrap (D5 1_part040) |
| flood 立即死 | 命令解析；他怀疑 send 非线程安全 | 没修完 (D5 1_part041) |
| 约五年每次重写都丢已提交条目 | 实现能覆盖已提交前缀。只在大量随机下出现 | 断言抓住，然后顺着打印查 (D5 1_part031) |
| 关掉 current-term 检查 | Figure 8 序列 | 两年才让测试撞上 (D5 2_part008) |
| 检查加回去，fuzzer 仍响 | 另一个实现 bug | 故意留着 (D5 2_part034) |
| Python 负索引 | off-by-one 得到 -1，读到最后一条 | 警告，不是当天的现场事故 (D2 2_part041) |
| 线程死了其余还在 | 去调“死锁” | `os._exit` (D4 1_part032) |
| ZeroMQ 版不可经典调试 | 结构，不是一条 if | 变成这门课的起因 (D1 1_part001) |
| 结对到第四天不说话 | 项目会制造残骸 | 残骸是预期，不是个人失败 (D1 1_part002) |

学生现场没被他修好的：消息到了 follower 但条目没接上；单条客户端请求 index 不对；Project 4 的基本冲突能过，仍不相信覆盖了 point 3 的全部角落；有人几乎忽略冲突删除。

## 以下是课程外补充

这些是 Raft 实现里常见、但字幕没有当成案例讲完的错误。不要写成“他在课上抓到了这个”。

- 选举计时器在错误的时候重置：自己发出 RequestVote 也重置，于是永远不开始下一轮；或者心跳到了也不重置。
- 旧 term 的 RPC 没有拒绝，只是“尽量处理”。
- 见到更高 term 只改数字，不 step down。
- 同一 term 把 `votedFor` 写成第二个人。
- `votedFor` 在回复之后才写盘，崩溃后投第二次。
- AppendEntries 冲突时只覆盖一条，留下后面的混血后缀。
- 没有冲突也删后缀。
- nextIndex 用新条目的 index 而不是 prev。
- 用 nextIndex 而不是 matchIndex 算提交。
- 提交不限制 current term。
- apply 不按顺序，或每次从 0 重放。
- 持有逻辑锁时同步发 RPC。
- RPC 重试被实现成再执行一次状态机命令，而不是再送一次日志。
- 计时器 goroutine 或线程泄漏，旧的超时在新 term 里开火。
- 忽略过期 term 的成功响应，把 matchIndex 写回旧值。
- 当选后不追加并复制一条本 term 条目，于是旧前缀永远不能提交。论文允许用客户端的下一条命令来充当这条；没有客户端流量时，只读和旧日志会卡住。空 no-op 是常见实现，不是这门课的要求。
- 读路径不确认领导权，线性读在分区时返回旧值。他讲了现象和 §8 草图，没有写成完整实现，所以“怎么写错读路径”仍算补充。
- 成员变更时同时存在两个配置的多数派。他知道 joint consensus 这个词，没写代码。

## 一条实用的自查顺序

出了事故时按这个顺序看，比通读 Figure 2 快：

1. 打印里的 term 是不是已经分叉，而某台还在用旧 term 发令。
2. 那台的 state 是不是还写着 Leader。
3. 失败的 AppendEntries 的 prev 是不是 nextIndex-1。
4. 成功之后 matchIndex 有没有动，commit 用的是不是 match 而不是 next。
5. 被提交的那条 term 是不是等于 leader 的 currentTerm。
6. lastApplied 是否跳过了某一格。
7. 有没有第二条线程也在改这些数。
8. 若是重启之后才错，先假设 term、投票、日志没落盘。这门课的代码在这一步本来就会错。

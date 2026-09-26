# 05 — Heartbeat 不是另一条协议

## 1. Problem

Follower 靠沉默判断 leader 是否还在。没有新命令时，如果 leader 什么都不发，所有 follower 会超时，开始选举，把一个健康的 leader 选下去。

同时 follower 不知道别的机器有没有复制成功。它们不互相说话。Leader 对客户端说“成功”的那一刻，follower 往往还没执行。必须有一条从 leader 到 follower 的通道，告诉它们：日志已经安全到第几条。

## 2. Naive Solution

- 单独做一个 `Heartbeat` RPC，只带“我还活着”。
- 只在有新命令时 `update`，另外用心跳填空档。学生自己说这更复杂 (D4 1_part006)。
- 每来一条客户端命令就立刻 AppendEntries。几百个客户端会把网络打满，而且上一条还没回来 (D4 1_part006–1_part007)。
- 心跳比 ping 还快，好“尽快发现故障”。如果往返要 10ms，更快的心跳学不到上一轮的结果 (D4 1_part008)。
- Follower 自己问其他 follower 提交到哪了。

## 3. Why It Fails

单独的心跳不带 `leaderCommit`，follower 就没有安全执行的信号。D2 他在一场混乱选举结束后盯着 visualizer：leader 一直在 ping。那个 ping 就是 AppendEntries，里面夹着“日志已经提交到哪、可以执行了” (D2 1_part003)。

只在提交时发送，落后的 follower 和空窗期的选举计时器都照顾不到。

每条命令一次 RPC：leader 在还不知道上一次是否成功时继续喷。后面的包是投机的。包太大也有反面：失败后回退、重新达成一致更慢。他用 50 条、后来 1000 条举例，没有规定批量大小 (D4 1_part006–1_part009)。

心跳快过物理延迟：你在制造还没人回答的消息。Roblox 面板上约 30ms，他当成可能已经是旧金山到弗吉尼亚的光速下限。Raft 不快。你在用性能换容错 (D4 1_part008–1_part009)。

## 4. Raft Solution

【课程内容】没有第二种 leader→follower 消息。

```text
AppendEntries(entries = [])
```

就是心跳。论文里那个容易误解的 “heartbeat” 就是它 (D4 1_part005)。他读到的句子是：AppendEntries 携带要存储的日志，空则是心跳，并且 **may** send more than one for efficiency。“may” 承担了全部批量规范 (D4 1_part007)。

一次心跳同时做四件事：

| 作用 | 怎么做 |
| --- | --- |
| 我还活着 | follower 收到当前 term 的 AppendEntries，重置选举计时 |
| 压选举 | candidate 收到当前或更高 term 的 AppendEntries，回到 follower |
| 传播 term | 字段 `term`。对方更旧就下台；对方更新就拒绝你 |
| 传播 commitIndex | 字段 `leaderCommit`。follower 用它前进，但不能超过自己已有的最后一条 |

空不代表无检查。`prevLogIndex` / `prevLogTerm` 仍在。对不上就失败，leader 把该 follower 的 nextIndex 减一。所以心跳也是发现分叉的探针。D3 visualizer 里就有 previous index 1、previous term 2、entries 为空的包，和另一条带条目的包同时在飞 (D3 1_part004)。

发送节奏：

- 必须在 follower 的选举超时之内到达，否则会被当成 leader 已死。
- 不必比 ping 更快。
- 不必完全均匀 (D4 1_part004)。
- 论文不要求所有 follower 同一个间隔。他不知道 etcd 是不是按 follower 分开 (D4 1_part008)。
- 可以等在途的 AppendEntries 回来，再把积压的命令打成一批。学生说的“每 x 毫秒把积压发出去，没有就发空的”他没有反对 (D4 1_part006–1_part007)。
- 加一条命令不必同步触发复制。时机可以过后由计时器触发。常识说应该马上发，协议不要求 (D4 1_part006)。

谁来触发，他没定 (D4 1_part004，1_part030–1_part032)：

```text
方案 A  外层计时器向 logic 投递 “update your followers”
方案 B  外层只投递时钟滴答，logic 自己数到该发心跳或该选举
方案 C  单独的 time-logic 对象，只看见滴答
```

他的顾虑：选举超时是算法的一部分。如果时间逻辑在 server、复制逻辑在 logic，算法就裂成两处。学生的反顾虑：server 已经在驱动控制台命令，follower 发现“好久没听到 leader”像是控制器的事。没有赢家。见 [12](12-concurrency-and-timers.md)。

重试没有预算。Follower 死了，leader 一直发，直到自己不再是 leader (D1 2_part001)。网络层如果 connect 失败，他倾向把这一条消息扔掉，不要在网络层做 Raft 重试。下一轮心跳会再试 (D2 1_part024)。

## 5. Example

三节点，没有新命令。

```text
时间 →   心跳间隔 << 选举超时   （关系，不是他给的毫秒数）

A Leader(T1):
  t=0   AppendEntries(entries=[], prev=3, leaderCommit=3) → B, C
  t=1   同上
  t=2   同上

B, C:
  每次收到都把选举倒计时拨回高点
  若自己的 commitIndex < 3，且本地日志至少到 3，就把 commitIndex 抬到 3
  然后 lastApplied 追上，执行

停掉 A 的发送:

B 的倒计时走到 0 → Candidate(T2)
```

D1 把 server 4 停掉再恢复：消息出去，没有回复，再出去，永远如此。活着的机器照常收到新条目。server 4 一恢复，下一次发送把它补上 (D1 2_part001)。

D4 他的控制台还没有计时器。`update` 命令就是用手按出来的心跳。他演示时把控制台弄崩了，demo 没做完。教训仍然有效：复制不是“命令一到就自动发生”，得有一个触发；那个触发以后应该变成计时器 (D4 1_part005–1_part006)。

## 6. Invariant

- Leader 在任期内持续发出 AppendEntries，有数据或没有数据。
- Follower 若在选举超时内听到当前 leader，就不会自己起来竞选。
- 心跳失败（超时或 prev 不匹配）不删除 leader 自己的日志。只移动针对那个 follower 的 nextIndex，或最终触发对方选举。
- commit 信息只从 leader 流向 follower，不在 follower 之间流动。
- 空 AppendEntries 仍然是一条会检查 term 和前缀的 RPC。

## 7. Failure Cases

**心跳丢失。** Follower 不前进 commit，也不重置计时。丢几次仍可能没超时。再丢下去就会选举。Leader 侧没有“失败计数器到 3 就把 follower 开除”。成员是固定的，这门课不改成员。

**心跳延迟，超过选举超时才到。** Follower 可能已经进入新 term。迟到的旧 term 心跳被拒绝。新 leader 已经存在。这是对的。

**心跳乱序。** 先到一个 prev 更靠后的，失败；后到一个 prev 刚好的，成功。Leader 的 nextIndex 只在成功时前进。乱序的失败回复如果被处理，可能把 nextIndex 多减。课上实现是减一重试，不是带世代的复杂确认。【从课程推导】稳妥的做法是：只接受当前 term 的回复，并且失败时不要减到已经 match 的位置以下。论文的优化是在失败响应里带上冲突的 index/term，一次跳一段。他选择不优化，作者也觉得没必要 (D3 1_part040)。

**分区中的旧 leader 继续发心跳。** 少数侧收得到，多数侧收不到。少数侧不会选出新人；多数侧会。旧 leader 的心跳改变不了多数侧。见 [09](09-failure-scenarios.md)。

**单向连通。** 一台机器能把投票请求喷出去，却收不到心跳。它会一直选举。这不是心跳间隔能修好的。D4 Cloudflare 故事，模型之外。pre-vote 不在论文里 (D4 1_part033–1_part034)。

## 8. Implementation Notes

他在 logic 里想要的接口不是“logic 自己 sleep”：

```text
handle_message(UpdateFollowers):
    for follower in peers:
        send AppendEntries built from nextIndex[follower]
        # send 只是把消息放进出列表
```

外层：

```text
loop:
    sleep(heartbeat_interval)   # 他没写这个函数，这是板上的方案之一
    inject UpdateFollowers
```

或者外层把滴答放进同一个事件队列，logic 决定这一拍是心跳还是选举超时。

不要在客户端命令处理函数里同步扇出 RPC。那会把“很多客户端”变成“很多在途复制”，也会把锁握在网络往返上。

D3 早上他就说不知道 `update_follower` 该由计时器、追加之后立刻、还是队列调用 (D3 1_part002)。到 D4 这个洞还在。这是实现问题，不是协议没说 leader 必须负责更新 follower。

## 9. Common Bugs

- 心跳不带 `leaderCommit`，follower 的状态机永远不跑。
- 心跳不带 prevLog，于是落后的 follower 看起来“活着且一致”。
- 每条客户端命令一条 AppendEntries，并且不等回复。nextIndex 被并发的发送搞乱。
- 心跳间隔大于选举超时。集群在没有故障时不停选举。
- 只有 leader 重置自己的计时器，follower 收到心跳却不重置。
- 收到任意 AppendEntries 都重置，包括旧 term。旧 leader 按住新选举。
- 把心跳做成不检查成功与否的 UDP 广播。失败路径（减 nextIndex）被跳过，分叉永不愈合。
- 网络层对心跳做应用级重试，和 logic 的周期发送叠在一起，重复和锁开始打架。

## 10. Course Insight

“我还活着”是心跳最不重要的那层含义。同一条空 AppendEntries 在传播任期、传播提交点、探测日志是否接得上、并阻止不必要的选举。

他把批量留给 “may”。工程上你要在“失败后回退的成本”和“往返次数”之间选一个数。协议不帮你选。Raft 的设计点不是低延迟，是故障时仍然只有一份历史。

# 06 — Log Replication：命令怎么走完一条命

## 1. Problem

客户端有一条命令，例如 `set x 42`。三台机器最后都要执行它，而且执行顺序要和别的命令一致。客户端还在等一个答复。中间任何一台可以没响应，任何一条消息可以丢。

D4 他把这个空档叫做奇迹：命令进入 Raft，一段时间之后，你才可以执行 (D4 2_part009)。

## 2. Naive Solution

- Leader 执行完，再把新状态广播给所有人。
- Leader 把命令 `append` 到 follower 的日志末尾。响应丢了就再 append 一次。
- 一条命令一次 RPC，发给所有人同一份载荷。
- 所有 follower 都回复了才算成功。
- Follower 一写入日志就执行，并回复客户端。
- Leader 一写入本地日志就回复客户端。

## 3. Why It Fails

**重试把命令写两遍。** 这是他在白板上故意制造的 bug (D2 2_part021–2_part022)。`setX42` 的响应丢了，计时器再发，follower 又 append 一次。日志里两份。他很强调：这就是你不能做的事。

**广播同一份载荷。** D3 visualizer：同一时刻，一条消息是 prev index 1、prev term 2、entries 为空；server 3 到 server 2 的消息是 prev index 0、prev term 0、带着条目 (D3 1_part003–1_part004)。各 follower 的落后程度不同。

**等所有人。** 死掉的 follower 永远不回复，提交就停了。多数派才是定义 (D3 2_part001)。

**本地写入就回复。** 这条命令可能只在 leader 上。Leader 崩溃后它不在任何人的日志里，或者只在少数派上，会被下一任覆盖。客户端却已经拿到成功。

**Follower 立刻执行。** 它们不知道这是不是多数派。D2：客户端听到成功，只说明 leader 执行了，不说明 follower 已经 apply。只说明命令已经保存在 follower 的日志里 (D2 1_part002–1_part003)。后一句要小心：在 current-term 规则讲完之前，“保存在多数 follower 上”仍不完全等于提交。D4 他先教多数派，D5 才补上那条限制。

## 4. Raft Solution

数据流：

```text
Client
  |
  v
Leader
  |  本地追加一条 (index, term, command)
  |
  +---- AppendEntries ---> Follower B
  |
  +---- AppendEntries ---> Follower C
  |
  多数成功
  |
  commitIndex 前进（还要满足当前 term，见 08）
  |
  apply 到状态机
  |
  reply Client
```

四个词不要并成“写完了”：

| 词 | 含义 | 客户端能看见吗 |
| --- | --- | --- |
| appended | 写进了某台的日志，可能只是 leader | 不能当成功 |
| replicated | 出现在至少另一台的日志里 | 仍不能，除非已是多数且规则允许提交 |
| committed | 已知不会被未来 leader 丢掉 | 这是可以对客户端承诺的边界 |
| applied | 状态机执行了 | leader 先执行再回复；follower 稍后 |

【课程内容】Leader 侧追加新命令总是成功。你是 leader，你说了算 (D3 1_part001)。失败发生在 follower 的 append entries：会制造洞，或者和已有条目冲突。

Follower 不执行来自客户端的命令。客户端只跟 leader 说话。不是 leader 就叫它走开 (D1 1_part041)。课上没有规定 follower 是否要在错误信息里指明现任 leader。他没做发现协议。

一次复制的 RPC：

```text
AppendEntries {
    term
    leaderId
    prevLogIndex
    prevLogTerm
    entries[]          可以多条，也可以空
    leaderCommit
}
```

```text
AppendEntriesResponse {
    term
    success
    # 论文 Figure 2 的结果只有这两项
    # visualizer 还有 matchIndex。见 07
}
```

接收侧要做的事，D3 要求按 Figure 2 的字面做，尤其不要跳过冲突时的删除 (D3 1_part008)。字段级规则在 [07](07-log-consistency.md)。这里只固定顺序：

```text
1. term 过旧 → false
2. prevLogIndex/prevLogTerm 对不上 → false
3. 同 index 不同 term → 删掉该条及之后所有
4. 把还没有的新条目接上
5. 用 leaderCommit 抬自己的 commitIndex，但不能超过自己现在的最后一条
```

Leader 侧：对每个 follower 单独组包，从 `nextIndex[follower]` 开始取条目。成功则前移光标；失败则减一重试。没有重试上限。

多数：3 台要 2，含 leader 自己；5 台要 3。Leader 自己算数，因为它的日志里当然有这条。

然后才 apply，然后才回复。回复的匹配是另一场噩梦，见本章实现笔记和 [08](08-commit-and-apply.md)。

## 5. Example

三节点，这是把 D2 的 `set name Guido` / `setX42` 收成一次完整写入。

```text
开始时大家日志为空（或只有他的 poison/index 0）
A 是 leader，term=1

Client → A: SET x=10

A 本地:
  index: 1
  term:  1
  cmd:   SET x=10

A → B: AppendEntries(term=1, prev=0, prevTerm=0, entries=[(1,1,SET x=10)], leaderCommit=0)
A → C: 同样的包   # 这一次恰好相同，因为两边都空。不要据此写成广播。

B 成功，C 的回复丢了

此时:
  appended:   A 有，B 有，C 没有
  replicated: 还不是多数？  A+B 已经是 3 台里的 2。多数已经达到。
  committed:  若 A 处理了 B 的成功，并且这条 term==currentTerm，可以 commit 1
  applied:    还没有，直到 A 的 apply 循环跑到

A apply SET x=10，回复 Client 成功

下一轮心跳或重试把条目和 leaderCommit=1 带给 C
C 写入，抬 commitIndex，再 apply
```

如果在 C 回复之前 A 就崩溃，见 [09](09-failure-scenarios.md)。关键是：客户端是否已经拿到成功。

D4 的现场：只启动 node 1，它成为 leader，`set x 42` 返回 pending。一台不够多数。再把别的服务器拉起来，才开始 commit，并打印 “applying SetX 42”。那个打印还不是真的 apply (D4 2_part032)。

## 6. Invariant

- 客户端命令只由 leader 追加。Follower 只追加 RPC 里带来的条目。
- Leader 不覆盖自己的日志。新命令只加在末尾。
- 同一 index 不会因为重试长出第二条。放置是按位置，不是按 append。
- 未达到提交条件之前，不回复客户端成功。
- 执行顺序等于日志顺序。可以晚，不能跳，不能换序 (D4 2_part005–2_part006)。
- 所有服务器最终在没有新故障、有多数派时日志会变得相同。这是他给 Figure 7 测试的 oracle：让它跑，日志应该相等 (D4 1_part003)。

## 7. Failure Cases

**复制前 leader 崩溃。** 条目只在本地，没人知道。下一任不会有它。客户端超时重试。正确。

**复制到一台 follower，没有多数，然后崩溃。** 5 台里 A+B 有 X，CDE 没有。X 未提交。新 leader 可以没有 X，并覆盖它。D5：只在两台上的尾巴会被吹掉，因为客户端没被通知 (D5 1_part003–1_part004)。

**已经 commit，回复客户端之前崩溃。** 条目还在多数派上，新 leader 必须带着它。客户端超时后重试，可能把同一逻辑命令再追加一次。Raft 会忠实复制重复。去重是应用的事 (D4 2_part041)。

**消息重复。** 同一 AppendEntries 到两次。prev 匹配、条目 term 相同，就是幂等的，不能再插一份。D3 学生把 idempotency 列为要测的情况 (D3 1_part007)。

**消息乱序。** 带 C1+C2 的包先到，只带 C1 的旧包后到。如果后到的包没有冲突，不能删除 C2。这是 D3 最重要的反例之一 (D3 1_part009–1_part010)。

**Follower 有更长的未提交尾巴。** Leader 的前缀检查会发现 term 冲突，follower 删掉冲突点之后的一切，再接上 leader 的条目。Leader 自己不删。

## 8. Implementation Notes

课上的最小成功不是五台 socket，是两台 logic、手工把消息递过去 (D3 2_part023–2_part024)：

```text
leader = RaftLogic(node=1, n=2)
follower = RaftLogic(node=2, n=2)

leader.handle(NewCommand("SET x=10"))
for msg in leader.get_outgoing():
    follower.handle(msg)
for msg in follower.get_outgoing():
    leader.handle(msg)
# 允许多轮，因为失败会减 nextIndex 再试
assert leader.log == follower.log
```

`send` 只是往列表里放。这是 chat 项目留下的形状 (D2 2_part019)。他关心的不是聊天，是：能收、能回、能在没有 socket 的测试里看见出去的消息。

真网络是 Project 5 后面可选的一步。假网络失败，真网络一定失败；假网络成功，真网络仍可能失败 (D3 2_part038)。

命令从 KV 的 socket 线程进 Raft，结果要从 apply 回到那条 socket。他试过的接法，全都没做完 (D4 2_part031–2_part041)：

- 立刻返回 pending。GET 不能这样，他要看到 42。
- `concurrent.futures`：submit 马上得到 future，`result()` 阻塞，或 done-callback。他看不到 future 怎么从提交函数穿越到 apply 函数。
- 阻塞在队列上。
- app callback：Raft 在 apply 时把命令交给应用。演示里 callback 能 `execute_command` 并得到 okay，okay 仍然到不了等在外面的客户端。
- UUID / 事务号：先还一个 id，客户端再来取。他后来在 D5 真的把 UUID 放进命令，放进 pending 表，apply 时用一个线程去通知。他说这很绕，也许有更优雅的，他没想到 (D5 2_part034–2_part035)。
- 学生的字典：客户端 → 日志 index，或用事务号而不是 index。

他的态度：Raft 不关心你用 future、队列还是别的。那是应用的问题 (D4 2_part036)。这也是为什么他认为接应用比选举更难 (D5 1_part000)。

库还是框架，他没选。Raft 包住应用，应用就得知道 Raft；应用包住 Raft，提交信号要从最内层喊到最外层。他说 everything is terrible，然后让大家去写 (D4 2_part007–2_part010)。

## 9. Common Bugs

- 响应丢失后 `log.append`。D2 的教学 bug。
- 所有 follower 共用一个 AppendEntries 载荷。
- 等齐所有回复才 commit。一台死，整个系统停。
- 在 `append_new_command` 里就 apply 并回复。
- 跳着 apply：commitIndex 从 3 跳到 5，直接执行 5。他和 Vin 讨论过，不允许有洞 (D4 2_part005)。
- 命令 4 比命令 3 先执行。计时可以不确定，顺序不行。
- 在持有锁时发 RPC，复制一慢，选举计时器和客户端一起卡死。
- 把“打印 applying”当成状态机已经跑了。这是他自己 D4 的状态。
- 单节点上期待 `set` 成功。他的 demo 返回 pending，因为没有多数。

## 10. Course Insight

复制不是把命令送到另一台就结束。一条命令有四段生命：本地追加、复制、提交、执行。客户端答复挂在提交并且执行之后。Follower 的执行更晚，靠后续 AppendEntries 里的 leaderCommit 追上来。

他希望你把测试写成“日志最后相等”，而不是“我看到一次 RPC 成功”。一次成功不够，Figure 7 那种落后、超前、term 冲突都要能在多轮消息之后收敛。

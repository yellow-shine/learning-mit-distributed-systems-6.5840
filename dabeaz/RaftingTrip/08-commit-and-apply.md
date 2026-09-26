# 08 — 什么叫提交，以及为什么还不能马上执行完就回复

## 1. Problem

日志里有一条命令。它已经出现在不止一台机器上。能不能执行？能不能告诉客户端成功？

学生的第一反应是：committed 等于已经落盘，applied 等于还没落盘。他把这两个词对调了 (D2 1_part003)。另一个反应是：所有 follower 都回复了再提交 (D3 2_part000)。第三个反应是：多数派复制了就可以把 commitIndex 移到那里，不管这条是哪一任 leader 写的。D4 他几乎就这么教了，然后在黑板上给当前 term 那条规则打了问号，留到 D5 (D4 2_part004)。

## 2. Naive Solution

```text
if majority(matchIndex) >= N:
    commitIndex = N
```

以及：

- 等所有人都回复。
- Follower 自己看本地日志长度，觉得够长就执行。
- commitIndex 一变，立刻把 `log[commitIndex]` 执行掉，中间的跳过。
- 客户端在 leader 本地 append 之后就收到成功。
- GET 不进 Raft，因为不改状态。

## 3. Why It Fails

**等所有人。** 一台死了，commit 永远不动。Visualizer 里两台停着，commitIndex 仍可以是 5，因为五台里的三台（含 leader）已经有这条 (D4 2_part001)。

**只看多数、不看 term。** 这是 Figure 8。他 D5 的转述不是论文插图的逐格拷贝，他在跟自己的画打架，还说了 “term 4 或 5 之类”。下面按他嘴里的因果写，再标出论文图的精确版。

他先给一个小例子，说明规则看起来多余 (D5 2_part000)：

```text
S1 成为 term 4 的 leader
它把已有日志复制到所有人
日志末尾的 term 是 3，currentTerm 是 4
看起来已经多数复制
不能把 commitIndex 移到末尾
只有再写入一条 term 4 的新条目，并且那条也被多数复制
才能把 commitIndex 移到那条 term 4 上
```

为什么旧 term 的多数不算数，他用 Figure 8 讲 (D5 2_part001–2_part003)：

```text
五台。S1 在复制中途崩溃。
S5 当选 term 3。它在自己的日志里放了一条 term 3 的条目，
还没复制出去就崩溃。
S1 回来，在更高 term（他说 4，或 4 或 5）当选。
它的日志和别人一样长，所以能赢。
它把“条目 2”复制到集群。看起来像提交了。
Figure 8 说：不行。

若你错误地标成已提交：
S5 重启，S1 崩溃。
S5 能赢，因为它日志里的 term 3 高于那个 “2”。
S5 把自己的条目复制到所有人，包括重启后的 S1。
你称为已提交的那条消失了。
```

极不可能，直到它发生。然后看起来像一笔丢了的交易，日志文件对不上，没人知道为什么 (D5 2_part002)。

修复：只能提交自己任期内的条目。一旦当前 term 的某条被提交，S5 就被锁在外面：它对自己和 S4 投得了赞成，另外三台反对，因为 term 4 比 term 3 优先 (D5 2_part003)。

【Raft 论文补充】论文 Figure 8 的标准序列更紧：

- (a) S1 是 term 2 的 leader，把 index 2 复制给 S2 后崩溃。
- (b) S5 以 term 3 当选（S3、S4 和自己），在 index 2 写下另一条，未复制即崩溃。
- (c) S1 重启，以 term 4 当选，继续复制它自己 index 2 上的 term-2 条目，现在 S1、S2、S3 都有。若 S1 据此提交，就错了。
- (d) S1 崩溃后 S5 可以当选，用自己的 term-3 条目覆盖 index 2，并复制到所有人。

课堂讲述和这张图是同一个洞，数字不要混用。他没有声称自己的转述等于印刷版的每一列。

**跳着 apply。** “如果 commitIndex 更大就 apply” 若被读成“直接 apply commitIndex 那一条”，中间会有洞。命令 4 不能跑在命令 3 前面 (D4 2_part005)。

**本地读。** 分区两侧都觉得自己是 leader 时，一边的 x 是 13，另一边是 42。只返回“已提交”仍不够，因为旧 leader 的已提交前缀是过期的世界。见本章后部和 [09](09-failure-scenarios.md)。

## 4. Raft Solution

### Leader 何时前进 commitIndex

【课程内容】D4 的算法，论文句子的一种实现：

论文的句子被他称为智商测试 (D4 2_part004)：若存在 N，N 大于 commitIndex，多数的 matchIndex ≥ N，则可以把 commitIndex 移到 N。句子没告诉你怎么算。

他的算法：

```text
把每个 follower 的 matchIndex 放进列表
把 leader 自己的最后一条也放进去   # leader 总是“有最多的”
排序
取中间那个                         # 5 台取第 3 个，3 台取第 2 个
那就是至少多数派都复制到的最高 index
若它大于当前 commitIndex，就前进
```

Visualizer 例子 (D4 2_part001–2_part002)：

```text
matchIndex: 8, 5, 8, 2, 7
排序:       2, 5, 7, 8, 8
中位数:     7
7 在多数机器上。图上 commit 在 7。
```

另一场：两台停了，leader 从回复里知道有的在 2、有的在 5，显示的 commitIndex 是 5，即三台里的最高。死掉的不阻止提交。

这段代码放在 AppendEntries 响应处理里。响应到达时才重算。不是一个后台扫描，除非你把重算放进心跳处理，而心跳响应也是这条路径。

**D4 版本不完整。** 还要：

```text
log[N].term == currentTerm
```

他当天打了问号，说是很可怕的边界，不讲故事 (D4 2_part004)。D5 讲完 Figure 8 之后，规则变成：

```text
找到最大的 N，使得
  多数 matchIndex >= N
  且 log[N].term == currentTerm
然后 commitIndex = N
```

中位数给出“多数派到达的最高点”。若那个点的 term 不是当前 term，不能把它设为 commitIndex。你可以向下找，直到碰到一条当前 term 的条目；找不到就不能只靠旧条目前进。

一旦当前 term 的 N 被提交，1..N 都提交。这依赖 log matching：能写下当前 term 的 N，前缀就和 leader 相同，而 leader 的前缀里含有更早的已提交条目。Figure 8 的洞是“旧 term 条目自己在多数派上，但没有一条更新的当前 term 条目把前缀钉死”。

【从课程推导】所以不是“多数复制”这个短语错了，是它少了一个量词：多数复制的那条，必须是本任 leader 写下的；本任之前的条因为这条被钉住，而不是因为它们自己碰巧在多数机器上。

### Follower 何时前进 commitIndex

Follower 不算多数派。它没有别人的 matchIndex。Leader 是唯一知道提交数据的服务器 (D4 2_part003–2_part004)。

```text
AppendEntries 里带 leaderCommit
若追加成功，且 leaderCommit > 自己的 commitIndex:
    commitIndex = min(leaderCommit, 自己日志最后一条的 index)
```

min 是因为 follower 可能落后，不能把 commitIndex 指到一条自己没有的日志上 (D4 2_part003)。他把这条定位成论文接收规则里的一点，口误听成 “basically 0.5”，对应 Figure 2 接收实现第 5 条。

### commitIndex 和 lastApplied

```text
log
 |
 | 复制到多数，且（leader 侧）本 term
 v
commitIndex     不会丢的最高位置
 |
 | 按顺序 apply
 v
lastApplied     状态机已经执行到的最高位置
 |
 v
State Machine
```

```text
lastApplied <= commitIndex
两者都不减少
```

论文里 lastApplied 大概只出现一次。他的转述：若发现 commitIndex 大于 lastApplied，就把 lastApplied 加一，并应用那一条 (D2 1_part004，D4 2_part005)。什么时候发现，论文不说。发现了再跑是安全的。

命令可以很慢。后面很多命令在 Raft 里 pending，等前面的执行完。提交可以跑在执行前面。这不是 bug (D2 1_part004–1_part005)。

他的放置：lastApplied 在 server 里，挨着 app，不在纯 logic 里。处理消息、发出响应之后：

```text
while lastApplied < logic.commitIndex:
    lastApplied += 1
    run log[lastApplied]
```

第一版只打印 “I'm running the command” (D4 2_part006，2_part009)。

已提交前缀冻结。右边什么都可能发生 (D4 2_part000)。D5 他把这个写成 assert：处理消息前后，最后一条已提交条目必须是同一条。他用这个 assert 抓到过连续多年的“丢掉已提交条目”，也在课程最后一天的 fuzzer 里又抓到一个不是 Figure 8 的 bug，并决定先留着 (D5 1_part031–1_part032，2_part034)。

### 客户端答复

成功答复表示：leader 已经执行。不表示每个 follower 已经执行。表示这条命令已进入不会丢的前缀 (D2 1_part003)。在 Figure 8 规则被实现之前，后半句可能是假的。所以 D4 的“多数即提交”不能单独上线。

GET 也要这个前缀，而且还要证明你问到的是现在的 leader。见 [09](09-failure-scenarios.md)。

## 5. Example

三节点，把中位数和 term 规则放在一起。

```text
A leader term=2
log:
  1: term1  C1     已在三台上
  2: term1  C2     已在三台上
  3: term2  C3     A 和 B 有，C 没有

matchIndex: A=3, B=3, C=2
排序: 2, 3, 3
中位数 = 3
log[3].term == 2 == currentTerm
commitIndex = 3
C1 C2 跟着安全，尽管它们的 term 是 1

若 A 还没写过 term2 的条目，日志停在 term1 的 C2，
即使三台都有 C2，currentTerm 是 2
也不能把 commitIndex 移到 2
要先写一条本 term 的条目（哪怕是 no-op）
```

【Raft 论文补充】很多实现在当选后立刻追加一条空命令并复制，用来推动 commitIndex 越过旧 term。课上没要求写 no-op。他的说法是：你得有一条本 term 的新条目被复制，才能前进。空命令是这条说法的一种实现，不是他演示过的代码。

五台中位数再看一次：`2, 5, 7, 8, 8` 的中间是 7。若 `log[7].term != currentTerm`，D4 的代码会错误提交，D5 的规则会拒绝，并可能提交更早的某条本 term 条目，如果存在的话。

## 6. Invariant

- commitIndex 不减少。
- lastApplied 不减少。
- lastApplied ≤ commitIndex。
- 已提交条目的内容不变。
- Leader 只把 commitIndex 移到本 term 的条目上。
- Follower 的 commitIndex 不超过自己日志的最后一条，也不超过 leader 宣布的值。
- 状态机在同一个 index 上不会执行两条不同命令。因为已提交前缀不会被改写，而 apply 按 index 顺序走。
- 客户端收到成功 ⇒ 该命令已在已提交前缀里。反向不成立：已提交但回复丢了。

## 7. Failure Cases

**Leader 提交后、回复前崩溃。** 新 leader 带得走这条。客户端超时。重试可能再追加一条。应用要有请求序号 (D4 2_part041，论文 §8)。

**Follower 的 leaderCommit 超前于本地日志。** min 把它夹住。下一次成功的 AppendEntries 补上条目后，再抬。

**旧 leader 在少数派上前进 matchIndex。** 它只有少数回复，中位数到不了新条目。不能提交。这是分区安全的一半。另一半是 term，防止它用旧身份去干扰多数派。

**错误实现提交了旧 term 条目。** Figure 8 序列能把那条抹掉。检测手段是“已提交条目不得变化”的 assert。没有这个 assert，你甚至不知道自己有这个 bug。他花了大约两年才让测试真的撞上 Figure 8 那种序列；关掉 term 检查、加上 assert、用很特定的随机调度 (D5 2_part007–2_part008)。断言通过也不证明没有 bug。那种序列可以跑几千小时不出现。

## 8. Implementation Notes

```text
on AppendEntries response from follower i:
    if response.term > currentTerm: step down; return
    if response.term < currentTerm: ignore
    if success:
        matchIndex[i] = prev_sent + len(entries_sent)
        nextIndex[i] = matchIndex[i] + 1
        maybe_advance_commit()
    else:
        nextIndex[i] = max(1, nextIndex[i] - 1)

maybe_advance_commit():
    # D4: median
    # D5: median 只是候选，还要 log[N].term == currentTerm
    # 更直接的论文写法是从后往前找最大的满足条件的 N
```

Apply 不要放在 RPC 处理的深处又去回调网络。他的循环在 server 层，logic 只暴露 commitIndex。

D5 的客户端匹配：命令带 UUID，进 pending 表，apply 时按 UUID 通知那个在等待的线程 (D5 2_part035)。这解决“哪一个客户端”，不解决“会不会执行两次”。两次执行要靠 §8 的客户端序列号，应用记住每个客户端处理过的最大序号。他当天把 UUID 接上了，没有把去重讲成已完成的代码。

## 9. Common Bugs

- 用 nextIndex 算中位数。nextIndex 是下一格，比已确认位置大 1，提交会超前。
- 排序取错位置。5 台取第 2 个或第 4 个，多数派算错。0-based 的下标是 `len // 2`，对奇数集群刚好是中间。
- 忘了把 leader 自己算进去。Follower 的 matchIndex 都不含 leader 时，两台 follower 的成功在三台集群里已经够，若你只排序 follower 会算错。
- D4 的中位数直接赋值，没有 current-term 检查。这是他明确推迟、然后用 Figure 8 说明必须补上的 bug。
- Follower 把 commitIndex 设成 leaderCommit，不取 min。指向不存在的条目，apply 崩溃或读到别的内存。
- apply 用 `for i in range(commitIndex)` 每次从头执行。lastApplied 就是为了避免这个。
- 多线程 apply。两个循环同时跑 lastApplied。
- 提交了就回复，但 apply 失败或还没跑。客户端看到成功，状态机没有那个键。他的演示曾经停在“打印 applying”而不是执行。
- 只读不走提交证明。见下一章。

## 10. Course Insight

提交不是“复制得够多”这一个事实。它是“复制得够多，并且这一任 leader 用自己的条目把这段历史钉死了”。钉死之前，多数派上的旧条目仍可能被一个带着更高 term 尾巴的节点在下一次选举里覆盖。

执行是另一件事。commitIndex 是许可，lastApplied 是进度。许可可以领先进度。进度不能领先许可，也不能跳。

他想让你对 Figure 8 保持不舒服。那条规则看起来像多余的 if。去掉它，系统在你能用手点出来的演示里仍然正常。这就是它危险的原因。

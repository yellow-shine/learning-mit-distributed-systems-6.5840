# 02 — 节点上到底存着什么

## 1. Problem

Figure 1 只说“有一份日志”。真要写代码，你得知道每个数字是谁的、崩溃后还在不在、什么时候允许变。这些名字长得像，课上学生和讲师自己都混过：

```text
log index
term
commitIndex
lastApplied
nextIndex
matchIndex
```

【课程内容】他不是第一天把 Figure 2 的状态表抄完的。D2 说 logic 里最终会有 current term、commit index、voted-for，“以及诸如此类”，logic 就是未来的 Figure 2 (D2 2_part001)。哪些要持久化，他当天没划线。D5 才有学生问持久化，他的回答是“怎么写到盘上是个可怕的问题”，不是一张清单 (D5 2_part038)。

所以下表里“论文要求持久化”和“这门课实现了持久化”是两列。不要并成一列。

## 2. Naive Solution

把所有数字都放进一个叫 `state` 的字典，崩溃就从零开始，leader 和 follower 共用同一套字段。或者反过来：每个 RPC 字段都在每个节点上存一份。

另一种混淆是用数组下标代替逻辑 index，用 `len(log)` 代替 lastLogIndex (D2 2_part039–2_part040)。论文是 1-based，而且论文谈的是 last log index，不是你手头这个数组的长度。日志一旦被截断，这两个数就分家。

## 3. Why It Fails

- `len` 和 lastLogIndex 不是一回事。他在 Project 4 结尾专门警告 (D2 2_part040)。
- Python 里 index 算成 -1 不会抛异常，`log[-1]` 是最后一条。算法炸了，你不知道为什么 (D2 2_part041)。
- 把 nextIndex 当成全集群一个光标：死掉的 follower 会把活着的拖住，或者活着的进度会让死掉的收到一个它对不上的前缀 (D3 1_part037)。
- 把 commitIndex 和 lastApplied 合成一个：命令可能跑很久，后面已经提交的命令必须排队，不能等应用跑完才叫提交 (D2 1_part004–1_part005)。
- 新 leader 以为自己继承了旧 leader 的 nextIndex 表。Visualizer 里那张表随旧 leader 一起死了 (D3 1_part039)。
- 在 apply 的时候才把状态写入磁盘。学生这么想，他不接受：要持久的是日志，而且要在掉电后还在的介质上，不是“应用执行了所以现在写盘” (D5 2_part038–2_part039)。

## 4. Raft Solution

【课程内容】+【Raft 论文补充】。论文 Figure 2 是规范。课上他读到过其中几行，没有逐行实现持久化。

### 所有服务器

| 变量 | 谁维护 | 生命周期 | 论文要求持久化？ | 课上做了吗 | 何时更新 | 解决什么 |
| --- | --- | --- | --- | --- | --- | --- |
| `currentTerm` | 每个服务器 | 单调增加，永不减少 | 是 | 没有落盘 | 开始选举时 +1；见到更大的 term 时跳到那个 term 并变 follower | 区分不同领导时期；让旧消息失效 |
| `votedFor` | 每个服务器 | 每个 term 最多一个 id，term 变了就作废 | 是，且必须在回复投票前落盘 | 没有 | 在本 term 第一次投出赞成票时 | 防止同一 term 投两票，从而防止两个 leader |
| `log[]` | 每个服务器 | 已提交前缀只增不改；未提交后缀可被删 | 是，追加后、回复成功前应落盘 | 他说日志必须持久，没做 | leader 本地追加；follower 在一致性检查通过后追加或删后缀 | 状态机的输入序列 |
| `commitIndex` | 每个服务器 | 单调增加 | 否，可重算，但重算规则不平凡 | 内存里做了 | leader 看到当前 term 的条目复制到多数派；follower 听从 `leaderCommit`，且不能超过自己已有的最后一条 | “不会丢”的最高位置 |
| `lastApplied` | 每个服务器 | 单调增加，且 `<= commitIndex` | 否 | 放在 server 侧，不放进纯 logic | 发现 `commitIndex > lastApplied` 时 +1 并执行那一条 | 状态机实际跑到哪 |

日志条目里至少有：命令、term。index 是位置。他在 D2 不确定 index 要不要存进条目里；D5 他第一次把 index 放进条目，写完了，坐在那里不知道喜不喜欢 (D2 2_part039，D5 2_part037)。

【课程内容】空日志是他拒绝处理的角落。创建日志对象必须带初始条目。学生放一条 dummy / poison，这样 index 0 存在。他重启时不再插入 poison，因为持久化应该把数据留住——而持久化他又没做。这是教学脚手架，不是论文规则 (D3 1_part007)。

### 只有 leader 有

| 变量 | 含义 | 持久化？ | 初始化 | 更新 |
| --- | --- | --- | --- | --- |
| `nextIndex[follower]` | 下一次准备发给这个 follower 的日志 index | 否。新 leader 没有旧表 | 变成 leader 时，对每个 follower 设成自己的下一条（论文：`lastLogIndex+1`） | 成功后前移；失败后减一，不能减过左端 |
| `matchIndex[follower]` | 已知该 follower 已经成功复制到的最高 index | 否 | 论文里初始化为 0 | 成功复制后更新 |

【课程内容】这两个数组是 leader 本地知识，不是复制状态。Follower 变成 follower 时不应该还带着它们。学生建议用 `Some` / `None`：是 leader 才有这张表 (D5 1_part030)。他同意这个方向，没有写成必做。

### 六个容易混的词

```text
index
  日志槽位。论文从 1 开始。
  “下一条要放的位置”和“前一条的位置”差 1。
  AppendEntries 里的 prevLogIndex 是 nextIndex - 1，不是新条目自己的 index。

term
  第几次领导时期，不是条目个数，也不是 index。
  日志条目上的 term 是“谁在自己的任期内写下它”，不是“当前服务器的 currentTerm”。
  一条旧条目可以在 currentTerm=4 的 leader 日志里仍然标着 term=2。

commitIndex
  已知安全、不会被未来 leader 丢掉的最高 index。
  不是“我本地有多长”，也不是“我已经执行到哪”。

lastApplied
  状态机已经执行到的最高 index。
  可以落后于 commitIndex。落后是正常的，不是 bug。

nextIndex
  leader 对某个 follower 的发送光标：下一次从这里开始送。
  它超前于 matchIndex。失败说明光标太靠前。

matchIndex
  leader 对某个 follower 的确认光标：我知道你已经有到这里。
  commit 看的是它，不是 nextIndex。
```

例子 (D3 visualizer 的形状)：

```text
Leader log:  1 2 3 4 5 6

Follower B 已经有 1 2 3

matchIndex[B] = 3
nextIndex[B]  = 4

发给 B 的 AppendEntries:
  prevLogIndex = 3
  prevLogTerm  = log[3].term
  entries      = log[4...]
```

成功之后 visualizer 里如果 follower 最高到 3，next 变成 4。他后来写成 `nextIndex = matchIndex + 1`，不是 `+= len(entries)` 那种他曾经怀疑的加法 (D3 1_part041，D3 2_part026)。

## 5. Example

五台，leader 是 S3，这是 D3 visualizer 里他实际指给学生看的形状，数字来自那场演示 (D3 1_part036–1_part037)：

```text
刚写入第一条:
  AppendEntries.prevLogIndex = 0
  prevLogTerm = 0
  新条目想放在位置 1
  成功后该 follower 的 nextIndex = 2

再写一条:
  nextIndex 变成 3
  消息里的 previous index 是 2

停掉 S5 再写入:
  活着的 nextIndex = 4
  死掉的停在 3
  发给 S5 的包仍是 prev=2, term=2，并且一直重试
  发给活机器的 prev=3

再停 S2，继续写:
  有的 nextIndex = 7
  死机器分别滞后在 5 和 3
```

一张全集群共享的 nextIndex 无法描述这张图。

## 6. Invariant

课上他后来真的写进 `handle_message` 前后的 (D5 1_part029–1_part031)：

- `currentTerm` 不减少。
- `commitIndex` 不减少。
- 处理消息之前记下最后一条已提交条目，处理之后它必须还是它。已提交就是冻结。

【Raft 论文补充】Figure 2 还有这些，课上没有全部写成 assert：

- 每个服务器每个 term 最多投一票（`votedFor`）。
- `lastApplied` 不减少，且 `lastApplied <= commitIndex`。
- leader 不覆盖、不删除自己的日志。删除后缀只发生在 follower 接受 AppendEntries 时。
- `matchIndex[i] < nextIndex[i]`，除非你把“下一条”和“已匹配”写反。
- 相同 index 且相同 term 的两条日志，它们的前缀也相同。这是 log matching，不是一个你赋值能维持的变量，是 AppendEntries 检查维持的性质。

## 7. Failure Cases

| 丢失什么 | 会发生什么 | 课上是否演示 |
| --- | --- | --- |
| 只丢内存里的 nextIndex/matchIndex | 新 leader 本来就要重建。乐观设成自己的下一位置，失败再退 | visualizer 演示了 |
| 丢 commitIndex | 理论上可从日志和多数派重算，但 current-term 规则让“重算”并不平凡。论文把它列为易失是因为只要日志和 term 在，协议还能往前走；重启后 commitIndex 常从 0 或快照点重新爬 | 没做 |
| 丢 lastApplied | 可能把已执行命令再执行一次。所以应用自己要能认出重复，或者 lastApplied 跟快照一起走 | 没做。他把 apply 和持久化的关系留成开放题 |
| 丢 currentTerm | 重启后用旧 term 发令，或在新 term 里再次投票 | 论文规则，课上没实现 |
| 丢 votedFor | 同一 term 投两次，可能选出两个 leader | 论文规则，课上没讲成实现步骤 |
| 丢 log | 已提交命令消失。这是他用 fuzzer 抓过的那类事故 | D5 用断言抓“已提交条目被改掉”，不是用磁盘 |

进程崩溃后再启动，和网络分区，不是同一个状态丢失。分区时状态还在，只是消息过不来。

## 8. Implementation Notes

他的对象边界 (D2 1_part026–1_part027，D3)：

```text
RaftServer          控制器。身份是节点号
  network           箭头
  RaftLogic         Figure 2 的规则和易失角色状态
  RaftLog           日志对象。Project 4 只测这个
  App               状态机。黑盒
```

Leader 专有的 nextIndex / matchIndex 放在 logic 里，而且只在自己是 leader 时有意义。

日志操作从 D3 起分成两个，不要合成一个 (D3 1_part000–1_part001)：

```text
leader:  append_new_command(term, command)     总是成功
follower: append_entries(prevIndex, prevTerm, entries, ...)
                                              可以失败
```

Project 4 不要把 commitIndex 放进日志对象。提交是集群事实，日志对象要在没有消息的情况下被测死 (D3 2_part001)。

D4 他把 lastApplied 放在 server、紧挨着 app，不放进纯 logic。处理完消息、发出去之后：

```text
while lastApplied < logic.commitIndex:
    lastApplied += 1
    app.run(log[lastApplied])
```

这是他的放置，不是论文规定的线程。论文只说：如果你发现 commitIndex 更大，就增加 lastApplied 并应用 (D2 1_part004，D4 2_part005–2_part006)。

## 9. Common Bugs

- `prevLogIndex = nextIndex`，而不是 `nextIndex - 1`。他称为 off-by-one 的高发区 (D3 1_part036)。
- 成功时 `nextIndex += len(entries)`。如果有的条目本来就在，或者 prev 的算法和你想的不一样，这个加法是猜。他改成 matchIndex + 1。
- 失败时 `nextIndex -= 1` 减到 0 以下，再撞上 Python 的负索引 (D3 2_part027，D2 2_part041)。
- 用 `len(log)` 当 lastLogIndex。
- 1-based 论文直接套 0-based 数组，又不存逻辑 index。
- follower 角色下还留着 nextIndex，于是一张过期表被当成知识。
- 把 matchIndex 当成“条目条数”而不是最高 index。他口头也滑过 “this many log entries”，随后用排序取中位数时用的是 index (D4 2_part000–2_part002)。
- 在日志对象里更新 commitIndex。
- 重启时再插入一条 poison，和持久化日志里的第一条打架 (D3 1_part007，这是学生的担心，不是他演示出来的事故)。

## 10. Course Insight

状态不是一份“Raft 结构体”抄下来就完了。每个数都在回答一个问题：我处在第几个领导时期、我把票给了谁、命令序列是什么、我可以安全执行到哪、我实际执行到哪、我对这一个 follower 的发送光标在哪、我确认他已经有到哪。

发送光标和确认光标必须分开。执行光标和提交光标必须分开。逻辑 index 和数组下标必须分开。这三对分开，比多写一个类更重要。

【Raft 论文补充】Figure 2 把持久状态写成三行：`currentTerm`、`votedFor`、`log[]`。易失状态是 `commitIndex`、`lastApplied`，外加 leader 的两张表。这门课把“日志必须活过崩溃”说清楚了，把另外两项的落盘时机留给了你和论文。见 [11](11-persistence-and-recovery.md)。

# 03 — 三种角色，和 term 为什么不是装饰

## 1. Problem

多台机器都在跑同一份代码。某一刻必须只有一个写入源。机器会死，网络会安静，旧的写入源会醒来并继续发令。

如果角色只是一个字符串 `"leader"`，没有任何单调的时期编号，醒来的旧 leader 无法从消息本身判断自己已经过期。

## 2. Naive Solution

- 配置文件写死 server 1 是 leader。这是他前四天允许、甚至鼓励的测试捷径 (D2 1_part000，D5 1_part000)。
- 超时了就直接把自己标成 leader。
- 谁最后发过言谁就是 leader。
- 用墙钟时间戳比较谁更新。
- Follower、Candidate、Leader 写成三个毫无共享状态的类，或者反过来所有行为堆在一个函数里靠 if 硬切。他没禁止三个类，只说共享逻辑怎么协调是问题，当天不做 (D2 2_part001)。

## 3. Why It Fails

写死 leader：server 1 一死，复制停。这可以用来把日志调通，不能用来交最终系统。

超时即 leader：D1 visualizer 里两台几乎同时到 0，都会去抢。他没有把那场演示做成选不出来，但消息量已经“极大”，两边在打架 (D1 2_part003)。

没有 term 的分区：

```text
Term 概念还不存在时的坏故事

A 是负责人，复制了 C1 C2，正在复制 C3
网络切开
A 侧:  A 和 S3 有 C3，A 仍觉得自己负责
另一侧三台选了新负责人，写入 C4

愈合之后，两边都觉得自己说了算
没有一个数字可以让 A 的后续消息被拒绝
```

这就是 D2 2_part024–2_part026 的 C3/C4 图，只是他当时已经开始用 term 3 和 term 4 当解药。没有那个数字，AppendEntries 无法判断“前一条”是不是同一次领导写下的。

学生还提出过：收到拒绝就按更高 term 下台？如果拒绝不携带对方的 term，发送者只会把 index 往回退再试，旧 leader 停不下来。他当场犹豫了一下，然后回到规则：回复里带上更高的 term，收到更高 term 就立刻变 follower (D5 1_part001)。

## 4. Raft Solution

### 角色

【课程内容】三种状态，D1 在 visualizer 里看见，D5 才写成规则。

```text
                 election timeout
                    （没听到当前 leader）
Follower ----------------------------------> Candidate
   ^                                            |
   | 收到当前 leader 的 AppendEntries            | 获得多数票
   | 或收到任何更高 term 的 RPC                   | （RequestVote 成功数含自己）
   |                                            v
   +---------------------------------------- Leader
                    更高 term 也让 Leader 下来
```

真实转换比这张简图多。把课上说过的补全：

| 事件 | 谁 | 变成 |
| --- | --- | --- |
| election timeout，且没听到 leader | Follower | Candidate。`currentTerm += 1`，投自己，发 RequestVote |
| 收到本 term 的 AppendEntries（来自合法 leader） | Follower 或 Candidate | 若是 Candidate，选举失败，回到 Follower。重置选举计时 |
| 获得多数票 | Candidate | Leader。立刻发 AppendEntries，宣布统治 (D5 1_part028) |
| 本 term 没有人赢，自己的选举超时再次到期 | Candidate（已变回 follower 或仍在等） | 新 term，再选一次。超时是随机的，所以换一个人先醒的概率高 (D5 1_part004) |
| 任何 RPC 的请求或响应，`term > currentTerm` | 任何角色 | `currentTerm = term`，变 Follower。这是全局规则，不是只对 AppendEntries (D5 1_part002) |
| 收到 `term < currentTerm` | 任何角色 | 不要执行。回复 false，并带上自己的 term，让对方自己下来 (D5 1_part001，Figure 2) |
| Leader 发现自己的 term 过期 | Leader | 下来。他的说法是 follower 不再主动发消息，所以上一任会停止发起通信 (D5 1_part002) |
| 启动 | 所有人 | Follower。没有 leader。坐等超时 (D5 1_part006) |

【从课程推导】Raft 是事件驱动状态机，不是三个线程。输入事件是：计时器到点、RequestVote、RequestVote 响应、AppendEntries、AppendEntries 响应、客户端命令。角色只决定这些事件的处理函数。这和他从 traffic light 移植过来的 `handle_message` 是同一个形状。计时器不要睡在状态机里面。见 [12](12-concurrency-and-timers.md)。

失败的选举不是错误。Candidate 输掉后，在本 term 剩余时间里当 follower；要么给别人投票，要么再超时进入新 term (D5 1_part004)。两台机器会互相变 candidate，但谁也凑不齐多数，这是他 D5 demo 里故意先给两台看的 (D5 1_part039)。

### Term

不要只说“term 像逻辑时钟”。要说它在堵什么。

```text
时期 1: A 是 leader

分区

时期 2: B 当选

A 恢复，仍认为自己是时期 1 的 leader
```

A 发出的 AppendEntries 带着 term=1。B 的 currentTerm 已经是 2。B 拒绝，并在回复里放 2。A 看到更大的 term，改掉自己的 term，变成 follower。从此 A 不能再提交任何东西。

规则就两条，他在 Figure 2 上指过 (D5 1_part001–1_part002)：

```text
if RPC.term > currentTerm:
    currentTerm = RPC.term
    become follower

if RPC.term < currentTerm:
    reply false (and include currentTerm)
    do not apply / do not vote
```

更高的 term 永远赢。Term 每次有人开始选举就加一，不是每条日志加一，也不是“当前 leader 的编号” (D2 2_part027，D5 1_part000)。选举失败，term 已经消耗掉，不会退回去。所以你可能看到 term 从 1 跳到 4，中间的选举没人赢。

日志条目上的 term 是写入时 leader 的 currentTerm。它用来比较“这条记录来自哪一次领导”，不是用来表示 index。

### Term 在两个 RPC 里各干什么

**RequestVote**

- 参数里的 `term` 是候选人已经加过一的新 term。例子：原来日志最后一条是 term 3，RPC 里的 term 应该是 4；如果前面有失败选举，会更高 (D5 1_part002–1_part003)。
- 另外两个字段 `lastLogIndex`、`lastLogTerm` 不是 term 本身，是“我的日志有多新”。投票规则在 [04](04-leader-election.md)。
- 如果投票者的 currentTerm 更大，拒绝，并让候选人下来。
- 如果候选人的 term 更大，投票者先更新 term、变成 follower，然后再决定投不投。更新 term 不等于必须投赞成。

**AppendEntries**

- `term` 证明发送者认为自己是哪一任 leader。
- 旧 term 的 AppendEntries 不能被执行。否则旧 leader 会把新日志覆盖掉。
- 新 term 的 AppendEntries 会把 candidate 打回 follower。这就是心跳压选举的一半：另一半是重置超时。
- `prevLogTerm` 是另一回事：它是前一条日志的 term，用来做前缀检查，不是发送者的 currentTerm。D2 的玩具用“前一项的内容”演示，真正上线时比的是 term (D2 2_part023–2_part026)。

## 5. Example

三节点。先用课程里的叙事，不提前使用 Figure 8 的细节。

```text
时间 →

A: Follower --timeout--> Candidate(term=1) --2 票--> Leader(term=1)
B: Follower ---------------- vote A ---------------------- Follower
C: Follower ---------------- vote A ---------------------- Follower

A 复制 set x=1 ，条目是 (index=1, term=1)

分区: A | B C

A: 仍是 Leader(term=1)，心跳出不去，选举计时在 B/C 上到期

B: timeout --> Candidate(term=2) --> 得到 C 的票 --> Leader(term=2)
C: vote B，变 Follower(term=2)

A 此时:
  仍然可以在本地 append
  得不到多数派的 AppendEntries 成功
  因此不能 commit，也不能对客户端说成功

分区愈合，B 的 AppendEntries(term=2) 到达 A

A: term 2 > 1 => currentTerm=2，变 Follower
   之后只听 B 的
```

D5 的课堂例子是五节点，数字不同，结构相同：server 5 用 last index 7、last term 3 去要票，两台只有更短日志的投了赞成，两台有 8 条的投了反对，加上自己一票，3/5 当选。反对票不会因为它“是 no”就让候选人下台——那些 no 的 term 和这次选举相同 (D5 1_part003)。

## 6. Invariant

- `currentTerm` 永不减少。见到更大的就跳过去，不会加一加一地追。
- 任一 term 最多一个 leader。课上他用“每 term 只能投一次”加“要多数票”来支撑，没有做形式证明。见 [10](10-safety-and-liveness.md)。
- 收到更高 term 的任何请求或响应，立刻不再以旧角色发号施令。
- 旧 term 的消息不能改变日志，除了“把发送者自己打下来”所需要的那次回复。
- Follower 不主动复制日志。他的口语是 follower 不再把消息发出去 (D5 1_part002)。【从课程推导】这指的是不再发起选举和 AppendEntries。对 RPC 的回复仍然要发，否则对方不知道你的 term。他的句子如果理解成“follower 的 socket 完全沉默”，会把 term 传播弄断。回复是 Figure 2 要求的。

## 7. Failure Cases

**延迟的旧 AppendEntries。** Leader 已经进入 term 5，一条 term 3 的包才到。接收者 term 更高，拒绝。不存在“虽然旧，但内容看起来对，就应用一下”。

**重复的 RequestVote。** 同一候选人因重试再发一次。`votedFor` 已经是他，可以再次投他。投给另一个候选人则不行。这是论文规则；课上强调的是“一次选举不能投两次” (D5 1_part003)。

**乱序。** 先到 term 4 的心跳，再到 term 3 的投票请求。处理 term 4 时已经把 currentTerm 抬高，term 3 的请求会被拒绝。所以处理函数必须先看 term，再看业务。

**Candidate 收到另一位同 term 候选人的 RequestVote。** 自己已经投给自己，所以拒绝。两人都不放弃的话，这个 term 可能选不出来。随机超时让下一轮只有一个先醒。D1 看到的就是这种打架，最后有一个赢了；他没有展示永远选不出的那一次 (D1 2_part003)。

**旧 leader 在分区的少数侧。** 它保持 Leader(旧 term) 直到看见更高 term。安全不依赖它“自觉下台”，依赖它凑不齐多数派。见 [09](09-failure-scenarios.md)。

## 8. Implementation Notes

课上的实现顺序故意把这一章放到最后 (D5 1_part006–1_part008)：

- 先有日志对象，再有复制，最后加两种消息：RequestVote 和 RequestVote 响应。
- 如果复制还是坏的，不要把选举堆上去。他原话接近：别让它更坏。
- 他自己的选举在 D5 上午“什么都做了，就是选不出 leader”。他没有在录音里讲出那一个 bug。下午三节点 demo 看起来通了。不要把“他后来通了”写成“选举很容易”。
- 变成 leader 的那一刻要做的事，他点了两件 (D5 1_part028)：立刻发 AppendEntries 宣示；以及 D3 已经讲过的，把每个 follower 的 nextIndex 初始化成自己的下一位置。
- 收到当前 leader 的 AppendEntries 要重置选举计时。漏了这一条，follower 会在 leader 健康时也起来造反。这是他列出的“琐碎但致命”的细节之一。

【Raft 论文补充】Figure 2 的 Candidates 一节还有：开始选举时重置选举计时器；收到同 term 的 AppendEntries 说明已有 leader，回到 follower。课上 demo 的行为符合这些，他没有逐条朗读。

## 9. Common Bugs

- 只在 AppendEntries 里检查 term，RequestVote 响应里的更高 term 被忽略。旧 leader 收到“我拒绝你，而且我是 term 6”却继续发心跳。
- 更高 term 只更新数字，不改变角色。
- 同一 term 里 `votedFor` 被第二次写入。
- 超时路径上忘了 `currentTerm += 1` 再发投票。于是和现任 leader 用同一个 term 抢。
- 变成 leader 后不立刻发心跳，等一个心跳周期。在这个窗口里，其他人的选举计时器可能也到了。
- 收到 AppendEntries 就重置计时，不看 term。旧 leader 的迟到心跳把新选举按掉。
- 把日志条目的 term 和 currentTerm 当成同一个变量。Figure 8 整个故事都建立在“日志末尾的 term 可以小于 currentTerm”上。
- 三个角色三个线程，同时改 `currentTerm`。

## 10. Course Insight

Term 是“第几任领导”的编号，用来让旧命令失效。角色是这个编号之下你正在做什么。

他希望你记住的操作感是：任何一条消息都先被 term 过滤。过滤通过之前，不要追加日志，不要投票，不要提交。过滤失败时，不要沉默——把你的 term 送回去，让对方下台。

另一件他花了很多口舌的事：不要因为论文第 5.2 节先写选举，就先写选举。选举的难度是假的。它复用的是你已经为日志复制建好的消息、term 和计时器。反过来，选得出 leader 但复制是错的，项目就空了 (D5 1_part006–1_part007)。

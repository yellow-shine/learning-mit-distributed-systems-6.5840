# 07 — 日志怎么接上：prevLog、冲突、nextIndex、matchIndex

## 1. Problem

Leader 和某个 follower 的日志可能：

- 完全相同。重试不应该改任何东西。
- follower 更短。中间不能留洞。
- follower 更长，多出来的是另一任 leader 写下的未提交尾巴。
- 同样的 index 上 term 不同。

RPC 还会迟到。一个只含旧前缀的包，可能在含更长前缀的包之后到达。

## 2. Naive Solution

- `follower.log.append(command)`。
- 只送 index，不送前一条的 term。
- 只要 index 已经被占，就删掉后缀。
- 问 follower：“你的日志到哪了？”然后一次跳到那里。
- 新 leader 沿用旧 leader 的 nextIndex 表。
- 成功回复只有一个布尔，leader 用 `nextIndex += len(entries)` 猜测。

## 3. Why It Fails

D2 的 Python 玩具把原因拆开了。日志里已有 ABC。函数签名是他口中“最怪的 append”：previous index、previous item、new item (D2 2_part022–2_part023)。

| 调用 | 结果 | 在堵什么 |
| --- | --- | --- |
| prev=0, prev item=ABC, 新项 | 写入 | 正常追加 |
| 再调一次同样的 | 日志不变 | 重试 |
| prev=2, prev item=DEF，但没有第 2 项 | 拒绝 | 洞。死了很久的 follower 会被指到一个大缺口上 |
| prev=1, 声称前一项是 DEF，实际不是 | 拒绝 | 两边对历史有分歧 |

只比内容还不够。分区之后两边可以在“看起来接得上的位置”写下不同命令。必须比 term (D2 2_part024–2_part026)。

C3/C4，五台，他的白板 (D2 2_part024–2_part025)：

```text
开始: C1 C2 已在所有机器上

客户端提交 C3
然后网络切开

一侧: leader 和 S3 有 C3          两台，不是多数，客户端等不到成功
另一侧三台选了新 leader（他说 server 1）
新客户端写入 C4

愈合时，两边都没看见对方的分歧
旧 leader 仍觉得自己是 leader
旧客户端可能还连着它，只是得不到响应
```

学生说客户端会知道该离开旧 leader。他否定。新 leader 的存在不等于旧 leader 自己知道。

如果 AppendEntries 只说“在这个 index 写上”，不说前一条的 term，愈合时无法判断该信哪边。

**过于Eager的删除。** D3 (1_part009–1_part010)：

```text
Leader 发出只含 C1 的 AppendEntries，包在网络里耽搁
Leader 又发出含 C1+C2 的包，先到达
Follower 已经有 C1 C2，而且可能已经复制到别人，甚至已经提交

迟到的单条 C1 到达
如果处理函数顺手把后缀删了，C2 没了
```

学生以为迟到的 C1 会被整个丢掉。不对。C1 本身没有冲突，应该保持，并且不能动 C2。MIT 学生指南把 Figure 2 里那个 “if” 标成关键。他让大家拿自己的代码去对 (D3 1_part008–1_part010)。

**问 follower 到哪了。** 可以优化，论文作者觉得没必要。Ousterhout 的实用主义，按他的转述：Raft 跑在有电池的数据中心机器上，不是壁橱里的机器，长时间落后不值得用复杂度去换。他引用的精神是：最好的性能改进是从不能跑到能跑 (D3 1_part040)。页码他没说完，不要编。

## 4. Raft Solution

### 每个字段为什么在

```text
AppendEntries {
    term           发送者的当前任期。旧的拒绝，新的让接收者下台
    leaderId       谁在当 leader。客户端发现、日志都用得上；协议安全主要不靠它

    prevLogIndex   新条目之前那一条的 index
    prevLogTerm    那一条的 term
                   两者一起证明：在这个位置之前，我和你是同一条历史

    entries[]      从 prev 之后要接上的条目。可以空（心跳），可以多条（批量）

    leaderCommit   leader 的 commitIndex。follower 不会自己算多数派
}
```

一致性检查：

```text
如果日志在 prevLogIndex 没有条目: 拒绝     # 太短，或洞
如果那一条的 term != prevLogTerm:   拒绝     # 历史分叉
```

通过之后：

```text
对 entries 里的每一条，按 index 看本地:
    没有 → 接上
    有，term 相同 → 不动          # 重试 / 迟到的非冲突包
    有，term 不同 → 删掉这条及后面所有，再接上 leader 的版本
```

【课程内容】删除规则是 Figure 2 接收实现。学生差点因为“日志应该不可变”忽略它。不可变的只是已提交前缀。未提交后缀在分区愈合时必须能被当前 leader 替换 (D3 1_part008–1_part009)。

他没有在 C3/C4 图上把删除一步步走完。规则停在 Figure 2 那句：冲突则删除该条及之后所有。哪一边在愈合后当 leader，取决于谁的 term 更高、谁拿到多数，不取决于谁的日志更长。

### Log Matching Property

【Raft 论文补充】名字是论文的。机制是课上这两条检查。

如果两份日志在某个 index 上有相同 term 的条目，那么：

- 这一条的命令相同（同一任 leader 在这个 index 上只写过一条，而且 leader 不改自己的日志）。
- 这个 index 之前的所有条目也相同。

归纳：要写下这条，prev 必须匹配；prev 匹配意味着前一条 index+term 相同；前一条相同又推出更前面相同。所以 `(index, term)` 是整条前缀的指纹，不必把前缀再传一遍。

这就是为什么只比 index 不够。Index 5 在两份日志里可以都存在，term 却一个是 2、一个是 4。指纹不同，前缀不可信。

### 例子：哪里匹配，哪里冲突

用户给出的形状，用课程规则走一遍。这是【从课程推导】，不是他白板上的那一张。

```text
Leader
index  1 2 3 4 5
term   1 1 2 2 3
       A B C D E

Follower
index  1 2 3 4 5
term   1 1 2 4 4
       A B C X Y
```

假设 leader 的 nextIndex 一开始被乐观地设成 6（“你应该和我一样长，下一条是 6”）。

```text
尝试 prev=5
Follower 的 index 5 term 是 4，leader 的 log[5].term 是 3
不一致 → false
nextIndex = 5

尝试 prev=4
Follower index 4 term 4，leader log[4].term 是 2
false
nextIndex = 4

尝试 prev=3
Follower index 3 term 2，leader log[3].term 是 2
匹配。共同前缀是 A B C
entries = [D@4 term2, E@5 term3]
Follower 在 index 4 发现 term 4 != 2，冲突
删除 X Y（index 4 及之后）
接上 D E
```

如果 leader 第一次就送 prev=3，结果一样，只是少几次失败。减一是在不知道分叉点时的搜索。

### nextIndex 和 matchIndex

```text
nextIndex[follower]
  下一次准备发给它的 index
  组包时:
    prevLogIndex = nextIndex - 1
    prevLogTerm  = log[prevLogIndex].term
    entries      = log[nextIndex : ]

matchIndex[follower]
  已知它已经成功复制到的最高 index
  commit 时看这个，不看 nextIndex
```

```text
Leader: 1 2 3 4 5 6
B 已有: 1 2 3

matchIndex[B] = 3
nextIndex[B]  = 4
```

失败：`nextIndex[B] -= 1`，立刻或等下一次心跳再试。不能减过左端。TLA 规格有这个下界，他的代码当时没做 (D3 2_part027)。

成功：他最终的写法是 `nextIndex = matchIndex + 1`。

【课程内容】vs【Raft 论文补充】这里必须分开，因为他在课堂上说重了。

- Visualizer 的 AppendEntries 回复里有 match index。TLA 规格把 matchIndex 定义成 previous index 加上成功追加的条数。三条约前进三 (D3 1_part041，2_part026)。
- 他读 Figure 2 的结果说明，只看到 term 和 success，没有 match index。他一度说论文里完全没提。
- 【Raft 论文补充】Figure 2 **状态**里 leader 有 `matchIndex[]`。**RPC 结果**里没有这个字段。Leader 在成功时自己更新：`matchIndex[i] = prevLogIndex + len(entries)`，`nextIndex[i] = matchIndex[i] + 1`。Follower 不必回报，因为 leader 知道自己送了什么，而 success 表示那些条目都被接受了。
- 把 matchIndex 放进回复能用，也是 visualizer 的做法。它不是论文 RPC 的必需字段。如果回复先于你发出的更新到达，或者你把旧回复套到新的 prev 上，额外字段会帮你，也会骗你。至少要忽略过期 term 的回复。

新 leader 没有这张表 (D3 1_part039–1_part040)：

```text
变成 leader 时:
  对每个 follower:
    nextIndex = 自己的最后一条 + 1
    matchIndex = 0

Visualizer: 新 leader 把大家都设成 7
发给只有 2 条的 S5:
  试图在位置 6 追加，prev term 2
  S5 返回 false
  7→6，再失败，6→5，5→4，一直减
理论上减够了就会接上。他没在演示里等到成功。
```

这就是“新 leader 不了解集群” (D4 1_part003)。第一轮乐观假设大家和自己一样，失败就是发现过程。

### 课程没有做的优化

【Raft 论文补充】Figure 2 下面的注释：follower 可以在拒绝时带回冲突条目的 term 和那个 term 的第一条 index，leader 一次跳过一整段。他明确不写。先让减一工作。

## 5. Example

三节点愈合，把 D2 的分区收成可以单步跟踪的版本。

```text
A leader term=1
日志: (1,1,C1) (2,1,C2) (3,1,C3)
只有 A 和 B 有 C3。C 没有。不是 3 中的 2？ 
A+B 已是多数。若 A 已 commit C3，故事不该删 C3。

改成他的“两台不是多数”版本，用五台更贴切:
五台，C3 只在 A 和 S3。未提交。

另一侧 S1 在 term=2 写入 (3,2,C4) 并复制到三台。
S1 可以 commit C4，因为 term 2 是它自己的 term，且有多数。

愈合后若 S1 仍是 leader:
  向 A 发送 prev=2（C2 的 term=1），entries=[(3,2,C4)]
  A 在 index 3 有 term 1，冲突
  A 删除 C3 及之后
  接上 C4
  客户端从没收到过 C3 的成功，所以这不是丢承诺
```

如果旧 leader A 先发来一个带着 C3 的包：S1 的 term 更高，直接拒绝，A 收到更高 term 后下台。不会用 C3 去覆盖已经提交的 C4。

## 6. Invariant

- 重试同一位置是日志上的空操作。
- 不允许洞。拒绝一次，而不是插到后面。
- 没有 term 冲突，就不删除后缀。
- 有冲突，就删除冲突条及其后的全部，再按 leader 的版本接上。
- Leader 的日志只追加。
- nextIndex 按 follower 独立。只在那个 follower 成功时前进。
- 新 leader 的 nextIndex 来自自己的日志，不来自前任的内存。
- nextIndex 不会小于 1（或你的最小合法槽）。他指出了，没写进代码。
- 相同 (index, term) ⇒ 相同前缀。由 prev 检查归纳得来。

## 7. Failure Cases

**Follower 崩溃很久。** nextIndex 一次次失败，每次减一，心跳一直打。慢，但是会接上。他接受这个慢。

**Follower 比 leader 更长。** prev 在更短的位置已经冲突，删除会把多出来的尾巴一起拿掉。如果那些尾巴未提交，正确。如果实现错误地提交过它们，这就是丢数据。所以提交规则不能松，见 [08](08-commit-and-apply.md)。

**成功回复丢失。** Leader 再送同一段。Follower 发现 term 相同，不重复插入。matchIndex 再报一次也无害，只要不要因此把 commitIndex 往回拉。

**失败回复迟到。** Leader 已经用更新的成功把 nextIndex 推到 10，然后一条旧的 false 到达，把 nextIndex 减到 4。可能重复劳动，不应删掉 leader 日志。【从课程推导】课上没专门演示这个竞态。减一算法在乱序回复下会抖，但只要不减过 matchIndex、并且只听当前 term，最终仍会回到正确前缀。这是实现时要加的护栏，不是他写过的代码。

**同时一台前进、一台后退。** D4：nextIndex 可以是 9、6、9、3、8 这种形状。Leader 要同时管理往前和往后 (D4 1_part002–1_part003)。

## 8. Implementation Notes

Project 4 只做日志对象上的 append，不把“leader 减 index”放进去 (D2 2_part026)。测试：

- 重复追加不变。
- 洞被拒绝。
- term 不匹配被拒绝。
- 冲突时删除该条及之后。
- 可以伪造 term，因为还没有选举 (D2 2_part027)。

Project 5 才把 `update_single_follower(follower_id)` 放进 logic。参数是节点编号，不是地址。节点 5 更新节点 2，就发一条定制的 AppendEntries (D3 1_part003)。

Figure 7 是测试生成器：follower 条目更少、更多、term 冲突。他没把图里的条目读出来。过程是：在 leader 末尾放一条新命令，让消息和重试跑完，断言两份日志相等 (D3 2_part024)。

TLA+ 规格不是可运行的 Raft。他把它当第三份说明书，用来看 matchIndex 怎么算、nextIndex 的下界在哪 (D3 2_part025–2_part027)。他写 `nextIndex = matchIndex + 1` 时没有对照 TLA，是事后对上的。

## 9. Common Bugs

- 把新条目的 index 填进 prevLogIndex。
- 冲突时只覆盖那一条，留下后面的 X、Y。分区愈合后日志是混血的。
- 没有冲突也删除后缀。迟到的 C1 杀掉 C2。
- 用命令字符串比较，不比 term。两任 leader 可以写下相同的 `set x 1`，term 不同，仍是冲突。
- nextIndex 减到 -1，Python 读到最后一条，行为神秘地错 (D2 2_part041)。
- 成功时用布尔加上“我这次想送 5 条”，没考虑对方只是确认了已有前缀。
- 处理旧 term 的成功回复，把 matchIndex 写成过期值，甚至把 commitIndex 带偏。
- 广播一个 nextIndex。
- 新 leader 向集群询问所有 nextIndex，把协议写成两阶段，然后在询问期间又有人崩溃。他让你别这么干。

## 10. Course Insight

AppendEntries 不是“把命令追加到末尾”。它是“在这个指纹之后接上这些条目；指纹不对就拒绝；接上去的时候如果打架，你的未提交未来作废”。

`(index, term)` 能代表前缀，是因为写入规则禁止在同一任、同一位置写下两条，也禁止在前缀不同时写下这一条。指纹是规则堆出来的，不是哈希碰巧。

nextIndex 是搜索分叉点的光标。matchIndex 是已经搜索完、可以拿去算提交的光标。混用这两个，提交会把没确认的位置算进去，或者发送会从已经确认的位置再猜一次。

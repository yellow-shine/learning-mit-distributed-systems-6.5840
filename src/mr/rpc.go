package mr

//
// RPC definitions.
//
// remember to capitalize all names.
//

//
// example to show how to declare the arguments
// and reply for an RPC.
//

type ExampleArgs struct {
	X int
}

type ExampleReply struct {
	Y int
}

// Add your RPC definitions here.

type TaskKind int

const (
	TaskWait TaskKind = iota
	TaskMap
	TaskReduce
	TaskExit
)

type AskArgs struct{}

type AskReply struct {
	Kind    TaskKind
	TaskId  int
	File    string
	NMap    int
	NReduce int
}

type ReportArgs struct {
	Kind   TaskKind
	TaskId int
}

type ReportReply struct{}

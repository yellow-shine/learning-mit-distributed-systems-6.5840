package mr

import "log"
import "net"
import "os"
import "net/rpc"
import "net/http"
import "sync"
import "time"

const taskTimeout = 10 * time.Second

type Coordinator struct {
	mu          sync.Mutex
	files       []string
	nReduce     int
	mapDone     []bool
	mapStart    []time.Time
	reduceDone  []bool
	reduceStart []time.Time
	nMapDone    int
	nRedDone    int
}

// Your code here -- RPC handlers for the worker to call.

func (c *Coordinator) GetTask(args *AskArgs, reply *AskReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	reply.NMap = len(c.files)
	reply.NReduce = c.nReduce

	if c.nMapDone < len(c.files) {
		for i, done := range c.mapDone {
			if !done && (c.mapStart[i].IsZero() || time.Since(c.mapStart[i]) > taskTimeout) {
				c.mapStart[i] = time.Now()
				reply.Kind = TaskMap
				reply.TaskId = i
				reply.File = c.files[i]
				return nil
			}
		}
		reply.Kind = TaskWait
		return nil
	}

	if c.nRedDone < c.nReduce {
		for i, done := range c.reduceDone {
			if !done && (c.reduceStart[i].IsZero() || time.Since(c.reduceStart[i]) > taskTimeout) {
				c.reduceStart[i] = time.Now()
				reply.Kind = TaskReduce
				reply.TaskId = i
				return nil
			}
		}
		reply.Kind = TaskWait
		return nil
	}

	reply.Kind = TaskExit
	return nil
}

func (c *Coordinator) ReportTask(args *ReportArgs, reply *ReportReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch args.Kind {
	case TaskMap:
		if !c.mapDone[args.TaskId] {
			c.mapDone[args.TaskId] = true
			c.nMapDone++
		}
	case TaskReduce:
		if !c.reduceDone[args.TaskId] {
			c.reduceDone[args.TaskId] = true
			c.nRedDone++
		}
	}
	return nil
}

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}

// start a thread that listens for RPCs from worker.go
func (c *Coordinator) server(sockname string) {
	rpc.Register(c)
	rpc.HandleHTTP()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatalf("listen error %s: %v", sockname, e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nRedDone == c.nReduce
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(sockname string, files []string, nReduce int) *Coordinator {
	c := Coordinator{}

	c.files = files
	c.nReduce = nReduce
	c.mapDone = make([]bool, len(files))
	c.mapStart = make([]time.Time, len(files))
	c.reduceDone = make([]bool, nReduce)
	c.reduceStart = make([]time.Time, nReduce)

	c.server(sockname)
	return &c
}

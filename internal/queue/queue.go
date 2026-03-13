package queue

import (
	"fmt"
	"sync/atomic"
)

// Job represents a single transcription request.
type Job struct {
	AudioPath string        // path to the temp audio file
	Result    chan JobResult // buffered channel (cap 1) for the response
}

// JobResult carries the transcript or an error back to the caller.
type JobResult struct {
	Text string
	Err  error
}

// Queue serialises transcription jobs through a single worker goroutine.
// Callers receive their position in the queue before the job starts so the
// Telegram handler can send a "queued (position N)" notice.
type Queue struct {
	jobs    chan *Job
	waiting atomic.Int32 // number of jobs ahead of any new submission
}

// New creates a Queue with the given backlog capacity and starts the worker.
// The worker calls doWork for each job in order.
func New(backlog int, doWork func(j *Job)) *Queue {
	q := &Queue{
		jobs: make(chan *Job, backlog),
	}
	go q.run(doWork)
	return q
}

// Submit enqueues a job and returns the current queue depth before this job
// (i.e. the number of jobs ahead of it). A depth of 0 means processing starts
// immediately (no one waiting). The caller should block on j.Result to get the
// transcription outcome.
func (q *Queue) Submit(j *Job) int {
	pos := int(q.waiting.Add(1)) - 1 // position 0 = next to run
	q.jobs <- j
	return pos
}

// run is the single worker goroutine.
func (q *Queue) run(doWork func(j *Job)) {
	for j := range q.jobs {
		q.waiting.Add(-1)
		doWork(j)
	}
}

// PositionMessage returns the human-readable status string sent to Telegram
// while the job is queued ahead of other work.
func PositionMessage(pos int) string {
	if pos == 0 {
		return "Transcribing now... (this might take a moment)"
	}
	return fmt.Sprintf("Queued (position %d) — will start once the current transcription finishes...", pos)
}
